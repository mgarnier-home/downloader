// Package archive lit les archives ZIP et RAR des packs, entrée par entrée.
package archive

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/nwaples/rardecode/v2"
)

// Entry est un fichier de l'archive.
type Entry struct {
	Path string // chemin dans l'archive, séparateur "/"
	Size int64  // taille décompressée
}

var ErrPassword = errors.New("archive protégée par mot de passe")

// Walk appelle fn pour chaque fichier régulier, dans l'ordre ; r n'est valable que pendant l'appel
// et fn peut ne pas le lire. Le format est détecté par les premiers octets, pas par le nom.
func Walk(path string, fn func(e Entry, r io.Reader) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, 8)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return err
	}
	switch head = head[:n]; {
	case bytes.HasPrefix(head, []byte("PK\x03\x04")), bytes.HasPrefix(head, []byte("PK\x05\x06")):
		return walkZip(path, fn)
	case bytes.HasPrefix(head, []byte("Rar!\x1a\x07")):
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		return walkRAR(f, fn)
	}
	return errors.New("format inconnu")
}

func walkZip(path string, fn func(Entry, io.Reader) error) error {
	z, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer func() { _ = z.Close() }()
	for _, zf := range z.File {
		if !zf.Mode().IsRegular() {
			continue
		}
		if zf.Flags&0x1 != 0 { // bit 0 : contenu chiffré, non pris en charge par archive/zip
			return ErrPassword
		}
		r, err := zf.Open()
		if err != nil {
			return err
		}
		err = fn(Entry{Path: zf.Name, Size: int64(zf.UncompressedSize64)}, r)
		_ = r.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func walkRAR(f io.Reader, fn func(Entry, io.Reader) error) error {
	r, err := rardecode.NewReader(f) // un seul volume : OpenReader chercherait les suivants
	if err != nil {
		return rarError(err)
	}
	for {
		h, err := r.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return rarError(err)
		}
		if h.Encrypted {
			return ErrPassword
		}
		if !h.Mode().IsRegular() {
			continue
		}
		if err := fn(Entry{Path: h.Name, Size: h.UnPackedSize}, r); err != nil {
			return rarError(err)
		}
	}
}

func rarError(err error) error {
	if errors.Is(err, rardecode.ErrArchiveEncrypted) || errors.Is(err, rardecode.ErrArchivedFileEncrypted) ||
		errors.Is(err, rardecode.ErrBadPassword) {
		return fmt.Errorf("%w (%w)", ErrPassword, err)
	}
	return err
}
