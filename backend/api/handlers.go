package api

import (
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mgarnier-home/utils/logger"

	"github.com/mgarnier-home/downloader/backend/deezer"
	"github.com/mgarnier-home/downloader/backend/naming"
	"github.com/mgarnier-home/downloader/backend/queue"
	"github.com/mgarnier-home/downloader/backend/source"
	"github.com/mgarnier-home/downloader/backend/store"
	"github.com/mgarnier-home/downloader/backend/tmdb"
)

const (
	statusTTL     = 10 * time.Minute
	statusTimeout = 30 * time.Second
	maxParallel   = 8 // résolutions et recherches TMDB simultanées par analyse
	maxBody       = 1 << 20
)

// Handlers sert l'API ; aucun champ de configuration secret n'y figure.
type Handlers struct {
	Store     *store.Store
	Index     *naming.Index
	MediaDir  string
	Sources   []source.Source
	TMDB      *tmdb.Client
	Deezer    *deezer.Client
	AllDebrid *source.AllDebrid
	Queue     *queue.Manager

	statusMu sync.Mutex
	status   Status
	statusAt time.Time
	enqueue  sync.Mutex // vérification des doublons et création atomiques
}

// Register ajoute les routes ; les écritures sont protégées contre les requêtes d'une autre origine.
func (h *Handlers) Register(mux *http.ServeMux) {
	cop := http.NewCrossOriginProtection()
	write := func(pattern string, f http.HandlerFunc) { mux.Handle(pattern, cop.Handler(f)) }
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "Route inconnue") // pas de repli sur le front
	})
	mux.HandleFunc("GET /api/status", h.getStatus)
	mux.HandleFunc("GET /api/tmdb/search", h.tmdbSearch)
	mux.HandleFunc("GET /api/deezer/search", h.deezerSearch)
	mux.HandleFunc("GET /api/queue", h.getQueue)
	mux.HandleFunc("GET /api/history", h.history)
	write("POST /api/videos/analyze", h.analyze)
	write("POST /api/queue", h.postQueue)
	write("POST /api/files/{id}/retry", h.retry)
	write("POST /api/files/{id}/replace", h.replace)
	write("DELETE /api/files/{id}", h.remove)
	write("POST /api/pause", func(w http.ResponseWriter, _ *http.Request) {
		h.Queue.Pause()
		w.WriteHeader(http.StatusNoContent)
	})
	write("POST /api/resume", func(w http.ResponseWriter, _ *http.Request) {
		h.Queue.Resume()
		w.WriteHeader(http.StatusNoContent)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logger.Warnf("api : écriture de la réponse : %v", err)
	}
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, ErrorBody{Error: msg})
}

// internal journalise une erreur inattendue et renvoie un 500 sans détail.
func internal(w http.ResponseWriter, what string, err error) {
	logger.Errorf("api : %s : %v", what, err)
	writeError(w, http.StatusInternalServerError, "Erreur interne : "+what)
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible")
		return false
	}
	return true
}

// parallel appelle fn(0..n-1), maxParallel à la fois.
func parallel(n int, fn func(i int)) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxParallel)
	for i := range n {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			fn(i)
		})
	}
	wg.Wait()
}

// --- État des services ---

func (h *Handlers) getStatus(w http.ResponseWriter, r *http.Request) {
	h.statusMu.Lock()
	if time.Since(h.statusAt) > statusTTL {
		// Détaché de la requête : un client parti ne doit pas mettre une erreur en cache 10 min.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), statusTimeout)
		h.status, h.statusAt = h.checkServices(ctx), time.Now()
		cancel()
	}
	s := h.status
	h.statusMu.Unlock()
	var fs syscall.Statfs_t
	if err := syscall.Statfs(h.MediaDir, &fs); err != nil {
		logger.Warnf("api : espace libre de la médiathèque : %v", err)
	} else {
		s.FreeBytes = int64(fs.Bavail) * int64(fs.Bsize)
	}
	writeJSON(w, http.StatusOK, s)
}

// checkServices interroge AllDebrid et Deezer en parallèle ; les erreurs des clients ne portent
// ni l'URL ni la clé.
func (h *Handlers) checkServices(ctx context.Context) Status {
	var s Status
	var wg sync.WaitGroup
	wg.Go(func() {
		premium, until, err := h.AllDebrid.Account(ctx)
		if err != nil {
			logger.Warnf("état : AllDebrid : %v", err)
			s.AllDebrid.Error = err.Error()
			return
		}
		s.AllDebrid = AllDebridStatus{OK: true, Premium: premium}
		if !until.IsZero() {
			s.AllDebrid.Until = until.Format(time.DateOnly)
		}
	})
	wg.Go(func() {
		if err := h.Deezer.CheckARL(ctx); err != nil {
			logger.Warnf("état : Deezer : %v", err)
			s.Deezer.Error = err.Error()
			return
		}
		s.Deezer.OK = true
	})
	wg.Wait()
	return s
}

// --- Recherches ---

func toTMDB(r tmdb.Result) TMDBResult { return TMDBResult(r) }

func (h *Handlers) tmdbSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if strings.TrimSpace(q.Get("q")) == "" {
		writeError(w, http.StatusBadRequest, "Recherche vide")
		return
	}
	year, _ := strconv.Atoi(q.Get("year"))
	tv, _ := strconv.ParseBool(q.Get("tv"))
	res, err := h.TMDB.Search(r.Context(), q.Get("q"), year, tv)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	out := make([]TMDBResult, len(res))
	for i, x := range res {
		out[i] = toTMDB(x)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handlers) deezerSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeError(w, http.StatusBadRequest, "Recherche vide")
		return
	}
	res, err := h.Deezer.Search(r.Context(), q)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	out := DeezerSearch{Artists: []DeezerArtist{}, Albums: []DeezerAlbum{}, Tracks: []DeezerTrack{}}
	for _, a := range res.Artists {
		out.Artists = append(out.Artists, DeezerArtist(a))
	}
	for _, a := range res.Albums {
		al := DeezerAlbum{ID: a.ID, Title: a.Title, Artist: a.Artist, Cover: a.Cover, Year: a.Year, Tracks: a.Tracks}
		if dir, err := naming.Within(h.MediaDir, naming.AlbumDir(a.Artist, a.Title)); err == nil {
			_, err := os.Stat(dir)
			al.Present = err == nil
		}
		out.Albums = append(out.Albums, al)
	}
	for _, t := range res.Tracks {
		out.Tracks = append(out.Tracks, DeezerTrack(t))
	}
	writeJSON(w, http.StatusOK, out)
}

// --- File d'attente ---

func (h *Handlers) getQueue(w http.ResponseWriter, _ *http.Request) {
	// shortcut: relit toute la table à chaque appel (toutes les 2 s) ; filtrer en SQL si l'historique grossit.
	files, err := h.Store.Files()
	if err != nil {
		internal(w, "lecture de la file", err)
		return
	}
	jobs, err := h.jobsOf(files)
	if err != nil {
		internal(w, "lecture des jobs", err)
		return
	}
	progress := h.Queue.Progress()
	view := QueueView{Paused: h.Queue.Paused(), Jobs: []JobView{}}
	byJob := map[int64]int{} // ID du job → index dans view.Jobs
	active := map[int64]bool{}
	for _, f := range files {
		i, ok := byJob[f.JobID]
		if !ok {
			j := jobs[f.JobID]
			i = len(view.Jobs)
			byJob[f.JobID] = i
			view.Jobs = append(view.Jobs, JobView{ID: j.ID, Kind: j.Kind, Title: j.Title})
		}
		fv := fileView(f, progress)
		view.Speed += fv.Speed
		view.Jobs[i].Files = append(view.Jobs[i].Files, fv)
		active[f.JobID] = active[f.JobID] || f.State != store.StateDone
	}
	view.Jobs = slices.DeleteFunc(view.Jobs, func(j JobView) bool { return !active[j.ID] })
	writeJSON(w, http.StatusOK, view)
}

func fileView(f store.File, progress map[int64]queue.Progress) FileView {
	v := FileView{ID: f.ID, Name: f.OriginalName, Dest: f.Dest, State: f.State, Error: f.Error, Total: f.Size, Attempts: f.Attempts}
	if p, ok := progress[f.ID]; ok {
		v.Bytes, v.Total, v.Speed, v.Phase = p.Bytes, p.Total, p.Speed, p.Phase
	} else if f.State == store.StateDone {
		v.Bytes = f.Size
	}
	return v
}

func (h *Handlers) jobsOf(files []store.File) (map[int64]store.Job, error) {
	ids := make([]int64, 0, len(files))
	for _, f := range files {
		if !slices.Contains(ids, f.JobID) {
			ids = append(ids, f.JobID)
		}
	}
	return h.Store.Jobs(ids)
}

func (h *Handlers) history(w http.ResponseWriter, r *http.Request) {
	filter := cmp.Or(r.URL.Query().Get("filter"), "all")
	keep := map[string]func(store.Job, store.File) bool{
		"all":   func(store.Job, store.File) bool { return true },
		"video": func(j store.Job, _ store.File) bool { return j.Kind != naming.Music },
		"music": func(j store.Job, _ store.File) bool { return j.Kind == naming.Music },
		"error": func(_ store.Job, f store.File) bool { return f.State == store.StateError },
	}[filter]
	if keep == nil {
		writeError(w, http.StatusBadRequest, "Filtre invalide : "+filter)
		return
	}
	files, err := h.Store.Files(store.StateDone, store.StateError)
	if err != nil {
		internal(w, "lecture de l'historique", err)
		return
	}
	jobs, err := h.jobsOf(files)
	if err != nil {
		internal(w, "lecture des jobs", err)
		return
	}
	rows := []HistoryRow{}
	for _, f := range files {
		if j := jobs[f.JobID]; keep(j, f) {
			row := HistoryRow{Title: j.Title, Name: f.OriginalName, Dest: f.Dest, Kind: j.Kind, Size: f.Size, State: f.State, Error: f.Error}
			if f.FinishedAt != nil {
				row.FinishedAt = *f.FinishedAt
			}
			var m store.PackMeta
			if naming.IsPack(f.OriginalName) && json.Unmarshal([]byte(f.Meta), &m) == nil {
				row.Pack = m.Pack
			}
			rows = append(rows, row)
		}
	}
	slices.SortStableFunc(rows, func(a, b HistoryRow) int { return b.FinishedAt.Compare(a.FinishedAt) })
	writeJSON(w, http.StatusOK, rows)
}

// --- Actions sur un fichier en erreur ---

// erroredFile lit le fichier {id} et vérifie qu'il est en erreur ; sinon répond et renvoie false.
func (h *Handlers) erroredFile(w http.ResponseWriter, r *http.Request) (store.File, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Identifiant invalide")
		return store.File{}, false
	}
	files, err := h.Store.Files()
	if err != nil {
		internal(w, "lecture des fichiers", err)
		return store.File{}, false
	}
	i := slices.IndexFunc(files, func(f store.File) bool { return f.ID == id })
	switch {
	case i < 0:
		writeError(w, http.StatusNotFound, "Fichier introuvable")
	case files[i].State != store.StateError:
		writeError(w, http.StatusConflict, "Action possible seulement sur un fichier en erreur")
	default:
		return files[i], true
	}
	return store.File{}, false
}

func (h *Handlers) retry(w http.ResponseWriter, r *http.Request) {
	f, ok := h.erroredFile(w, r)
	if !ok {
		return
	}
	// Le .part est gardé : la reprise ou le seul déplacement suffit.
	if err := h.Store.SetState(f.ID, store.StatePending, ""); err != nil {
		internal(w, "remise en attente", err)
		return
	}
	h.Queue.Wake()
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) replace(w http.ResponseWriter, r *http.Request) {
	var req ReplaceRequest
	if !readJSON(w, r, &req) {
		return
	}
	f, ok := h.erroredFile(w, r)
	if !ok {
		return
	}
	nf, err := h.replacement(r.Context(), f, strings.TrimSpace(req.Input))
	if err != nil {
		h.fail(w, err)
		return
	}
	h.enqueue.Lock()
	defer h.enqueue.Unlock()
	if err := h.checkQueued([]store.File{nf}); err != nil {
		h.fail(w, err)
		return
	}
	h.Queue.Cancel(f.ID)
	if err := h.Store.Replace(f.ID, nf.Input, nf.OriginalName, nf.Dest, nf.Size); err != nil {
		internal(w, "remplacement du lien", err)
		return
	}
	h.Queue.Wake()
	w.WriteHeader(http.StatusNoContent)
}

// replacement relit le nouveau lien de f, qui garde sa source : taille et nom sont repris (ceux du
// lien mort fausseraient la reprise et le contrôle de longueur), l'extension de la destination suit.
func (h *Handlers) replacement(ctx context.Context, f store.File, input string) (store.File, error) {
	s, found := source.Pick(h.Sources, input)
	if !found || s.Name() != f.Source {
		return f, &httpError{http.StatusBadRequest, "Lien non reconnu par la source " + f.Source}
	}
	items, err := s.Resolve(ctx, input)
	if err != nil {
		return f, &httpError{http.StatusBadGateway, err.Error()}
	}
	if len(items) != 1 {
		return f, &httpError{http.StatusBadRequest, "Le lien doit désigner un seul fichier"}
	}
	if !items[0].Online {
		return f, &httpError{http.StatusBadRequest, cmp.Or(items[0].Error, "Lien hors ligne")}
	}
	name := cmp.Or(items[0].Name, f.OriginalName)
	pack := naming.IsPack(f.OriginalName)
	if pack != naming.IsPack(name) {
		return f, &httpError{http.StatusBadRequest, "Un pack ne se remplace que par un pack"}
	}
	f.Input, f.OriginalName, f.Size = input, name, items[0].Size
	if ext := path.Ext(f.OriginalName); !pack && ext != "" && ext != path.Ext(f.Dest) { // un pack garde son dossier
		f.Dest = strings.TrimSuffix(f.Dest, path.Ext(f.Dest)) + ext
		if _, err := naming.Within(h.MediaDir, f.Dest); err != nil {
			return f, &httpError{http.StatusBadRequest, "Destination invalide : " + f.Dest}
		}
	}
	return f, nil
}

func (h *Handlers) remove(w http.ResponseWriter, r *http.Request) {
	f, ok := h.erroredFile(w, r)
	if !ok {
		return
	}
	h.Queue.Cancel(f.ID)
	if err := h.Store.Delete(f.ID); err != nil {
		internal(w, "suppression du fichier", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
