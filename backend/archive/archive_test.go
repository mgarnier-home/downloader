package archive

import (
	"archive/zip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type got struct {
	path, data string
	size       int64
}

// zipFile écrit un ZIP sans extension, comme un .part ; un nom finissant par "/" est un dossier.
func zipFile(t *testing.T, names []string, files map[string]string, flags uint16) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "1.part")
	out, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(out)
	for _, n := range names {
		fw, err := w.CreateHeader(&zip.FileHeader{Name: n, Method: zip.Deflate, Flags: flags})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(fw, files[n]); err != nil {
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

// fixture renvoie une archive de testdata ; elles sont produites par testdata/make.sh.
func fixture(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("testdata", name)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("%s absent : lancer testdata/make.sh", name)
	}
	return p
}

func walkAll(t *testing.T, p string) []got {
	t.Helper()
	var out []got
	err := Walk(p, func(e Entry, r io.Reader) error {
		b, err := io.ReadAll(r)
		out = append(out, got{e.Path, string(b), e.Size})
		return err
	})
	if err != nil {
		t.Fatalf("Walk : %v", err)
	}
	return out
}

var want = []got{
	{"S01/Serie.S01E01.mkv", "episode 1", 9},
	{"Serie.S01E02.mkv", "episode 2", 9},
	{"readme.txt", "readme", 6},
}

func check(t *testing.T, g []got) {
	t.Helper()
	if len(g) != len(want) {
		t.Fatalf("entrées = %+v, attendu %+v", g, want)
	}
	for i := range want {
		if g[i] != want[i] {
			t.Errorf("entrée %d = %+v, attendu %+v", i, g[i], want[i])
		}
	}
}

func TestWalkZip(t *testing.T) {
	names := []string{"S01/", "S01/Serie.S01E01.mkv", "Serie.S01E02.mkv", "readme.txt"}
	files := map[string]string{"S01/Serie.S01E01.mkv": "episode 1", "Serie.S01E02.mkv": "episode 2", "readme.txt": "readme"}
	check(t, walkAll(t, zipFile(t, names, files, 0)))
}

func TestWalkRAR(t *testing.T) {
	for _, name := range []string{"rar4.rar", "rar5.rar"} {
		t.Run(name, func(t *testing.T) { check(t, walkAll(t, fixture(t, name))) })
	}
}

func TestWalkSkipUnread(t *testing.T) {
	var last string
	err := Walk(fixture(t, "rar5.rar"), func(e Entry, r io.Reader) error {
		if e.Path != "readme.txt" {
			return nil
		}
		b, err := io.ReadAll(r)
		last = string(b)
		return err
	})
	if err != nil || last != "readme" {
		t.Fatalf("dernière entrée = %q, %v", last, err)
	}
}

func TestWalkPassword(t *testing.T) {
	read := func(_ Entry, r io.Reader) error { _, err := io.ReadAll(r); return err }
	t.Run("zip", func(t *testing.T) {
		p := zipFile(t, []string{"a.mkv"}, map[string]string{"a.mkv": "x"}, 0x1)
		if err := Walk(p, read); !errors.Is(err, ErrPassword) {
			t.Fatalf("err = %v, attendu ErrPassword", err)
		}
	})
	for _, name := range []string{"rar5-headers.rar", "rar5-files.rar"} {
		t.Run(name, func(t *testing.T) {
			if err := Walk(fixture(t, name), read); !errors.Is(err, ErrPassword) {
				t.Fatalf("err = %v, attendu ErrPassword", err)
			}
		})
	}
}

func TestWalkUnknownFormat(t *testing.T) {
	p := filepath.Join(t.TempDir(), "1.part")
	if err := os.WriteFile(p, []byte("<html>erreur</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	called := false
	err := Walk(p, func(Entry, io.Reader) error { called = true; return nil })
	if err == nil || !strings.Contains(err.Error(), "format inconnu") || called {
		t.Fatalf("err = %v, appel = %v", err, called)
	}
}

func TestWalkFnError(t *testing.T) {
	stop := errors.New("stop")
	p := zipFile(t, []string{"a.mkv"}, map[string]string{"a.mkv": "x"}, 0)
	if err := Walk(p, func(Entry, io.Reader) error { return stop }); !errors.Is(err, stop) {
		t.Fatalf("err = %v, attendu stop", err)
	}
}
