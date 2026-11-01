package source

import (
	"context"
	"errors"
	"fmt"
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

func adServer(t *testing.T, infos, unlock string, direct http.HandlerFunc, over ...map[string]http.HandlerFunc) *AllDebrid {
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
		if len(over) > 0 && over[0][r.URL.Path] != nil {
			over[0][r.URL.Path](w, r)
			return
		}
		requirePost(t, r)
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
	return &AllDebrid{BaseURL: srv.URL, Key: "secret", sleep: func(context.Context, time.Duration) error { return nil }}
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

func delayedServer(t *testing.T, statuses ...int) *AllDebrid {
	t.Helper()
	polls := 0
	return adServer(t, "", `{"status":"success","data":{"delayed":77}}`,
		func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("late")) },
		map[string]http.HandlerFunc{"/v4/link/delayed": func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			if r.Method != http.MethodPost || r.PostForm.Get("id") != "77" {
				t.Errorf("delayed : méthode %s, id %q", r.Method, r.PostForm.Get("id"))
			}
			st := statuses[min(polls, len(statuses)-1)]
			polls++
			link := ""
			if st == 2 {
				link = `,"link":"` + "http://" + r.Host + `/direct"`
			}
			_, _ = fmt.Fprintf(w, `{"status":"success","data":{"status":%d%s}}`, st, link)
		}})
}

func TestAllDebridDelayedUnlock(t *testing.T) {
	a := delayedServer(t, 1, 2)
	it, _ := a.Reopen(context.Background(), "https://1fichier.com/?abc", "")
	body, ok, err := it.Open(context.Background(), 0)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	b, _ := io.ReadAll(body)
	_ = body.Close()
	if string(b) != "late" {
		t.Errorf("body = %q", b)
	}
}

func TestAllDebridDelayedFailed(t *testing.T) {
	a := delayedServer(t, 1, 3)
	it, _ := a.Reopen(context.Background(), "https://1fichier.com/?abc", "")
	if _, _, err := it.Open(context.Background(), 0); err == nil || !strings.Contains(err.Error(), "AllDebrid : génération du lien échouée") {
		t.Errorf("err = %v", err)
	}
}

func TestAllDebridDelayedUnknownStatus(t *testing.T) {
	for _, st := range []int{0, 4} {
		a := delayedServer(t, 1, st)
		it, _ := a.Reopen(context.Background(), "https://1fichier.com/?abc", "")
		if _, _, err := it.Open(context.Background(), 0); err == nil || !strings.Contains(err.Error(), "AllDebrid : génération du lien échouée") {
			t.Errorf("statut %d : err = %v", st, err)
		}
	}
}

func TestAllDebridDelayedTimeout(t *testing.T) {
	a := delayedServer(t, 1)
	var waited time.Duration
	a.sleep = func(_ context.Context, d time.Duration) error { waited += d; return nil }
	it, _ := a.Reopen(context.Background(), "https://1fichier.com/?abc", "")
	if _, _, err := it.Open(context.Background(), 0); err == nil || !strings.Contains(err.Error(), "AllDebrid : génération du lien trop longue") {
		t.Errorf("err = %v", err)
	}
	if waited != 10*time.Minute {
		t.Errorf("attente totale = %s", waited)
	}
}

func TestAllDebridUnlockWithoutLink(t *testing.T) {
	a := adServer(t, "", `{"status":"success","data":{}}`, nil)
	it, _ := a.Reopen(context.Background(), "https://1fichier.com/?abc", "")
	if _, _, err := it.Open(context.Background(), 0); err == nil || !strings.Contains(err.Error(), "aucun lien") {
		t.Errorf("err = %v", err)
	}
}

func TestAllDebridPremiumUntilForms(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want time.Time
	}{
		{`"1545757200"`, time.Unix(1545757200, 0)},
		{`1545757200`, time.Unix(1545757200, 0)},
		{`0`, time.Time{}},
		{`"0"`, time.Time{}},
	} {
		a := adServer(t, "", "", nil, map[string]http.HandlerFunc{"/v4/user": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"status":"success","data":{"user":{"isPremium":true,"premiumUntil":` + tc.raw + `}}}`))
		}})
		_, until, err := a.Account(context.Background())
		if err != nil || !until.Equal(tc.want) {
			t.Errorf("%s : until=%v err=%v", tc.raw, until, err)
		}
	}
}

func TestAllDebridStatusErrors(t *testing.T) {
	for _, code := range []int{429, 503} {
		a := adServer(t, "", "", nil, map[string]http.HandlerFunc{"/v4/user": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"status":"error","error":{"code":"X","message":"m"}}`))
		}})
		_, _, err := a.Account(context.Background())
		var se *StatusError
		if !errors.As(err, &se) || se.Code != code {
			t.Errorf("%d : err = %v", code, err)
		}
	}
}

// requirePost signale tout appel non POST sur les routes qui exigent POST.
func requirePost(t *testing.T, r *http.Request) {
	t.Helper()
	switch r.URL.Path {
	case "/v4/link/infos", "/v4/link/unlock", "/v4/link/delayed":
		if r.Method != http.MethodPost {
			t.Errorf("%s doit être en POST, reçu %s", r.URL.Path, r.Method)
		}
	}
}

const infosOK = `{"status":"success","data":{"infos":[{"link":"https://1fichier.com/?abc","filename":"Film.mkv","size":42}]}}`

func TestAllDebridResolveUnlockRefused(t *testing.T) {
	a := adServer(t, infosOK, `{"status":"error","error":{"code":"LINK_HOST_UNAVAILABLE","message":"Hébergeur en maintenance"}}`, nil)
	items, err := a.Resolve(context.Background(), "https://1fichier.com/?abc")
	if err != nil || len(items) != 1 || items[0].Online || items[0].Error != "1fichier : Hébergeur en maintenance" {
		t.Fatalf("err=%v items=%+v", err, items)
	}
}

func TestAllDebridResolveDelayedWithoutWaiting(t *testing.T) {
	a := adServer(t, infosOK, `{"status":"success","data":{"delayed":77}}`, nil,
		map[string]http.HandlerFunc{"/v4/link/delayed": func(http.ResponseWriter, *http.Request) {
			t.Error("l'analyse ne doit pas attendre la génération")
		}})
	items, err := a.Resolve(context.Background(), "https://1fichier.com/?abc")
	if err != nil || !items[0].Online || items[0].Name != "Film.mkv" {
		t.Fatalf("err=%v items=%+v", err, items)
	}
}

func TestAllDebridResolveUnlockTransient(t *testing.T) {
	a := adServer(t, infosOK, "", nil, map[string]http.HandlerFunc{"/v4/link/unlock": func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}})
	var se *StatusError
	if _, err := a.Resolve(context.Background(), "https://1fichier.com/?abc"); !errors.As(err, &se) {
		t.Errorf("err = %v", err)
	}
}
