package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/mgarnier-home/downloader/backend/deezer"
)

// deezerServer simule Deezer pour un album de 3 titres dont le 2e n'a pas de source MP3 128.
func deezerServer(t *testing.T) *Deezer {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var id string
		switch {
		case r.URL.Path == "/album/302127":
			_, _ = io.WriteString(w, `{"id":302127,"title":"Discovery","nb_disk":1,"artist":{"name":"Daft Punk"},"tracks":{"data":[{"id":1},{"id":2},{"id":3}]}}`)
		case strings.HasPrefix(r.URL.Path, "/track/"):
			id = strings.TrimPrefix(r.URL.Path, "/track/")
			_, _ = fmt.Fprintf(w, `{"id":%s,"title":"T%s","track_position":%s,"disk_number":1,"artist":{"name":"Daft Punk"},"album":{"id":302127}}`, id, id, id)
		case r.URL.Query().Get("method") == "deezer.getUserData":
			_, _ = io.WriteString(w, `{"error":[],"results":{"checkForm":"tok","USER":{"USER_ID":42,"OPTIONS":{"license_token":"lic"}}}}`)
		case r.URL.Query().Get("method") == "song.getListData":
			var body struct {
				SNGIDS []string `json:"SNG_IDS"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			_, _ = fmt.Fprintf(w, `{"error":[],"results":{"data":[{"TRACK_TOKEN":"tt%s"}]}}`, body.SNGIDS[0])
		case r.URL.Path == "/v1/get_url":
			var body struct {
				Tokens []string `json:"track_tokens"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Tokens[0] == "tt2" {
				_, _ = io.WriteString(w, `{"data":[{"media":[]}]}`)
				return
			}
			_, _ = fmt.Fprintf(w, `{"data":[{"media":[{"sources":[{"url":"%s/cdn/%s"}]}]}]}`, srv.URL, strings.TrimPrefix(body.Tokens[0], "tt"))
		case strings.HasPrefix(r.URL.Path, "/cdn/"):
			if r.URL.Path == "/cdn/404" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = io.WriteString(w, "audio-"+strings.TrimPrefix(r.URL.Path, "/cdn/")) // < 2048 octets : en clair
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return &Deezer{Client: &deezer.Client{APIURL: srv.URL, GatewayURL: srv.URL, MediaURL: srv.URL, ARL: "arl"}}
}

func readItem(ctx context.Context, it Item, offset int64) (string, bool, error) {
	rc, ok, err := it.Open(ctx, offset)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(rc)
	return string(b), ok, err
}

func TestDeezerSourceAlbumPartialFailure(t *testing.T) {
	d := deezerServer(t)
	items, err := d.Resolve(context.Background(), "https://www.deezer.com/fr/album/302127")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("%d items", len(items))
	}
	if items[0].Name != "Daft Punk - T1.mp3" || items[0].Track == nil || items[0].Track.Track != 1 || !items[0].Online {
		t.Errorf("item 1 = %+v", items[0])
	}
	got := make([]string, 3)
	errs := make([]error, 3)
	var wg sync.WaitGroup
	for i, it := range items {
		wg.Go(func() { got[i], _, errs[i] = readItem(context.Background(), it, 0) })
	}
	wg.Wait()
	if errs[0] != nil || got[0] != "audio-1" || errs[2] != nil || got[2] != "audio-3" {
		t.Errorf("titres 1 et 3 : %q %v / %q %v", got[0], errs[0], got[2], errs[2])
	}
	if errs[1] == nil || errs[1].Error() != "Deezer : titre indisponible en MP3 128" {
		t.Errorf("titre 2 : err = %v", errs[1])
	}
}

func TestDeezerSourceReopenAndOffset(t *testing.T) {
	d := deezerServer(t)
	items, err := d.Resolve(context.Background(), "deezer:track:3")
	if err != nil || len(items) != 1 {
		t.Fatalf("items = %v, err = %v", items, err)
	}
	it, err := d.Reopen(context.Background(), "deezer:track:3", items[0].Meta)
	if err != nil {
		t.Fatal(err)
	}
	if it.Track == nil || it.Track.Title != "T3" {
		t.Errorf("Track = %+v", it.Track)
	}
	body, ok, err := readItem(context.Background(), it, 500)
	if err != nil || ok || body != "audio-3" {
		t.Errorf("offset 500 : %q %v %v", body, ok, err)
	}
	if _, ok, _ := readItem(context.Background(), it, 0); !ok {
		t.Error("offset 0 doit être respecté")
	}
	if _, err := d.Reopen(context.Background(), "deezer:track:3", "{"); err == nil {
		t.Error("méta illisible acceptée")
	}
	m := strings.Replace(items[0].Meta, `"SongID":"3"`, `"SongID":"404"`, 1)
	it404, _ := d.Reopen(context.Background(), "", m)
	var se *StatusError
	if _, _, err := it404.Open(context.Background(), 0); !errors.As(err, &se) || se.Code != 404 {
		t.Errorf("CDN 404 : err = %v", err)
	}
}

func TestDeezerMatch(t *testing.T) {
	d := &Deezer{}
	for in, want := range map[string]bool{
		"https://www.deezer.com/fr/album/302127":  true,
		"https://deezer.com/track/3135556":        true,
		"https://www.deezer.com/album/302127?x=1": true,
		"deezer:album:302127":                     true,
		"deezer:track:3135556":                    true,
		"https://1fichier.com/?x":                 false,
		"https://www.deezer.com/fr/artist/27":     false,
		"https://evil.com/deezer.com/album/1":     false,
	} {
		if got := d.Match(in); got != want {
			t.Errorf("Match(%q) = %v", in, got)
		}
	}
}
