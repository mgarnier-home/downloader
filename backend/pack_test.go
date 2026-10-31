package main

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mgarnier-home/downloader/backend/naming"
	"github.com/mgarnier-home/downloader/backend/store"
)

type entry struct{ name, data string }

// zipPack écrit un ZIP aux entrées ordonnées ; flags 0x1 marque les entrées chiffrées.
func zipPack(t *testing.T, flags uint16, entries ...entry) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "1.part")
	out, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(out)
	for _, e := range entries {
		fw, err := w.CreateHeader(&zip.FileHeader{Name: e.name, Method: zip.Deflate, Flags: flags})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(fw, e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

func (fz *finisher) addPack(t *testing.T) (store.Job, store.File) {
	t.Helper()
	j := store.Job{Kind: naming.Series, Title: "Breaking Bad", Ref: "tmdb:42"}
	j, files, err := fz.store.CreateJob(j, []store.File{{Source: "http", Input: "http://x/p.zip",
		OriginalName: "Breaking.Bad.S01-S02.zip", Dest: "series/BreakingBad", Meta: `{"pack":{}}`}})
	if err != nil {
		t.Fatal(err)
	}
	return j, files[0]
}

func (fz *finisher) put(t *testing.T, rel, data string) {
	t.Helper()
	p := filepath.Join(fz.mediaDir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (fz *finisher) meta(t *testing.T, id int64) string {
	t.Helper()
	files, err := fz.store.Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.ID == id {
			return f.Meta
		}
	}
	t.Fatalf("fichier %d absent", id)
	return ""
}

func (fz *finisher) noTemp(t *testing.T, id int64) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(fz.mediaDir, "download", strconv.FormatInt(id, 10))); !os.IsNotExist(err) {
		t.Errorf("dossier temporaire toujours présent : %v", err)
	}
}

func TestPackPlaces(t *testing.T) {
	fz := newFinisher(t)
	j, f := fz.addPack(t)
	fz.put(t, "series/BreakingBad/S01/BreakingBad.S01.E02.mkv", "ancien")
	part := zipPack(t, 0,
		entry{"S01/Breaking.Bad.S01E01.mkv", "e1"},
		entry{"Breaking.Bad.S02E01.MKV", "s2e1"},
		entry{"S02/Making-of.mkv", "making"},
		entry{"S01/readme.nfo", "nfo"},
		entry{"Breaking.Bad.S01E02.mkv", "nouveau"},
		entry{"720p/Breaking.Bad.S01E01.mkv", "doublon"},
	)
	var done, total int64
	if err := fz.pack(context.Background(), f, j, part, func(d, t int64) { done, total = d, t }); err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]string{
		"series/BreakingBad/S01/BreakingBad.S01.E01.mkv": "e1",
		"series/BreakingBad/S02/BreakingBad.S02.E01.MKV": "s2e1",
		"series/BreakingBad/S02/Bonus/Making-of.mkv":     "making",
		"series/BreakingBad/S01/BreakingBad.S01.E02.mkv": "ancien",
	} {
		if got := string(readFile(t, filepath.Join(fz.mediaDir, rel))); got != want {
			t.Errorf("%s = %q, attendu %q", rel, got, want)
		}
	}
	if m := fz.meta(t, f.ID); m != `{"pack":{"placed":2,"present":2,"bonus":1}}` {
		t.Errorf("meta = %s", m)
	}
	if _, err := os.Stat(filepath.Join(fz.mediaDir, "series/BreakingBad/S01/readme.nfo")); !os.IsNotExist(err) {
		t.Errorf(".nfo extrait : %v", err)
	}
	if n, ok := fz.index.Lookup(naming.Series, 42); !ok || n != "BreakingBad" {
		t.Errorf("index : %q %v", n, ok)
	}
	if want := int64(len("e1s2e1making")); done != want || total != want { // seules les vidéos extraites comptent
		t.Errorf("progression %d / %d, attendu %d", done, total, want)
	}
	fz.noTemp(t, f.ID)
}

func TestPackAllPresent(t *testing.T) {
	fz := newFinisher(t)
	j, f := fz.addPack(t)
	fz.put(t, "series/BreakingBad/S01/BreakingBad.S01.E01.mkv", "ancien")
	part := zipPack(t, 0, entry{"Breaking.Bad.S01E01.mkv", "e1"})
	if err := fz.pack(context.Background(), f, j, part, func(int64, int64) {}); err != nil {
		t.Fatal(err)
	}
	if m := fz.meta(t, f.ID); m != `{"pack":{"placed":0,"present":1,"bonus":0}}` {
		t.Errorf("meta = %s", m)
	}
}

func TestPackErrors(t *testing.T) {
	html := filepath.Join(t.TempDir(), "html.part")
	if err := os.WriteFile(html, []byte("<html>erreur</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, part, want string
		free             int64
	}{
		{"chiffré", zipPack(t, 0x1, entry{"Breaking.Bad.S01E01.mkv", "e1"}), "Archive protégée par mot de passe", 0},
		{"sans vidéo", zipPack(t, 0, entry{"readme.nfo", "nfo"}), "Aucune vidéo dans l'archive", 0},
		{"vide", zipPack(t, 0), "Aucune vidéo dans l'archive", 0},
		{"espace", zipPack(t, 0, entry{"Breaking.Bad.S01E01.mkv", "e1"}), "Espace insuffisant pour extraire", 1},
		{"html", html, "Archive illisible : ", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fz := newFinisher(t)
			if c.free > 0 {
				fz.free = func(string) (int64, error) { return c.free, nil }
			}
			j, f := fz.addPack(t)
			err := fz.pack(context.Background(), f, j, c.part, func(int64, int64) {})
			if err == nil || !strings.HasPrefix(err.Error(), c.want) {
				t.Fatalf("err = %v, attendu %q", err, c.want)
			}
			if _, err := os.Stat(filepath.Join(fz.mediaDir, "series")); !os.IsNotExist(err) {
				t.Errorf("fichier écrit malgré l'erreur : %v", err)
			}
			fz.noTemp(t, f.ID)
		})
	}
}

func TestPackEvilPath(t *testing.T) {
	fz := newFinisher(t)
	j, f := fz.addPack(t)
	part := zipPack(t, 0, entry{"../../evil.mkv", "evil"})
	if err := fz.pack(context.Background(), f, j, part, func(int64, int64) {}); err != nil {
		t.Fatal(err)
	}
	if got := string(readFile(t, filepath.Join(fz.mediaDir, "series/BreakingBad/S01/Bonus/evil.mkv"))); got != "evil" {
		t.Errorf("contenu %q", got)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(fz.mediaDir), "evil.mkv")); !os.IsNotExist(err) {
		t.Errorf("écrit hors de la médiathèque : %v", err)
	}
}

func TestPackCancelled(t *testing.T) {
	fz := newFinisher(t)
	j, f := fz.addPack(t)
	part := zipPack(t, 0, entry{"Breaking.Bad.S01E01.mkv", "e1"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := fz.pack(ctx, f, j, part, func(int64, int64) {}); err == nil {
		t.Fatal("aucune erreur après annulation")
	}
	if _, err := os.Stat(filepath.Join(fz.mediaDir, "series")); !os.IsNotExist(err) {
		t.Errorf("épisode rangé malgré l'annulation : %v", err)
	}
	fz.noTemp(t, f.ID)
}

func readFile(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Reprise après un arrêt : seules les vidéos absentes comptent pour l'espace libre.
func TestPackResumeNeedsOnlyRemaining(t *testing.T) {
	fz := newFinisher(t)
	j, f := fz.addPack(t)
	fz.put(t, "series/BreakingBad/S01/BreakingBad.S01.E01.mkv", "ancien")
	fz.free = func(string) (int64, error) { return 10, nil }
	part := zipPack(t, 0, entry{"Breaking.Bad.S01E01.mkv", strings.Repeat("x", 100)}, entry{"Breaking.Bad.S01E02.mkv", "e2"})
	if err := fz.pack(context.Background(), f, j, part, func(int64, int64) {}); err != nil {
		t.Fatal(err)
	}
	if m := fz.meta(t, f.ID); m != `{"pack":{"placed":1,"present":1,"bonus":0}}` {
		t.Errorf("meta = %s", m)
	}
}

func TestPackAllPresentDiskFull(t *testing.T) {
	fz := newFinisher(t)
	j, f := fz.addPack(t)
	fz.put(t, "series/BreakingBad/S01/BreakingBad.S01.E01.mkv", "ancien")
	fz.free = func(string) (int64, error) { return 0, nil }
	part := zipPack(t, 0, entry{"Breaking.Bad.S01E01.mkv", "e1"})
	if err := fz.pack(context.Background(), f, j, part, func(int64, int64) {}); err != nil {
		t.Fatal(err)
	}
}

// Une autre extraction range le même épisode pendant la copie : il n'est pas écrasé.
func TestPackNoOverwriteRace(t *testing.T) {
	fz := newFinisher(t)
	j, f := fz.addPack(t)
	part := zipPack(t, 0, entry{"Breaking.Bad.S01E01.mkv", "e1"})
	dest := "series/BreakingBad/S01/BreakingBad.S01.E01.mkv"
	raced := false
	progress := func(done, _ int64) {
		if done > 0 && !raced {
			raced = true
			fz.put(t, dest, "autre")
		}
	}
	if err := fz.pack(context.Background(), f, j, part, progress); err != nil {
		t.Fatal(err)
	}
	if got := string(readFile(t, filepath.Join(fz.mediaDir, dest))); got != "autre" {
		t.Errorf("épisode écrasé : %q", got)
	}
	if m := fz.meta(t, f.ID); m != `{"pack":{"placed":0,"present":1,"bonus":0}}` {
		t.Errorf("meta = %s", m)
	}
	fz.noTemp(t, f.ID)
}
