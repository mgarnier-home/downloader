// Package store gère la file et l'historique persistants (SQLite).
package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pilote pur Go (CGO_ENABLED=0)

	"github.com/mgarnier-home/downloader/backend/naming"
)

type State string

const (
	StatePending State = "pending"
	StateRunning State = "running"
	StateDone    State = "done"
	StateError   State = "error"
)

type Job struct {
	ID        int64
	Kind      naming.Kind
	Title     string
	Ref       string
	CreatedAt time.Time
}

type File struct {
	ID, JobID                   int64
	Source, Input, OriginalName string
	Size                        int64
	Dest                        string
	Overwrite                   bool
	State                       State
	Error                       string
	Attempts                    int
	StartedAt, FinishedAt       *time.Time
	Meta                        string // JSON opaque propre à la source
}

// PackReport est le bilan d'un pack, rangé dans File.Meta sous la clé "pack".
type PackReport struct {
	Placed  int `json:"placed"`  // épisodes rangés
	Present int `json:"present"` // vidéos déjà présentes, non écrasées
	Bonus   int `json:"bonus"`   // bonus rangés
}

type PackMeta struct {
	Pack *PackReport `json:"pack"`
}

type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS jobs(
  id INTEGER PRIMARY KEY, kind TEXT, title TEXT, ref TEXT, created_at TIMESTAMP);
CREATE TABLE IF NOT EXISTS files(
  id INTEGER PRIMARY KEY, job_id INTEGER REFERENCES jobs(id), source TEXT, input TEXT,
  original_name TEXT, size INTEGER, dest TEXT, overwrite INTEGER, state TEXT, error TEXT,
  attempts INTEGER, started_at TIMESTAMP, finished_at TIMESTAMP, meta TEXT);
CREATE INDEX IF NOT EXISTS files_state ON files(state);
PRAGMA user_version = 1;`

const fileCols = `id, job_id, source, input, original_name, size, dest, overwrite, state, error, attempts, started_at, finished_at, meta`

// Open ouvre (et crée au besoin) la base. Pas de WAL : la base peut vivre sur NFS.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(DELETE)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("ouverture de la base : %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("création du schéma : %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) CreateJob(j Job, files []File) (Job, []File, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return j, nil, err
	}
	defer func() { _ = tx.Rollback() }()
	j.CreatedAt = time.Now().UTC()
	res, err := tx.Exec(`INSERT INTO jobs(kind, title, ref, created_at) VALUES(?,?,?,?)`, j.Kind, j.Title, j.Ref, j.CreatedAt)
	if err != nil {
		return j, nil, fmt.Errorf("création du job : %w", err)
	}
	if j.ID, err = res.LastInsertId(); err != nil {
		return j, nil, err
	}
	out := make([]File, len(files))
	for i, f := range files {
		f.JobID = j.ID
		if f.State == "" {
			f.State = StatePending
		}
		res, err := tx.Exec(`INSERT INTO files(job_id, source, input, original_name, size, dest, overwrite, state, error, attempts, meta)
			VALUES(?,?,?,?,?,?,?,?,?,?,?)`, f.JobID, f.Source, f.Input, f.OriginalName, f.Size, f.Dest, f.Overwrite, f.State, f.Error, f.Attempts, f.Meta)
		if err != nil {
			return j, nil, fmt.Errorf("création du fichier : %w", err)
		}
		if f.ID, err = res.LastInsertId(); err != nil {
			return j, nil, err
		}
		out[i] = f
	}
	return j, out, tx.Commit()
}

// Files renvoie les fichiers dans les états donnés (tous si aucun), triés par ID.
func (s *Store) Files(states ...State) ([]File, error) {
	q := `SELECT ` + fileCols + ` FROM files`
	var args []any
	if len(states) > 0 {
		q += ` WHERE state IN (?` + strings.Repeat(",?", len(states)-1) + `)`
		for _, st := range states {
			args = append(args, st)
		}
	}
	rows, err := s.db.Query(q+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []File
	for rows.Next() {
		var f File
		var started, finished sql.NullTime
		if err := rows.Scan(&f.ID, &f.JobID, &f.Source, &f.Input, &f.OriginalName, &f.Size, &f.Dest, &f.Overwrite,
			&f.State, &f.Error, &f.Attempts, &started, &finished, &f.Meta); err != nil {
			return nil, err
		}
		if started.Valid {
			f.StartedAt = &started.Time
		}
		if finished.Valid {
			f.FinishedAt = &finished.Time
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) Jobs(ids []int64) (map[int64]Job, error) {
	out := map[int64]Job{}
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.db.Query(`SELECT id, kind, title, ref, created_at FROM jobs WHERE id IN (?`+strings.Repeat(",?", len(ids)-1)+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var j Job
		if err := rows.Scan(&j.ID, &j.Kind, &j.Title, &j.Ref, &j.CreatedAt); err != nil {
			return nil, err
		}
		out[j.ID] = j
	}
	return out, rows.Err()
}

func (s *Store) SetState(id int64, st State, errMsg string) error {
	now := time.Now().UTC()
	var q string
	switch st {
	case StateRunning:
		q = `UPDATE files SET state=?, error=?, started_at=? WHERE id=?`
	case StateDone, StateError:
		q = `UPDATE files SET state=?, error=?, finished_at=? WHERE id=?`
	default: // pending : le fichier repart de zéro côté fin
		q = `UPDATE files SET state=?, error=?, finished_at=NULL, started_at=? WHERE id=?`
	}
	arg := any(now)
	if st == StatePending {
		arg = nil
	}
	return s.exec(q, st, errMsg, arg, id)
}

// SetMeta remplace le JSON propre au fichier (bilan d'un pack).
func (s *Store) SetMeta(id int64, meta string) error {
	return s.exec(`UPDATE files SET meta=? WHERE id=?`, meta, id)
}

func (s *Store) IncAttempts(id int64) error {
	return s.exec(`UPDATE files SET attempts=attempts+1 WHERE id=?`, id)
}

// Replace remet le fichier en attente avec un nouveau lien, son nom, sa destination et sa taille.
func (s *Store) Replace(id int64, input, name, dest string, size int64) error {
	return s.exec(`UPDATE files SET input=?, original_name=?, dest=?, size=?, state=?, error='', attempts=0, started_at=NULL, finished_at=NULL WHERE id=?`,
		input, name, dest, size, StatePending, id)
}

// Delete supprime le fichier, puis son job s'il n'en a plus.
func (s *Store) Delete(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var jobID int64
	if err := tx.QueryRow(`SELECT job_id FROM files WHERE id=?`, id).Scan(&jobID); err != nil {
		return fmt.Errorf("fichier %d : %w", id, err)
	}
	if _, err := tx.Exec(`DELETE FROM files WHERE id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM jobs WHERE id=? AND NOT EXISTS (SELECT 1 FROM files WHERE job_id=?)`, jobID, jobID); err != nil {
		return err
	}
	return tx.Commit()
}

// DestTaken dit si un fichier encore dans la file (ni rangé ni en erreur) vise dest.
func (s *Store) DestTaken(dest string) (bool, error) { return s.inQueue("dest", dest) }

// InputTaken dit si un fichier encore dans la file (ni rangé ni en erreur) a cette entrée.
func (s *Store) InputTaken(input string) (bool, error) { return s.inQueue("input", input) }

// inQueue compte les fichiers non terminés dont la colonne col (constante) vaut v.
func (s *Store) inQueue(col, v string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM files WHERE `+col+`=? AND state NOT IN (?,?)`, v, StateDone, StateError).Scan(&n)
	return n > 0, err
}

func (s *Store) ResetRunning() error {
	return s.exec(`UPDATE files SET state=? WHERE state=?`, StatePending, StateRunning)
}

func (s *Store) exec(q string, args ...any) error {
	_, err := s.db.Exec(q, args...)
	return err
}
