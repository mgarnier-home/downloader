package source

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPResolveContentDisposition(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="A.mkv"`)
		w.Header().Set("Content-Length", "1234")
	}))
	defer srv.Close()
	items, err := (&HTTP{}).Resolve(context.Background(), srv.URL+"/x/y.bin")
	if err != nil || len(items) != 1 {
		t.Fatalf("err=%v items=%v", err, items)
	}
	if it := items[0]; it.Name != "A.mkv" || it.Size != 1234 || !it.Online {
		t.Errorf("got %+v", it)
	}
}

func TestHTTPResolveFallbackGetAndOffline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/gone":
			w.WriteHeader(404)
		case r.Method == http.MethodHead:
			w.WriteHeader(405)
		default:
			if r.Header.Get("Range") != "bytes=0-0" {
				t.Errorf("Range = %q", r.Header.Get("Range"))
			}
			w.Header().Set("Content-Range", "bytes 0-0/999")
			w.WriteHeader(206)
			_, _ = w.Write([]byte("x"))
		}
	}))
	defer srv.Close()
	items, err := (&HTTP{}).Resolve(context.Background(), srv.URL+"/a%20b.mkv")
	if err != nil || items[0].Name != "a b.mkv" || items[0].Size != 999 || !items[0].Online {
		t.Fatalf("err=%v items=%+v", err, items)
	}
	items, _ = (&HTTP{}).Resolve(context.Background(), srv.URL+"/gone")
	if items[0].Online || items[0].Error != "Lien hors ligne (HTTP 404)" {
		t.Errorf("got %+v", items[0])
	}
}

func TestHTTPOpenRangeIgnored(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer srv.Close()
	h := &HTTP{}
	it, err := h.Reopen(context.Background(), srv.URL+"/f", "")
	if err != nil {
		t.Fatal(err)
	}
	body, ok, err := it.Open(context.Background(), 10)
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	_ = body.Close()
	body, ok, err = it.Open(context.Background(), 0)
	if err != nil || !ok {
		t.Fatalf("offset 0 : ok=%v err=%v", ok, err)
	}
	b, _ := io.ReadAll(body)
	_ = body.Close()
	if string(b) != "0123456789" {
		t.Errorf("body = %q", b)
	}
}

func TestHTTPOpenStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer srv.Close()
	it, _ := (&HTTP{}).Reopen(context.Background(), srv.URL, "")
	_, _, err := it.Open(context.Background(), 0)
	var se *StatusError
	if !errors.As(err, &se) || se.Code != 503 {
		t.Errorf("err = %v", err)
	}
}

func TestHTTPOpenPartial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "bytes=4-" {
			t.Errorf("Range = %q", r.Header.Get("Range"))
		}
		w.WriteHeader(206)
	}))
	defer srv.Close()
	it, _ := (&HTTP{}).Reopen(context.Background(), srv.URL, "")
	body, ok, err := it.Open(context.Background(), 4)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	_ = body.Close()
}

func TestHTTPResolveHTMLPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}))
	defer srv.Close()
	items, err := (&HTTP{}).Resolve(context.Background(), srv.URL+"/fichier.mkv")
	if err != nil || items[0].Online || items[0].Error != "Page web, pas un fichier" {
		t.Fatalf("err=%v items=%+v", err, items)
	}
}
