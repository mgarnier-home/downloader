package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mgarnier-home/utils/logger"
	"github.com/mgarnier-home/utils/version"

	"github.com/mgarnier-home/downloader/backend/api"
	"github.com/mgarnier-home/downloader/backend/deezer"
	"github.com/mgarnier-home/downloader/backend/naming"
	"github.com/mgarnier-home/downloader/backend/queue"
	"github.com/mgarnier-home/downloader/backend/source"
	"github.com/mgarnier-home/downloader/backend/store"
	"github.com/mgarnier-home/downloader/backend/tmdb"
)

//go:embed all:web/browser
var webFS embed.FS

const apiTimeout = 30 * time.Second

func main() {
	logger.InitAppLogger("downloader")

	cfg, err := loadConfig()
	if err != nil {
		logger.Errorf("configuration invalide : %v", err)
		os.Exit(1)
	}
	if err := run(cfg); err != nil {
		logger.Errorf("arrêt du serveur : %v", err)
		os.Exit(1)
	}
}

// streamClient borne l'attente des en-têtes sans couper un long téléchargement (pas de Timeout global).
func streamClient() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.ResponseHeaderTimeout = apiTimeout
	return &http.Client{Transport: t}
}

func run(cfg Config) error {
	front, err := fs.Sub(webFS, "web/browser")
	if err != nil {
		return fmt.Errorf("front embarqué introuvable : %w", err)
	}
	st, err := store.Open(filepath.Join(cfg.DataDir, "downloader.db"))
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	ix, err := naming.BuildIndex(cfg.MediaDir)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	m, mux := wire(ctx, cfg, st, ix, front)
	srv := &http.Server{Addr: ":" + cfg.Port, Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	queueDone := make(chan struct{})
	go func() { m.Run(ctx); close(queueDone) }()
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()

	logger.Infof("écoute sur le port %s", cfg.Port)
	err = srv.ListenAndServe()
	stop()
	<-queueDone // les transferts interrompus repassent en attente
	if errors.Is(err, http.ErrServerClosed) {
		logger.Infof("arrêt demandé")
		return nil
	}
	return err
}

// wire crée les clients, les sources, la file et les routes.
func wire(ctx context.Context, cfg Config, st *store.Store, ix *naming.Index, front fs.FS) (*queue.Manager, *http.ServeMux) {
	apiHTTP, stream := &http.Client{Timeout: apiTimeout}, streamClient()
	// Le client AllDebrid sert aussi au flux débridé : pas de délai global.
	ad := &source.AllDebrid{BaseURL: "https://api.alldebrid.com", Key: cfg.AllDebridKey, HTTP: stream}
	loadCtx, cancel := context.WithTimeout(ctx, apiTimeout)
	if err := ad.LoadHosts(loadCtx); err != nil {
		logger.Errorf("AllDebrid : liste des hébergeurs illisible, liens traités en HTTP direct : %v", err)
	}
	cancel()
	dz := &deezer.Client{ARL: cfg.DeezerARL, HTTP: apiHTTP}
	sources := []source.Source{&source.Deezer{Client: dz}, ad, &source.HTTP{Client: stream}}
	fz := &finisher{store: st, index: ix, mediaDir: cfg.MediaDir, http: apiHTTP}
	m := &queue.Manager{
		Store: st, Sources: sources, MediaDir: cfg.MediaDir, VideoLimit: 3, MusicLimit: 5,
		Delays: []time.Duration{10 * time.Second, 30 * time.Second, 90 * time.Second},
		Finish: fz.finish, // Sleep nul : attente interrompue par l'arrêt
	}
	h := &api.Handlers{
		Store: st, Index: ix, MediaDir: cfg.MediaDir, Sources: sources,
		TMDB:   &tmdb.Client{BaseURL: "https://api.themoviedb.org/3", Key: cfg.TMDBKey, HTTP: apiHTTP},
		Deezer: dz, AllDebrid: ad, Queue: m,
	}

	mux := http.NewServeMux()
	h.Register(mux)
	mux.Handle("GET /version", version.Handler())
	mux.Handle("/", spaHandler(front))
	return m, mux
}
