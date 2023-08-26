// Modified from https://github.com/quic-go/quic-go/blob/58cedf7a4f/internal/handshake/hkdf.go

package quicutils

import (
	"encoding/binary"
	"golang.org/x/crypto/hkdf"
	"hash"
	"io"
)

// HkdfExpandLabel HKDF expands a label.
// Since this implementation avoids using a cryptobyte.Builder, it is about 15% faster than the
// hkdfExpandLabel in the standard library.
func HkdfExpandLabel(h func() hash.Hash, secret, label []byte, context []byte, length int) ([]byte, error) {
	b := make([]byte, 3+6+len(label)+1+len(context))
	binary.BigEndian.PutUint16(b, uint16(length))
	b[2] = uint8(6 + len(label))
	copy(b[3:], "tls13 ")
	copy(b[9:], label)
	b[9+len(label)] = uint8(len(context))
	copy(b[10+len(label):], context)

	out := make([]byte, length)
	if _, err := io.ReadFull(hkdf.Expand(h, secret, b), out); err != nil {
		return nil, err
	}
	return out, nil
}
