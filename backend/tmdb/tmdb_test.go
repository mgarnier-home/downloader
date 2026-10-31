package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serve(t *testing.T, body string, check func(r *http.Request)) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if check != nil {
			check(r)
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, Key: "k3y"}
}

func TestTMDBSearchMovieFrench(t *testing.T) {
	c := serve(t, `{"results":[{"id":374720,"title":"Dunkerque","release_date":"2017-07-19","genre_ids":[28,18]}]}`, func(r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/search/movie" || q.Get("language") != "fr-FR" || q.Get("year") != "2017" || q.Get("api_key") != "k3y" || q.Get("query") != "Dunkirk" {
			t.Errorf("requête inattendue : %s", r.URL)
		}
	})
	got, err := c.Search(context.Background(), "Dunkirk", 2017, false)
	if err != nil || len(got) != 1 {
		t.Fatalf("err=%v got=%v", err, got)
	}
	if want := (Result{ID: 374720, Title: "Dunkerque", Year: 2017}); got[0] != want {
		t.Errorf("got %+v, want %+v", got[0], want)
	}
}

func TestTMDBAnimationGenre(t *testing.T) {
	c := serve(t, `{"results":[{"id":1,"name":"Naruto","first_air_date":"2002-10-03","genre_ids":[16,10751],"poster_path":"/p.jpg"}]}`, func(r *http.Request) {
		if r.URL.Path != "/search/tv" || r.URL.Query().Get("first_air_date_year") != "2002" {
			t.Errorf("requête inattendue : %s", r.URL)
		}
	})
	got, err := c.Search(context.Background(), "Naruto", 2002, true)
	if err != nil || len(got) != 1 {
		t.Fatalf("err=%v got=%v", err, got)
	}
	if !got[0].Animation || !got[0].TV || got[0].Year != 2002 || got[0].Title != "Naruto" || got[0].Poster != "https://image.tmdb.org/t/p/w154/p.jpg" {
		t.Errorf("got %+v", got[0])
	}
}

func TestTMDBErrorHidesKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }))
	c := &Client{BaseURL: srv.URL, Key: "k3y"}
	_, err := c.Search(context.Background(), "x", 0, false)
	if err == nil || !strings.HasPrefix(err.Error(), "TMDB : ") {
		t.Fatalf("err=%v", err)
	}
	srv.Close()
	_, err = c.Search(context.Background(), "x", 0, false)
	if err == nil || strings.Contains(err.Error(), "k3y") {
		t.Errorf("la clé fuit : %v", err)
	}
}
