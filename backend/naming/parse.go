// Package naming lit les noms de fichier et calcule les chemins de rangement.
package naming

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Parsed est le résultat de la lecture d'un nom de fichier vidéo.
type Parsed struct {
	Title      string
	Year       int
	Season     int
	Episode    int
	HasEpisode bool
	Ext        string // inclut le point (".mkv")
}

const sep = `[ ._-]`

var (
	reSxxExx = regexp.MustCompile(`(?i)(?:^|` + sep + `)S(\d{1,2})` + sep + `?E(\d{1,3})(?:$|` + sep + `)`)
	reNxNN   = regexp.MustCompile(`(?i)(?:^|` + sep + `)(\d{1,2})x(\d{2})(?:$|` + sep + `)`)
	reSxx    = regexp.MustCompile(`(?i)(?:^|` + sep + `)S(\d{1,2})(?:$|` + sep + `)`)
	reYear   = regexp.MustCompile(`^(?:19|20)\d{2}$`)
)

var releaseTags = map[string]bool{
	"multi": true, "vff": true, "vostfr": true, "french": true, "2160p": true,
	"1080p": true, "720p": true, "web": true, "bluray": true, "hdr": true,
}

func isSep(r byte) bool { return r == ' ' || r == '.' || r == '_' || r == '-' }

// scanTokens renvoie la position de la dernière année plausible et de la première balise de release (-1 si absentes).
func scanTokens(base string) (year, yearAt, tagAt int) {
	maxYear := time.Now().Year() + 1
	yearAt, tagAt = -1, -1
	for start, n := 0, 0; start <= len(base); n++ {
		end := start
		for end < len(base) && !isSep(base[end]) {
			end++
		}
		tok := base[start:end]
		// Le premier jeton est le titre ("1917"), jamais une année.
		if y := atoi(tok); n > 0 && reYear.MatchString(tok) && y >= 1900 && y <= maxYear {
			year, yearAt = y, start
		}
		if n > 0 && tagAt < 0 && releaseTags[strings.ToLower(tok)] {
			tagAt = start
		}
		start = end + 1
	}
	return year, yearAt, tagAt
}

// scanEpisode cherche SxxExx, NxNN puis Sxx seul ; renvoie la position du marqueur (-1 si absent).
func scanEpisode(base string, p *Parsed) int {
	if m := reSxxExx.FindStringSubmatchIndex(base); m != nil {
		p.Season, p.Episode, p.HasEpisode = atoi(base[m[2]:m[3]]), atoi(base[m[4]:m[5]]), true
		return m[0]
	}
	if m := reNxNN.FindStringSubmatchIndex(base); m != nil {
		p.Season, p.Episode, p.HasEpisode = atoi(base[m[2]:m[3]]), atoi(base[m[4]:m[5]]), true
		return m[0]
	}
	if m := reSxx.FindStringSubmatchIndex(base); m != nil {
		p.Season = atoi(base[m[2]:m[3]])
		return m[0]
	}
	return -1
}

// ParseFilename extrait titre, année, saison/épisode et extension d'un nom de fichier.
func ParseFilename(name string) Parsed {
	p := Parsed{Ext: filepath.Ext(name)}
	base := strings.TrimSuffix(name, p.Ext)
	var yearAt, tagAt int
	p.Year, yearAt, tagAt = scanTokens(base)

	// Le titre s'arrête au premier marqueur trouvé (épisode, année ou balise).
	cut := len(base)
	for _, i := range []int{scanEpisode(base, &p), yearAt, tagAt} {
		if i >= 0 && i < cut {
			cut = i
		}
	}
	p.Title = strings.Trim(strings.Join(strings.Fields(strings.NewReplacer(".", " ", "_", " ").Replace(base[:cut])), " "), " -")
	return p
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

// SeasonOf lit Sxx (ou NxNN) dans un nom quelconque, sans retirer d'extension ; 0 sinon.
func SeasonOf(s string) int {
	var p Parsed
	scanEpisode(s, &p)
	return p.Season
}

func hasExt(name string, exts ...string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	for _, e := range exts {
		if ext == e {
			return true
		}
	}
	return false
}

// IsPack est vrai pour une archive de série (.zip, .rar).
func IsPack(name string) bool { return hasExt(name, ".zip", ".rar") }

// IsVideo est vrai pour les extensions vidéo extraites d'un pack.
func IsVideo(name string) bool { return hasExt(name, ".mkv", ".mp4", ".avi", ".m4v", ".ts") }
