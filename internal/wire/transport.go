// Package wire implements the ESPHome native API transport: frame codecs for the
// plaintext and Noise-encrypted variants, from the device (responder) side.
package wire

import (
	"encoding/binary"
	"errors"
	"io"
)

// Frame is one API message: a type ID from api.proto's (id) option plus its
// encoded protobuf payload.
type Frame struct {
	Type uint32
	Data []byte
}

type Transport interface {
	// Handshake completes connection setup. It must be called before Read or Write.
	Handshake() error
	Read() (Frame, error)
	Write(Frame) error
	Close() error
}

var (
	ErrBadIndicator   = errors.New("wire: bad frame indicator byte")
	ErrShortPayload   = errors.New("wire: payload shorter than declared length")
	ErrLengthMismatch = errors.New("wire: declared message length does not match payload")
	ErrNotReady       = errors.New("wire: handshake not complete")

	ErrPlaintextAttempted = errors.New("wire: client attempted plaintext on an encrypted transport")
)

func be16(b []byte) uint32 {
	return uint32(b[0])<<8 | uint32(b[1])
}

func putBE16(b []byte, v uint32) {
	b[0] = byte(v >> 8)
	b[1] = byte(v)
}

// readUvarint reads a varint a byte at a time; the stream is shared with payload
// data, so we cannot over-read into it.
func readUvarint(r io.Reader) (uint64, error) {
	var buf [1]byte
	var x uint64
	var s uint
	for i := 0; ; i++ {
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return 0, err
		}
		b := buf[0]
		if b < 0x80 {
			if i > binary.MaxVarintLen64 || i == binary.MaxVarintLen64-1 && b > 1 {
				return 0, errors.New("wire: varint overflow")
			}
			return x | uint64(b)<<s, nil
		}
		x |= uint64(b&0x7f) << s
		s += 7
	}
}
