package deezer

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bogem/id3v2/v2"
)

// WriteTags remplace les tags ID3v2.4 du fichier par ceux de m ; cover (JPEG) est intégrée si non vide.
func WriteTags(path string, m TrackMeta, cover []byte) error {
	tag, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		return fmt.Errorf("tags : %w", err)
	}
	defer func() { _ = tag.Close() }()
	tag.DeleteAllFrames()
	tag.SetVersion(4)
	tag.SetDefaultEncoding(id3v2.EncodingUTF8)
	artists := m.Artists
	if len(artists) == 0 {
		artists = []string{m.Info.Artist}
	}
	frames := map[string]string{
		"TIT2": m.Info.Title,
		"TPE1": strings.Join(artists, ", "),
		"TALB": m.Info.Album,
		"TPE2": m.Info.AlbumArtist,
		"TRCK": strconv.Itoa(m.Info.Track),
		"TPOS": fmt.Sprintf("%d/%d", m.Info.Disc, max(m.Info.DiskCount, 1)),
		"TCON": m.Genre,
	}
	if m.Year > 0 {
		frames["TDRC"] = strconv.Itoa(m.Year)
	}
	for id, v := range frames {
		if v != "" {
			tag.AddTextFrame(id, id3v2.EncodingUTF8, v)
		}
	}
	if len(cover) > 0 {
		tag.AddAttachedPicture(id3v2.PictureFrame{
			Encoding: id3v2.EncodingUTF8, MimeType: "image/jpeg", PictureType: id3v2.PTFrontCover,
			Description: "Front cover", Picture: cover,
		})
	}
	if err := tag.Save(); err != nil {
		return fmt.Errorf("tags : %w", err)
	}
	return nil
}
