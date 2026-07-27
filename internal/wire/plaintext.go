package wire

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
)

const plaintextIndicator = 0x00

type Plaintext struct {
	rw  io.ReadWriteCloser
	r   *bufio.Reader
	wmu sync.Mutex
	buf []byte
}

func NewPlaintext(rw io.ReadWriteCloser) *Plaintext {
	return &Plaintext{
		rw:  rw,
		r:   bufio.NewReader(rw),
		buf: make([]byte, 0, 1024),
	}
}

func (p *Plaintext) Handshake() error { return nil }

func (p *Plaintext) Read() (Frame, error) {
	marker, err := readUvarint(p.r)
	if err != nil {
		return Frame{}, err
	}
	if marker != plaintextIndicator {
		return Frame{}, fmt.Errorf("%w: %#x", ErrBadIndicator, marker)
	}

	length, err := readUvarint(p.r)
	if err != nil {
		return Frame{}, err
	}
	msgType, err := readUvarint(p.r)
	if err != nil {
		return Frame{}, err
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(p.r, data); err != nil {
		return Frame{}, err
	}
	return Frame{Type: uint32(msgType), Data: data}, nil
}

func (p *Plaintext) Write(f Frame) error {
	p.wmu.Lock()
	defer p.wmu.Unlock()

	p.buf = p.buf[:0]
	p.buf = append(p.buf, plaintextIndicator)
	p.buf = binary.AppendUvarint(p.buf, uint64(len(f.Data)))
	p.buf = binary.AppendUvarint(p.buf, uint64(f.Type))
	p.buf = append(p.buf, f.Data...)

	_, err := p.rw.Write(p.buf)
	return err
}

func (p *Plaintext) Close() error { return p.rw.Close() }
