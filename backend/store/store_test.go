package store

import (
	"path/filepath"
	"testing"

	"github.com/mgarnier-home/downloader/backend/naming"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newJob(t *testing.T, s *Store, dests ...string) []File {
	t.Helper()
	var files []File
	for _, d := range dests {
		files = append(files, File{Source: "alldebrid", Input: "http://x/" + d, Dest: d, State: StatePending})
	}
	_, out, err := s.CreateJob(Job{Kind: naming.Movie, Title: "T", Ref: "r"}, files)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSchemaCreatedOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	for range 2 {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var v int
		var mode string
		if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
			t.Fatal(err)
		}
		if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
			t.Fatal(err)
		}
		if v != 1 || mode != "delete" {
			t.Fatalf("user_version=%d journal_mode=%s", v, mode)
		}
		_ = s.Close()
	}
}

func TestJobLifecycle(t *testing.T) {
	s := openTemp(t)
	files := newJob(t, s, "a", "b")
	if len(files) != 2 || files[0].ID == 0 || files[0].JobID == 0 {
		t.Fatalf("fichiers: %+v", files)
	}
	got, err := s.Files(StatePending)
	if err != nil || len(got) != 2 || got[0].ID > got[1].ID {
		t.Fatalf("pending: %v %v", got, err)
	}
	id := files[0].ID
	if err := s.SetState(id, StateRunning, ""); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Files(StateRunning)
	if len(got) != 1 || got[0].StartedAt == nil {
		t.Fatalf("running: %+v", got)
	}
	if err := s.SetState(id, StateDone, ""); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Files(StateDone, StateError)
	if len(got) != 1 || got[0].FinishedAt == nil || got[0].ID != id {
		t.Fatalf("done: %+v", got)
	}
	jobs, err := s.Jobs([]int64{got[0].JobID})
	if err != nil || jobs[got[0].JobID].Title != "T" || jobs[got[0].JobID].CreatedAt.IsZero() {
		t.Fatalf("jobs: %+v %v", jobs, err)
	}
}

func TestReplaceAndAttempts(t *testing.T) {
	s := openTemp(t)
	id := newJob(t, s, "a")[0].ID
	_ = s.IncAttempts(id)
	_ = s.SetState(id, StateError, "boom")
	if err := s.Replace(id, "http://new", "new.mp4", "films/X/new.mp4", 20); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Files(StatePending)
	if len(got) != 1 || got[0].Input != "http://new" || got[0].Attempts != 0 || got[0].Error != "" || got[0].FinishedAt != nil ||
		got[0].OriginalName != "new.mp4" || got[0].Dest != "films/X/new.mp4" || got[0].Size != 20 {
		t.Fatalf("replace: %+v", got)
	}
}

func TestResetRunning(t *testing.T) {
	s := openTemp(t)
	id := newJob(t, s, "a")[0].ID
	_ = s.SetState(id, StateRunning, "")
	if err := s.ResetRunning(); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Files(StatePending); len(got) != 1 {
		t.Fatalf("pending: %+v", got)
	}
}

func TestDestTaken(t *testing.T) {
	s := openTemp(t)
	id := newJob(t, s, "a")[0].ID
	if ok, _ := s.DestTaken("a"); !ok {
		t.Fatal("attendu pris")
	}
	_ = s.SetState(id, StateError, "x")
	if ok, _ := s.DestTaken("a"); ok {
		t.Fatal("attendu libre")
	}
}

func TestDeleteLastFileDeletesJob(t *testing.T) {
	s := openTemp(t)
	files := newJob(t, s, "a", "b")
	jid := files[0].JobID
	_ = s.Delete(files[0].ID)
	if m, _ := s.Jobs([]int64{jid}); len(m) != 1 {
		t.Fatal("le job doit rester")
	}
	_ = s.Delete(files[1].ID)
	if m, _ := s.Jobs([]int64{jid}); len(m) != 0 {
		t.Fatal("le job doit disparaître")
	}
}

func TestInputTaken(t *testing.T) {
	s := openTemp(t)
	id := newJob(t, s, "a")[0].ID
	if ok, err := s.InputTaken("http://x/a"); err != nil || !ok {
		t.Fatalf("attendu pris : %v %v", ok, err)
	}
	if ok, _ := s.InputTaken("http://x/b"); ok {
		t.Fatal("lien inconnu attendu libre")
	}
	for _, st := range []State{StateDone, StateError} {
		_ = s.SetState(id, st, "")
		if ok, _ := s.InputTaken("http://x/a"); ok {
			t.Fatalf("attendu libre en %s", st)
		}
		if ok, _ := s.DestTaken("a"); ok {
			t.Fatalf("destination attendue libre en %s", st)
		}
	}
}
