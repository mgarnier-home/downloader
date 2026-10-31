package queue

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/mgarnier-home/utils/logger"

	"github.com/mgarnier-home/downloader/backend/naming"
	"github.com/mgarnier-home/downloader/backend/source"
	"github.com/mgarnier-home/downloader/backend/store"
)

func (m *Manager) partPath(id int64) string {
	return filepath.Join(m.MediaDir, "download", strconv.FormatInt(id, 10)+".part")
}

// work exécute un transfert puis enregistre son état final.
func (m *Manager) work(ctx context.Context, f store.File, t *transfer) {
	defer m.wg.Done()
	logger.Infof("fichier %d (%s) : démarrage", f.ID, f.OriginalName)
	err := m.attempts(ctx, f, t)
	m.mu.Lock()
	removed := t.removed
	m.mu.Unlock()
	var st store.State
	switch {
	case err == nil:
		st = store.StateDone
		logger.Infof("fichier %d (%s) : rangé", f.ID, f.OriginalName)
	case ctx.Err() != nil && removed: // Cancel : l'appelant décide du sort du fichier
	case ctx.Err() != nil:
		st, err = store.StatePending, nil // pause ou arrêt : le .part reste pour la reprise
	default:
		st = store.StateError
		logger.Errorf("fichier %d (%s) : %v", f.ID, f.OriginalName, err)
	}
	if st != "" {
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		if serr := m.Store.SetState(f.ID, st, msg); serr != nil {
			logger.Errorf("fichier %d : enregistrement de l'état impossible : %v", f.ID, serr)
		}
	}
	m.mu.Lock()
	delete(m.running, f.ID)
	m.mu.Unlock()
	t.cancel()
	close(t.done)
	m.Wake()
}

// attempts télécharge f, en retentant les erreurs passagères après chaque délai, puis le range.
func (m *Manager) attempts(ctx context.Context, f store.File, t *transfer) error {
	part := m.partPath(f.ID)
	for i := 0; ; i++ {
		err := m.fetch(ctx, f, part, t)
		if err == nil {
			return m.finish(ctx, f, part, t)
		}
		if ctx.Err() != nil || !transient(err) || i >= len(m.Delays) {
			return err
		}
		if err := m.Store.IncAttempts(f.ID); err != nil {
			logger.Errorf("fichier %d : compteur de tentatives : %v", f.ID, err)
		}
		logger.Warnf("fichier %d (%s) : %v, nouvel essai dans %s", f.ID, f.OriginalName, err, m.Delays[i])
		if err := m.sleep(ctx, m.Delays[i]); err != nil {
			return err
		}
	}
}

func (m *Manager) sleep(ctx context.Context, d time.Duration) error {
	if m.Sleep != nil {
		return m.Sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// transient dit si err mérite un nouvel essai : 429, 5xx, coupure réseau ou flux tronqué.
func transient(err error) bool {
	var se *source.StatusError
	if errors.As(err, &se) {
		return se.Code == http.StatusTooManyRequests || se.Code >= 500
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return false // erreur disque locale : syscall.Errno passerait pour une net.Error
	}
	var ne net.Error
	return errors.As(err, &ne) || errors.Is(err, io.ErrUnexpectedEOF)
}

// fetch complète le .part à partir de sa taille actuelle.
func (m *Manager) fetch(ctx context.Context, f store.File, part string, t *transfer) error {
	if err := os.MkdirAll(filepath.Dir(part), 0o755); err != nil {
		return fmt.Errorf("dossier temporaire : %w", err)
	}
	out, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("fichier temporaire : %w", err)
	}
	defer func() { _ = out.Close() }()
	offset, err := out.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if f.Size > 0 && offset == f.Size {
		return nil // déjà complet : seule la fin reste à faire
	}
	if f.Size <= 0 || offset > f.Size {
		offset = 0 // taille inconnue : impossible de savoir si le .part est complet, on repart de zéro
	}
	body, honoured, err := m.open(ctx, f, offset)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()
	if offset > 0 && !honoured {
		offset = 0 // le flux repart du début : on réécrit tout
	}
	if err := out.Truncate(offset); err != nil {
		return err
	}
	if _, err := out.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	return m.copy(out, body, f, t, offset)
}

// open retrouve la source du fichier et ouvre son flux à partir de offset.
func (m *Manager) open(ctx context.Context, f store.File, offset int64) (io.ReadCloser, bool, error) {
	for _, s := range m.Sources {
		if s.Name() != f.Source {
			continue
		}
		item, err := s.Reopen(ctx, f.Input, f.Meta)
		if err != nil {
			return nil, false, err
		}
		if item.Open == nil {
			return nil, false, errors.New("source sans téléchargement")
		}
		return item.Open(ctx, offset)
	}
	return nil, false, fmt.Errorf("source inconnue : %s", f.Source)
}

// copy écrit body dans out en suivant la progression.
func (m *Manager) copy(out *os.File, body io.Reader, f store.File, t *transfer, offset int64) error {
	m.mu.Lock()
	t.reset(offset, t.total, time.Now())
	m.mu.Unlock()
	n, err := io.Copy(io.MultiWriter(out, progressWriter{m, t}), body)
	if err != nil {
		return err
	}
	if f.Size > 0 && offset+n < f.Size {
		return io.ErrUnexpectedEOF
	}
	return out.Close()
}

type progressWriter struct {
	m *Manager
	t *transfer
}

func (w progressWriter) Write(p []byte) (int, error) {
	w.m.mu.Lock()
	w.t.add(len(p), time.Now())
	w.m.mu.Unlock()
	return len(p), nil
}

// finish appelle Finish sur le .part puis le range à sa destination. En cas d'échec le .part reste.
// Un pack est rangé par Finish lui-même ; son .part n'est supprimé qu'en cas de succès.
func (m *Manager) finish(ctx context.Context, f store.File, part string, t *transfer) error {
	progress := func(done, total int64) {
		m.mu.Lock()
		t.phase, t.bytes, t.total = "extraction", done, total
		m.mu.Unlock()
	}
	if naming.IsPack(f.OriginalName) {
		if m.Finish == nil {
			return errors.New("pack sans finalisation")
		}
		if err := m.Finish(ctx, f, part, progress); err != nil {
			return err // message déjà lisible, affiché tel quel
		}
		return os.Remove(part)
	}
	dest, err := naming.Within(m.MediaDir, f.Dest)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dest); err == nil && !f.Overwrite {
		return errors.New("Destination déjà présente") //nolint:staticcheck // message affiché tel quel
	}
	if m.Finish != nil {
		if err := m.Finish(ctx, f, part, progress); err != nil {
			return fmt.Errorf("finalisation : %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("dossier de destination : %w", err)
	}
	if err := os.Rename(part, dest); err != nil {
		return fmt.Errorf("déplacement : %w", err)
	}
	return nil
}
