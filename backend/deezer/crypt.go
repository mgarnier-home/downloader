// Package deezer interroge Deezer (recherche, métadonnées, URL des flux), déchiffre les flux et
// écrit les tags ID3v2.
package deezer

import (
	"crypto/cipher"
	"crypto/md5" //nolint:gosec // imposé par l'algorithme de déchiffrement public de Deezer
	"encoding/hex"
	"errors"
	"io"

	"golang.org/x/crypto/blowfish" //nolint:staticcheck // chiffrement imposé par le format des flux Deezer
)

const (
	blockSize = 2048
	keySecret = "g4el58wc0zvf9na1"
)

var iv = []byte{0, 1, 2, 3, 4, 5, 6, 7}

// TrackKey calcule la clé Blowfish d'un morceau selon l'algorithme de déchiffrement public de Deezer.
func TrackKey(songID string) []byte {
	sum := md5.Sum([]byte(songID)) //nolint:gosec // voir l'import
	h := hex.EncodeToString(sum[:])
	key := make([]byte, 16)
	for i := range key {
		key[i] = h[i] ^ h[i+16] ^ keySecret[i]
	}
	return key
}

// decrypter déchiffre un flux bloc par bloc : seuls les blocs pleins d'indice multiple de 3 sont
// chiffrés (Blowfish CBC, IV repris à chaque bloc).
type decrypter struct {
	r       io.Reader
	c       *blowfish.Cipher
	block   []byte
	pending []byte
	index   int
	err     error
}

// NewDecrypter renvoie un lecteur qui déchiffre r à la volée.
func NewDecrypter(r io.Reader, key []byte) io.Reader {
	c, err := blowfish.NewCipher(key)
	if err != nil {
		err = errors.New("Deezer : clé de déchiffrement invalide")
	}
	return &decrypter{r: r, c: c, block: make([]byte, blockSize), err: err}
}

func (d *decrypter) Read(p []byte) (int, error) {
	for len(d.pending) == 0 {
		if d.err != nil {
			return 0, d.err
		}
		d.fill()
	}
	n := copy(p, d.pending)
	d.pending = d.pending[n:]
	return n, nil
}

// fill lit et déchiffre le bloc suivant.
func (d *decrypter) fill() {
	n, err := io.ReadFull(d.r, d.block)
	if n == blockSize && d.index%3 == 0 {
		cipher.NewCBCDecrypter(d.c, iv).CryptBlocks(d.block, d.block)
	}
	d.index++
	d.pending = d.block[:n]
	if errors.Is(err, io.ErrUnexpectedEOF) {
		err = io.EOF
	}
	d.err = err
}
