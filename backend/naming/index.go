package naming

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Index retrouve un dossier existant à partir de son identifiant TMDB.
type Index struct {
	mu   sync.RWMutex
	byID map[Kind]map[int]string
}

type nfo struct {
	TmdbID int `xml:"tmdbid"`
}

// BuildIndex parcourt series/, films/ et animes/ et lit les fichiers .nfo ; les illisibles sont ignorés.
func BuildIndex(mediaDir string) (*Index, error) {
	if _, err := os.Stat(mediaDir); err != nil {
		return nil, fmt.Errorf("lecture de la médiathèque : %w", err)
	}
	ix := &Index{byID: map[Kind]map[int]string{}}
	layout := []struct {
		kind  Kind
		dir   string
		files func(name string) []string
	}{
		{Series, "series", func(string) []string { return []string{"tvshow.nfo"} }},
		{Movie, "films", func(n string) []string { return []string{"movie.nfo", n + ".nfo"} }},
		{Anime, "animes", func(n string) []string { return []string{"movie.nfo", n + ".nfo"} }},
	}
	for _, l := range layout {
		entries, _ := os.ReadDir(filepath.Join(mediaDir, l.dir)) // dossier absent : rien à indexer
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			for _, f := range l.files(e.Name()) {
				if id := readTmdbID(filepath.Join(mediaDir, l.dir, e.Name(), f)); id != 0 {
					ix.Add(l.kind, id, e.Name())
					break
				}
			}
		}
	}
	return ix, nil
}

func readTmdbID(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var n nfo
	if xml.Unmarshal(data, &n) != nil {
		return 0
	}
	return n.TmdbID
}

// Lookup renvoie le nom du dossier associé à un identifiant TMDB.
func (ix *Index) Lookup(kind Kind, tmdbID int) (name string, ok bool) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	name, ok = ix.byID[kind][tmdbID]
	return name, ok
}

// Add enregistre un dossier (créé après la construction de l'index).
func (ix *Index) Add(kind Kind, tmdbID int, name string) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.byID[kind] == nil {
		ix.byID[kind] = map[int]string{}
	}
	ix.byID[kind][tmdbID] = name
}
