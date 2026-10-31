// Package source transforme une entrée de l'utilisateur en fichiers à télécharger.
package source

import (
	"context"
	"fmt"
	"io"

	"github.com/mgarnier-home/downloader/backend/naming"
)

// Item est un fichier à télécharger.
type Item struct {
	Name   string
	Size   int64
	Online bool
	Error  string // message lisible si !Online
	// Open renvoie le flux à partir de offset ; le booléen est vrai si offset a été respecté.
	Open  func(ctx context.Context, offset int64) (io.ReadCloser, bool, error)
	Track *naming.TrackInfo // musique uniquement
	Meta  string            // JSON opaque stocké dans store.File.Meta
}

// Source reconnaît une entrée et la résout en fichiers.
type Source interface {
	Name() string
	Match(input string) bool
	Resolve(ctx context.Context, input string) ([]Item, error)
	// Reopen recrée Item.Open depuis store.File (Input, Meta), après redémarrage.
	Reopen(ctx context.Context, input, meta string) (Item, error)
}

// StatusError est renvoyée par Open pour toute réponse HTTP non 2xx ; le gestionnaire
// de file traite les 5xx comme passagères via errors.As.
type StatusError struct{ Code int }

func (e *StatusError) Error() string { return fmt.Sprintf("réponse HTTP %d", e.Code) }

// Pick renvoie la première source dont Match est vrai.
func Pick(sources []Source, input string) (Source, bool) {
	for _, s := range sources {
		if s.Match(input) {
			return s, true
		}
	}
	return nil, false
}
