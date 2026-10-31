package queue

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mgarnier-home/downloader/backend/naming"
	"github.com/mgarnier-home/downloader/backend/source"
	"github.com/mgarnier-home/downloader/backend/store"
)

var content = bytes.Repeat([]byte("0123456789abcdef"), 1000) // 16 000 octets

type env struct {
	t      *testing.T
	st     *store.Store
	m      *Manager
	mu     sync.Mutex
	delays []time.Duration
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	e := &env{t: t, st: st}
	e.m = &Manager{
		Store: st, Sources: []source.Source{&source.HTTP{}}, MediaDir: t.TempDir(),
		VideoLimit: 3, MusicLimit: 5,
		Delays: []time.Duration{10 * time.Second, 30 * time.Second, 90 * time.Second},
		Sleep: func(ctx context.Context, d time.Duration) error {
			e.mu.Lock()
			e.delays = append(e.delays, d)
			e.mu.Unlock()
			return ctx.Err()
		},
	}
	return e
}

// run lance la boucle ; l'arrêt attend la fin de Run.
func (e *env) run() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.m.Run(ctx); close(done) }()
	e.t.Cleanup(func() { cancel(); <-done })
}

func (e *env) add(kind naming.Kind, input, dest string, overwrite bool) store.File {
	e.t.Helper()
	_, files, err := e.st.CreateJob(store.Job{Kind: kind, Title: "A"},
		[]store.File{{Source: "http", Input: input, OriginalName: "a.mkv", Size: int64(len(content)), Dest: dest, Overwrite: overwrite}})
	if err != nil {
		e.t.Fatal(err)
	}
	e.m.Wake()
	return files[0]
}

func (e *env) file(id int64) store.File {
	e.t.Helper()
	files, err := e.st.Files()
	if err != nil {
		e.t.Fatal(err)
	}
	for _, f := range files {
		if f.ID == id {
			return f
		}
	}
	e.t.Fatalf("fichier %d absent", id)
	return store.File{}
}

// eventually attend que cond soit vraie, avec une limite de 10 s.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("délai dépassé : %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (e *env) waitState(id int64, want store.State) store.File {
	e.t.Helper()
	var f store.File
	eventually(e.t, "état "+string(want), func() bool { f = e.file(id); return f.State == want })
	return f
}

func (e *env) dest(rel string) string { return filepath.Join(e.m.MediaDir, rel) }

func (e *env) part(id int64) string { return e.m.partPath(id) }

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func serveContent(w http.ResponseWriter, r *http.Request) {
	http.ServeContent(w, r, "a.mkv", time.Time{}, bytes.NewReader(content))
}

func TestDownloadAndMove(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(serveContent))
	t.Cleanup(srv.Close)
	e := newEnv(t)
	e.run()
	f := e.add(naming.Movie, srv.URL+"/a.mkv", "films/A/A.mkv", false)
	e.waitState(f.ID, store.StateDone)
	if !bytes.Equal(readFile(t, e.dest("films/A/A.mkv")), content) {
		t.Fatal("contenu final différent de la source")
	}
	if _, err := os.Stat(e.part(f.ID)); !os.IsNotExist(err) {
		t.Fatalf(".part toujours présent : %v", err)
	}
	eventually(t, "progression vidée", func() bool { return len(e.m.Progress()) == 0 })
}

// blockingServer envoie la moitié du contenu puis attend la fin de la requête.
func blockingServer(t *testing.T, started chan<- string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "16000")
		_, _ = w.Write(content[:8000])
		w.(http.Flusher).Flush()
		started <- r.URL.Path
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (e *env) running() (video, music int) {
	files, err := e.st.Files(store.StateRunning)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasPrefix(f.Dest, "musics/") {
			music++
		} else {
			video++
		}
	}
	return video, music
}

func TestLimits(t *testing.T) {
	started := make(chan string, 20)
	srv := blockingServer(t, started)
	e := newEnv(t)
	kinds := []naming.Kind{naming.Series, naming.Movie, naming.Anime, naming.Movie, naming.Series}
	for i, k := range kinds {
		e.add(k, srv.URL+"/v", "films/V/"+string(rune('a'+i))+".mkv", false)
	}
	for i := range 6 {
		e.add(naming.Music, srv.URL+"/m", "musics/X/"+string(rune('a'+i))+".mp3", false)
	}
	e.run()
	for range 8 {
		<-started
	}
	e.m.schedule(context.Background()) // un passage de plus ne doit rien démarrer
	if v, m := e.running(); v != 3 || m != 5 {
		t.Fatalf("en cours : %d vidéos, %d morceaux ; attendu 3 et 5", v, m)
	}
	if n := len(e.m.Progress()); n != 8 {
		t.Fatalf("%d transferts suivis, attendu 8", n)
	}
}

func writePart(t *testing.T, e *env, id int64, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(e.part(id)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.part(id), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResumeWithRange(t *testing.T) {
	var ranges []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ranges = append(ranges, r.Header.Get("Range"))
		mu.Unlock()
		serveContent(w, r)
	}))
	t.Cleanup(srv.Close)
	e := newEnv(t)
	f := e.add(naming.Movie, srv.URL+"/a.mkv", "films/A/A.mkv", false)
	writePart(t, e, f.ID, content[:100])
	e.run()
	e.waitState(f.ID, store.StateDone)
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(ranges, []string{"bytes=100-"}) {
		t.Fatalf("en-têtes Range : %q", ranges)
	}
	if !bytes.Equal(readFile(t, e.dest("films/A/A.mkv")), content) {
		t.Fatal("contenu final différent de la source")
	}
}

func TestResumeRangeIgnored(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content) // 200 complet, Range ignoré
	}))
	t.Cleanup(srv.Close)
	e := newEnv(t)
	f := e.add(naming.Movie, srv.URL+"/a.mkv", "films/A/A.mkv", false)
	writePart(t, e, f.ID, content[:100])
	e.run()
	e.waitState(f.ID, store.StateDone)
	if !bytes.Equal(readFile(t, e.dest("films/A/A.mkv")), content) {
		t.Fatal("contenu final différent de la source (octets en double ?)")
	}
}

func failingServer(t *testing.T, failures int32) *httptest.Server {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) <= failures {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		serveContent(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRetryDelays(t *testing.T) {
	e := newEnv(t)
	e.run()
	f := e.add(naming.Movie, failingServer(t, 2).URL+"/a.mkv", "films/A/A.mkv", false)
	if got := e.waitState(f.ID, store.StateDone); got.Attempts != 2 {
		t.Fatalf("tentatives : %d, attendu 2", got.Attempts)
	}
	e.mu.Lock()
	if !slices.Equal(e.delays, []time.Duration{10 * time.Second, 30 * time.Second}) {
		t.Fatalf("délais : %v", e.delays)
	}
	e.delays = nil
	e.mu.Unlock()

	f = e.add(naming.Movie, failingServer(t, 4).URL+"/b.mkv", "films/B/B.mkv", false)
	if got := e.waitState(f.ID, store.StateError); got.Error != "réponse HTTP 503" {
		t.Fatalf("message : %q", got.Error)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !slices.Equal(e.delays, []time.Duration{10 * time.Second, 30 * time.Second, 90 * time.Second}) {
		t.Fatalf("délais : %v", e.delays)
	}
}

func TestPermanentErrorNoRetry(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	e := newEnv(t)
	e.run()
	f := e.add(naming.Movie, srv.URL+"/a.mkv", "films/A/A.mkv", false)
	if got := e.waitState(f.ID, store.StateError); got.Error != "réponse HTTP 404" {
		t.Fatalf("message : %q", got.Error)
	}
	_, files, err := e.st.CreateJob(store.Job{Kind: naming.Movie}, []store.File{{Source: "inconnue", Input: "x", Dest: "films/C/C.mkv"}})
	if err != nil {
		t.Fatal(err)
	}
	e.m.Wake()
	if got := e.waitState(files[0].ID, store.StateError); got.Error != "source inconnue : inconnue" {
		t.Fatalf("message : %q", got.Error)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.delays) != 0 {
		t.Fatalf("nouvelles tentatives inattendues : %v", e.delays)
	}
}

func TestMoveFailureKeepsPart(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		serveContent(w, r)
	}))
	t.Cleanup(srv.Close)
	e := newEnv(t)
	var fail atomic.Bool
	fail.Store(true)
	var finished []string
	e.m.Finish = func(_ context.Context, _ store.File, path string, _ func(int64, int64)) error {
		if fail.Load() {
			return errors.New("tags illisibles")
		}
		finished = append(finished, path)
		return nil
	}
	e.run()
	f := e.add(naming.Music, srv.URL+"/a.mp3", "musics/A/a.mp3", false)
	if got := e.waitState(f.ID, store.StateError); !strings.Contains(got.Error, "tags illisibles") {
		t.Fatalf("message : %q", got.Error)
	}
	if !bytes.Equal(readFile(t, e.part(f.ID)), content) {
		t.Fatal(".part absent ou incomplet")
	}

	// Réessayer : le .part est complet, seule la fin est refaite.
	fail.Store(false)
	if err := e.st.SetState(f.ID, store.StatePending, ""); err != nil {
		t.Fatal(err)
	}
	e.m.Wake()
	e.waitState(f.ID, store.StateDone)
	if n := requests.Load(); n != 1 {
		t.Fatalf("%d requêtes, attendu 1", n)
	}
	if !slices.Equal(finished, []string{e.part(f.ID)}) {
		t.Fatalf("Finish appelé avec %q", finished)
	}
	if !bytes.Equal(readFile(t, e.dest("musics/A/a.mp3")), content) {
		t.Fatal("contenu final différent de la source")
	}
}

func TestDestinationExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(serveContent))
	t.Cleanup(srv.Close)
	e := newEnv(t)
	dest := e.dest("films/A/A.mkv")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("ancien"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.run()
	f := e.add(naming.Movie, srv.URL+"/a.mkv", "films/A/A.mkv", false)
	if got := e.waitState(f.ID, store.StateError); got.Error != "Destination déjà présente" {
		t.Fatalf("message : %q", got.Error)
	}
	if string(readFile(t, dest)) != "ancien" {
		t.Fatal("fichier existant modifié")
	}
	f = e.add(naming.Movie, srv.URL+"/a.mkv", "films/A/A.mkv", true)
	e.waitState(f.ID, store.StateDone)
	if !bytes.Equal(readFile(t, dest), content) {
		t.Fatal("fichier existant non remplacé")
	}
}

func TestPauseResume(t *testing.T) {
	started := make(chan string, 1)
	var block atomic.Bool
	block.Store(true)
	var lastRange atomic.Value
	blocking := blockingServer(t, started).Config.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastRange.Store(r.Header.Get("Range"))
		if block.Load() {
			blocking.ServeHTTP(w, r)
			return
		}
		serveContent(w, r)
	}))
	t.Cleanup(srv.Close)
	e := newEnv(t)
	e.run()
	f := e.add(naming.Movie, srv.URL+"/a.mkv", "films/A/A.mkv", false)
	<-started
	eventually(t, "moitié reçue", func() bool { return e.m.Progress()[f.ID].Bytes == 8000 })

	block.Store(false)
	e.m.Pause()
	if !e.m.Paused() {
		t.Fatal("Paused() faux après Pause")
	}
	e.waitState(f.ID, store.StatePending)
	if b := readFile(t, e.part(f.ID)); len(b) != 8000 {
		t.Fatalf(".part de %d octets, attendu 8000", len(b))
	}
	e.m.schedule(context.Background()) // en pause : rien ne démarre
	if got := e.file(f.ID); got.State != store.StatePending {
		t.Fatalf("état %s pendant la pause", got.State)
	}

	e.m.Resume()
	e.waitState(f.ID, store.StateDone)
	if r := lastRange.Load(); r != "bytes=8000-" {
		t.Fatalf("Range à la reprise : %v", r)
	}
	if !bytes.Equal(readFile(t, e.dest("films/A/A.mkv")), content) {
		t.Fatal("contenu final différent de la source")
	}
}

func TestCancel(t *testing.T) {
	started := make(chan string, 1)
	srv := blockingServer(t, started)
	e := newEnv(t)
	e.run()
	f := e.add(naming.Movie, srv.URL+"/a.mkv", "films/A/A.mkv", false)
	<-started
	e.m.Cancel(f.ID)
	if _, ok := e.m.Progress()[f.ID]; ok {
		t.Fatal("transfert toujours suivi après Cancel")
	}
	if _, err := os.Stat(e.part(f.ID)); !os.IsNotExist(err) {
		t.Fatalf(".part toujours présent : %v", err)
	}
	if got := e.file(f.ID); got.State == store.StateError || got.State == store.StatePending {
		t.Fatalf("état %s après Cancel", got.State)
	}
}

func TestRestartResetsRunning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(serveContent))
	t.Cleanup(srv.Close)
	e := newEnv(t)
	f := e.add(naming.Movie, srv.URL+"/a.mkv", "films/A/A.mkv", false)
	if err := e.st.SetState(f.ID, store.StateRunning, ""); err != nil {
		t.Fatal(err)
	}
	e.run()
	e.waitState(f.ID, store.StateDone)
}

func TestSpeed(t *testing.T) {
	t0 := time.Now()
	tr := &transfer{}
	tr.reset(0, 100, t0)
	tr.add(1000, t0.Add(time.Second))
	tr.add(1000, t0.Add(2*time.Second))
	if s := tr.speed(t0.Add(2 * time.Second)); s != 1000 {
		t.Fatalf("débit %v, attendu 1000", s)
	}
	if s := tr.speed(t0.Add(20 * time.Second)); s != 0 {
		t.Fatalf("débit %v après 18 s sans données, attendu 0", s)
	}
}

func TestDiskErrorNotRetried(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(serveContent))
	t.Cleanup(srv.Close)
	e := newEnv(t)
	// download/ est un fichier ordinaire : la création du .part échoue (erreur disque).
	if err := os.WriteFile(filepath.Join(e.m.MediaDir, "download"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	e.run()
	f := e.add(naming.Movie, srv.URL+"/a.mkv", "films/A/A.mkv", false)
	if got := e.waitState(f.ID, store.StateError); got.Attempts != 0 {
		t.Fatalf("tentatives : %d, attendu 0", got.Attempts)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.delays) != 0 {
		t.Fatalf("nouvelles tentatives sur erreur disque : %v", e.delays)
	}
}

func TestSpeedSamplesBounded(t *testing.T) {
	t0 := time.Now()
	tr := &transfer{}
	tr.reset(0, 0, t0)
	for i := range 10000 { // 1000 s, un point toutes les 100 ms, sans lecture du débit
		tr.add(10, t0.Add(time.Duration(i)*100*time.Millisecond))
	}
	if n := len(tr.samples); n > 60 {
		t.Fatalf("%d points gardés, attendu au plus 60 (fenêtre de 5 s)", n)
	}
}

func (e *env) addPack(input string) store.File {
	e.t.Helper()
	_, files, err := e.st.CreateJob(store.Job{Kind: naming.Series, Title: "Serie"},
		[]store.File{{Source: "http", Input: input, OriginalName: "Serie.S01.rar", Size: int64(len(content)), Dest: "series/Serie"}})
	if err != nil {
		e.t.Fatal(err)
	}
	e.m.Wake()
	return files[0]
}

func TestPackDelegated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(serveContent))
	t.Cleanup(srv.Close)
	e := newEnv(t)
	var seen Progress
	var gotPart string
	e.m.Finish = func(_ context.Context, f store.File, part string, progress func(int64, int64)) error {
		gotPart = part
		progress(5, 10)
		seen = e.m.Progress()[f.ID]
		return nil
	}
	e.run()
	f := e.addPack(srv.URL + "/p.rar")
	e.waitState(f.ID, store.StateDone)
	if seen != (Progress{Bytes: 5, Total: 10, Phase: "extraction"}) {
		t.Fatalf("progression pendant l'extraction : %+v", seen)
	}
	if gotPart != e.part(f.ID) {
		t.Fatalf("Finish appelé avec %q", gotPart)
	}
	if _, err := os.Stat(e.part(f.ID)); !os.IsNotExist(err) {
		t.Fatalf(".part toujours présent : %v", err)
	}
	if _, err := os.Stat(e.dest("series/Serie")); !os.IsNotExist(err) {
		t.Fatalf("le .part a été déplacé vers le dossier : %v", err)
	}
}

func TestPackErrorKeepsPart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(serveContent))
	t.Cleanup(srv.Close)
	e := newEnv(t)
	e.m.Finish = func(context.Context, store.File, string, func(int64, int64)) error {
		return errors.New("Aucune vidéo dans l'archive") //nolint:staticcheck // message affiché tel quel
	}
	e.run()
	f := e.addPack(srv.URL + "/p.rar")
	if got := e.waitState(f.ID, store.StateError); got.Error != "Aucune vidéo dans l'archive" {
		t.Fatalf("message : %q", got.Error)
	}
	if !bytes.Equal(readFile(t, e.part(f.ID)), content) {
		t.Fatal(".part absent ou incomplet")
	}
}

func TestPackExistingFolder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(serveContent))
	t.Cleanup(srv.Close)
	e := newEnv(t)
	if err := os.MkdirAll(e.dest("series/Serie"), 0o755); err != nil {
		t.Fatal(err)
	}
	var called atomic.Bool
	e.m.Finish = func(context.Context, store.File, string, func(int64, int64)) error { called.Store(true); return nil }
	e.run()
	f := e.addPack(srv.URL + "/p.rar")
	e.waitState(f.ID, store.StateDone)
	if !called.Load() {
		t.Fatal("Finish non appelé")
	}
}
