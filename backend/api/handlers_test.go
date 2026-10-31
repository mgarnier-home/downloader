package api

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mgarnier-home/downloader/backend/deezer"
	"github.com/mgarnier-home/downloader/backend/naming"
	"github.com/mgarnier-home/downloader/backend/queue"
	"github.com/mgarnier-home/downloader/backend/source"
	"github.com/mgarnier-home/downloader/backend/store"
	"github.com/mgarnier-home/downloader/backend/tmdb"
)

// fakeSource résout tout lien en un fichier nommé d'après la fin de l'URL.
type fakeSource struct{}

func (fakeSource) Name() string         { return "fake" }
func (fakeSource) Match(in string) bool { return strings.HasPrefix(in, "https://") }
func (fakeSource) Reopen(context.Context, string, string) (source.Item, error) {
	return source.Item{}, errors.New("inutile en test")
}
func (fakeSource) Resolve(_ context.Context, in string) ([]source.Item, error) {
	if strings.Contains(in, "offline") {
		return []source.Item{{Name: path.Base(in), Error: "hôte : fichier supprimé"}}, nil
	}
	size := int64(10)
	if strings.Contains(in, "big") {
		size = 20
	}
	return []source.Item{{Name: path.Base(in), Size: size, Online: true}}, nil
}

type env struct {
	t         *testing.T
	h         *Handlers
	mux       *http.ServeMux
	tmdbCalls atomic.Int32
	tmdbBody  map[string]string // chemin TMDB → corps JSON
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{t: t, tmdbBody: map[string]string{}}
	tm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.tmdbCalls.Add(1)
		w.Write([]byte(cmp.Or(e.tmdbBody[r.URL.Path], `{"results":[]}`))) //nolint:errcheck
	}))
	t.Cleanup(tm.Close)
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	media := t.TempDir()
	ix, err := naming.BuildIndex(media)
	if err != nil {
		t.Fatal(err)
	}
	e.h = &Handlers{
		Store: st, Index: ix, MediaDir: media, Sources: []source.Source{fakeSource{}},
		TMDB:   &tmdb.Client{BaseURL: tm.URL, Key: "k"},
		Deezer: &deezer.Client{}, AllDebrid: &source.AllDebrid{},
		Queue: &queue.Manager{Store: st, MediaDir: media},
	}
	e.mux = http.NewServeMux()
	e.h.Register(e.mux)
	return e
}

func (e *env) do(method, target, body string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("corps illisible (%d) : %s", rec.Code, rec.Body)
	}
	return v
}

func (e *env) analyze(links ...string) []AnalyzeRow {
	e.t.Helper()
	body, _ := json.Marshal(AnalyzeRequest{Links: links})
	rec := e.do(http.MethodPost, "/api/videos/analyze", string(body))
	if rec.Code != http.StatusOK {
		e.t.Fatalf("analyse : %d %s", rec.Code, rec.Body)
	}
	return decode[[]AnalyzeRow](e.t, rec)
}

const theBoys = `{"results":[{"id":76479,"name":"The Boys","first_air_date":"2019-07-25"}]}`

func TestAnalyzeSeries(t *testing.T) {
	e := newEnv(t)
	e.tmdbBody["/search/tv"] = theBoys
	rows := e.analyze("https://h/The.Boys.S02E01.MULTi.1080p.mkv", "https://h/The.Boys.S02E02.MULTi.1080p.mkv")
	if len(rows) != 2 {
		t.Fatalf("%d lignes", len(rows))
	}
	for _, r := range rows {
		if r.Kind != naming.Series || r.TMDB == nil || r.TMDB.ID != 76479 || r.NeedsReview {
			t.Errorf("ligne : %+v", r)
		}
	}
	if rows[0].Dest != "series/TheBoys/S02/TheBoys.S02.E01.mkv" || rows[1].Dest != "series/TheBoys/S02/TheBoys.S02.E02.mkv" {
		t.Errorf("destinations : %q, %q", rows[0].Dest, rows[1].Dest)
	}
	if n := e.tmdbCalls.Load(); n != 1 {
		t.Errorf("TMDB appelé %d fois", n)
	}
}

func TestAnalyzeAnime(t *testing.T) {
	e := newEnv(t)
	e.tmdbBody["/search/movie"] = `{"results":[{"id":5,"title":"Spirit : l'indomptable","release_date":"2021-06-04","genre_ids":[16]}]}`
	r := e.analyze("https://h/Spirit.Untamed.2021.MULTi.1080p.mkv")[0]
	if r.Kind != naming.Anime || r.Dest != "animes/SpiritLIndomptable/SpiritLIndomptable.mkv" {
		t.Errorf("ligne : %+v", r)
	}
}

func TestAnalyzeReusesFolder(t *testing.T) {
	e := newEnv(t)
	e.tmdbBody["/search/tv"] = theBoys
	e.h.Index.Add(naming.Series, 76479, "the boys")
	if r := e.analyze("https://h/The.Boys.S02E01.mkv")[0]; r.Dest != "series/the boys/S02/the boys.S02.E01.mkv" {
		t.Errorf("destination : %q", r.Dest)
	}
}

func TestAnalyzeNoTMDB(t *testing.T) {
	e := newEnv(t)
	r := e.analyze("https://h/Mon.Film.2020.mkv")[0]
	if !r.NeedsReview || r.Error != "TMDB : aucune fiche" || r.Dest != "films/MonFilm/MonFilm.mkv" || r.TMDB != nil {
		t.Errorf("ligne : %+v", r)
	}
}

func TestAnalyzeExists(t *testing.T) {
	e := newEnv(t)
	p := filepath.Join(e.h.MediaDir, "films/MonFilm/MonFilm.mkv")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if r := e.analyze("https://h/Mon.Film.2020.mkv")[0]; !r.Exists {
		t.Errorf("ligne : %+v", r)
	}
}

func TestAnalyzeOfflineAndSeasonOnly(t *testing.T) {
	e := newEnv(t)
	e.tmdbBody["/search/tv"] = theBoys
	rows := e.analyze("https://h/offline/The.Boys.S02E01.mkv", "https://h/The.Boys.S02.Bonus.mkv", "ftp://x")
	if r := rows[0]; r.Online || r.Error == "" || r.Dest != "" {
		t.Errorf("hors ligne : %+v", r)
	}
	if r := rows[1]; r.Kind != naming.Series || !r.NeedsReview || r.Dest != "" || r.Season != 2 {
		t.Errorf("saison sans épisode : %+v", r)
	}
	if r := rows[2]; r.Online || r.Error != "Lien non reconnu" {
		t.Errorf("lien inconnu : %+v", r)
	}
}

func queueBody(dest string) string {
	b, _ := json.Marshal(QueueRequest{Kind: naming.Movie, Title: "Mon film", Ref: "tmdb:1",
		Files: []QueueFile{{Input: "https://h/a.mkv", Name: "a.mkv", Size: 10, Dest: dest}}})
	return string(b)
}

func (e *env) files() []store.File {
	e.t.Helper()
	f, err := e.h.Store.Files()
	if err != nil {
		e.t.Fatal(err)
	}
	return f
}

func TestQueueRejectsEscape(t *testing.T) {
	e := newEnv(t)
	for _, d := range []string{"../x.mkv", "/etc/x", "films/../../x"} {
		rec := e.do(http.MethodPost, "/api/queue", queueBody(d))
		if rec.Code != http.StatusBadRequest || decode[ErrorBody](t, rec).Error != "Destination invalide : "+d {
			t.Errorf("%s : %d %s", d, rec.Code, rec.Body)
		}
	}
	if n := len(e.files()); n != 0 {
		t.Errorf("%d fichiers en base", n)
	}
}

func TestQueueDuplicate(t *testing.T) {
	e := newEnv(t)
	body := queueBody("films/A/A.mkv")
	if rec := e.do(http.MethodPost, "/api/queue", body); rec.Code != http.StatusCreated {
		t.Fatalf("premier ajout : %d %s", rec.Code, rec.Body)
	}
	rec := e.do(http.MethodPost, "/api/queue", body)
	if rec.Code != http.StatusConflict || decode[ErrorBody](t, rec).Error != "Déjà dans la file : films/A/A.mkv" {
		t.Errorf("second ajout : %d %s", rec.Code, rec.Body)
	}
	// Même lien vers une autre destination : refusé aussi.
	if rec := e.do(http.MethodPost, "/api/queue", queueBody("films/B/B.mkv")); rec.Code != http.StatusConflict {
		t.Errorf("même lien : %d %s", rec.Code, rec.Body)
	}
	if n := len(e.files()); n != 1 {
		t.Errorf("%d fichiers en base", n)
	}
}

func TestCrossOriginRejected(t *testing.T) {
	e := newEnv(t)
	if rec := e.do(http.MethodPost, "/api/queue", queueBody("films/A/A.mkv"), "Sec-Fetch-Site", "cross-site"); rec.Code != http.StatusForbidden {
		t.Errorf("code %d", rec.Code)
	}
	if n := len(e.files()); n != 0 {
		t.Errorf("%d fichiers en base", n)
	}
}

func TestStatusHidesSecrets(t *testing.T) {
	e := newEnv(t)
	// Un serveur fermé : les deux vérifications échouent sur une erreur réseau.
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	e.h.AllDebrid = &source.AllDebrid{BaseURL: srv.URL, Key: "cle-alldebrid-secrete"}
	e.h.Deezer = &deezer.Client{APIURL: srv.URL, GatewayURL: srv.URL, MediaURL: srv.URL, ARL: "arl-deezer-secret"}
	rec := e.do(http.MethodGet, "/api/status", "")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || strings.Contains(body, "cle-alldebrid-secrete") || strings.Contains(body, "arl-deezer-secret") {
		t.Fatalf("%d %s", rec.Code, body)
	}
	s := decode[Status](t, rec)
	if s.AllDebrid.OK || s.AllDebrid.Error == "" || s.Deezer.OK || s.Deezer.Error == "" || s.FreeBytes <= 0 {
		t.Errorf("état : %+v", s)
	}
}

func (e *env) job(kind naming.Kind, states ...store.State) []store.File {
	e.t.Helper()
	var files []store.File
	for i, st := range states {
		files = append(files, store.File{Source: "fake", Input: "https://h/" + string(rune('a'+i)), OriginalName: "f", Size: 10,
			Dest: "films/X/" + string(kind) + string(rune('a'+i)) + ".mkv", State: st})
	}
	_, out, err := e.h.Store.CreateJob(store.Job{Kind: kind, Title: "T"}, files)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, f := range out {
		if f.State == store.StateDone || f.State == store.StateError {
			_ = e.h.Store.SetState(f.ID, f.State, "erreur "+string(f.State)) // pose finished_at
		}
	}
	return out
}

func TestQueueViewGroupsByJob(t *testing.T) {
	e := newEnv(t)
	e.job(naming.Movie, store.StateDone, store.StateRunning, store.StatePending)
	e.job(naming.Music, store.StateDone) // job terminé : absent de la vue
	v := decode[QueueView](t, e.do(http.MethodGet, "/api/queue", ""))
	if len(v.Jobs) != 1 || len(v.Jobs[0].Files) != 3 || v.Jobs[0].Kind != naming.Movie {
		t.Fatalf("vue : %+v", v)
	}
	if f := v.Jobs[0].Files[0]; f.State != store.StateDone || f.Bytes != 10 {
		t.Errorf("fichier rangé : %+v", f)
	}
}

func TestFileActionsNeedError(t *testing.T) {
	e := newEnv(t)
	files := e.job(naming.Movie, store.StatePending, store.StateError, store.StateError)
	for _, target := range []string{"/api/files/%d/retry", "/api/files/%d"} {
		method := http.MethodPost
		if !strings.HasSuffix(target, "retry") {
			method = http.MethodDelete
		}
		if rec := e.do(method, strings.Replace(target, "%d", itoa(files[0].ID), 1), ""); rec.Code != http.StatusConflict {
			t.Errorf("%s sur un fichier en attente : %d", target, rec.Code)
		}
	}
	if rec := e.do(http.MethodPost, "/api/files/"+itoa(files[1].ID)+"/retry", ""); rec.Code != http.StatusNoContent {
		t.Errorf("réessayer : %d %s", rec.Code, rec.Body)
	}
	if rec := e.do(http.MethodPost, "/api/files/"+itoa(files[2].ID)+"/replace", `{"input":"https://h/new"}`); rec.Code != http.StatusNoContent {
		t.Errorf("remplacer : %d %s", rec.Code, rec.Body)
	}
	got := e.files()
	if got[1].State != store.StatePending || got[2].State != store.StatePending || got[2].Input != "https://h/new" {
		t.Errorf("états : %+v", got)
	}
	_ = e.h.Store.SetState(files[2].ID, store.StateError, "x")
	if rec := e.do(http.MethodDelete, "/api/files/"+itoa(files[2].ID), ""); rec.Code != http.StatusNoContent {
		t.Errorf("retirer : %d %s", rec.Code, rec.Body)
	}
	if rec := e.do(http.MethodDelete, "/api/files/999", ""); rec.Code != http.StatusNotFound {
		t.Errorf("inconnu : %d", rec.Code)
	}
	if n := len(e.files()); n != 2 {
		t.Errorf("%d fichiers en base", n)
	}
}

func TestReplaceRereadsLink(t *testing.T) {
	e := newEnv(t)
	files := e.job(naming.Movie, store.StateError, store.StateError)
	replace := func(id int64, input string) *httptest.ResponseRecorder {
		return e.do(http.MethodPost, "/api/files/"+itoa(id)+"/replace", `{"input":"`+input+`"}`)
	}
	if rec := replace(files[0].ID, "https://h/big.mp4"); rec.Code != http.StatusNoContent {
		t.Fatalf("remplacer : %d %s", rec.Code, rec.Body)
	}
	got := e.files()
	if f := got[0]; f.Size != 20 || f.OriginalName != "big.mp4" || f.Dest != "films/X/moviea.mp4" || f.State != store.StatePending {
		t.Errorf("fichier remplacé : %+v", f)
	}
	rec := replace(files[1].ID, "https://h/offline.mkv")
	if rec.Code != http.StatusBadRequest || decode[ErrorBody](t, rec).Error != "hôte : fichier supprimé" {
		t.Errorf("lien hors ligne : %d %s", rec.Code, rec.Body)
	}
	if f := e.files()[1]; !reflect.DeepEqual(f, got[1]) {
		t.Errorf("fichier modifié malgré le refus : %+v", f)
	}
	// Même destination de départ que le 1er fichier : la nouvelle extension la fait viser moviea.mp4, en file.
	other := e.job(naming.Movie, store.StateError)[0]
	rec = replace(other.ID, "https://h/big2.mp4")
	if rec.Code != http.StatusConflict || decode[ErrorBody](t, rec).Error != "Déjà dans la file : films/X/moviea.mp4" {
		t.Errorf("destination prise : %d %s", rec.Code, rec.Body)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestHistoryFilter(t *testing.T) {
	e := newEnv(t)
	e.job(naming.Movie, store.StateDone, store.StateError, store.StatePending)
	e.job(naming.Music, store.StateDone)
	for filter, want := range map[string]int{"all": 3, "video": 2, "music": 1, "error": 1} {
		rows := decode[[]HistoryRow](t, e.do(http.MethodGet, "/api/history?filter="+filter, ""))
		if len(rows) != want {
			t.Errorf("%s : %d lignes, attendu %d", filter, len(rows), want)
		}
	}
	if rec := e.do(http.MethodGet, "/api/history?filter=x", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("filtre invalide : %d", rec.Code)
	}
}

func TestDeezerSearchPresent(t *testing.T) {
	e := newEnv(t)
	dz := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search/album" {
			w.Write([]byte(`{"data":[{"id":1,"title":"Fortitude","artist":{"name":"Gojira"}},{"id":2,"title":"Magma","artist":{"name":"Gojira"}}]}`)) //nolint:errcheck
			return
		}
		w.Write([]byte(`{"data":[]}`)) //nolint:errcheck
	}))
	t.Cleanup(dz.Close)
	e.h.Deezer = &deezer.Client{APIURL: dz.URL}
	if err := os.MkdirAll(filepath.Join(e.h.MediaDir, "musics/Gojira/Fortitude"), 0o755); err != nil {
		t.Fatal(err)
	}
	res := decode[DeezerSearch](t, e.do(http.MethodGet, "/api/deezer/search?q=gojira", ""))
	if len(res.Albums) != 2 || !res.Albums[0].Present || res.Albums[1].Present {
		t.Errorf("albums : %+v", res.Albums)
	}
}

func TestQueueCleansDest(t *testing.T) {
	e := newEnv(t)
	rec := e.do(http.MethodPost, "/api/queue", queueBody("download/1.part"))
	if rec.Code != http.StatusBadRequest || decode[ErrorBody](t, rec).Error != "Destination invalide : download/1.part" {
		t.Errorf("download : %d %s", rec.Code, rec.Body)
	}
	if rec := e.do(http.MethodPost, "/api/queue", queueBody("films/A/./A.mkv")); rec.Code != http.StatusCreated {
		t.Fatalf("ajout : %d %s", rec.Code, rec.Body)
	}
	if f := e.files(); len(f) != 1 || f[0].Dest != "films/A/A.mkv" {
		t.Fatalf("fichiers : %+v", f)
	}
	b, _ := json.Marshal(QueueRequest{Kind: naming.Movie, Files: []QueueFile{{Input: "https://h/autre.mkv", Dest: "films//A/A.mkv"}}})
	if rec := e.do(http.MethodPost, "/api/queue", string(b)); rec.Code != http.StatusConflict {
		t.Errorf("même destination normalisée : %d %s", rec.Code, rec.Body)
	}
}

// withDeezer place devant les sources une source Deezer servie par un faux serveur (album 1, titres 11 et 12).
func (e *env) withDeezer() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		track := `{"id":%s,"title":"T%s","track_position":%s,"disk_number":1,"artist":{"name":"Gojira"},"album":{"id":1}}`
		switch {
		case r.URL.Path == "/album/1":
			w.Write([]byte(`{"id":1,"title":"Fortitude","nb_disk":1,"nb_tracks":2,"artist":{"name":"Gojira"}}`)) //nolint:errcheck
		case r.URL.Path == "/album/1/tracks":
			w.Write([]byte(`{"data":[` + strings.ReplaceAll(track, "%s", "11") + `,` + strings.ReplaceAll(track, "%s", "12") + `]}`)) //nolint:errcheck
		case strings.HasPrefix(r.URL.Path, "/track/"):
			w.Write([]byte(strings.ReplaceAll(track, "%s", path.Base(r.URL.Path)))) //nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	}))
	e.t.Cleanup(srv.Close)
	e.h.Sources = append([]source.Source{&source.Deezer{Client: &deezer.Client{APIURL: srv.URL}}}, e.h.Sources...)
}

func TestQueueDeezer(t *testing.T) {
	e := newEnv(t)
	e.withDeezer()
	rec := e.do(http.MethodPost, "/api/queue", `{"deezer":"deezer:album:1"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("album : %d %s", rec.Code, rec.Body)
	}
	if v := decode[JobView](t, rec); v.Kind != naming.Music || v.Title != "Fortitude" || len(v.Files) != 2 ||
		v.Files[0].Dest != "musics/Gojira/Fortitude/Gojira - Fortitude - 11 - T11.mp3" {
		t.Errorf("job : %+v", v)
	}
	if f := e.files(); len(f) != 2 || f[0].Source != "deezer" || f[0].Input != "deezer:track:11" || f[0].Meta == "" {
		t.Errorf("fichiers : %+v", f)
	}
	if rec := e.do(http.MethodPost, "/api/queue", `{"deezer":"deezer:track:12"}`); rec.Code != http.StatusConflict {
		t.Errorf("titre déjà dans la file : %d %s", rec.Code, rec.Body)
	}
	if n := len(e.files()); n != 2 {
		t.Errorf("%d fichiers en base", n)
	}
}

func TestAnalyzeDeezerLink(t *testing.T) {
	e := newEnv(t)
	e.withDeezer()
	rows := e.analyze("https://www.deezer.com/fr/album/1")
	if len(rows) != 1 || rows[0].Online || rows[0].Error != "Lien Deezer : utiliser l'onglet Musique" {
		t.Errorf("lignes : %+v", rows)
	}
}

const breakingBad = `{"results":[{"id":1396,"name":"Breaking Bad","first_air_date":"2008-01-20"}]}`

func TestAnalyzePack(t *testing.T) {
	e := newEnv(t)
	e.tmdbBody["/search/tv"] = breakingBad
	r := e.analyze("https://h/Breaking.Bad.Integrale.MULTI.1080p.rar")[0]
	if !r.Pack || r.Kind != naming.Series || r.TMDB == nil || r.TMDB.ID != 1396 || r.Dest != "series/BreakingBad" || r.Exists || r.NeedsReview {
		t.Errorf("ligne : %+v", r)
	}
}

func TestAnalyzePackSeasonAndFolder(t *testing.T) {
	e := newEnv(t)
	e.tmdbBody["/search/tv"] = breakingBad
	e.h.Index.Add(naming.Series, 1396, "Breaking_Bad")
	r := e.analyze("https://h/Breaking.Bad.S03.rar")[0]
	if !r.Pack || r.Season != 3 || r.Dest != "series/Breaking_Bad" {
		t.Errorf("ligne : %+v", r)
	}
}

func packBody(kind naming.Kind, dests ...string) string {
	var files []QueueFile
	for i, d := range dests {
		files = append(files, QueueFile{Input: "https://h/p" + strconv.Itoa(i) + ".rar", Name: "Breaking.Bad.S0" + strconv.Itoa(i+1) + ".rar", Size: 10, Dest: d})
	}
	b, _ := json.Marshal(QueueRequest{Kind: kind, Title: "Breaking Bad", Ref: "tmdb:1396", Files: files})
	return string(b)
}

func TestQueueTwoPacksSameSeries(t *testing.T) {
	e := newEnv(t)
	if rec := e.do(http.MethodPost, "/api/queue", packBody(naming.Series, "series/BreakingBad", "series/BreakingBad")); rec.Code != http.StatusCreated {
		t.Fatalf("deux packs : %d %s", rec.Code, rec.Body)
	}
	b, _ := json.Marshal(QueueRequest{Kind: naming.Series, Title: "Breaking Bad", Ref: "tmdb:1396",
		Files: []QueueFile{{Input: "https://h/autre.rar", Name: "Breaking.Bad.S03.rar", Size: 10, Dest: "series/BreakingBad"}}})
	if rec := e.do(http.MethodPost, "/api/queue", string(b)); rec.Code != http.StatusCreated {
		t.Fatalf("troisième pack : %d %s", rec.Code, rec.Body)
	}
	for _, f := range e.files() {
		if f.Meta != `{"pack":{}}` {
			t.Errorf("meta de %s : %q", f.OriginalName, f.Meta)
		}
	}
	if rec := e.do(http.MethodPost, "/api/queue", string(b)); rec.Code != http.StatusConflict {
		t.Errorf("même lien : %d %s", rec.Code, rec.Body)
	}
}

func TestQueuePackBadDest(t *testing.T) {
	e := newEnv(t)
	for _, d := range []string{"series/A/B", "films/A", "series"} {
		rec := e.do(http.MethodPost, "/api/queue", packBody(naming.Series, d))
		if rec.Code != http.StatusBadRequest || decode[ErrorBody](t, rec).Error != "Destination invalide : "+d {
			t.Errorf("%s : %d %s", d, rec.Code, rec.Body)
		}
	}
	rec := e.do(http.MethodPost, "/api/queue", packBody(naming.Movie, "series/A"))
	if rec.Code != http.StatusBadRequest || !strings.HasPrefix(decode[ErrorBody](t, rec).Error, "Type invalide") {
		t.Errorf("film : %d %s", rec.Code, rec.Body)
	}
	if n := len(e.files()); n != 0 {
		t.Errorf("%d fichiers en base", n)
	}
}

func TestFileViewPhase(t *testing.T) {
	v := fileView(store.File{ID: 7, State: store.StateRunning}, map[int64]queue.Progress{7: {Bytes: 5, Total: 10, Phase: "extraction"}})
	if v.Phase != "extraction" || v.Bytes != 5 || v.Total != 10 {
		t.Errorf("vue : %+v", v)
	}
}

func TestHistoryPack(t *testing.T) {
	e := newEnv(t)
	_, files, err := e.h.Store.CreateJob(store.Job{Kind: naming.Series, Title: "Breaking Bad"}, []store.File{
		{Source: "fake", Input: "https://h/p.rar", OriginalName: "Breaking.Bad.rar", Dest: "series/BreakingBad", Meta: `{"pack":{"placed":62,"present":2,"bonus":1}}`},
		{Source: "fake", Input: "https://h/e.mkv", OriginalName: "Breaking.Bad.S01E01.mkv", Dest: "series/BreakingBad/S01/BreakingBad.S01.E01.mkv"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		_ = e.h.Store.SetState(f.ID, store.StateDone, "")
	}
	rows := decode[[]HistoryRow](t, e.do(http.MethodGet, "/api/history", ""))
	byName := map[string]HistoryRow{}
	for _, r := range rows {
		byName[r.Name] = r
	}
	if p := byName["Breaking.Bad.rar"].Pack; p == nil || *p != (store.PackReport{Placed: 62, Present: 2, Bonus: 1}) {
		t.Errorf("bilan du pack : %+v", p)
	}
	if p := byName["Breaking.Bad.S01E01.mkv"].Pack; p != nil {
		t.Errorf("bilan sur un épisode : %+v", p)
	}
}
