package deezer

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/mgarnier-home/downloader/backend/naming"
)

const (
	defaultAPIURL     = "https://api.deezer.com"
	defaultGatewayURL = "https://www.deezer.com"
	defaultMediaURL   = "https://media.deezer.com"
	// La passerelle attend un navigateur.
	userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/79.0.3945.130 Safari/537.36"
	// Modèle d'URL de pochette construit depuis md5_image : l'API publique ne donne que des tailles
	// fixes (cover_xl = 1000 px), ce modèle permet d'obtenir 800 px (tags) et 1200 px (cover.jpg).
	coverTemplate = "https://e-cdns-images.dzcdn.net/images/cover/%s/{size}x{size}-000000-80-0-0.jpg"
)

var (
	errARLExpired  = errors.New("Deezer : arl expiré")
	errUnavailable = errors.New("Deezer : titre indisponible en MP3 128")
	errBadToken    = errors.New("Deezer : jeton de session refusé")
)

// Client interroge l'API publique, la passerelle privée (cookie arl) et le serveur de médias.
// Les URL vides prennent les adresses officielles.
type Client struct {
	APIURL, GatewayURL, MediaURL, ARL string
	HTTP                              *http.Client

	mu   sync.Mutex
	sess session
}

// session est l'état de connexion à la passerelle, lu par deezer.getUserData.
type session struct{ token, license, sid string }

type Artist struct {
	ID            int
	Name, Picture string
}

type Album struct {
	ID                   int
	Title, Artist, Cover string
	Year, Tracks         int
}

type Track struct {
	ID                   int
	Title, Artist, Album string
	AlbumID, Duration    int
}

type SearchResult struct {
	Artists []Artist
	Albums  []Album
	Tracks  []Track
}

// TrackMeta porte tout ce qu'il faut pour télécharger, ranger et taguer un morceau.
type TrackMeta struct {
	SongID   string
	Info     naming.TrackInfo
	Artists  []string
	Year     int
	Genre    string
	CoverURL string // modèle avec {size}, remplacé par 800 ou 1200
}

// StatusError signale une erreur passagère : HTTP 429 ou 5xx, ou quota de l'API dépassé (Code 429).
type StatusError struct{ Code int }

func (e *StatusError) Error() string { return fmt.Sprintf("Deezer : réponse HTTP %d", e.Code) }

var defaultHTTP = &http.Client{Timeout: 30 * time.Second}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return defaultHTTP
}

// stripURL retire l'URL (qui porte le jeton de session) d'une erreur réseau.
func stripURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// do exécute req et décode la réponse JSON dans out.
func (c *Client) do(req *http.Request, out any) (*http.Response, error) {
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("Deezer : %w", stripURL(err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, &StatusError{Code: resp.StatusCode}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("Deezer : erreur HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return nil, errors.New("Deezer : réponse illisible")
	}
	return resp, nil
}

// get appelle l'API publique.
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	u := cmp.Or(c.APIURL, defaultAPIURL) + path
	if q != nil {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return errors.New("Deezer : requête invalide")
	}
	var raw json.RawMessage
	if _, err := c.do(req, &raw); err != nil {
		return err
	}
	var e struct {
		Error *struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &e) == nil && e.Error != nil {
		if e.Error.Code == 4 { // Quota limit exceeded
			return &StatusError{Code: http.StatusTooManyRequests}
		}
		return fmt.Errorf("Deezer : %s", e.Error.Message)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return errors.New("Deezer : réponse illisible")
	}
	return nil
}

// gw appelle une méthode de la passerelle et renvoie le cookie sid éventuellement posé.
func (c *Client) gw(ctx context.Context, s session, method string, args, out any) (string, error) {
	q := url.Values{"api_version": {"1.0"}, "api_token": {cmp.Or(s.token, "null")}, "input": {"3"}, "method": {method}}
	body, err := json.Marshal(args)
	if err != nil {
		return "", errors.New("Deezer : requête invalide")
	}
	u := cmp.Or(c.GatewayURL, defaultGatewayURL) + "/ajax/gw-light.php?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return "", errors.New("Deezer : requête invalide")
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "arl", Value: c.ARL})
	if s.sid != "" {
		req.AddCookie(&http.Cookie{Name: "sid", Value: s.sid})
	}
	var env struct {
		Error   json.RawMessage `json:"error"`
		Results json.RawMessage `json:"results"`
	}
	resp, err := c.do(req, &env)
	if err != nil {
		return "", err
	}
	if err := gwError(env.Error); err != nil {
		return "", err
	}
	// Un résultat vide ([] ou null) laisse out à zéro.
	if r := string(env.Results); r != "" && r != "[]" && r != "null" && json.Unmarshal(env.Results, out) != nil {
		return "", errors.New("Deezer : réponse illisible")
	}
	for _, ck := range resp.Cookies() {
		if ck.Name == "sid" {
			return ck.Value, nil
		}
	}
	return "", nil
}

// gwError traduit le champ error de la passerelle ([] ou {} quand tout va bien).
func gwError(raw json.RawMessage) error {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil || len(m) == 0 {
		return nil
	}
	if m["GATEWAY_ERROR"] != nil || m["VALID_TOKEN_REQUIRED"] != nil {
		return errBadToken
	}
	for k, v := range m {
		return fmt.Errorf("Deezer : %s : %v", k, v)
	}
	return nil
}

// login lit le jeton et la licence de session ; c.mu doit être verrouillé.
func (c *Client) login(ctx context.Context) error {
	var r struct {
		CheckForm string `json:"checkForm"`
		User      struct {
			UserID  json.Number `json:"USER_ID"`
			Options struct {
				License string `json:"license_token"`
			} `json:"OPTIONS"`
		} `json:"USER"`
	}
	c.sess = session{}
	sid, err := c.gw(ctx, session{}, "deezer.getUserData", struct{}{}, &r)
	if err != nil {
		return err
	}
	if r.User.UserID == "" || r.User.UserID == "0" {
		return errARLExpired
	}
	c.sess = session{token: r.CheckForm, license: r.User.Options.License, sid: sid}
	return nil
}

// session renvoie la session en cache, relue si absente ou si son jeton vaut stale.
func (c *Client) session(ctx context.Context, stale string) (session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sess.token == "" || c.sess.token == stale {
		if err := c.login(ctx); err != nil {
			return session{}, err
		}
	}
	return c.sess, nil
}

// call appelle la passerelle avec la session en cache, renouvelée une fois si son jeton est refusé.
func (c *Client) call(ctx context.Context, method string, args, out any) (session, error) {
	s, err := c.session(ctx, "")
	if err != nil {
		return s, err
	}
	_, err = c.gw(ctx, s, method, args, out)
	if errors.Is(err, errBadToken) {
		if s, err = c.session(ctx, s.token); err != nil {
			return s, err
		}
		_, err = c.gw(ctx, s, method, args, out)
	}
	return s, err
}

// CheckARL vérifie le cookie arl par une nouvelle connexion.
func (c *Client) CheckARL(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.login(ctx)
}

// StreamURL renvoie l'URL du flux MP3 128 chiffré ; elle expire, à demander juste avant l'ouverture.
func (c *Client) StreamURL(ctx context.Context, songID string) (string, error) {
	var list struct {
		Data []struct {
			Token string `json:"TRACK_TOKEN"`
		} `json:"data"`
	}
	s, err := c.call(ctx, "song.getListData", map[string][]string{"SNG_IDS": {songID}}, &list)
	if err != nil {
		return "", err
	}
	if len(list.Data) == 0 || list.Data[0].Token == "" {
		return "", errUnavailable
	}
	return c.mediaURL(ctx, s.license, list.Data[0].Token)
}

func (c *Client) mediaURL(ctx context.Context, license, token string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"license_token": license,
		"media":         []any{map[string]any{"type": "FULL", "formats": []any{map[string]string{"cipher": "BF_CBC_STRIPE", "format": "MP3_128"}}}},
		"track_tokens":  []string{token},
	})
	u := cmp.Or(c.MediaURL, defaultMediaURL) + "/v1/get_url"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return "", errors.New("Deezer : requête invalide")
	}
	req.Header.Set("Content-Type", "application/json")
	var r struct {
		Data []struct {
			Media []struct {
				Sources []struct {
					URL string `json:"url"`
				} `json:"sources"`
			} `json:"media"`
			Errors []struct {
				Code int `json:"code"`
			} `json:"errors"`
		} `json:"data"`
	}
	if _, err := c.do(req, &r); err != nil {
		return "", err
	}
	if len(r.Data) == 0 {
		return "", errUnavailable
	}
	d := r.Data[0]
	if len(d.Errors) > 0 && d.Errors[0].Code == 2002 {
		return "", errors.New("Deezer : titre indisponible dans ce pays")
	}
	if len(d.Media) == 0 || len(d.Media[0].Sources) == 0 || d.Media[0].Sources[0].URL == "" {
		return "", errUnavailable
	}
	return d.Media[0].Sources[0].URL, nil
}

type name struct {
	Name string `json:"name"`
}

// Search cherche artistes, albums et titres.
func (c *Client) Search(ctx context.Context, q string) (SearchResult, error) {
	v := url.Values{"q": {q}}
	var res SearchResult
	var ar struct {
		Data []struct {
			ID      int    `json:"id"`
			Name    string `json:"name"`
			Picture string `json:"picture_medium"`
		} `json:"data"`
	}
	if err := c.get(ctx, "/search/artist", v, &ar); err != nil {
		return res, err
	}
	for _, a := range ar.Data {
		res.Artists = append(res.Artists, Artist(a))
	}
	var al struct {
		Data []struct {
			ID     int    `json:"id"`
			Title  string `json:"title"`
			Cover  string `json:"cover_medium"`
			Tracks int    `json:"nb_tracks"`
			Artist name   `json:"artist"`
		} `json:"data"`
	}
	if err := c.get(ctx, "/search/album", v, &al); err != nil {
		return res, err
	}
	for _, a := range al.Data {
		res.Albums = append(res.Albums, Album{ID: a.ID, Title: a.Title, Artist: a.Artist.Name, Cover: a.Cover, Tracks: a.Tracks})
	}
	var tr struct {
		Data []struct {
			ID       int    `json:"id"`
			Title    string `json:"title"`
			Duration int    `json:"duration"`
			Artist   name   `json:"artist"`
			Album    struct {
				ID    int    `json:"id"`
				Title string `json:"title"`
			} `json:"album"`
		} `json:"data"`
	}
	if err := c.get(ctx, "/search", v, &tr); err != nil {
		return res, err
	}
	for _, t := range tr.Data {
		res.Tracks = append(res.Tracks, Track{ID: t.ID, Title: t.Title, Artist: t.Artist.Name, Album: t.Album.Title, AlbumID: t.Album.ID, Duration: t.Duration})
	}
	return res, nil
}

type apiAlbum struct {
	Title       string `json:"title"`
	MD5Image    string `json:"md5_image"`
	ReleaseDate string `json:"release_date"`
	NbDisk      int    `json:"nb_disk"`
	NbTracks    int    `json:"nb_tracks"`
	Artist      name   `json:"artist"`
	Genres      struct {
		Data []name `json:"data"`
	} `json:"genres"`
}

type apiTrack struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	TrackPosition int    `json:"track_position"`
	DiskNumber    int    `json:"disk_number"`
	ReleaseDate   string `json:"release_date"`
	Artist        name   `json:"artist"`
	Contributors  []name `json:"contributors"`
	Album         struct {
		ID int `json:"id"`
	} `json:"album"`
}

func (c *Client) album(ctx context.Context, id int) (apiAlbum, error) {
	var a apiAlbum
	return a, c.get(ctx, "/album/"+strconv.Itoa(id), nil, &a)
}

func (c *Client) track(ctx context.Context, id int) (apiTrack, error) {
	var t apiTrack
	return t, c.get(ctx, "/track/"+strconv.Itoa(id), nil, &t)
}

// AlbumTracks renvoie les métadonnées de chaque titre de l'album, lus dans la liste paginée
// /album/<id>/tracks (position et disque compris).
func (c *Client) AlbumTracks(ctx context.Context, albumID int) ([]TrackMeta, error) {
	a, err := c.album(ctx, albumID)
	if err != nil {
		return nil, err
	}
	var metas []TrackMeta
	for path := "/album/" + strconv.Itoa(albumID) + "/tracks"; path != ""; {
		var page struct {
			Data []apiTrack `json:"data"`
			Next string     `json:"next"`
		}
		if err := c.get(ctx, path, nil, &page); err != nil {
			return nil, err
		}
		for _, t := range page.Data {
			// shortcut: artistes secondaires absents de la liste paginée, appeler /track/<id> si on veut les featurings
			metas = append(metas, buildMeta(a, t))
		}
		if len(page.Data) == 0 { // évite une boucle sur une page vide
			break
		}
		path = nextPath(page.Next)
	}
	if len(metas) < a.NbTracks {
		return nil, errors.New("Deezer : liste des pistes incomplète")
	}
	return metas, nil
}

// nextPath garde le chemin et la requête de l'URL suivante, pour rester sur c.APIURL.
func nextPath(next string) string {
	u, err := url.Parse(next)
	if next == "" || err != nil {
		return ""
	}
	return u.RequestURI()
}

// TrackMeta renvoie les métadonnées d'un titre ; l'album donne l'artiste de l'album, le genre et le nombre de disques.
func (c *Client) TrackMeta(ctx context.Context, trackID int) (TrackMeta, error) {
	t, err := c.track(ctx, trackID)
	if err != nil {
		return TrackMeta{}, err
	}
	a, err := c.album(ctx, t.Album.ID)
	if err != nil {
		return TrackMeta{}, err
	}
	return buildMeta(a, t), nil
}

func buildMeta(a apiAlbum, t apiTrack) TrackMeta {
	m := TrackMeta{SongID: strconv.Itoa(t.ID), Info: naming.TrackInfo{
		AlbumArtist: a.Artist.Name, Artist: t.Artist.Name, Album: a.Title, Title: t.Title,
		Track: t.TrackPosition, Disc: max(t.DiskNumber, 1), DiskCount: max(a.NbDisk, 1),
	}}
	for _, ct := range t.Contributors {
		if !slices.Contains(m.Artists, ct.Name) {
			m.Artists = append(m.Artists, ct.Name)
		}
	}
	if len(m.Artists) == 0 {
		m.Artists = []string{t.Artist.Name}
	}
	if d := cmp.Or(a.ReleaseDate, t.ReleaseDate); len(d) >= 4 {
		m.Year, _ = strconv.Atoi(d[:4])
	}
	if len(a.Genres.Data) > 0 {
		m.Genre = a.Genres.Data[0].Name
	}
	if a.MD5Image != "" {
		m.CoverURL = fmt.Sprintf(coverTemplate, a.MD5Image)
	}
	return m
}
