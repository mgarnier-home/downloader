package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// AllDebrid débride les liens d'hébergeurs via l'API v4.
type AllDebrid struct {
	BaseURL, Key string
	HTTP         *http.Client
	hosts        []string
	sleep        func(context.Context, time.Duration) error // injectable pour les tests
	mu           sync.RWMutex
}

func (a *AllDebrid) Name() string { return "alldebrid" }

func (a *AllDebrid) client() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return http.DefaultClient
}

type envelope struct {
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"`
	Error  *apiError       `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// call exécute un appel API et décode data dans out.
func (a *AllDebrid) call(ctx context.Context, method, endpoint string, form url.Values, out any) error {
	q := url.Values{"agent": {"downloader"}}
	var body io.Reader
	if method == http.MethodGet {
		for k, v := range form {
			q[k] = v
		}
	} else {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, a.BaseURL+endpoint+"?"+q.Encode(), body)
	if err != nil {
		return errors.New("AllDebrid : requête invalide")
	}
	req.Header.Set("Authorization", "Bearer "+a.Key)
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := a.client().Do(req)
	if err != nil {
		return fmt.Errorf("AllDebrid : %w", stripURL(err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return &StatusError{Code: resp.StatusCode} // passager : la file retente
	}
	var env envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		if resp.StatusCode >= 400 {
			return &StatusError{Code: resp.StatusCode}
		}
		return fmt.Errorf("AllDebrid : réponse illisible : %w", err)
	}
	if env.Status != "success" {
		if env.Error != nil {
			return fmt.Errorf("AllDebrid : %s", env.Error.Message)
		}
		return fmt.Errorf("AllDebrid : erreur HTTP %d", resp.StatusCode)
	}
	return json.Unmarshal(env.Data, out)
}

// LoadHosts lit la liste des domaines pris en charge.
func (a *AllDebrid) LoadHosts(ctx context.Context) error {
	var data struct {
		Hosts map[string]struct {
			Domains []string `json:"domains"`
		} `json:"hosts"`
	}
	if err := a.call(ctx, http.MethodGet, "/v4/hosts", nil, &data); err != nil {
		return err
	}
	var hosts []string
	for _, h := range data.Hosts {
		for _, d := range h.Domains {
			hosts = append(hosts, strings.ToLower(d))
		}
	}
	a.mu.Lock()
	a.hosts = hosts
	a.mu.Unlock()
	return nil
}

// Match est vrai si l'hôte du lien, ou un de ses domaines parents, est connu.
func (a *AllDebrid) Match(input string) bool {
	u, err := url.Parse(input)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	for host := strings.ToLower(u.Hostname()); host != ""; {
		for _, h := range a.hosts {
			if h == host {
				return true
			}
		}
		_, host, _ = strings.Cut(host, ".")
	}
	return false
}

// hostLabel donne le nom affiché : domaine sans « www. » ni extension.
func hostLabel(link string) string {
	u, err := url.Parse(link)
	if err != nil || u.Hostname() == "" {
		return "AllDebrid"
	}
	parts := strings.Split(strings.TrimPrefix(strings.ToLower(u.Hostname()), "www."), ".")
	if len(parts) >= 2 {
		return parts[len(parts)-2]
	}
	return parts[0]
}

func hostError(link string, e apiError) string {
	if e.Code == "LINK_DOWN" {
		return hostLabel(link) + " : fichier supprimé par l'hébergeur"
	}
	return hostLabel(link) + " : " + e.Message
}

func (a *AllDebrid) item(link string) Item {
	return Item{Online: true, Open: func(ctx context.Context, offset int64) (io.ReadCloser, bool, error) {
		// Les liens débridés expirent : on débride au moment du téléchargement.
		direct, err := a.unlock(ctx, link)
		if err != nil {
			return nil, false, fmt.Errorf("%s : %w", hostLabel(link), err)
		}
		return openURL(ctx, a.client(), direct, offset)
	}}
}

func (a *AllDebrid) Reopen(_ context.Context, input, _ string) (Item, error) {
	return a.item(input), nil
}

func (a *AllDebrid) Resolve(ctx context.Context, input string) ([]Item, error) {
	var data struct {
		Infos []struct {
			Filename string    `json:"filename"`
			Size     int64     `json:"size"`
			Error    *apiError `json:"error"`
		} `json:"infos"`
	}
	if err := a.call(ctx, http.MethodPost, "/v4/link/infos", url.Values{"link[]": {input}}, &data); err != nil {
		return nil, err
	}
	if len(data.Infos) != 1 {
		return nil, errors.New("AllDebrid : réponse inattendue")
	}
	it, info := a.item(input), data.Infos[0]
	if info.Error != nil {
		it.Online, it.Error = false, hostError(input, *info.Error)
		return []Item{it}, nil
	}
	it.Name, it.Size = info.Filename, info.Size
	return []Item{it}, nil
}

// Account lit l'état de l'abonnement.
func (a *AllDebrid) Account(ctx context.Context) (bool, time.Time, error) {
	var data struct {
		User struct {
			IsPremium    bool     `json:"isPremium"`
			PremiumUntil unixTime `json:"premiumUntil"`
		} `json:"user"`
	}
	if err := a.call(ctx, http.MethodGet, "/v4/user", nil, &data); err != nil {
		return false, time.Time{}, err
	}
	return data.User.IsPremium, data.User.PremiumUntil.Time, nil
}

// delayedPoll est l'attente entre deux interrogations d'un lien en génération, delayedMax le total
// des attentes au-delà duquel on abandonne.
const (
	delayedPoll = 5 * time.Second
	delayedMax  = 10 * time.Minute
)

func (a *AllDebrid) wait(ctx context.Context) error {
	if a.sleep != nil {
		return a.sleep(ctx, delayedPoll)
	}
	t := time.NewTimer(delayedPoll)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// unlock débride link ; si AllDebrid diffère la génération, interroge link/delayed.
func (a *AllDebrid) unlock(ctx context.Context, link string) (string, error) {
	var data struct {
		Link    string          `json:"link"`
		Delayed json.RawMessage `json:"delayed"`
	}
	if err := a.call(ctx, http.MethodPost, "/v4/link/unlock", url.Values{"link": {link}}, &data); err != nil {
		return "", err
	}
	if data.Link != "" {
		return data.Link, nil
	}
	id := strings.Trim(string(data.Delayed), `" `)
	if id == "" || id == "null" || id == "0" {
		return "", errors.New("AllDebrid : aucun lien débridé dans la réponse")
	}
	for waited := time.Duration(0); ; waited += delayedPoll {
		var d struct {
			Status int    `json:"status"`
			Link   string `json:"link"`
		}
		if err := a.call(ctx, http.MethodPost, "/v4/link/delayed", url.Values{"id": {id}}, &d); err != nil {
			return "", err
		}
		switch {
		case d.Status == 2:
			if d.Link == "" {
				return "", errors.New("AllDebrid : aucun lien débridé dans la réponse")
			}
			return d.Link, nil
		case d.Status != 1: // 1 : en cours ; tout autre statut est un échec
			return "", errors.New("AllDebrid : génération du lien échouée")
		case waited >= delayedMax:
			return "", errors.New("AllDebrid : génération du lien trop longue")
		}
		if err := a.wait(ctx); err != nil {
			return "", err
		}
	}
}

// unixTime décode un timestamp Unix donné en entier ou en chaîne ; 0 donne le temps zéro.
type unixTime struct{ time.Time }

func (u *unixTime) UnmarshalJSON(b []byte) error {
	n, err := strconv.ParseInt(strings.Trim(string(b), `" `), 10, 64)
	if err != nil {
		return fmt.Errorf("date d'abonnement illisible : %w", err)
	}
	if n != 0 {
		u.Time = time.Unix(n, 0)
	}
	return nil
}
