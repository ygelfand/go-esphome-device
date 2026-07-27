package wire

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/flynn/noise"
)

const noiseIndicator = 0x01

// Handshake packet bodies are prefixed with one byte: 0x00 for a handshake
// message, 0x01 for a plaintext error string.
const (
	handshakeMsg = 0x00
	handshakeErr = 0x01
)

var noisePrologue = []byte("NoiseAPIInit\x00\x00")

// HandshakeError is an error the peer reported, or one we reported to it. The
// message travels in the clear.
type HandshakeError struct {
	Msg string
	Err error
}

func (e *HandshakeError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("wire: noise handshake: %s: %v", e.Msg, e.Err)
	}
	return "wire: noise handshake: " + e.Msg
}

func (e *HandshakeError) Unwrap() error { return e.Err }

// Noise is the device side of the encrypted transport: Noise_NNpsk0 over
// 25519/ChaChaPoly/SHA256, with the device as responder.
type Noise struct {
	rw   io.ReadWriteCloser
	r    *bufio.Reader
	psk  []byte
	name string

	enc, dec *noise.CipherState
	wmu      sync.Mutex
	buf      []byte
}

func NewNoise(rw io.ReadWriteCloser, psk []byte, deviceName string) *Noise {
	return &Noise{
		rw:   rw,
		r:    bufio.NewReader(rw),
		psk:  psk,
		name: deviceName,
		buf:  make([]byte, 0, 1024),
	}
}

func (n *Noise) Handshake() error {
	if len(n.psk) != 32 {
		return errors.New("wire: noise psk must be 32 bytes")
	}

	hs, err := noise.NewHandshakeState(noise.Config{
		CipherSuite:           noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashSHA256),
		Pattern:               noise.HandshakeNN,
		Initiator:             false,
		Prologue:              noisePrologue,
		PresharedKey:          n.psk,
		PresharedKeyPlacement: 0,
	})
	if err != nil {
		return err
	}

	// The client opens with an empty packet, then we announce ourselves.
	if _, err := n.readPacket(); err != nil {
		return err
	}
	hello := make([]byte, 0, len(n.name)+2)
	hello = append(hello, handshakeMsg+1) // 0x01 introduces the name
	hello = append(hello, n.name...)
	hello = append(hello, 0x00)
	if err := n.writePacket(hello); err != nil {
		return err
	}

	body, err := n.readPacket()
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return n.fail(&HandshakeError{Msg: "Empty handshake message"})
	}
	if body[0] == handshakeErr {
		return &HandshakeError{Msg: string(body[1:])}
	}
	if body[0] != handshakeMsg {
		return n.fail(&HandshakeError{Msg: "Bad handshake error byte"})
	}
	if _, _, _, err := hs.ReadMessage(nil, body[1:]); err != nil {
		return n.fail(&HandshakeError{Msg: "Handshake MAC failure", Err: err})
	}

	// flynn/noise splits by direction, not by role: cs1 always carries
	// initiator->responder, cs2 responder->initiator. We are the responder.
	reply, dec, enc, err := hs.WriteMessage([]byte{handshakeMsg}, nil)
	if err != nil {
		return n.fail(&HandshakeError{Msg: "Handshake error", Err: err})
	}
	if enc == nil || dec == nil {
		return errors.New("wire: handshake did not complete")
	}
	if err := n.writePacket(reply); err != nil {
		return err
	}

	n.enc, n.dec = enc, dec
	return nil
}

// fail reports err to the peer in the clear before returning it, matching what
// the client expects on a rejected handshake.
func (n *Noise) fail(he *HandshakeError) error {
	body := make([]byte, 0, len(he.Msg)+1)
	body = append(body, handshakeErr)
	body = append(body, he.Msg...)
	if werr := n.writePacket(body); werr != nil {
		return errors.Join(he, werr)
	}
	return he
}

func (n *Noise) Read() (Frame, error) {
	if n.dec == nil {
		return Frame{}, ErrNotReady
	}
	packet, err := n.readPacket()
	if err != nil {
		return Frame{}, err
	}
	plain, err := n.dec.Decrypt(nil, nil, packet)
	if err != nil {
		return Frame{}, err
	}
	if len(plain) < 4 {
		return Frame{}, ErrShortPayload
	}
	msgType := be16(plain[0:2])
	msgLen := be16(plain[2:4])
	payload := plain[4:]
	if uint32(len(payload)) != msgLen {
		return Frame{}, ErrLengthMismatch
	}
	return Frame{Type: msgType, Data: payload}, nil
}

func (n *Noise) Write(f Frame) error {
	n.wmu.Lock()
	defer n.wmu.Unlock()

	if n.enc == nil {
		return ErrNotReady
	}

	header := make([]byte, 4)
	putBE16(header[0:2], f.Type)
	putBE16(header[2:4], uint32(len(f.Data)))

	n.buf = n.buf[:0]
	n.buf = append(n.buf, header...)
	n.buf = append(n.buf, f.Data...)

	sealed, err := n.enc.Encrypt(nil, nil, n.buf)
	if err != nil {
		return err
	}
	return n.writePacket(sealed)
}

func (n *Noise) Close() error { return n.rw.Close() }

func (n *Noise) readPacket() ([]byte, error) {
	var header [3]byte
	if _, err := io.ReadFull(n.r, header[:]); err != nil {
		return nil, err
	}
	if header[0] == plaintextIndicator {
		return nil, ErrPlaintextAttempted
	}
	if header[0] != noiseIndicator {
		return nil, fmt.Errorf("%w: %#x", ErrBadIndicator, header[0])
	}
	length := be16(header[1:3])
	if length == 0 {
		return nil, nil
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(n.r, body); err != nil {
		return nil, err
	}
	return body, nil
}

func (n *Noise) writePacket(parts ...[]byte) error {
	total := 0
	for _, p := range parts {
		total += len(p)
	}

	out := make([]byte, 0, 3+total)
	out = append(out, noiseIndicator)
	out = append(out, byte(total>>8), byte(total))
	for _, p := range parts {
		out = append(out, p...)
	}

	_, err := n.rw.Write(out)
	return err
}
