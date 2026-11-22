package esphomedevice

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/ygelfand/go-esphome-device/api"
	"github.com/ygelfand/go-esphome-device/internal/wire"
)

func TestHelloSaysCapabilitiesVersion(t *testing.T) {
	_, peer := startServer(t, nil)
	if resp := peer.hello(); resp.GetApiVersionMinor() < 15 {
		t.Fatalf("api minor = %d: below 1.15 Home Assistant never asks for capabilities", resp.GetApiVersionMinor())
	}
}

func TestCapabilitiesMatchTheDeviceInfo(t *testing.T) {
	_, peer := startServer(t, nil)
	peer.hello()
	peer.send(&api.DeviceCapabilitiesRequest{})
	resp, ok := peer.recv().(*api.DeviceCapabilitiesResponse)
	if !ok {
		t.Fatal("expected DeviceCapabilitiesResponse")
	}
	if resp.GetSendspin() != nil {
		t.Errorf("sendspin = %v, want none for a device without Sendspin", resp.GetSendspin())
	}
	if resp.GetVoiceAssistant().GetFeatureFlags() != 0 || resp.GetBluetoothProxy().GetFeatureFlags() != 0 {
		t.Errorf("flags = %v, want none", resp)
	}
}

// startSendspin is startServer for a voice satellite with a Sendspin client.
func startSendspin(t *testing.T, h Handler, p SendspinPairing) *testPeer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	info := testInfo()
	info.VoiceFeatures = FeatureVoiceAssistant | FeatureSpeaker
	info.BluetoothFeatures = BluetoothPassiveScan
	info.Sendspin = func() SendspinPairing { return p }
	srv := &Server{Info: info, Handler: h, Logger: slog.New(slog.DiscardHandler)}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.Serve(ctx, ln) }()
	nc, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		cancel()
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = nc.Close(); cancel(); <-done })
	return &testPeer{t: t, transport: wire.NewPlaintext(nc), conn: nc, addr: ln.Addr().String()}
}

func TestCapabilitiesCarryTheSendspinClient(t *testing.T) {
	peer := startSendspin(t, nil, SendspinPairing{ClientID: "client-id", Token: "SP:0secret"})
	peer.hello()
	peer.send(&api.DeviceCapabilitiesRequest{})
	resp := peer.recv().(*api.DeviceCapabilitiesResponse)
	if got := resp.GetSendspin(); got.GetFeatureFlags() != SendspinFeaturePairingToken || got.GetClientId() != "client-id" {
		t.Errorf("sendspin = %v", got)
	}
	if resp.GetVoiceAssistant().GetFeatureFlags() != uint32(FeatureVoiceAssistant|FeatureSpeaker) {
		t.Errorf("voice flags = %d", resp.GetVoiceAssistant().GetFeatureFlags())
	}
	if resp.GetBluetoothProxy().GetFeatureFlags() != uint32(BluetoothPassiveScan) {
		t.Errorf("bluetooth flags = %d", resp.GetBluetoothProxy().GetFeatureFlags())
	}
}

func TestSendspinTokenNeedsAProvisionedKey(t *testing.T) {
	peer := startSendspin(t, nil, SendspinPairing{ClientID: "client-id", Token: "SP:0secret"})
	peer.hello()
	peer.send(&api.SendspinPairingTokenRequest{})
	resp := peer.recv().(*api.SendspinPairingTokenResponse)
	if resp.GetStatus() != api.SendspinPairingTokenStatus_SENDSPIN_PAIRING_TOKEN_STATUS_ENCRYPTION_REQUIRED || resp.GetToken() != "" {
		t.Errorf("plaintext: status %v token %q", resp.GetStatus(), resp.GetToken())
	}
}

func TestSendspinTokenWithoutSendspinGoesToTheHandler(t *testing.T) {
	got := make(chan proto.Message, 1)
	_, peer := startServer(t, HandlerFunc(func(_ context.Context, _ *Conn, m proto.Message) error {
		got <- m
		return nil
	}))
	peer.hello()
	peer.send(&api.SendspinPairingTokenRequest{})
	if _, ok := (<-got).(*api.SendspinPairingTokenRequest); !ok {
		t.Fatal("the handler did not get the request")
	}
}

// keyed is a transport encrypted with a provisioned key, or with the zero key.
type keyed struct {
	wire.Transport
	real bool
}

func (k keyed) Keyed() bool { return k.real }

func TestSendspinTokenStatus(t *testing.T) {
	for _, tc := range []struct {
		name  string
		real  bool
		p     SendspinPairing
		want  api.SendspinPairingTokenStatus
		token string
	}{
		{"zero key", false, SendspinPairing{Token: "SP:0secret"}, api.SendspinPairingTokenStatus_SENDSPIN_PAIRING_TOKEN_STATUS_ENCRYPTION_REQUIRED, ""},
		{"ok", true, SendspinPairing{ClientID: "c", Token: "SP:0secret"}, api.SendspinPairingTokenStatus_SENDSPIN_PAIRING_TOKEN_STATUS_OK, "SP:0secret"},
		{"not started", true, SendspinPairing{}, api.SendspinPairingTokenStatus_SENDSPIN_PAIRING_TOKEN_STATUS_NOT_READY, ""},
		{"switched off", true, SendspinPairing{Disabled: true, Token: "SP:0secret"}, api.SendspinPairingTokenStatus_SENDSPIN_PAIRING_TOKEN_STATUS_DISABLED, ""},
	} {
		p := tc.p
		c := newConn(keyed{real: tc.real}, Info{Sendspin: func() SendspinPairing { return p }}, nil, slog.New(slog.DiscardHandler), hooks{})
		r := c.sendspinToken()
		if r.GetStatus() != tc.want || r.GetToken() != tc.token {
			t.Errorf("%s: status %v token %q, want %v %q", tc.name, r.GetStatus(), r.GetToken(), tc.want, tc.token)
		}
	}
}

func TestNoiseKeyed(t *testing.T) {
	var a, b net.Conn
	a, b = net.Pipe()
	defer a.Close()
	defer b.Close()
	if wire.NewNoise(a, make([]byte, 32), "n", "m").Keyed() {
		t.Error("the zero key counts as provisioned")
	}
	if !wire.NewNoise(a, bytes.Repeat([]byte{7}, 32), "n", "m").Keyed() {
		t.Error("a real key does not count as provisioned")
	}
}
