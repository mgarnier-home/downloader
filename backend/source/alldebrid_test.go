package source

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeSource struct{ name string }

func (f fakeSource) Name() string                                         { return f.name }
func (f fakeSource) Match(in string) bool                                 { return strings.HasPrefix(in, "deezer:") }
func (f fakeSource) Resolve(context.Context, string) ([]Item, error)      { return nil, nil }
func (f fakeSource) Reopen(context.Context, string, string) (Item, error) { return Item{}, nil }

func adServer(t *testing.T, infos, unlock string, direct http.HandlerFunc) *AllDebrid {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/direct" {
			if r.Header.Get("Authorization") != "" {
				t.Error("la clé ne doit pas être envoyée à l'hébergeur")
			}
			direct(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("Authorization absent sur %s", r.URL.Path)
		}
		if r.URL.Query().Get("agent") != "downloader" {
			t.Errorf("agent absent sur %s", r.URL)
		}
		switch r.URL.Path {
		case "/v4/hosts":
			_, _ = w.Write([]byte(`{"status":"success","data":{"hosts":{"1fichier":{"name":"1fichier","domains":["1fichier.com","alterupload.com"]}},"streams":{},"redirectors":{}}}`))
		case "/v4/link/infos":
			_, _ = w.Write([]byte(infos))
		case "/v4/link/unlock":
			_, _ = w.Write([]byte(strings.ReplaceAll(unlock, "DIRECT", srv.URL+"/direct")))
		case "/v4/user":
			_, _ = w.Write([]byte(`{"status":"success","data":{"user":{"isPremium":true,"premiumUntil":1893456000}}}`))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return &AllDebrid{BaseURL: srv.URL, Key: "secret"}
}

func TestAllDebridMatch(t *testing.T) {
	a := adServer(t, "", "", nil)
	if err := a.LoadHosts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !a.Match("https://1fichier.com/?abc") || !a.Match("https://www.alterupload.com/x") || a.Match("https://example.org/a.mkv") || a.Match("pas un lien") {
		t.Error("Match incorrect")
	}
}

func TestAllDebridLinkDown(t *testing.T) {
	a := adServer(t, `{"status":"success","data":{"infos":[{"link":"https://1fichier.com/?abc","error":{"code":"LINK_DOWN","message":"Link is dead"}}]}}`, "", nil)
	items, err := a.Resolve(context.Background(), "https://1fichier.com/?abc")
	if err != nil || len(items) != 1 {
		t.Fatalf("err=%v", err)
	}
	if items[0].Online || items[0].Error != "1fichier : fichier supprimé par l'hébergeur" {
		t.Errorf("got %+v", items[0])
	}
}

func TestAllDebridOtherErrorAndEnvelope(t *testing.T) {
	a := adServer(t, `{"status":"success","data":{"infos":[{"link":"https://1fichier.com/?abc","error":{"code":"X","message":"Boom"}}]}}`, "", nil)
	items, _ := a.Resolve(context.Background(), "https://1fichier.com/?abc")
	if items[0].Error != "1fichier : Boom" {
		t.Errorf("got %q", items[0].Error)
	}
	a = adServer(t, `{"status":"error","error":{"code":"AUTH_BAD_APIKEY","message":"clé refusée"}}`, "", nil)
	if _, err := a.Resolve(context.Background(), "https://1fichier.com/?abc"); err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("err = %v", err)
	}
}

func TestAllDebridResolveAndOpenUnlocksAtOpen(t *testing.T) {
	a := adServer(t,
		`{"status":"success","data":{"infos":[{"link":"https://1fichier.com/?abc","filename":"Film.mkv","size":42}]}}`,
		`{"status":"success","data":{"link":"DIRECT","filename":"Film.mkv"}}`,
		func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("data")) })
	items, err := a.Resolve(context.Background(), "https://1fichier.com/?abc")
	if err != nil || items[0].Name != "Film.mkv" || items[0].Size != 42 || !items[0].Online {
		t.Fatalf("err=%v items=%+v", err, items)
	}
	body, ok, err := items[0].Open(context.Background(), 0)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	b, _ := io.ReadAll(body)
	_ = body.Close()
	if string(b) != "data" {
		t.Errorf("body = %q", b)
	}
	it, err := a.Reopen(context.Background(), "https://1fichier.com/?abc", items[0].Meta)
	if err != nil || it.Open == nil {
		t.Errorf("Reopen : %v", err)
	}
}

func TestAllDebridAccount(t *testing.T) {
	a := adServer(t, "", "", nil)
	premium, until, err := a.Account(context.Background())
	if err != nil || !premium || !until.Equal(time.Unix(1893456000, 0)) {
		t.Errorf("premium=%v until=%v err=%v", premium, until, err)
	}
}

func TestPickOrder(t *testing.T) {
	a := adServer(t, "", "", nil)
	_ = a.LoadHosts(context.Background())
	h := &HTTP{}
	srcs := []Source{fakeSource{"deezer"}, a, h}
	if s, ok := Pick(srcs, "https://1fichier.com/?abc"); !ok || s.Name() != "alldebrid" {
		t.Errorf("1fichier -> %v", s)
	}
	if s, ok := Pick(srcs, "https://example.org/a.mkv"); !ok || s.Name() != "http" {
		t.Errorf("quelconque -> %v", s)
	}
	if _, ok := Pick(srcs, "rien"); ok {
		t.Error("aucune source attendue")
	}
}
