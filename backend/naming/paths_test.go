package naming

import "testing"

func TestPascalCase(t *testing.T) {
	for in, want := range map[string]string{
		"Spirit : l'indomptable":                "SpiritLIndomptable",
		"Dune : Première partie":                "DunePremierePartie",
		"Les Minions 2 : Il était une fois Gru": "LesMinions2IlEtaitUneFoisGru",
		"the boys":                              "TheBoys",
		"Œuvre ÉTÉ":                             "OeuvreETE",
	} {
		if got := PascalCase(in); got != want {
			t.Errorf("PascalCase(%q) = %q, attendu %q", in, got, want)
		}
	}
}

func TestVideoPaths(t *testing.T) {
	for got, want := range map[string]string{
		VideoPath(Series, "TheBoys", 2, 1, ".mkv"):           "series/TheBoys/S02/TheBoys.S02.E01.mkv",
		VideoPath(Movie, "Dunkerque", 0, 0, ".mkv"):          "films/Dunkerque/Dunkerque.mkv",
		VideoPath(Anime, "SpiritLIndomptable", 0, 0, ".mp4"): "animes/SpiritLIndomptable/SpiritLIndomptable.mp4",
		BonusPath("Arcane", 2, "Arcane Bonus Making Of.mkv"): "series/Arcane/S02/Bonus/Arcane Bonus Making Of.mkv",
	} {
		if got != want {
			t.Errorf("obtenu %q, attendu %q", got, want)
		}
	}
}

func TestMusicPath(t *testing.T) {
	tr := TrackInfo{AlbumArtist: "Gojira", Artist: "Gojira", Album: "Fortitude", Title: "Born For One Thing", Track: 1, Disc: 1, DiskCount: 1}
	if got, want := MusicPath(tr), "musics/Gojira/Fortitude/Gojira - Fortitude - 01 - Born For One Thing.mp3"; got != want {
		t.Errorf("obtenu %q, attendu %q", got, want)
	}
	tr.Disc, tr.DiskCount = 2, 2
	if got, want := MusicPath(tr), "musics/Gojira/Fortitude/CD2/Gojira - Fortitude - 01 - Born For One Thing.mp3"; got != want {
		t.Errorf("obtenu %q, attendu %q", got, want)
	}
	tr.Disc, tr.DiskCount, tr.Title = 1, 1, "AC/DC: Live?"
	if got, want := MusicPath(tr), "musics/Gojira/Fortitude/Gojira - Fortitude - 01 - AC_DC_ Live_.mp3"; got != want {
		t.Errorf("obtenu %q, attendu %q", got, want)
	}
}

func TestWithin(t *testing.T) {
	if got, err := Within("/media", "films/A/A.mkv"); err != nil || got != "/media/films/A/A.mkv" {
		t.Errorf("obtenu %q, %v", got, err)
	}
	for _, rel := range []string{"../etc/passwd", "films/../../etc", "/etc/passwd", ""} {
		if got, err := Within("/media", rel); err == nil {
			t.Errorf("Within(%q) devrait échouer, obtenu %q", rel, got)
		}
	}
}
