package esphomedevice

import (
	"net"
	"testing"

	"github.com/ygelfand/go-esphome-device/internal/wire"
)

func unprovisioned() *Server {
	return &Server{
		Info:               Info{Name: "echolocal"},
		PSK:                Unprovisioned(),
		OnSetEncryptionKey: func(PSK) error { return nil },
	}
}

// chose is the transport a server picks for a client that opens with this byte.
func chose(t *testing.T, s *Server, first byte) wire.Transport {
	t.Helper()

	c1, c2 := net.Pipe()
	t.Cleanup(func() {
		_ = c1.Close()
		_ = c2.Close()
	})

	go func() { _, _ = c2.Write([]byte{first}) }()

	got, err := s.transport(c1)
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	return got
}

// Home Assistant reads the device over plaintext to add it, then opens a zero-PSK Noise connection
// to hand a key over, so an unprovisioned server has to take both.
func TestUnprovisionedTakesEitherTransport(t *testing.T) {
	s := unprovisioned()

	if got := chose(t, s, 0x00); !isPlaintext(got) {
		t.Errorf("a plaintext client got %T", got)
	}
	if got := chose(t, s, 0x01); isPlaintext(got) {
		t.Errorf("a noise client got %T", got)
	}
}

// Once a key is in, the device is nobody's to adopt: plaintext is refused and the key is required.
func TestProvisioningEndsAdoption(t *testing.T) {
	s := unprovisioned()
	if !s.adopting() {
		t.Fatal("a server with the zero key is not offering itself for adoption")
	}

	var key PSK
	key[0] = 0x2a
	if err := s.setKey(key); err != nil {
		t.Fatal(err)
	}

	if s.adopting() {
		t.Error("still offering itself for adoption after a key was pushed")
	}
	if got := s.psk(); got == nil || *got != key {
		t.Errorf("the server is serving %v, want the pushed key", got)
	}
	if got := chose(t, s, 0x00); isPlaintext(got) {
		t.Error("a plaintext client is still served after provisioning")
	}
}

// The key Home Assistant pushes has to reach whatever stores it, not only the server.
func TestProvisioningReachesTheHandler(t *testing.T) {
	var stored PSK
	s := unprovisioned()
	s.OnSetEncryptionKey = func(k PSK) error { stored = k; return nil }

	var key PSK
	key[31] = 0x07
	if err := s.setKey(key); err != nil {
		t.Fatal(err)
	}
	if stored != key {
		t.Errorf("the handler was given %v", stored)
	}
}

func isPlaintext(tr wire.Transport) bool {
	_, ok := tr.(*wire.Plaintext)
	return ok
}
