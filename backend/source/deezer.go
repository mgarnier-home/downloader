package source

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"

	"github.com/mgarnier-home/downloader/backend/deezer"
)

// Deezer télécharge les albums et titres Deezer, déchiffrés à la volée.
type Deezer struct {
	Client *deezer.Client
	HTTP   *http.Client // téléchargement des flux
}

// deezerInput reconnaît un lien d'album ou de titre (préfixe de langue facultatif) ou un
// identifiant interne venu de la recherche.
var deezerInput = regexp.MustCompile(
	`^(?:https?://(?:www\.)?deezer\.com/(?:[a-z]{2}(?:-[a-z]{2})?/)?(album|track)/(\d+)(?:[/?#].*)?|deezer:(album|track):(\d+))$`)

func (d *Deezer) Name() string { return "deezer" }

func (d *Deezer) Match(input string) bool { return deezerInput.MatchString(input) }

func (d *Deezer) client() *http.Client {
	if d.HTTP != nil {
		return d.HTTP
	}
	return http.DefaultClient
}

func (d *Deezer) Resolve(ctx context.Context, input string) ([]Item, error) {
	m := deezerInput.FindStringSubmatch(input)
	if m == nil {
		return nil, errors.New("Deezer : lien non reconnu")
	}
	kind, id := m[1]+m[3], m[2]+m[4]
	n, err := strconv.Atoi(id)
	if err != nil {
		return nil, errors.New("Deezer : identifiant invalide")
	}
	var metas []deezer.TrackMeta
	if kind == "album" {
		metas, err = d.Client.AlbumTracks(ctx, n)
	} else {
		var t deezer.TrackMeta
		t, err = d.Client.TrackMeta(ctx, n)
		metas = []deezer.TrackMeta{t}
	}
	if err != nil {
		return nil, err
	}
	items := make([]Item, 0, len(metas))
	for _, t := range metas {
		items = append(items, d.item(t))
	}
	return items, nil
}

// Reopen recrée l'item depuis sa TrackMeta stockée, sans relire l'album.
func (d *Deezer) Reopen(_ context.Context, _, meta string) (Item, error) {
	var t deezer.TrackMeta
	if err := json.Unmarshal([]byte(meta), &t); err != nil || t.SongID == "" {
		return Item{}, errors.New("Deezer : métadonnées illisibles")
	}
	return d.item(t), nil
}

func (d *Deezer) item(t deezer.TrackMeta) Item {
	meta, _ := json.Marshal(t)
	return Item{
		Name: t.Info.Artist + " - " + t.Info.Title + ".mp3", Online: true, Track: &t.Info, Meta: string(meta),
		// Le flux chiffré ne se reprend pas en cours : un morceau repart toujours du début.
		Open: func(ctx context.Context, offset int64) (io.ReadCloser, bool, error) {
			u, err := d.Client.StreamURL(ctx, t.SongID) // les URL expirent : relue à chaque ouverture
			if err != nil {
				return nil, false, err
			}
			body, _, err := openURL(ctx, d.client(), u, 0)
			if err != nil {
				return nil, false, err
			}
			return struct {
				io.Reader
				io.Closer
			}{deezer.NewDecrypter(body, deezer.TrackKey(t.SongID)), body}, offset == 0, nil
		},
	}
}
