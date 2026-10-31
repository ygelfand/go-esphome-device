package esphomedevice

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestTheDeviceDialsItsRememberedTarget(t *testing.T) {
	home, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = home.Close() }()
	port := home.Addr().(*net.TCPAddr).Port

	psk, err := GeneratePSK()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Info: Info{Name: "kitchen", MACAddress: "A1:B2:C3:D4:E5:F6"},
		PSK:  &psk,
		Dial: &Dial{
			Port:  port,
			Delay: 10 * time.Millisecond,
			Load:  func() (netip.Addr, bool) { return netip.MustParseAddr("127.0.0.1"), true },
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- s.Serve(ctx, ln) }()
	defer func() {
		cancel()
		if err := <-served; err != nil {
			t.Errorf("serve: %v", err)
		}
	}()

	if err := home.(*net.TCPListener).SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	c, err := home.Accept()
	if err != nil {
		t.Fatalf("the device never dialed: %v", err)
	}
	defer func() { _ = c.Close() }()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))

	head := make([]byte, 3)
	if _, err := io.ReadFull(c, head); err != nil {
		t.Fatal(err)
	}
	body := make([]byte, int(head[1])<<8|int(head[2]))
	if _, err := io.ReadFull(c, body); err != nil {
		t.Fatal(err)
	}
	want := append([]byte{0x01}, []byte("kitchen\x00a1b2c3d4e5f6\x00")...)
	if head[0] != 0x01 || !bytes.Equal(body, want) {
		t.Errorf("first frame %x %q, want the server hello %q", head[0], body, want)
	}
}
