package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mgarnier-home/utils/logger"

	"github.com/mgarnier-home/downloader/backend/deezer"
	"github.com/mgarnier-home/downloader/backend/naming"
	"github.com/mgarnier-home/downloader/backend/store"
)

// finisher complète un fichier téléchargé, sur son .part, avant son déplacement : tags et
// pochettes pour la musique, index des dossiers pour la vidéo.
type finisher struct {
	store    *store.Store
	index    *naming.Index
	mediaDir string
	http     *http.Client
}

var kindDirs = map[naming.Kind]string{naming.Series: "series", naming.Movie: "films", naming.Anime: "animes"}

const maxCover = 20 << 20

func (fz *finisher) finish(f store.File, part string) error {
	jobs, err := fz.store.Jobs([]int64{f.JobID})
	if err != nil {
		return err
	}
	j := jobs[f.JobID]
	if j.Kind == naming.Music {
		return fz.music(f, part)
	}
	// Le dossier de l'œuvre est réutilisé par les prochaines analyses.
	id, err := strconv.Atoi(strings.TrimPrefix(j.Ref, "tmdb:"))
	parts := strings.Split(f.Dest, "/")
	if err == nil && strings.HasPrefix(j.Ref, "tmdb:") && len(parts) >= 3 && parts[0] == kindDirs[j.Kind] {
		fz.index.Add(j.Kind, id, parts[1])
	}
	return nil
}

func (fz *finisher) music(f store.File, part string) error {
	var m deezer.TrackMeta
	if err := json.Unmarshal([]byte(f.Meta), &m); err != nil {
		return fmt.Errorf("métadonnées illisibles : %w", err)
	}
	var cover []byte
	if m.CoverURL != "" {
		var err error
		if cover, err = fz.get(m.CoverURL, 800); err != nil {
			return fmt.Errorf("pochette : %w", err)
		}
		fz.albumCover(f, m.CoverURL)
	}
	return deezer.WriteTags(part, m, cover)
}

// albumCover écrit cover.jpg (1200 px) dans le dossier du morceau s'il manque ; un échec est
// seulement journalisé, le morceau suivant de l'album réessaiera.
func (fz *finisher) albumCover(f store.File, tmpl string) {
	dir, err := naming.Within(fz.mediaDir, filepath.Dir(f.Dest))
	if err != nil {
		return
	}
	p := filepath.Join(dir, "cover.jpg")
	if _, err := os.Stat(p); err == nil {
		return
	}
	data, err := fz.get(tmpl, 1200)
	if err == nil {
		err = os.MkdirAll(dir, 0o755)
	}
	if err == nil {
		err = writeAtomic(p, data)
	}
	if err != nil {
		logger.Warnf("fichier %d : cover.jpg : %v", f.ID, err)
	}
}

func (fz *finisher) get(tmpl string, size int) ([]byte, error) {
	resp, err := fz.http.Get(strings.ReplaceAll(tmpl, "{size}", strconv.Itoa(size)))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("réponse HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxCover))
}

// writeAtomic écrit via un fichier temporaire : plusieurs morceaux d'un album finissent en même temps.
func writeAtomic(p string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(p), ".cover-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}
