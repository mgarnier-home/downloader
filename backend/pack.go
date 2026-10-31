package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/mgarnier-home/downloader/backend/archive"
	"github.com/mgarnier-home/downloader/backend/naming"
	"github.com/mgarnier-home/downloader/backend/store"
)

// pack extrait les vidéos de l'archive part et les range dans le dossier de la série (f.Dest).
// Rien n'est écrasé : une destination existante, ou déjà servie par ce pack, compte « déjà présente ».
// shortcut: un arrêt pendant l'extraction la relance entière ; les épisodes déjà rangés comptent
// alors en déjà présents, reprise fine si le besoin apparaît.
func (fz *finisher) pack(ctx context.Context, f store.File, j store.Job, part string, progress func(done, total int64)) error {
	total, err := inventory(part)
	if err != nil {
		return err
	}
	free, err := fz.freeBytes()
	if err != nil {
		return fmt.Errorf("Extraction : %w", err) //nolint:staticcheck // message affiché tel quel
	}
	if free < total {
		return errors.New("Espace insuffisant pour extraire") //nolint:staticcheck // message affiché tel quel
	}
	run := &packRun{
		ctx: ctx, mediaDir: fz.mediaDir, name: strings.TrimPrefix(f.Dest, "series/"),
		season: naming.ParseFilename(f.OriginalName).Season, total: total, progress: progress,
		tmp: filepath.Join(fz.mediaDir, "download", strconv.FormatInt(f.ID, 10)), used: map[string]bool{},
	}
	defer func() { _ = os.RemoveAll(run.tmp) }()
	if err := os.MkdirAll(run.tmp, 0o755); err != nil {
		return fmt.Errorf("Extraction : %w", err) //nolint:staticcheck // message affiché tel quel
	}
	progress(0, total)
	if err := archive.Walk(part, run.entry); err != nil {
		if errors.Is(err, archive.ErrPassword) {
			return archiveError(err)
		}
		return fmt.Errorf("Extraction : %w", err) //nolint:staticcheck // message affiché tel quel
	}
	meta, err := json.Marshal(store.PackMeta{Pack: &run.report})
	if err != nil {
		return err
	}
	if err := fz.store.SetMeta(f.ID, string(meta)); err != nil {
		return fmt.Errorf("bilan du pack : %w", err)
	}
	if id, err := strconv.Atoi(strings.TrimPrefix(j.Ref, "tmdb:")); err == nil && strings.HasPrefix(j.Ref, "tmdb:") {
		fz.index.Add(naming.Series, id, run.name)
	}
	return nil
}

// inventory renvoie la taille totale des vidéos de l'archive, sans les lire.
func inventory(part string) (int64, error) {
	var total int64
	videos := 0
	err := archive.Walk(part, func(e archive.Entry, _ io.Reader) error {
		if naming.IsVideo(e.Path) {
			total += e.Size
			videos++
		}
		return nil
	})
	if err != nil {
		return 0, archiveError(err)
	}
	if videos == 0 {
		return 0, errors.New("Aucune vidéo dans l'archive") //nolint:staticcheck // message affiché tel quel
	}
	return total, nil
}

// packRun est l'état d'une extraction : destinations servies, octets traités, bilan.
type packRun struct {
	ctx                 context.Context
	mediaDir, name, tmp string
	season, n           int
	total, done         int64
	progress            func(done, total int64)
	used                map[string]bool
	report              store.PackReport
}

func (p *packRun) entry(e archive.Entry, r io.Reader) error {
	if !naming.IsVideo(e.Path) {
		return nil
	}
	p.n++
	dest, bonus := naming.PackDest(p.name, e.Path, p.season)
	abs, err := naming.Within(p.mediaDir, dest)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); p.used[dest] || err == nil {
		p.report.Present++
		p.done += e.Size
		p.progress(p.done, p.total)
		return nil
	}
	p.used[dest] = true
	tmp := filepath.Join(p.tmp, strconv.Itoa(p.n)+".tmp")
	if err := extract(p.ctx, tmp, r, func(k int) { p.done += int64(k); p.progress(p.done, p.total) }); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, abs); err != nil {
		return err
	}
	if bonus {
		p.report.Bonus++
	} else {
		p.report.Placed++
	}
	return nil
}

func archiveError(err error) error {
	if errors.Is(err, archive.ErrPassword) {
		return errors.New("Archive protégée par mot de passe") //nolint:staticcheck // message affiché tel quel
	}
	return fmt.Errorf("Archive illisible : %w", err) //nolint:staticcheck // message affiché tel quel
}

func (fz *finisher) freeBytes() (int64, error) {
	if fz.free != nil {
		return fz.free(fz.mediaDir)
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(fz.mediaDir, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

// extract copie r dans p ; la copie s'arrête dès que ctx est annulé.
func extract(ctx context.Context, p string, r io.Reader, add func(int)) error {
	out, err := os.Create(p)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, ctxReader{ctx, r, add})
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
	add func(int)
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := c.r.Read(p)
	c.add(n)
	return n, err
}
