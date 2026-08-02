package esphomedevice

import (
	"context"
	"sync"

	"google.golang.org/protobuf/proto"

	"github.com/ygelfand/go-esphome-device/api"
)

// BluetoothFeature is what a proxy tells Home Assistant it can do, in Info.BluetoothFeatures.
type BluetoothFeature uint32

const (
	BluetoothPassiveScan BluetoothFeature = 1 << iota
	BluetoothActiveConnections
	BluetoothRemoteCaching
	BluetoothPairing
	BluetoothCacheClearing
	BluetoothRawAdvertisements
	BluetoothStateAndMode
	BluetoothConnectionParams
)

// ScannerState is what the radio is doing.
type ScannerState = api.BluetoothScannerState

const (
	ScannerIdle     = api.BluetoothScannerState_BLUETOOTH_SCANNER_STATE_IDLE
	ScannerStarting = api.BluetoothScannerState_BLUETOOTH_SCANNER_STATE_STARTING
	ScannerRunning  = api.BluetoothScannerState_BLUETOOTH_SCANNER_STATE_RUNNING
	ScannerFailed   = api.BluetoothScannerState_BLUETOOTH_SCANNER_STATE_FAILED
	ScannerStopping = api.BluetoothScannerState_BLUETOOTH_SCANNER_STATE_STOPPING
	ScannerStopped  = api.BluetoothScannerState_BLUETOOTH_SCANNER_STATE_STOPPED
)

// BluetoothProxy forwards BLE advertisements to Home Assistant.
//
// Use it as a Handler, usually via Chain alongside Entities. Scanning belongs to the caller: this
// carries what the radio found and never touches hardware.
type BluetoothProxy struct {
	// OnSubscribed fires when Home Assistant starts or stops wanting advertisements.
	OnSubscribed func(subscribed bool)

	// OnMode fires when Home Assistant chooses active or passive scanning. Active asks the
	// controller to request scan responses, which is a transmission rather than a listen.
	OnMode func(active bool)

	mu     sync.Mutex
	conn   *Conn
	active bool
	state  ScannerState
}

// Subscribed reports whether Home Assistant is listening.
func (b *BluetoothProxy) Subscribed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.conn != nil
}

// Active reports whether Home Assistant asked for active scanning.
func (b *BluetoothProxy) Active() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.active
}

// Advertise sends a batch of reports, and gives up the subscription if the connection has gone:
// nothing tells a handler that a client disappeared, so a failed write is the notice.
func (b *BluetoothProxy) Advertise(ads []*api.BluetoothLERawAdvertisement) error {
	if len(ads) == 0 {
		return nil
	}

	b.mu.Lock()
	conn := b.conn
	b.mu.Unlock()

	if conn == nil {
		return ErrNoSubscriber
	}

	err := conn.Send(&api.BluetoothLERawAdvertisementsResponse{Advertisements: ads})
	if err != nil {
		b.forget(conn)
	}
	return err
}

// Report tells Home Assistant what the radio is doing.
func (b *BluetoothProxy) Report(state ScannerState) error {
	b.mu.Lock()
	b.state = state
	conn, mode := b.conn, b.mode()
	b.mu.Unlock()

	if conn == nil {
		return ErrNoSubscriber
	}
	return conn.Send(&api.BluetoothScannerStateResponse{State: state, Mode: mode, ConfiguredMode: mode})
}

func (b *BluetoothProxy) Handle(_ context.Context, c *Conn, msg proto.Message) error {
	switch m := msg.(type) {
	case *api.SubscribeBluetoothLEAdvertisementsRequest:
		return b.subscribe(c, true)

	case *api.UnsubscribeBluetoothLEAdvertisementsRequest:
		return b.subscribe(c, false)

	case *api.BluetoothScannerSetModeRequest:
		return b.setMode(c, m.GetMode() == api.BluetoothScannerMode_BLUETOOTH_SCANNER_MODE_ACTIVE)

	// Answered even though connections are not offered: silence leaves Home Assistant waiting.
	case *api.SubscribeBluetoothConnectionsFreeRequest:
		return c.Send(&api.BluetoothConnectionsFreeResponse{})
	}
	return nil
}

func (b *BluetoothProxy) subscribe(c *Conn, want bool) error {
	b.mu.Lock()
	was := b.conn != nil
	switch {
	case want:
		b.conn = c
	case b.conn == c:
		b.conn = nil
	}
	now, notify, state, mode := b.conn != nil, b.OnSubscribed, b.state, b.mode()
	b.mu.Unlock()

	if now != was && notify != nil {
		notify(now)
	}
	if now {
		return c.Send(&api.BluetoothScannerStateResponse{State: state, Mode: mode, ConfiguredMode: mode})
	}
	return nil
}

func (b *BluetoothProxy) setMode(c *Conn, active bool) error {
	b.mu.Lock()
	changed := b.active != active
	b.active = active
	notify, state, mode := b.OnMode, b.state, b.mode()
	b.mu.Unlock()

	if changed && notify != nil {
		notify(active)
	}
	return c.Send(&api.BluetoothScannerStateResponse{State: state, Mode: mode, ConfiguredMode: mode})
}

// mode is the current scanning mode. Held with mu.
func (b *BluetoothProxy) mode() api.BluetoothScannerMode {
	if b.active {
		return api.BluetoothScannerMode_BLUETOOTH_SCANNER_MODE_ACTIVE
	}
	return api.BluetoothScannerMode_BLUETOOTH_SCANNER_MODE_PASSIVE
}

func (b *BluetoothProxy) forget(c *Conn) {
	b.mu.Lock()
	gone := b.conn == c
	if gone {
		b.conn = nil
	}
	notify := b.OnSubscribed
	b.mu.Unlock()

	if gone && notify != nil {
		notify(false)
	}
}
