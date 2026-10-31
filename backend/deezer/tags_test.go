package deezer

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bogem/id3v2/v2"
	"github.com/mgarnier-home/downloader/backend/naming"
)

func TestWriteTags(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.mp3")
	audio := []byte("\xff\xfb" + strings.Repeat("audio", 100))
	if err := os.WriteFile(p, audio, 0o600); err != nil {
		t.Fatal(err)
	}
	m := TrackMeta{
		SongID:  "3135556",
		Info:    naming.TrackInfo{AlbumArtist: "Daft Punk", Artist: "Daft Punk", Album: "Discovery", Title: "One More Time", Track: 1, Disc: 1, DiskCount: 2},
		Artists: []string{"Daft Punk", "Romanthony"}, Year: 2001, Genre: "Électro",
	}
	if err := WriteTags(p, m, []byte("jpeg")); err != nil {
		t.Fatal(err)
	}
	tag, err := id3v2.Open(p, id3v2.Options{Parse: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tag.Close() }()
	checks := map[string]string{
		"TIT2": "One More Time", "TPE1": "Daft Punk, Romanthony", "TALB": "Discovery", "TPE2": "Daft Punk",
		"TRCK": "1", "TPOS": "1/2", "TDRC": "2001", "TCON": "Électro",
	}
	for id, want := range checks {
		if got := tag.GetTextFrame(id).Text; got != want {
			t.Errorf("%s = %q, attendu %q", id, got, want)
		}
	}
	pics := tag.GetFrames("APIC")
	if len(pics) != 1 {
		t.Fatalf("%d images APIC", len(pics))
	}
	if pf := pics[0].(id3v2.PictureFrame); pf.MimeType != "image/jpeg" || pf.PictureType != id3v2.PTFrontCover || string(pf.Picture) != "jpeg" {
		t.Errorf("APIC = %+v", pf)
	}
	b, _ := os.ReadFile(p)
	if !bytes.HasSuffix(b, audio) {
		t.Error("l'audio n'est pas conservé après le tag")
	}
}
