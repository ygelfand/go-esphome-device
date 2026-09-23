package esphomedevice

import (
	"bufio"
	"io"
	"net"
	"time"

	"github.com/ygelfand/go-esphome-device/internal/wire"
)

// Home Assistant adopts an unprovisioned device over two connections: the config flow reads the
// device over plaintext, then the manager opens a zero-PSK Noise one to hand a real key over. An
// unprovisioned server takes whichever arrives, so the first byte decides.
const plaintextFirst = 0x00

// sniffFor is how long a connection has to say which it is. A client that opens a socket and sends
// nothing must not hold a goroutine.
const sniffFor = 10 * time.Second

// sniffed is a connection whose first byte has been read and put back.
type sniffed struct {
	r io.Reader
	c net.Conn
}

func (s sniffed) Read(p []byte) (int, error)  { return s.r.Read(p) }
func (s sniffed) Write(p []byte) (int, error) { return s.c.Write(p) }
func (s sniffed) Close() error                { return s.c.Close() }

// psk is the key in use, which provisioning replaces while connections are being served.
func (s *Server) psk() *PSK {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.provisioned()
}

// provisioned reads the key with the lock already held.
func (s *Server) provisioned() *PSK {
	if s.key != nil {
		return s.key
	}
	return s.PSK
}

// setKey stores the key Home Assistant pushed and takes it into use, so the connection after this
// one is authenticated rather than still unprovisioned.
func (s *Server) setKey(k PSK) error {
	if s.OnSetEncryptionKey != nil {
		if err := s.OnSetEncryptionKey(k); err != nil {
			return err
		}
	}

	s.mu.Lock()
	s.key = &k
	s.mu.Unlock()
	return nil
}

// adopting reports whether this server takes either transport: unprovisioned, and with somewhere to
// put the key that arrives.
func (s *Server) adopting() bool {
	psk := s.psk()
	return psk != nil && psk.IsZero() && s.OnSetEncryptionKey != nil
}

// transport is what to speak on a connection, chosen by the first byte where the server is waiting
// to be adopted and by the key otherwise.
func (s *Server) transport(nc net.Conn) (wire.Transport, error) {
	psk := s.psk()

	if !s.adopting() {
		if psk == nil {
			return wire.NewPlaintext(nc), nil
		}
		return wire.NewNoise(nc, psk[:], s.Info.Name), nil
	}

	if err := nc.SetReadDeadline(time.Now().Add(sniffFor)); err != nil {
		return nil, err
	}

	r := bufio.NewReader(nc)
	first, err := r.Peek(1)
	if err != nil {
		return nil, err
	}
	if err := nc.SetReadDeadline(time.Time{}); err != nil {
		return nil, err
	}

	conn := sniffed{r: r, c: nc}
	if first[0] == plaintextFirst {
		return wire.NewPlaintext(conn), nil
	}
	return wire.NewNoise(conn, psk[:], s.Info.Name), nil
}
