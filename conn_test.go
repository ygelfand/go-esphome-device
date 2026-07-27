package esphomedevice

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/ygelfand/go-esphome-device/api"
	"github.com/ygelfand/go-esphome-device/internal/wire"
)

func testInfo() Info {
	return Info{
		Name:          "echolocal-kitchen",
		FriendlyName:  "Kitchen Echo",
		MACAddress:    "aa:bb:cc:dd:ee:ff",
		Manufacturer:  "Amazon",
		Model:         "Echo Dot 2",
		Version:       "test",
		SuggestedArea: "Kitchen",
	}
}

// startServer runs a plaintext server on a loopback port and returns a client speaking
// the same transport. Noise is covered in internal/wire.
func startServer(t *testing.T, h Handler) (*Server, *testPeer) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	srv := &Server{
		Info:    testInfo(),
		Handler: h,
		Logger:  slog.New(slog.DiscardHandler),
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Serve(ctx, ln)
	}()

	nc, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		cancel()
		t.Fatalf("dial: %v", err)
	}

	t.Cleanup(func() {
		_ = nc.Close()
		cancel()
		<-done
	})

	return srv, &testPeer{t: t, transport: wire.NewPlaintext(nc), conn: nc}
}

type testPeer struct {
	t         *testing.T
	transport wire.Transport
	conn      net.Conn
}

func (p *testPeer) send(msg proto.Message) {
	p.t.Helper()
	info, ok := Lookup(msg)
	if !ok {
		p.t.Fatalf("no id for %T", msg)
	}
	data, err := proto.Marshal(msg)
	if err != nil {
		p.t.Fatalf("marshal: %v", err)
	}
	if err := p.transport.Write(wire.Frame{Type: info.ID, Data: data}); err != nil {
		p.t.Fatalf("write: %v", err)
	}
}

func (p *testPeer) recv() proto.Message {
	p.t.Helper()
	_ = p.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	frame, err := p.transport.Read()
	if err != nil {
		p.t.Fatalf("read: %v", err)
	}
	msg, ok := NewMessage(frame.Type)
	if !ok {
		p.t.Fatalf("unknown message id %d", frame.Type)
	}
	if err := proto.Unmarshal(frame.Data, msg); err != nil {
		p.t.Fatalf("unmarshal: %v", err)
	}
	return msg
}

func (p *testPeer) hello() *api.HelloResponse {
	p.t.Helper()
	p.send(&api.HelloRequest{
		ClientInfo:      "test client",
		ApiVersionMajor: APIVersionMajor,
		ApiVersionMinor: APIVersionMinor,
	})
	resp, ok := p.recv().(*api.HelloResponse)
	if !ok {
		p.t.Fatal("expected HelloResponse")
	}
	return resp
}

func TestHelloHandshake(t *testing.T) {
	_, peer := startServer(t, nil)

	resp := peer.hello()
	if resp.GetName() != "echolocal-kitchen" {
		t.Errorf("name = %q, want echolocal-kitchen", resp.GetName())
	}
	if resp.GetApiVersionMajor() != APIVersionMajor {
		t.Errorf("api major = %d, want %d", resp.GetApiVersionMajor(), APIVersionMajor)
	}
	if resp.GetServerInfo() == "" {
		t.Error("server_info should not be empty")
	}
}

func TestDeviceInfo(t *testing.T) {
	_, peer := startServer(t, nil)
	peer.hello()

	peer.send(&api.DeviceInfoRequest{})
	resp, ok := peer.recv().(*api.DeviceInfoResponse)
	if !ok {
		t.Fatal("expected DeviceInfoResponse")
	}

	want := testInfo()
	if resp.GetName() != want.Name {
		t.Errorf("name = %q, want %q", resp.GetName(), want.Name)
	}
	if resp.GetFriendlyName() != want.FriendlyName {
		t.Errorf("friendly_name = %q, want %q", resp.GetFriendlyName(), want.FriendlyName)
	}
	if resp.GetMacAddress() != want.MACAddress {
		t.Errorf("mac = %q, want %q", resp.GetMacAddress(), want.MACAddress)
	}
	if resp.GetSuggestedArea() != want.SuggestedArea {
		t.Errorf("suggested_area = %q, want %q", resp.GetSuggestedArea(), want.SuggestedArea)
	}
	if !resp.GetApiEncryptionSupported() {
		t.Error("api_encryption_supported should be true")
	}
}

func TestPing(t *testing.T) {
	_, peer := startServer(t, nil)
	peer.hello()

	peer.send(&api.PingRequest{})
	if _, ok := peer.recv().(*api.PingResponse); !ok {
		t.Fatal("expected PingResponse")
	}
}

func TestDisconnectClosesConnection(t *testing.T) {
	_, peer := startServer(t, nil)
	peer.hello()

	peer.send(&api.DisconnectRequest{})
	if _, ok := peer.recv().(*api.DisconnectResponse); !ok {
		t.Fatal("expected DisconnectResponse")
	}

	_ = peer.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := peer.transport.Read(); err == nil {
		t.Error("expected the server to close after disconnect")
	} else if err != io.EOF {
		t.Logf("closed with %v", err)
	}
}

func TestBroadcastRequiresSubscription(t *testing.T) {
	srv, peer := startServer(t, nil)
	peer.hello()

	// Nothing subscribed yet, so this must not reach anyone.
	if err := srv.Broadcast(&api.PingRequest{}); err != nil {
		t.Fatalf("broadcast: %v", err)
	}

	peer.send(&api.SubscribeStatesRequest{})

	// Wait for the server to record the subscription before broadcasting.
	deadline := time.Now().Add(2 * time.Second)
	for {
		srv.mu.Lock()
		var subscribed bool
		for c := range srv.conns {
			subscribed = subscribed || c.StatesSubscribed()
		}
		srv.mu.Unlock()
		if subscribed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("subscription never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}

	if err := srv.Broadcast(&api.PingRequest{}); err != nil {
		t.Fatalf("broadcast: %v", err)
	}
	if _, ok := peer.recv().(*api.PingRequest); !ok {
		t.Fatal("expected the broadcast to arrive")
	}
}

func TestHandlerReceivesUnhandledMessages(t *testing.T) {
	got := make(chan proto.Message, 4)
	h := HandlerFunc(func(_ context.Context, _ *Conn, msg proto.Message) error {
		got <- msg
		return nil
	})

	_, peer := startServer(t, h)
	peer.hello()
	peer.send(&api.ListEntitiesRequest{})

	select {
	case msg := <-got:
		if _, ok := msg.(*api.ListEntitiesRequest); !ok {
			t.Errorf("handler got %T, want *api.ListEntitiesRequest", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handler never saw the message")
	}
}

func TestLifecycleMessagesBypassHandler(t *testing.T) {
	got := make(chan proto.Message, 4)
	h := HandlerFunc(func(_ context.Context, _ *Conn, msg proto.Message) error {
		got <- msg
		return nil
	})

	_, peer := startServer(t, h)
	peer.hello()
	peer.send(&api.PingRequest{})
	peer.recv()

	select {
	case msg := <-got:
		t.Errorf("handler should not see lifecycle messages, got %T", msg)
	case <-time.After(200 * time.Millisecond):
	}
}
