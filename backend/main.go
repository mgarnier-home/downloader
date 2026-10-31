package main

import (
	"embed"
	"io/fs"
	"net/http"
	"os"

	"github.com/mgarnier-home/utils/logger"
	"github.com/mgarnier-home/utils/version"
)

//go:embed all:web/browser
var webFS embed.FS

func main() {
	logger.InitAppLogger("downloader")

	cfg, err := loadConfig()
	if err != nil {
		logger.Errorf("configuration invalide : %v", err)
		os.Exit(1)
	}

	front, err := fs.Sub(webFS, "web/browser")
	if err != nil {
		logger.Errorf("front embarqué introuvable : %v", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /version", version.Handler())
	mux.Handle("/", spaHandler(front))

	logger.Infof("écoute sur le port %s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		logger.Errorf("arrêt du serveur : %v", err)
		os.Exit(1)
	}
}
