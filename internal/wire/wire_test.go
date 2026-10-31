package wire

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/flynn/noise"
)

func TestPlaintextRoundTrip(t *testing.T) {
	c1, c2 := pipePair(t)

	device := NewPlaintext(c1)
	peer := NewPlaintext(c2)

	sent := Frame{Type: 42, Data: []byte("hello esphome")}
	go func() {
		if err := peer.Write(sent); err != nil {
			t.Errorf("peer write: %v", err)
		}
	}()

	got, err := device.Read()
	if err != nil {
		t.Fatalf("device read: %v", err)
	}
	if got.Type != sent.Type || !bytes.Equal(got.Data, sent.Data) {
		t.Errorf("got %+v, want %+v", got, sent)
	}
}

func TestPlaintextEmptyPayload(t *testing.T) {
	c1, c2 := pipePair(t)

	device := NewPlaintext(c1)
	peer := NewPlaintext(c2)

	go func() {
		if err := peer.Write(Frame{Type: 9}); err != nil {
			t.Errorf("peer write: %v", err)
		}
	}()

	got, err := device.Read()
	if err != nil {
		t.Fatalf("device read: %v", err)
	}
	if got.Type != 9 || len(got.Data) != 0 {
		t.Errorf("got %+v, want type 9 with no payload", got)
	}
}

func TestPlaintextBadIndicator(t *testing.T) {
	c1, c2 := pipePair(t)

	device := NewPlaintext(c1)
	go func() {
		_, _ = c2.Write([]byte{0x99, 0x00, 0x00})
	}()

	if _, err := device.Read(); !errors.Is(err, ErrBadIndicator) {
		t.Errorf("err = %v, want ErrBadIndicator", err)
	}
}

func TestNoiseHandshakeAndRoundTrip(t *testing.T) {
	psk := testPSK()
	c1, c2 := pipePair(t)

	device := NewNoise(c1, psk, "echolocal-kitchen", testMAC)
	errc := make(chan error, 1)
	go func() { errc <- device.Handshake() }()

	client := newTestInitiator(c2)
	if err := client.handshake(psk); err != nil {
		t.Fatalf("client handshake: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("device handshake: %v", err)
	}

	if client.serverName != "echolocal-kitchen" || client.serverMAC != testMAC {
		t.Errorf("hello = %q %q, want %q %q", client.serverName, client.serverMAC, "echolocal-kitchen", testMAC)
	}

	// device -> client
	want := Frame{Type: 2, Data: []byte("device says hi")}
	go func() {
		if err := device.Write(want); err != nil {
			t.Errorf("device write: %v", err)
		}
	}()
	got, err := client.read()
	if err != nil {
		t.Fatalf("client read: %v", err)
	}
	if got.Type != want.Type || !bytes.Equal(got.Data, want.Data) {
		t.Errorf("device->client: got %+v, want %+v", got, want)
	}

	// client -> device
	want = Frame{Type: 1, Data: []byte("client says hi")}
	go func() {
		if err := client.write(want); err != nil {
			t.Errorf("client write: %v", err)
		}
	}()
	got, err = device.Read()
	if err != nil {
		t.Fatalf("device read: %v", err)
	}
	if got.Type != want.Type || !bytes.Equal(got.Data, want.Data) {
		t.Errorf("client->device: got %+v, want %+v", got, want)
	}
}

func TestNoiseMultipleFramesNonceOrder(t *testing.T) {
	psk := testPSK()
	c1, c2 := pipePair(t)

	device := NewNoise(c1, psk, "echolocal", testMAC)
	errc := make(chan error, 1)
	go func() { errc <- device.Handshake() }()

	client := newTestInitiator(c2)
	if err := client.handshake(psk); err != nil {
		t.Fatalf("client handshake: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("device handshake: %v", err)
	}

	const n = 20
	go func() {
		for i := range n {
			if err := device.Write(Frame{Type: uint32(i), Data: []byte{byte(i)}}); err != nil {
				t.Errorf("write %d: %v", i, err)
				return
			}
		}
	}()

	for i := range n {
		got, err := client.read()
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if got.Type != uint32(i) || len(got.Data) != 1 || got.Data[0] != byte(i) {
			t.Fatalf("frame %d: got %+v", i, got)
		}
	}
}

func TestNoiseFrameTooLarge(t *testing.T) {
	psk := testPSK()
	c1, c2 := pipePair(t)

	device := NewNoise(c1, psk, "echolocal", testMAC)
	errc := make(chan error, 1)
	go func() { errc <- device.Handshake() }()

	client := newTestInitiator(c2)
	if err := client.handshake(psk); err != nil {
		t.Fatalf("client handshake: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("device handshake: %v", err)
	}

	// net.Pipe is unbuffered: a frame that was written would block here with nobody reading.
	if err := device.Write(Frame{Type: 1, Data: make([]byte, maxNoisePayload+1)}); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("oversized write err = %v, want ErrFrameTooLarge", err)
	}

	// The largest frame that fits, then a small one: both arrive whole, so the refused write left
	// the connection in sync.
	big := Frame{Type: 2, Data: bytes.Repeat([]byte{0xab}, maxNoisePayload)}
	small := Frame{Type: 3, Data: []byte("still in sync")}
	go func() {
		for _, f := range []Frame{big, small} {
			if err := device.Write(f); err != nil {
				t.Errorf("device write: %v", err)
				return
			}
		}
	}()
	for _, want := range []Frame{big, small} {
		got, err := client.read()
		if err != nil {
			t.Fatalf("client read: %v", err)
		}
		if got.Type != want.Type || !bytes.Equal(got.Data, want.Data) {
			t.Fatalf("got type %d with %d bytes, want type %d with %d bytes", got.Type, len(got.Data), want.Type, len(want.Data))
		}
	}
}

func TestNoiseWrongPSKReportsError(t *testing.T) {
	deviceKey := testPSK()
	clientKey := make([]byte, 32)
	copy(clientKey, deviceKey)
	clientKey[0] ^= 0xff

	c1, c2 := pipePair(t)

	device := NewNoise(c1, deviceKey, "echolocal", testMAC)
	errc := make(chan error, 1)
	go func() { errc <- device.Handshake() }()

	client := newTestInitiator(c2)
	clientErr := client.handshake(clientKey)
	deviceErr := <-errc

	if deviceErr == nil {
		t.Fatal("device accepted a mismatched psk")
	}
	var he *HandshakeError
	if !errors.As(deviceErr, &he) {
		t.Fatalf("device err = %v, want *HandshakeError", deviceErr)
	}

	// The failure is reported to the peer in the clear so it can say something useful.
	if clientErr == nil {
		t.Fatal("client did not observe the rejection")
	}
	if !bytes.Contains([]byte(clientErr.Error()), []byte("MAC failure")) {
		t.Errorf("client err = %v, want it to carry the device's message", clientErr)
	}
}

func TestNoisePlaintextAttemptedSendsReject(t *testing.T) {
	c1, c2 := pipePair(t)

	device := NewNoise(c1, testPSK(), "echolocal", testMAC)
	errc := make(chan error, 1)
	go func() { errc <- device.Handshake() }()

	go func() {
		_, _ = c2.Write([]byte{0x00, 0x05, 0x01})
	}()

	// net.Pipe is unbuffered, so the reject is read while the handshake is still writing it rather
	// than after: reading once Handshake has returned is a deadlock, not a slow test.
	type reply struct {
		body []byte
		err  error
	}
	replies := make(chan reply, 1)
	go func() {
		buf := make([]byte, 64)
		n, err := c2.Read(buf)
		replies <- reply{buf[:n], err}
	}()

	deviceErr := <-errc
	if !errors.Is(deviceErr, ErrPlaintextAttempted) {
		t.Fatalf("device err = %v, want ErrPlaintextAttempted", deviceErr)
	}

	said := <-replies
	if said.err != nil {
		t.Fatalf("client read: %v", said.err)
	}
	got := said.body
	if len(got) == 0 || got[0] != noiseIndicator {
		t.Fatalf("got %v, want packet starting with noiseIndicator (0x01)", got)
	}
	if !bytes.Contains(got, []byte("Bad indicator byte")) {
		t.Fatalf("got %q, want 'Bad indicator byte'", got)
	}
}

func TestNoiseShortPSK(t *testing.T) {
	c1, _ := pipePair(t)

	device := NewNoise(c1, []byte("too short"), "echolocal", testMAC)
	if err := device.Handshake(); err == nil {
		t.Error("expected a short psk to be rejected")
	}
}

func TestNoiseNotReadyBeforeHandshake(t *testing.T) {
	c1, _ := pipePair(t)

	device := NewNoise(c1, testPSK(), "echolocal", testMAC)
	if err := device.Write(Frame{Type: 1}); !errors.Is(err, ErrNotReady) {
		t.Errorf("write err = %v, want ErrNotReady", err)
	}
	if _, err := device.Read(); !errors.Is(err, ErrNotReady) {
		t.Errorf("read err = %v, want ErrNotReady", err)
	}
}

func pipePair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	c1, c2 := net.Pipe()
	t.Cleanup(func() {
		_ = c1.Close()
		_ = c2.Close()
	})
	return c1, c2
}

const testMAC = "a1b2c3d4e5f6"

func TestAnnouncedHelloIsNotSentTwice(t *testing.T) {
	psk := testPSK()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	c2, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	c1, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c1.Close(); _ = c2.Close() })
	_ = c1.SetDeadline(time.Now().Add(5 * time.Second))
	_ = c2.SetDeadline(time.Now().Add(5 * time.Second))

	device := NewNoise(c1, psk, "echolocal", testMAC)
	errc := make(chan error, 1)
	go func() {
		if err := device.Announce(); err != nil {
			errc <- err
			return
		}
		errc <- device.Handshake()
	}()

	client := newTestInitiator(c2)
	if err := client.handshake(psk); err != nil {
		t.Fatalf("client handshake: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("device handshake: %v", err)
	}
	if client.serverMAC != testMAC {
		t.Errorf("hello MAC = %q, want %q", client.serverMAC, testMAC)
	}

	want := Frame{Type: 7, Data: []byte("dialed")}
	go func() { _ = device.Write(want) }()
	got, err := client.read()
	if err != nil || got.Type != want.Type || !bytes.Equal(got.Data, want.Data) {
		t.Fatalf("after an announced handshake got %+v, %v; want %+v", got, err, want)
	}
}

func testPSK() []byte {
	psk := make([]byte, 32)
	for i := range psk {
		psk[i] = byte(i)
	}
	return psk
}

// testInitiator is the Home Assistant side of the handshake, just complete enough to
// exercise the device. net.Pipe is unbuffered, so it alternates strictly.
type testInitiator struct {
	rw         io.ReadWriteCloser
	r          *bufio.Reader
	enc, dec   *noise.CipherState
	serverName string
	serverMAC  string
}

func newTestInitiator(rw io.ReadWriteCloser) *testInitiator {
	return &testInitiator{rw: rw, r: bufio.NewReader(rw)}
}

func (c *testInitiator) handshake(psk []byte) error {
	hs, err := noise.NewHandshakeState(noise.Config{
		CipherSuite:           noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashSHA256),
		Pattern:               noise.HandshakeNN,
		Initiator:             true,
		Prologue:              noisePrologue,
		PresharedKey:          psk,
		PresharedKeyPlacement: 0,
	})
	if err != nil {
		return err
	}

	if err := c.writePacket(nil); err != nil {
		return err
	}

	hello, err := c.readPacket()
	if err != nil {
		return err
	}
	parts := bytes.Split(hello[min(len(hello), 1):], []byte{0x00})
	if len(hello) < 2 || hello[0] != 0x01 || len(parts) != 3 || len(parts[2]) != 0 {
		return errors.New("malformed hello")
	}
	c.serverName, c.serverMAC = string(parts[0]), string(parts[1])

	msg, _, _, err := hs.WriteMessage([]byte{handshakeMsg}, nil)
	if err != nil {
		return err
	}
	if err := c.writePacket(msg); err != nil {
		return err
	}

	body, err := c.readPacket()
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return errors.New("empty handshake reply")
	}
	if body[0] == handshakeErr {
		return &HandshakeError{Msg: string(body[1:])}
	}
	// Initiator side of the same split: cs1 is initiator->responder.
	_, enc, dec, err := hs.ReadMessage(nil, body[1:])
	if err != nil {
		return err
	}
	c.enc, c.dec = enc, dec
	return nil
}

func (c *testInitiator) read() (Frame, error) {
	packet, err := c.readPacket()
	if err != nil {
		return Frame{}, err
	}
	plain, err := c.dec.Decrypt(nil, nil, packet)
	if err != nil {
		return Frame{}, err
	}
	if len(plain) < 4 {
		return Frame{}, ErrShortPayload
	}
	return Frame{Type: be16(plain[0:2]), Data: plain[4:]}, nil
}

func (c *testInitiator) write(f Frame) error {
	buf := make([]byte, 4, 4+len(f.Data))
	putBE16(buf[0:2], f.Type)
	putBE16(buf[2:4], uint32(len(f.Data)))
	buf = append(buf, f.Data...)

	sealed, err := c.enc.Encrypt(nil, nil, buf)
	if err != nil {
		return err
	}
	return c.writePacket(sealed)
}

func (c *testInitiator) readPacket() ([]byte, error) {
	if dl, ok := c.rw.(interface{ SetReadDeadline(time.Time) error }); ok {
		_ = dl.SetReadDeadline(time.Now().Add(5 * time.Second))
	}
	var header [3]byte
	if _, err := io.ReadFull(c.r, header[:]); err != nil {
		return nil, err
	}
	if header[0] != noiseIndicator {
		return nil, ErrBadIndicator
	}
	length := be16(header[1:3])
	if length == 0 {
		return nil, nil
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(c.r, body); err != nil {
		return nil, err
	}
	return body, nil
}

func (c *testInitiator) writePacket(body []byte) error {
	out := make([]byte, 0, 3+len(body))
	out = append(out, noiseIndicator, byte(len(body)>>8), byte(len(body)))
	out = append(out, body...)
	_, err := c.rw.Write(out)
	return err
}
