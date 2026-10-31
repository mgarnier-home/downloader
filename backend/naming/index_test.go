package naming

import (
	"os"
	"path/filepath"
	"testing"
)

func writeNfo(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIndex(t *testing.T) {
	root := t.TempDir()
	writeNfo(t, root, "series/TheBoys/tvshow.nfo", "<tvshow><tmdbid>76479</tmdbid></tvshow>")
	writeNfo(t, root, "films/Dunkerque/movie.nfo", "<movie><tmdbid>374720</tmdbid></movie>")
	writeNfo(t, root, "animes/Spirit/Spirit.nfo", "<movie><tmdbid>5</tmdbid></movie>")
	writeNfo(t, root, "films/Vide/movie.nfo", "<movie><title>x</title></movie>")

	ix, err := BuildIndex(root)
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := ix.Lookup(Series, 76479); !ok || n != "TheBoys" {
		t.Errorf("série: %q %v", n, ok)
	}
	if n, ok := ix.Lookup(Movie, 374720); !ok || n != "Dunkerque" {
		t.Errorf("film: %q %v", n, ok)
	}
	if n, ok := ix.Lookup(Anime, 5); !ok || n != "Spirit" {
		t.Errorf("animé: %q %v", n, ok)
	}
	if _, ok := ix.Lookup(Anime, 374720); ok {
		t.Error("l'animé 374720 ne devrait pas exister")
	}
	ix.Add(Anime, 1, "X")
	if n, ok := ix.Lookup(Anime, 1); !ok || n != "X" {
		t.Errorf("ajout: %q %v", n, ok)
	}
}

func TestBuildIndexEmptyDir(t *testing.T) {
	if _, err := BuildIndex(t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func TestBuildIndexReadDirError(t *testing.T) {
	root := t.TempDir()
	writeNfo(t, root, "series", "pas un dossier")
	if _, err := BuildIndex(root); err == nil {
		t.Fatal("une erreur de lecture autre que l'absence doit être renvoyée")
	}
}
