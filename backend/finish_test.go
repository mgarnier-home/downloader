package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bogem/id3v2/v2"

	"github.com/mgarnier-home/downloader/backend/deezer"
	"github.com/mgarnier-home/downloader/backend/naming"
	"github.com/mgarnier-home/downloader/backend/store"
)

func newFinisher(t *testing.T) *finisher {
	t.Helper()
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
	return &finisher{store: st, index: ix, mediaDir: media, http: http.DefaultClient}
}

func (fz *finisher) addFile(t *testing.T, j store.Job, f store.File) store.File {
	t.Helper()
	_, files, err := fz.store.CreateJob(j, []store.File{f})
	if err != nil {
		t.Fatal(err)
	}
	return files[0]
}

func TestFinishVideoIndexes(t *testing.T) {
	fz := newFinisher(t)
	f := fz.addFile(t, store.Job{Kind: naming.Series, Ref: "tmdb:76479"}, store.File{Dest: "series/the boys/S01/the boys.S01.E01.mkv"})
	if err := fz.finish(f, "inutile"); err != nil {
		t.Fatal(err)
	}
	if n, ok := fz.index.Lookup(naming.Series, 76479); !ok || n != "the boys" {
		t.Errorf("index : %q %v", n, ok)
	}
}

func TestFinishMusicTagsAndCover(t *testing.T) {
	fz := newFinisher(t)
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("jpeg-" + strings.TrimPrefix(r.URL.Path, "/"))) //nolint:errcheck
	}))
	t.Cleanup(cdn.Close)
	m := deezer.TrackMeta{SongID: "1", CoverURL: cdn.URL + "/{size}",
		Info: naming.TrackInfo{AlbumArtist: "Gojira", Artist: "Gojira", Album: "Fortitude", Title: "Amazonia", Track: 2, Disc: 1, DiskCount: 1}}
	meta, _ := json.Marshal(m)
	f := fz.addFile(t, store.Job{Kind: naming.Music}, store.File{Dest: naming.MusicPath(m.Info), Meta: string(meta)})
	part := filepath.Join(t.TempDir(), "1.part")
	if err := os.WriteFile(part, []byte("\xff\xfb"+strings.Repeat("audio", 100)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fz.finish(f, part); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(fz.mediaDir, "musics/Gojira/Fortitude/cover.jpg")); err != nil || string(b) != "jpeg-1200" {
		t.Errorf("cover.jpg : %q %v", b, err)
	}
	// Album à deux disques : la pochette reste à la racine de l'album, pas dans CD2/.
	m.Info.Disc, m.Info.DiskCount, m.Info.Album = 2, 2, "Magma"
	meta, _ = json.Marshal(m)
	f2 := fz.addFile(t, store.Job{Kind: naming.Music}, store.File{Dest: naming.MusicPath(m.Info), Meta: string(meta)})
	if err := fz.finish(f2, part); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(fz.mediaDir, "musics/Gojira/Magma/cover.jpg")); err != nil {
		t.Errorf("cover.jpg de l'album : %v", err)
	}
	if _, err := os.Stat(filepath.Join(fz.mediaDir, "musics/Gojira/Magma/CD2/cover.jpg")); err == nil {
		t.Error("cover.jpg ne doit pas aller dans CD2/")
	}
	tag, err := id3v2.Open(part, id3v2.Options{Parse: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tag.Close() }()
	pics := tag.GetFrames(tag.CommonID("Attached picture"))
	if tag.Title() != "Amazonia" || len(pics) != 1 || string(pics[0].(id3v2.PictureFrame).Picture) != "jpeg-800" {
		t.Errorf("tags : titre %q, %d pochettes", tag.Title(), len(pics))
	}
}

func TestFinishCoverErrorHidesURL(t *testing.T) {
	fz := newFinisher(t)
	cdn := httptest.NewServer(http.NotFoundHandler())
	cdn.Close()
	meta, _ := json.Marshal(deezer.TrackMeta{SongID: "1", CoverURL: cdn.URL + "/{size}", Info: naming.TrackInfo{AlbumArtist: "A", Album: "B"}})
	f := fz.addFile(t, store.Job{Kind: naming.Music}, store.File{Dest: "musics/A/B/x.mp3", Meta: string(meta)})
	err := fz.finish(f, filepath.Join(t.TempDir(), "absent.part"))
	if err == nil || strings.Contains(err.Error(), "http") {
		t.Errorf("erreur : %v", err)
	}
}
