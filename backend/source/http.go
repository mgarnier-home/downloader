package source

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

// HTTP télécharge un lien HTTP(S) direct.
type HTTP struct{ Client *http.Client }

func (h *HTTP) Name() string { return "http" }

func (h *HTTP) Match(input string) bool {
	return strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://")
}

func (h *HTTP) client() *http.Client {
	if h.Client != nil {
		return h.Client
	}
	return http.DefaultClient
}

// stripURL retire l'URL (qui peut porter un secret) d'une erreur réseau.
func stripURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// openURL ouvre u à partir de offset ; le booléen est vrai si le décalage est respecté.
func openURL(ctx context.Context, c *http.Client, u string, offset int64) (io.ReadCloser, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false, errors.New("lien invalide")
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, false, stripURL(err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_ = resp.Body.Close()
		return nil, false, &StatusError{Code: resp.StatusCode}
	}
	return resp.Body, offset == 0 || resp.StatusCode == http.StatusPartialContent, nil
}

func (h *HTTP) item(u string) Item {
	return Item{Online: true, Open: func(ctx context.Context, offset int64) (io.ReadCloser, bool, error) {
		return openURL(ctx, h.client(), u, offset)
	}}
}

func (h *HTTP) Reopen(_ context.Context, input, _ string) (Item, error) {
	return h.item(input), nil
}

func (h *HTTP) Resolve(ctx context.Context, input string) ([]Item, error) {
	resp, err := h.probe(ctx, http.MethodHead, input)
	if err == nil && resp.StatusCode >= 400 {
		_ = resp.Body.Close()
		// Certains serveurs refusent HEAD : repli sur un GET d'un seul octet.
		resp, err = h.probe(ctx, http.MethodGet, input)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	it := h.item(input)
	if resp.StatusCode >= 400 {
		it.Online, it.Error = false, fmt.Sprintf("Lien hors ligne (HTTP %d)", resp.StatusCode)
	}
	if mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); it.Online && mt == "text/html" {
		it.Online, it.Error = false, "Page web, pas un fichier" // page d'un hébergeur non reconnu
	}
	it.Name = fileName(resp, input)
	it.Size = size(resp)
	return []Item{it}, nil
}

func (h *HTTP) probe(ctx context.Context, method, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, errors.New("lien invalide")
	}
	if method == http.MethodGet {
		req.Header.Set("Range", "bytes=0-0")
	}
	resp, err := h.client().Do(req)
	if err != nil {
		return nil, stripURL(err)
	}
	return resp, nil
}

func fileName(resp *http.Response, u string) string {
	if _, p, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil && p["filename"] != "" {
		return path.Base(p["filename"])
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return path.Base(parsed.Path) // Path est déjà décodé
}

func size(resp *http.Response) int64 {
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		if i := strings.LastIndex(cr, "/"); i >= 0 {
			if n, err := strconv.ParseInt(cr[i+1:], 10, 64); err == nil {
				return n
			}
		}
	}
	if resp.StatusCode == http.StatusPartialContent {
		return 0
	}
	return max(resp.ContentLength, 0)
}
