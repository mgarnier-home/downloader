package api

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"strings"

	"github.com/mgarnier-home/downloader/backend/deezer"
	"github.com/mgarnier-home/downloader/backend/naming"
	"github.com/mgarnier-home/downloader/backend/source"
	"github.com/mgarnier-home/downloader/backend/store"
)

// httpError est une erreur renvoyée telle quelle au client.
type httpError struct {
	code int
	msg  string
}

func (e *httpError) Error() string { return e.msg }

// postQueue crée un job : tout est vérifié (destinations, doublons) avant la moindre écriture.
func (h *Handlers) postQueue(w http.ResponseWriter, r *http.Request) {
	var req QueueRequest
	if !readJSON(w, r, &req) {
		return
	}
	var job store.Job
	var files []store.File
	var err error
	if req.Deezer != "" {
		job, files, err = h.deezerJob(r.Context(), req)
	} else {
		job, files, err = h.videoJob(req)
	}
	if err == nil {
		err = h.checkFiles(files)
	}
	if err != nil {
		h.fail(w, err)
		return
	}
	h.enqueue.Lock()
	defer h.enqueue.Unlock()
	if err := h.checkQueued(files); err != nil {
		h.fail(w, err)
		return
	}
	job, files, err = h.Store.CreateJob(job, files)
	if err != nil {
		internal(w, "création du job", err)
		return
	}
	h.Queue.Wake()
	view := JobView{ID: job.ID, Kind: job.Kind, Title: job.Title}
	for _, f := range files {
		view.Files = append(view.Files, fileView(f, nil))
	}
	writeJSON(w, http.StatusCreated, view)
}

func (h *Handlers) fail(w http.ResponseWriter, err error) {
	var he *httpError
	if errors.As(err, &he) {
		writeError(w, he.code, he.msg)
		return
	}
	internal(w, "vérification des doublons", err)
}

func (h *Handlers) videoJob(req QueueRequest) (store.Job, []store.File, error) {
	if req.Kind != naming.Series && req.Kind != naming.Movie && req.Kind != naming.Anime {
		return store.Job{}, nil, &httpError{http.StatusBadRequest, "Type invalide : " + string(req.Kind)}
	}
	if len(req.Files) == 0 {
		return store.Job{}, nil, &httpError{http.StatusBadRequest, "Aucun fichier à ajouter"}
	}
	files := make([]store.File, len(req.Files))
	for i, f := range req.Files {
		input := strings.TrimSpace(f.Input)
		s, ok := source.Pick(h.Sources, input)
		if !ok {
			return store.Job{}, nil, &httpError{http.StatusBadRequest, "Lien non reconnu : " + f.Name}
		}
		files[i] = store.File{Source: s.Name(), Input: input, OriginalName: f.Name, Size: f.Size, Dest: f.Dest, Overwrite: f.Overwrite}
		if naming.IsPack(f.Name) {
			// Un pack vise le dossier d'une saison (series/<Nom>/Sxx), qu'il ne remplace jamais.
			if req.Kind != naming.Series {
				return store.Job{}, nil, &httpError{http.StatusBadRequest, "Type invalide pour un pack : " + string(req.Kind)}
			}
			dest := path.Clean(f.Dest)
			if parts := strings.Split(dest, "/"); len(parts) != 3 || parts[0] != "series" || naming.PackDir(parts[1], naming.SeasonOf(parts[2])) != dest {
				return store.Job{}, nil, &httpError{http.StatusBadRequest, "Destination invalide : " + f.Dest}
			}
			files[i].Overwrite, files[i].Meta = false, `{"pack":{}}`
		}
	}
	return store.Job{Kind: req.Kind, Title: req.Title, Ref: req.Ref}, files, nil
}

// deezerJob résout l'album ou le titre : un fichier par morceau, rangé d'après ses tags.
func (h *Handlers) deezerJob(ctx context.Context, req QueueRequest) (store.Job, []store.File, error) {
	in := strings.TrimSpace(req.Deezer)
	s, ok := source.Pick(h.Sources, in)
	if !ok || s.Name() != "deezer" {
		return store.Job{}, nil, &httpError{http.StatusBadRequest, "Entrée Deezer non reconnue"}
	}
	items, err := s.Resolve(ctx, in)
	if err != nil {
		return store.Job{}, nil, &httpError{http.StatusBadGateway, err.Error()}
	}
	files := make([]store.File, 0, len(items))
	for _, it := range items {
		var m deezer.TrackMeta
		if it.Track == nil || json.Unmarshal([]byte(it.Meta), &m) != nil {
			return store.Job{}, nil, &httpError{http.StatusBadGateway, "Deezer : métadonnées illisibles"}
		}
		files = append(files, store.File{Source: s.Name(), Input: "deezer:track:" + m.SongID, OriginalName: it.Name,
			Dest: naming.MusicPath(*it.Track), Meta: it.Meta})
	}
	if len(files) == 0 {
		return store.Job{}, nil, &httpError{http.StatusBadGateway, "Deezer : aucun morceau"}
	}
	title := items[0].Track.Album
	if strings.Contains(in, "track") {
		title = items[0].Track.Artist + " - " + items[0].Track.Title
	}
	return store.Job{Kind: naming.Music, Title: cmp.Or(req.Title, title), Ref: cmp.Or(req.Ref, in)}, files, nil
}

// checkFiles refuse une destination hors de la médiathèque et les doublons au sein de la requête.
func (h *Handlers) checkFiles(files []store.File) error {
	seen := map[string]bool{}
	for i := range files {
		raw := files[i].Dest
		files[i].Dest = path.Clean(raw) // DestTaken et l'index comparent des chemins normalisés
		f := files[i]
		_, err := naming.Within(h.MediaDir, f.Dest)
		if err != nil || strings.SplitN(f.Dest, "/", 2)[0] == "download" { // download/ : dossier des .part
			return &httpError{http.StatusBadRequest, "Destination invalide : " + raw}
		}
		pack := naming.IsPack(f.OriginalName) // destination partagée : seul le lien compte
		if !pack && seen["d:"+f.Dest] || seen["i:"+f.Input] {
			return &httpError{http.StatusConflict, "Déjà dans la file : " + f.Dest}
		}
		seen["i:"+f.Input] = true
		if !pack {
			seen["d:"+f.Dest] = true
		}
	}
	return nil
}

// checkQueued refuse une destination ou un lien déjà présents dans un fichier non terminé.
func (h *Handlers) checkQueued(files []store.File) error {
	for _, f := range files {
		dest := false
		if !naming.IsPack(f.OriginalName) {
			var err error
			if dest, err = h.Store.DestTaken(f.Dest); err != nil {
				return err
			}
		}
		input, err := h.Store.InputTaken(f.Input)
		if err != nil {
			return err
		}
		if dest || input {
			return &httpError{http.StatusConflict, "Déjà dans la file : " + f.Dest}
		}
	}
	return nil
}
