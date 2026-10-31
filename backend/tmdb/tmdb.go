// Package tmdb interroge l'API TMDB v3 pour retrouver la fiche d'un film ou d'une série.
package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// Result est une fiche TMDB.
type Result struct {
	ID        int
	Title     string
	Year      int
	Animation bool
	Poster    string
	TV        bool
}

// Client appelle TMDB ; HTTP vaut http.DefaultClient s'il est nul.
type Client struct {
	BaseURL, Key string
	HTTP         *http.Client
}

const posterBase = "https://image.tmdb.org/t/p/w154"

// animationGenre est l'identifiant du genre « Animation » chez TMDB.
const animationGenre = 16

type rawResult struct {
	ID           int    `json:"id"`
	Title        string `json:"title"`
	Name         string `json:"name"`
	ReleaseDate  string `json:"release_date"`
	FirstAirDate string `json:"first_air_date"`
	GenreIDs     []int  `json:"genre_ids"`
	PosterPath   string `json:"poster_path"`
}

// Search cherche une fiche en fr-FR ; year <= 0 ne filtre pas sur l'année.
func (c *Client) Search(ctx context.Context, title string, year int, tv bool) ([]Result, error) {
	kind, yearParam := "movie", "year"
	if tv {
		kind, yearParam = "tv", "first_air_date_year"
	}
	q := url.Values{"api_key": {c.Key}, "query": {title}, "language": {"fr-FR"}}
	if year > 0 {
		q.Set(yearParam, strconv.Itoa(year))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/search/"+kind+"?"+q.Encode(), nil)
	if err != nil {
		return nil, errors.New("TMDB : requête invalide")
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		// L'URL (donc la clé) figure dans *url.Error : on ne garde que la cause.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("TMDB : %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("TMDB : %s", resp.Status)
	}
	var body struct {
		Results []rawResult `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("TMDB : réponse illisible : %w", err)
	}
	out := make([]Result, 0, len(body.Results))
	for _, r := range body.Results {
		out = append(out, r.result(tv))
	}
	return out, nil
}

func (r rawResult) result(tv bool) Result {
	res := Result{ID: r.ID, Title: r.Title, TV: tv}
	date := r.ReleaseDate
	if tv {
		res.Title, date = r.Name, r.FirstAirDate
	}
	if y, err := strconv.Atoi(strings.SplitN(date, "-", 2)[0]); err == nil {
		res.Year = y
	}
	res.Animation = slices.Contains(r.GenreIDs, animationGenre)
	if r.PosterPath != "" {
		res.Poster = posterBase + r.PosterPath
	}
	return res
}
