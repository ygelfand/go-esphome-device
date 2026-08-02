package esphomedevice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"google.golang.org/protobuf/proto"

	"github.com/ygelfand/go-esphome-device/api"
	"github.com/ygelfand/go-esphome-device/internal/wire"
)

// Handler receives messages the connection does not answer itself. Returning messages
// from Handle sends them back on the same connection.
//
// Handle runs on the connection's read loop, so blocking work belongs in a goroutine.
// Messages are delivered in order, so the library cannot dispatch them concurrently.
type Handler interface {
	Handle(ctx context.Context, c *Conn, msg proto.Message) error
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc func(ctx context.Context, c *Conn, msg proto.Message) error

func (f HandlerFunc) Handle(ctx context.Context, c *Conn, msg proto.Message) error {
	return f(ctx, c, msg)
}

var ErrDisconnect = errors.New("esphomedevice: client disconnected")

// Conn is a single Home Assistant connection. Writes are safe from multiple goroutines;
// the ESPHome transports carry a per-direction nonce, so frames must not interleave.
type Conn struct {
	transport wire.Transport
	info      Info
	handler   Handler
	log       *slog.Logger
	hooks     hooks

	wmu sync.Mutex

	mu             sync.Mutex
	clientInfo     string
	statesSubbed   bool
	logsSubbed     bool
	logLevel       api.LogLevel
	helloCompleted bool
}

// hooks are the server's callbacks, handed down so a connection can reach them without holding the
// server itself. They run on the connection's read loop and must not block it.
type hooks struct {
	setKey     func(PSK) error
	subscribed func()
}

func newConn(t wire.Transport, info Info, h Handler, log *slog.Logger, hk hooks) *Conn {
	return &Conn{transport: t, info: info, handler: h, log: log, hooks: hk}
}

// setEncryptionKey handles Home Assistant provisioning a real key. The new key applies
// to future connections; this one keeps its established cipher state.
func (c *Conn) setEncryptionKey(raw []byte) bool {
	if c.hooks.setKey == nil {
		c.log.Warn("refusing encryption key provisioning: no handler configured")
		return false
	}
	if len(raw) != 32 {
		c.log.Warn("rejecting encryption key", "len", len(raw))
		return false
	}

	var k PSK
	copy(k[:], raw)
	if k.IsZero() {
		c.log.Warn("rejecting all-zeros key: reserved to mark a device unprovisioned")
		return false
	}
	if err := c.hooks.setKey(k); err != nil {
		c.log.Error("storing encryption key", "err", err)
		return false
	}
	c.log.Info("encryption key provisioned, applies on next connection")
	return true
}

// ClientInfo reports the client's self-description from HelloRequest, e.g. "Home Assistant".
func (c *Conn) ClientInfo() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.clientInfo
}

// StatesSubscribed reports whether the client asked for entity state updates. Pushing
// state before then is wasted work.
func (c *Conn) StatesSubscribed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.statesSubbed
}

// LogsSubscribed reports whether the client asked for logs at all, whatever the level.
func (c *Conn) LogsSubscribed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.logsSubbed
}

// WantsLog reports whether the client asked for log lines at this level or quieter. The
// levels count upwards from none, so a client that asked for INFO is not sent DEBUG.
func (c *Conn) WantsLog(level api.LogLevel) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.logsSubbed && level <= c.logLevel
}

// Send encodes and writes one message.
func (c *Conn) Send(msg proto.Message) error {
	info, ok := Lookup(msg)
	if !ok {
		return fmt.Errorf("esphomedevice: %T has no api.proto id", msg)
	}
	data, err := proto.Marshal(msg)
	if err != nil {
		return err
	}

	c.wmu.Lock()
	defer c.wmu.Unlock()
	return c.transport.Write(wire.Frame{Type: info.ID, Data: data})
}

func (c *Conn) Close() error { return c.transport.Close() }

func (c *Conn) serve(ctx context.Context) error {
	if err := c.transport.Handshake(); err != nil {
		return err
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		frame, err := c.transport.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		msg, ok := NewMessage(frame.Type)
		if !ok {
			// Unknown IDs are expected when Home Assistant is newer than our proto.
			c.log.Debug("ignoring unknown message", "id", frame.Type)
			continue
		}
		if err := proto.Unmarshal(frame.Data, msg); err != nil {
			return fmt.Errorf("esphomedevice: decoding id %d: %w", frame.Type, err)
		}

		if err := c.dispatch(ctx, msg); err != nil {
			if errors.Is(err, ErrDisconnect) {
				return nil
			}
			return err
		}
	}
}

// dispatch answers the connection-lifecycle messages itself and passes everything else
// to the handler.
func (c *Conn) dispatch(ctx context.Context, msg proto.Message) error {
	switch m := msg.(type) {
	case *api.HelloRequest:
		return c.onHello(m)

	case *api.DeviceInfoRequest:
		return c.Send(c.deviceInfo())

	case *api.PingRequest:
		return c.Send(&api.PingResponse{})

	case *api.DisconnectRequest:
		if err := c.Send(&api.DisconnectResponse{}); err != nil {
			return err
		}
		return ErrDisconnect

	case *api.SubscribeStatesRequest:
		c.mu.Lock()
		c.statesSubbed = true
		c.mu.Unlock()

		if c.hooks.subscribed != nil {
			c.hooks.subscribed()
		}
		return c.handle(ctx, msg)

	case *api.SubscribeLogsRequest:
		c.mu.Lock()
		c.logsSubbed = true
		c.logLevel = m.GetLevel()
		c.mu.Unlock()
		return nil

	case *api.NoiseEncryptionSetKeyRequest:
		return c.Send(&api.NoiseEncryptionSetKeyResponse{Success: c.setEncryptionKey(m.GetKey())})

	default:
		return c.handle(ctx, msg)
	}
}

func (c *Conn) handle(ctx context.Context, msg proto.Message) error {
	if c.handler == nil {
		return nil
	}
	return c.handler.Handle(ctx, c, msg)
}

func (c *Conn) onHello(m *api.HelloRequest) error {
	c.mu.Lock()
	c.clientInfo = m.GetClientInfo()
	c.helloCompleted = true
	c.mu.Unlock()

	c.log.Debug("hello",
		"client", m.GetClientInfo(),
		"api", fmt.Sprintf("%d.%d", m.GetApiVersionMajor(), m.GetApiVersionMinor()))

	return c.Send(&api.HelloResponse{
		ApiVersionMajor: APIVersionMajor,
		ApiVersionMinor: APIVersionMinor,
		ServerInfo:      c.info.Name + " (go-esphome-device)",
		Name:            c.info.Name,
	})
}

func (c *Conn) deviceInfo() *api.DeviceInfoResponse {
	var devices []*api.DeviceInfo
	for _, d := range c.info.Devices {
		devices = append(devices, &api.DeviceInfo{DeviceId: d.ID, Name: d.Name, AreaId: d.AreaID})
	}

	return &api.DeviceInfoResponse{
		Devices:                    devices,
		Name:                       c.info.Name,
		FriendlyName:               c.info.FriendlyName,
		MacAddress:                 c.info.MACAddress,
		Manufacturer:               c.info.Manufacturer,
		Model:                      c.info.Model,
		EsphomeVersion:             c.info.Version,
		SuggestedArea:              c.info.SuggestedArea,
		ApiEncryptionSupported:     true,
		VoiceAssistantFeatureFlags: uint32(c.info.VoiceFeatures),
		BluetoothProxyFeatureFlags: uint32(c.info.BluetoothFeatures),
	}
}
