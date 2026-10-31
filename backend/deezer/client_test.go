package deezer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const testARL = "arl-secret-123"

// fakeDeezer simule l'API publique, la passerelle et le serveur de médias ; over remplace une route.
func fakeDeezer(t *testing.T, over map[string]http.HandlerFunc) *Client {
	t.Helper()
	routes := map[string]http.HandlerFunc{
		"/album/302127": text(`{"id":302127,"title":"Discovery","md5_image":"abc","release_date":"2001-03-07","nb_disk":2,
			"artist":{"name":"Daft Punk"},"genres":{"data":[{"name":"Électro"}]},"tracks":{"data":[{"id":1},{"id":2}]}}`),
		"/track/1": text(`{"id":1,"title":"One More Time","track_position":1,"disk_number":1,"artist":{"name":"Daft Punk"},
			"contributors":[{"name":"Daft Punk"},{"name":"Romanthony"}],"album":{"id":302127}}`),
		"/track/2": text(`{"id":2,"title":"Aerodynamic","track_position":1,"disk_number":2,"artist":{"name":"Daft Punk"},"album":{"id":302127}}`),
		"/ajax/gw-light.php": func(w http.ResponseWriter, r *http.Request) {
			if c, err := r.Cookie("arl"); err != nil || c.Value != testARL {
				t.Error("cookie arl absent")
			}
			switch r.URL.Query().Get("method") {
			case "deezer.getUserData":
				http.SetCookie(w, &http.Cookie{Name: "sid", Value: "s1"})
				_, _ = w.Write([]byte(`{"error":[],"results":{"checkForm":"tok","USER":{"USER_ID":42,"OPTIONS":{"license_token":"lic"}}}}`))
			case "song.getListData":
				_, _ = w.Write([]byte(`{"error":[],"results":{"data":[{"SNG_ID":"1","TRACK_TOKEN":"tt1"}]}}`))
			}
		},
		"/v1/get_url": text(`{"data":[{"media":[{"sources":[{"url":"https://cdn.example/1.mp3"}]}]}]}`),
	}
	for k, v := range over {
		routes[k] = v
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := routes[r.URL.Path]; h != nil {
			h(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"error":{"type":"DataException","message":"no data","code":800}}`))
	}))
	t.Cleanup(srv.Close)
	return &Client{APIURL: srv.URL, GatewayURL: srv.URL, MediaURL: srv.URL, ARL: testARL}
}

func text(s string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(s)) }
}

func TestARLExpired(t *testing.T) {
	c := fakeDeezer(t, map[string]http.HandlerFunc{
		"/ajax/gw-light.php": text(`{"error":[],"results":{"checkForm":"tok","USER":{"USER_ID":0}}}`),
	})
	err := c.CheckARL(context.Background())
	if err == nil || err.Error() != "Deezer : arl expiré" {
		t.Fatalf("err = %v", err)
	}
	if _, err := c.StreamURL(context.Background(), "1"); err == nil || strings.Contains(err.Error(), testARL) {
		t.Fatalf("StreamURL err = %v", err)
	}
}

func TestStreamURL(t *testing.T) {
	var body map[string]any
	c := fakeDeezer(t, map[string]http.HandlerFunc{
		"/v1/get_url": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&body)
			_, _ = w.Write([]byte(`{"data":[{"media":[{"sources":[{"url":"https://cdn.example/1.mp3"}]}]}]}`))
		},
	})
	u, err := c.StreamURL(context.Background(), "1")
	if err != nil || u != "https://cdn.example/1.mp3" {
		t.Fatalf("u = %q, err = %v", u, err)
	}
	got, _ := json.Marshal(body)
	want := `{"license_token":"lic","media":[{"formats":[{"cipher":"BF_CBC_STRIPE","format":"MP3_128"}],"type":"FULL"}],"track_tokens":["tt1"]}`
	if string(got) != want {
		t.Errorf("corps get_url = %s", got)
	}
}

func TestStreamURLUnavailable(t *testing.T) {
	for name, resp := range map[string]string{
		"sans média":  `{"data":[{"media":[]}]}`,
		"sans source": `{"data":[{"media":[{"sources":[]}]}]}`,
	} {
		c := fakeDeezer(t, map[string]http.HandlerFunc{"/v1/get_url": text(resp)})
		_, err := c.StreamURL(context.Background(), "1")
		if err == nil || err.Error() != "Deezer : titre indisponible en MP3 128" {
			t.Errorf("%s : err = %v", name, err)
		}
	}
}

func TestTokenRenewedOnce(t *testing.T) {
	var logins, calls atomic.Int32
	c := fakeDeezer(t, map[string]http.HandlerFunc{
		"/ajax/gw-light.php": func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("method") == "deezer.getUserData" {
				if q.Get("api_token") != "null" {
					t.Errorf("api_token de getUserData = %q", q.Get("api_token"))
				}
				http.SetCookie(w, &http.Cookie{Name: "sid", Value: "s1"})
				_, _ = fmt.Fprintf(w, `{"error":[],"results":{"checkForm":"tok%d","USER":{"USER_ID":42,"OPTIONS":{"license_token":"lic"}}}}`, logins.Add(1))
				return
			}
			if s, err := r.Cookie("sid"); err != nil || s.Value != "s1" {
				t.Error("cookie sid non renvoyé")
			}
			if calls.Add(1) == 1 {
				_, _ = w.Write([]byte(`{"error":{"VALID_TOKEN_REQUIRED":"Invalid CSRF token"},"results":{}}`))
				return
			}
			if q.Get("api_token") != "tok2" {
				t.Errorf("api_token après renouvellement = %q", q.Get("api_token"))
			}
			_, _ = w.Write([]byte(`{"error":[],"results":{"data":[{"TRACK_TOKEN":"tt1"}]}}`))
		},
	})
	if _, err := c.StreamURL(context.Background(), "1"); err != nil {
		t.Fatal(err)
	}
	if logins.Load() != 2 || calls.Load() != 2 {
		t.Errorf("connexions = %d, appels = %d", logins.Load(), calls.Load())
	}
}

func TestAlbumTracksMultiDisc(t *testing.T) {
	c := fakeDeezer(t, nil)
	tracks, err := c.AlbumTracks(context.Background(), 302127)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 2 {
		t.Fatalf("%d pistes", len(tracks))
	}
	a, b := tracks[0], tracks[1]
	if a.Info.DiskCount != 2 || a.Info.Disc != 1 || a.Info.Track != 1 || b.Info.Disc != 2 || b.Info.Track != 1 {
		t.Errorf("disques/pistes : %+v / %+v", a.Info, b.Info)
	}
	if a.SongID != "1" || a.Info.Title != "One More Time" || a.Info.Album != "Discovery" || a.Info.AlbumArtist != "Daft Punk" ||
		a.Year != 2001 || a.Genre != "Électro" || strings.Join(a.Artists, ",") != "Daft Punk,Romanthony" ||
		a.CoverURL != "https://e-cdns-images.dzcdn.net/images/cover/abc/{size}x{size}-000000-80-0-0.jpg" {
		t.Errorf("méta = %+v", a)
	}
	if strings.Join(b.Artists, ",") != "Daft Punk" {
		t.Errorf("artistes sans contributeurs = %v", b.Artists)
	}
}

func TestTrackMetaAndAPIError(t *testing.T) {
	c := fakeDeezer(t, nil)
	m, err := c.TrackMeta(context.Background(), 2)
	if err != nil || m.Info.Disc != 2 || m.Info.DiskCount != 2 || m.Info.Album != "Discovery" {
		t.Fatalf("m = %+v, err = %v", m, err)
	}
	if _, err := c.TrackMeta(context.Background(), 999); err == nil || err.Error() != "Deezer : no data" {
		t.Fatalf("err = %v", err)
	}
}

func TestSearch(t *testing.T) {
	c := fakeDeezer(t, map[string]http.HandlerFunc{
		"/search": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("q") != "daft punk" {
				t.Errorf("q = %q", r.URL.Query().Get("q"))
			}
			_, _ = w.Write([]byte(`{"data":[{"id":1,"title":"One More Time","duration":320,"artist":{"name":"Daft Punk"},"album":{"id":302127,"title":"Discovery"}}]}`))
		},
		"/search/artist": text(`{"data":[{"id":27,"name":"Daft Punk","picture_medium":"p.jpg"}]}`),
		"/search/album":  text(`{"data":[{"id":302127,"title":"Discovery","cover_medium":"c.jpg","nb_tracks":14,"artist":{"name":"Daft Punk"}}]}`),
	})
	res, err := c.Search(context.Background(), "daft punk")
	if err != nil {
		t.Fatal(err)
	}
	want := SearchResult{
		Artists: []Artist{{ID: 27, Name: "Daft Punk", Picture: "p.jpg"}},
		Albums:  []Album{{ID: 302127, Title: "Discovery", Artist: "Daft Punk", Cover: "c.jpg", Tracks: 14}},
		Tracks:  []Track{{ID: 1, Title: "One More Time", Artist: "Daft Punk", Album: "Discovery", AlbumID: 302127, Duration: 320}},
	}
	g, _ := json.Marshal(res)
	w, _ := json.Marshal(want)
	if string(g) != string(w) {
		t.Errorf("résultat = %s", g)
	}
}
