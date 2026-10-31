package main

import (
	"strings"
	"testing"
)

func setSecrets(t *testing.T) {
	t.Helper()
	t.Setenv("DOWNLOADER_ALLDEBRID_API_KEY", "a")
	t.Setenv("DOWNLOADER_DEEZER_ARL", "b")
	t.Setenv("DOWNLOADER_TMDB_API_KEY", "c")
}

func TestLoadConfigDefaults(t *testing.T) {
	setSecrets(t)
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("erreur inattendue : %v", err)
	}
	if cfg.Port != "8080" || cfg.MediaDir != "/media" || cfg.DataDir != "/data" {
		t.Fatalf("valeurs par défaut incorrectes : %+v", cfg)
	}
}

func TestLoadConfigMissingSecret(t *testing.T) {
	setSecrets(t)
	t.Setenv("DOWNLOADER_DEEZER_ARL", "")
	_, err := loadConfig()
	if err == nil || !strings.Contains(err.Error(), "DOWNLOADER_DEEZER_ARL") {
		t.Fatalf("erreur attendue sur DOWNLOADER_DEEZER_ARL, obtenu : %v", err)
	}
}

func TestLoadConfigBadPort(t *testing.T) {
	setSecrets(t)
	t.Setenv("DOWNLOADER_SERVER_PORT", "70000")
	if _, err := loadConfig(); err == nil {
		t.Fatal("erreur attendue pour un port hors plage")
	}
}
