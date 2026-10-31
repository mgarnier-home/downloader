package main

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/mgarnier-home/utils/config"
)

// Config regroupe les variables d'environnement de l'application.
type Config struct {
	Port         string `key:"DOWNLOADER_SERVER_PORT" default-value:"8080"`
	MediaDir     string `key:"DOWNLOADER_MEDIA_DIR" default-value:"/media"`
	DataDir      string `key:"DOWNLOADER_DATA_DIR" default-value:"/data"`
	AllDebridKey string `key:"DOWNLOADER_ALLDEBRID_API_KEY" required:"true"`
	DeezerARL    string `key:"DOWNLOADER_DEEZER_ARL" required:"true"`
	TMDBKey      string `key:"DOWNLOADER_TMDB_API_KEY" required:"true"`
}

// loadConfig lit la configuration et rejette les valeurs manquantes ou invalides.
func loadConfig() (Config, error) {
	var cfg Config
	if err := errors.Join(config.GetConfig(&cfg)...); err != nil {
		return cfg, err
	}
	if p, err := strconv.Atoi(cfg.Port); err != nil || p < 1 || p > 65535 {
		return cfg, fmt.Errorf("DOWNLOADER_SERVER_PORT invalide : %q (attendu 1 à 65535)", cfg.Port)
	}
	return cfg, nil
}
