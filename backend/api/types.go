// Package api expose l'API HTTP consommée par le front.
package api

import (
	"time"

	"github.com/mgarnier-home/downloader/backend/naming"
	"github.com/mgarnier-home/downloader/backend/store"
)

// Les noms JSON sont en camelCase : le front reprend ces types tels quels.

type AllDebridStatus struct {
	OK      bool   `json:"ok"`
	Premium bool   `json:"premium"`
	Until   string `json:"until"` // AAAA-MM-JJ, vide sans abonnement
	Error   string `json:"error"`
}

type DeezerStatus struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

type Status struct {
	AllDebrid AllDebridStatus `json:"alldebrid"`
	Deezer    DeezerStatus    `json:"deezer"`
	FreeBytes int64           `json:"freeBytes"`
}

type AnalyzeRequest struct {
	Links []string `json:"links"`
}

type TMDBResult struct {
	ID        int    `json:"id"`
	Title     string `json:"title"`
	Year      int    `json:"year"`
	Animation bool   `json:"animation"`
	Poster    string `json:"poster"`
	TV        bool   `json:"tv"`
}

type AnalyzeRow struct {
	Input       string      `json:"input"`
	Name        string      `json:"name"`
	Size        int64       `json:"size"`
	Online      bool        `json:"online"`
	Error       string      `json:"error"`
	Kind        naming.Kind `json:"kind"`
	TMDB        *TMDBResult `json:"tmdb"`
	Season      int         `json:"season"`
	Episode     int         `json:"episode"`
	NeedsReview bool        `json:"needsReview"`
	Dest        string      `json:"dest"`
	Exists      bool        `json:"exists"` // « Déjà présent » : ligne décochée par défaut
}

type QueueFile struct {
	Input     string `json:"input"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	Dest      string `json:"dest"`
	Overwrite bool   `json:"overwrite"`
}

type QueueRequest struct {
	Kind  naming.Kind `json:"kind"`
	Title string      `json:"title"`
	Ref   string      `json:"ref"` // "tmdb:<id>" ou "deezer:album:<id>"
	Files []QueueFile `json:"files"`
	// Deezer : lien ou "deezer:album:<id>" / "deezer:track:<id>" ; Files est alors vide.
	Deezer string `json:"deezer"`
}

type ReplaceRequest struct {
	Input string `json:"input"`
}

type FileView struct {
	ID       int64       `json:"id"`
	Name     string      `json:"name"`
	Dest     string      `json:"dest"`
	State    store.State `json:"state"`
	Error    string      `json:"error"`
	Bytes    int64       `json:"bytes"`
	Total    int64       `json:"total"`
	Speed    float64     `json:"speed"`
	Attempts int         `json:"attempts"`
}

type JobView struct {
	ID    int64       `json:"id"`
	Kind  naming.Kind `json:"kind"`
	Title string      `json:"title"`
	Files []FileView  `json:"files"`
}

// QueueView ne contient que les jobs ayant au moins un fichier non rangé.
type QueueView struct {
	Paused bool      `json:"paused"`
	Speed  float64   `json:"speed"`
	Jobs   []JobView `json:"jobs"`
}

type HistoryRow struct {
	FinishedAt time.Time   `json:"finishedAt"`
	Title      string      `json:"title"`
	Name       string      `json:"name"`
	Dest       string      `json:"dest"`
	Kind       naming.Kind `json:"kind"`
	Size       int64       `json:"size"`
	State      store.State `json:"state"`
	Error      string      `json:"error"`
}

type DeezerArtist struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

type DeezerAlbum struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Artist  string `json:"artist"`
	Cover   string `json:"cover"`
	Year    int    `json:"year"`
	Tracks  int    `json:"tracks"`
	Present bool   `json:"present"` // le dossier de l'album existe déjà
}

type DeezerTrack struct {
	ID       int    `json:"id"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Album    string `json:"album"`
	AlbumID  int    `json:"albumId"`
	Duration int    `json:"duration"`
}

type DeezerSearch struct {
	Artists []DeezerArtist `json:"artists"`
	Albums  []DeezerAlbum  `json:"albums"`
	Tracks  []DeezerTrack  `json:"tracks"`
}

// ErrorBody est le corps de toute réponse en erreur.
type ErrorBody struct {
	Error string `json:"error"`
}
