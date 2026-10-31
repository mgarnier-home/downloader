package naming

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"unicode"
)

// Kind est le type de média rangé dans la médiathèque.
type Kind string

const (
	Series Kind = "series"
	Movie  Kind = "movie"
	Anime  Kind = "anime"
	Music  Kind = "music"
)

// shortcut: table des accents latins courants, à compléter si un titre la met en défaut
var accents = strings.NewReplacer(
	"à", "a", "â", "a", "ä", "a", "é", "e", "è", "e", "ê", "e", "ë", "e",
	"î", "i", "ï", "i", "ô", "o", "ö", "o", "ù", "u", "û", "u", "ü", "u",
	"ÿ", "y", "ç", "c", "ñ", "n", "œ", "oe", "æ", "ae",
	"À", "A", "Â", "A", "Ä", "A", "É", "E", "È", "E", "Ê", "E", "Ë", "E",
	"Î", "I", "Ï", "I", "Ô", "O", "Ö", "O", "Ù", "U", "Û", "U", "Ü", "U",
	"Ÿ", "Y", "Ç", "C", "Ñ", "N", "Œ", "Oe", "Æ", "Ae",
)

// PascalCase transforme un titre en nom de dossier sans accent ni séparateur.
func PascalCase(title string) string {
	words := strings.FieldsFunc(accents.Replace(title), func(r rune) bool {
		return r > unicode.MaxASCII || !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	var b strings.Builder
	for _, w := range words {
		b.WriteString(strings.ToUpper(w[:1]) + w[1:])
	}
	return b.String()
}

var unsafeChars = strings.NewReplacer("/", "_", `\`, "_", ":", "_", "*", "_", "?", "_", `"`, "_", "<", "_", ">", "_", "|", "_")

// SafeName remplace les caractères interdits dans un nom de fichier et retire les espaces de bord.
// Un résultat vide, "." ou ".." devient "_" pour ne pas sortir de son dossier.
func SafeName(s string) string {
	s = strings.TrimSpace(unsafeChars.Replace(s))
	if s == "" || s == "." || s == ".." {
		return "_"
	}
	return s
}

// VideoPath renvoie le chemin relatif d'une vidéo (épisode de série, film ou animé).
func VideoPath(kind Kind, name string, season, episode int, ext string) string {
	switch kind {
	case Series:
		return fmt.Sprintf("series/%s/S%02d/%s.S%02d.E%02d%s", name, season, name, season, episode, ext)
	case Anime:
		return fmt.Sprintf("animes/%s/%s%s", name, name, ext)
	default:
		return fmt.Sprintf("films/%s/%s%s", name, name, ext)
	}
}

// BonusPath renvoie le chemin relatif d'un bonus de série, en gardant son nom d'origine.
func BonusPath(name string, season int, original string) string {
	return fmt.Sprintf("series/%s/S%02d/Bonus/%s", name, season, SafeName(original))
}

// TrackInfo décrit un morceau à ranger.
type TrackInfo struct {
	AlbumArtist, Artist, Album, Title string
	Track, Disc, DiskCount            int
}

// AlbumDir est le dossier d'un album : son existence le marque « déjà présent », cover.jpg y est rangé.
func AlbumDir(albumArtist, album string) string {
	return path.Join("musics", SafeName(albumArtist), SafeName(album))
}

// MusicPath renvoie le chemin relatif d'un morceau (extension .mp3).
func MusicPath(t TrackInfo) string {
	parts := []string{AlbumDir(t.AlbumArtist, t.Album)}
	if t.DiskCount > 1 {
		parts = append(parts, fmt.Sprintf("CD%d", t.Disc))
	}
	file := fmt.Sprintf("%s - %s - %02d - %s.mp3", t.Artist, t.Album, t.Track, t.Title)
	return path.Join(append(parts, SafeName(file))...)
}

// Within renvoie le chemin absolu de rel sous root, ou une erreur si rel est vide, absolu ou sort de root.
func Within(root, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) {
		return "", errors.New("chemin relatif attendu")
	}
	abs := filepath.Join(root, rel)
	if r, err := filepath.Rel(root, abs); err != nil || r == "." || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", errors.New("chemin hors de la médiathèque")
	}
	return abs, nil
}
