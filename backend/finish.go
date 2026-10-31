package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	free     func(dir string) (int64, error) // espace libre ; nil : statfs
}

var kindDirs = map[naming.Kind]string{naming.Series: "series", naming.Movie: "films", naming.Anime: "animes"}

const maxCover = 20 << 20

func (fz *finisher) finish(ctx context.Context, f store.File, part string, progress func(done, total int64)) error {
	jobs, err := fz.store.Jobs([]int64{f.JobID})
	if err != nil {
		return err
	}
	j := jobs[f.JobID]
	if naming.IsPack(f.OriginalName) { // même critère que la file, qui supprime le .part après succès
		if j.Kind != naming.Series {
			return errors.New("Un pack doit être rangé comme une série") //nolint:staticcheck // message affiché tel quel
		}
		return fz.pack(ctx, f, j, part, progress)
	}
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
		fz.albumCover(f.ID, naming.AlbumDir(m.Info.AlbumArtist, m.Info.Album), m.CoverURL)
	}
	return deezer.WriteTags(part, m, cover)
}

// albumCover écrit cover.jpg (1200 px) à la racine de l'album s'il manque ; un échec est
// seulement journalisé, le morceau suivant de l'album réessaiera.
func (fz *finisher) albumCover(id int64, album, tmpl string) {
	dir, err := naming.Within(fz.mediaDir, album)
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
		logger.Warnf("fichier %d : cover.jpg : %v", id, err)
	}
}

func (fz *finisher) get(tmpl string, size int) ([]byte, error) {
	resp, err := fz.http.Get(strings.ReplaceAll(tmpl, "{size}", strconv.Itoa(size)))
	if err != nil {
		var ue *url.Error // l'URL finirait dans l'erreur du fichier, servie par l'API
		if errors.As(err, &ue) {
			err = ue.Err
		}
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
