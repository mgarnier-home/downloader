package deezer

import (
	"bytes"
	"crypto/cipher"
	"encoding/hex"
	"io"
	"testing"

	"golang.org/x/crypto/blowfish" //nolint:staticcheck // chiffrement imposé par le format des flux Deezer
)

func TestTrackKey(t *testing.T) {
	if got := hex.EncodeToString(TrackKey("3135556")); got != "6c6c666b39662c37652575603c643439" {
		t.Fatalf("clé = %s", got)
	}
}

// chunkReader rend au plus n octets par lecture.
type chunkReader struct {
	r io.Reader
	n int
}

func (c chunkReader) Read(p []byte) (int, error) { return c.r.Read(p[:min(len(p), c.n)]) }

func TestDecrypterStripes(t *testing.T) {
	plain := make([]byte, 4*2048+100)
	for i := range plain {
		plain[i] = byte(i * 7)
	}
	key := TrackKey("3135556")
	c, err := blowfish.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	enc := bytes.Clone(plain)
	for _, b := range []int{0, 3} {
		blk := enc[b*2048 : (b+1)*2048]
		cipher.NewCBCEncrypter(c, []byte{0, 1, 2, 3, 4, 5, 6, 7}).CryptBlocks(blk, blk)
	}
	for _, chunk := range []int{1 << 20, 700} {
		got, err := io.ReadAll(NewDecrypter(chunkReader{bytes.NewReader(enc), chunk}, key))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, plain) {
			t.Fatalf("morceaux de %d : flux déchiffré différent de l'original", chunk)
		}
	}
}
