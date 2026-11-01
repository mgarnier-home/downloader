package api

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/mgarnier-home/utils/logger"

	"github.com/mgarnier-home/downloader/backend/naming"
	"github.com/mgarnier-home/downloader/backend/source"
	"github.com/mgarnier-home/downloader/backend/tmdb"
)

var errNoTMDB = errors.New("TMDB : aucune fiche")

// analyze résout les liens en parallèle, puis propose type, fiche TMDB et destination.
func (h *Handlers) analyze(w http.ResponseWriter, r *http.Request) {
	var req AnalyzeRequest
	if !readJSON(w, r, &req) {
		return
	}
	var links []string
	for _, l := range req.Links {
		if l = strings.TrimSpace(l); l != "" {
			links = append(links, l)
		}
	}
	resolved := make([][]AnalyzeRow, len(links))
	parallel(len(links), func(i int) { resolved[i] = h.resolve(r.Context(), links[i]) })
	rows := slices.Concat(resolved...)
	h.place(r.Context(), rows)
	if rows == nil {
		rows = []AnalyzeRow{}
	}
	writeJSON(w, http.StatusOK, rows)
}

// resolve donne une ligne par fichier du lien ; une erreur de source devient une ligne hors ligne.
func (h *Handlers) resolve(ctx context.Context, link string) []AnalyzeRow {
	s, ok := source.Pick(h.Sources, link)
	if !ok {
		return []AnalyzeRow{{Input: link, Error: "Lien non reconnu"}}
	}
	if s.Name() == "deezer" {
		return []AnalyzeRow{{Input: link, Error: "Lien Deezer : utiliser l'onglet Musique"}}
	}
	var items []source.Item
	if ad, ok := h.named("alldebrid"); ok && s.Name() == "http" {
		// AllDebrid d'abord, même pour un hôte absent de sa liste : le direct n'est qu'un repli.
		if it, err := ad.Resolve(ctx, link); err == nil && len(it) == 1 && it[0].Online {
			s, items = ad, it
		}
	}
	if items == nil {
		var err error
		if items, err = s.Resolve(ctx, link); err != nil {
			return []AnalyzeRow{{Input: link, Error: err.Error(), Source: s.Name()}}
		}
	}
	if len(items) == 0 {
		return []AnalyzeRow{{Input: link, Error: "Aucun fichier", Source: s.Name()}}
	}
	rows := make([]AnalyzeRow, len(items))
	for i, it := range items {
		rows[i] = AnalyzeRow{Input: link, Name: it.Name, Size: it.Size, Online: it.Online, Error: it.Error, Source: s.Name()}
	}
	return rows
}

// named renvoie la source de ce nom.
func (h *Handlers) named(name string) (source.Source, bool) {
	for _, s := range h.Sources {
		if s.Name() == name {
			return s, true
		}
	}
	return nil, false
}

// pick choisit la source d'input. Un lien que la liste des hébergeurs d'AllDebrid ignore lui reste
// confié quand l'analyse a vérifié qu'il le débride (prefer vaut alors "alldebrid").
func (h *Handlers) pick(input, prefer string) (source.Source, bool) {
	s, ok := source.Pick(h.Sources, input)
	if ok && s.Name() == "http" && prefer == "alldebrid" {
		return h.named("alldebrid")
	}
	return s, ok
}

type searchKey struct {
	title string
	year  int
	tv    bool
}

// keyOf regroupe les recherches TMDB ; un pack est toujours une série.
func keyOf(p naming.Parsed, pack bool) searchKey {
	return searchKey{strings.ToLower(p.Title), p.Year, pack || p.HasEpisode || p.Season > 0}
}

type searchResult struct {
	res *tmdb.Result
	err error
}

// place cherche chaque titre une seule fois sur TMDB puis remplit les lignes en ligne.
func (h *Handlers) place(ctx context.Context, rows []AnalyzeRow) {
	parsed := make([]naming.Parsed, len(rows))
	var keys []searchKey
	for i, row := range rows {
		if !row.Online {
			continue
		}
		parsed[i] = naming.ParseFilename(row.Name)
		if k := keyOf(parsed[i], naming.IsPack(row.Name)); !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	results := make([]searchResult, len(keys))
	parallel(len(keys), func(i int) { results[i] = h.search(ctx, keys[i]) })
	for i := range rows {
		if rows[i].Online {
			h.fill(&rows[i], parsed[i], results[slices.Index(keys, keyOf(parsed[i], naming.IsPack(rows[i].Name)))])
		}
	}
}

func (h *Handlers) search(ctx context.Context, k searchKey) searchResult {
	if k.title == "" {
		return searchResult{err: errNoTMDB}
	}
	res, err := h.TMDB.Search(ctx, k.title, k.year, k.tv)
	if err != nil {
		logger.Warnf("analyse : %v", err)
		return searchResult{err: err}
	}
	if len(res) == 0 {
		return searchResult{err: errNoTMDB}
	}
	return searchResult{res: &res[0]}
}

// fill applique la règle de type, le nom (dossier existant, sinon PascalCase) et la destination.
func (h *Handlers) fill(row *AnalyzeRow, p naming.Parsed, sr searchResult) {
	row.Season, row.Episode = p.Season, p.Episode
	row.Pack = naming.IsPack(row.Name)
	tv := row.Pack || p.HasEpisode || p.Season > 0
	switch {
	case tv:
		row.Kind = naming.Series
	case sr.res != nil && sr.res.Animation:
		row.Kind = naming.Anime
	default:
		row.Kind = naming.Movie
	}
	name := naming.PascalCase(p.Title)
	if sr.err != nil {
		row.Error, row.NeedsReview = sr.err.Error(), true
	} else {
		t := toTMDB(*sr.res)
		row.TMDB = &t
		if n, ok := h.Index.Lookup(row.Kind, t.ID); ok {
			name = n
		} else {
			name = cmp.Or(naming.PascalCase(t.Title), name)
		}
	}
	if row.Pack && name != "" { // les épisodes et leur présence se voient après extraction
		// Un pack est une saison : sans Sxx dans le nom, la 1 est proposée, à relire.
		row.Season, row.NeedsReview = cmp.Or(p.Season, 1), row.NeedsReview || p.Season == 0
		row.Dest = naming.PackDir(name, row.Season)
		return
	}
	if name == "" || tv && !p.HasEpisode { // série sans épisode : numéro ou Bonus à choisir
		row.NeedsReview = true
		return
	}
	row.Dest = naming.VideoPath(row.Kind, name, p.Season, p.Episode, p.Ext)
	if abs, err := naming.Within(h.MediaDir, row.Dest); err == nil {
		_, err := os.Stat(abs)
		row.Exists = err == nil
	}
}
