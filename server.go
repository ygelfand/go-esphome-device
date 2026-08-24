package esphomedevice

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/ygelfand/go-esphome-device/api"
	"github.com/ygelfand/go-esphome-device/internal/wire"
)

type Server struct {
	Info Info

	// PSK selects the transport, and the three options serve different purposes:
	//
	//   a real key       authenticated Noise — production
	//   Unprovisioned()  Noise with the reserved zero key, so Home Assistant can push a
	//                    real one; needs aioesphomeapi 45.6.0 or newer
	//   nil              plaintext — readable on the wire, so useful for debugging,
	//                    but ESPHome is removing it in 2027.2.0
	PSK *PSK

	// OnSetEncryptionKey handles Home Assistant provisioning a real key over a zero-PSK
	// connection. Leave nil to refuse, which is right when keys are installed
	// out-of-band.
	OnSetEncryptionKey func(PSK) error

	// OnSubscribed fires when a client asks for entity state, which is the first moment
	// anything sent to it will arrive. Something the device wants to report but could not
	// while it was alone waits for this.
	//
	// It fires once per connection, on that connection's read loop, so it must not block.
	OnSubscribed func()

	// WriteTimeout bounds one write. Zero leaves writes unbounded, so a client that stops reading
	// blocks the write lock until TCP gives up and every other sender waits behind it.
	WriteTimeout time.Duration

	// Addr defaults to ":6053".
	Addr string

	Handler Handler
	Logger  *slog.Logger

	mu       sync.Mutex
	listener net.Listener
	conns    map[*Conn]struct{}
}

func (s *Server) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

func (s *Server) addr() string {
	if s.Addr != "" {
		return s.Addr
	}
	return ":" + strconv.Itoa(DefaultPort)
}

// ListenAndServe accepts connections until ctx is cancelled or Close is called.
func (s *Server) ListenAndServe(ctx context.Context) error {
	if err := s.Info.validate(); err != nil {
		return err
	}

	ln, err := net.Listen("tcp", s.addr())
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.listener = ln
	s.conns = map[*Conn]struct{}{}
	s.mu.Unlock()

	return s.Serve(ctx, ln)
}

// serverBinder lets a handler wire itself up for broadcasting without the caller having
// to remember an extra call.
type serverBinder interface{ bindServer(*Server) }

func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	log := s.logger()

	if b, ok := s.Handler.(serverBinder); ok {
		b.bindServer(s)
	}
	switch {
	case s.PSK == nil:
		log.Warn("plaintext api: no encryption, no authentication")
	case s.PSK.IsZero():
		log.Warn("unprovisioned: zero-psk accepts any client")
	}
	log.Info("listening", "addr", ln.Addr().String(), "name", s.Info.Name)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Closing the listener alone is not enough: connection goroutines sit in a blocking
	// read, so shutdown would wait on them forever.
	go func() {
		<-ctx.Done()
		_ = ln.Close()
		s.closeConns()
	}()

	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		nc, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}

		wg.Go(func() { s.serveConn(ctx, nc) })
	}
}

// bounded gives every write its own deadline, which one SetWriteDeadline cannot: a deadline is a
// point in time, not a per-call budget.
type bounded struct {
	net.Conn
	d time.Duration
}

func (b bounded) Write(p []byte) (int, error) {
	if err := b.SetWriteDeadline(time.Now().Add(b.d)); err != nil {
		return 0, err
	}
	n, err := b.Conn.Write(p)
	if err != nil {
		// A half-written frame desynchronises the transport's nonce, so the connection cannot be
		// reused. Closing it also stops every later write inheriting a fresh deadline.
		_ = b.Conn.Close()
	}
	return n, err
}

func (s *Server) serveConn(ctx context.Context, nc net.Conn) {
	log := s.logger().With("peer", nc.RemoteAddr().String())
	if s.WriteTimeout > 0 {
		nc = bounded{Conn: nc, d: s.WriteTimeout}
	}

	var transport wire.Transport
	if s.PSK != nil {
		transport = wire.NewNoise(nc, s.PSK[:], s.Info.Name)
	} else {
		transport = wire.NewPlaintext(nc)
	}

	c := newConn(transport, s.Info, s.Handler, log, hooks{
		setKey:     s.OnSetEncryptionKey,
		subscribed: s.OnSubscribed,
	})

	s.track(c, true)
	defer s.track(c, false)
	defer func() { _ = c.Close() }()

	if err := c.serve(ctx); err != nil && !errors.Is(err, context.Canceled) {
		if errors.Is(err, wire.ErrPlaintextAttempted) {
			s.logPlaintextMismatch(log)
			return
		}
		log.Info("connection closed", "err", err)
		return
	}
	log.Debug("connection closed")
}

func (s *Server) logPlaintextMismatch(log *slog.Logger) {
	if s.PSK != nil && s.PSK.IsZero() {
		log.Error("client connected in plaintext but this device is unprovisioned (zero-psk Noise). " +
			"Home Assistant needs aioesphomeapi 45.6.0+ for zero-psk provisioning; " +
			"otherwise set a real key, or run plaintext explicitly")
		return
	}
	log.Error("client connected in plaintext but this device requires Noise; check the key in Home Assistant")
}

// Reconnect drops every client, so Home Assistant reconnects and reads the device afresh.
//
// Some of what a device reports is only fetched once per connection — the voice satellite's
// available wake words among it — and there is no message for revising it. When that changes, the
// connection is the only lever: Home Assistant reconnects on its own, within seconds.
func (s *Server) Reconnect() { s.closeConns() }

func (s *Server) closeConns() {
	s.mu.Lock()
	conns := make([]*Conn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()

	for _, c := range conns {
		_ = c.Close()
	}
}

func (s *Server) track(c *Conn, add bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conns == nil {
		s.conns = map[*Conn]struct{}{}
	}
	if add {
		s.conns[c] = struct{}{}
	} else {
		delete(s.conns, c)
	}
}

// Broadcast sends a message to every connection that has subscribed to state updates,
// which is how entity state is pushed.
func (s *Server) Broadcast(msg proto.Message) error {
	s.mu.Lock()
	conns := make([]*Conn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()

	var errs []error
	for _, c := range conns {
		if !c.StatesSubscribed() {
			continue
		}
		if err := c.Send(msg); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", c.ClientInfo(), err))
		}
	}
	return errors.Join(errs...)
}

// FireEvent asks Home Assistant to put an event on its bus. The name is passed whole and must be
// under the esphome domain, which is the only one Home Assistant accepts here.
//
// Values are strings because the wire carries no other kind. Home Assistant adds the device id
// itself, so an event says which device it came from without being told.
func (s *Server) FireEvent(name string, data map[string]string) error {
	fields := make([]*api.HomeassistantServiceMap, 0, len(data))
	for key, value := range data {
		fields = append(fields, &api.HomeassistantServiceMap{Key: key, Value: value})
	}

	return s.Broadcast(&api.HomeassistantActionRequest{
		Service: name,
		IsEvent: true,
		Data:    fields,
	})
}

// LogsSubscribed reports whether any client is taking logs. Lines produced before one is have
// nowhere to go, so a caller holding a backlog can wait rather than discard it.
func (s *Server) LogsSubscribed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for c := range s.conns {
		if c.LogsSubscribed() {
			return true
		}
	}
	return false
}

// Log sends one line to every client that subscribed at this level or louder. A write that fails is
// left to the read loop to notice, since logging is not worth failing anything else over.
func (s *Server) Log(level api.LogLevel, line string) {
	s.mu.Lock()
	conns := make([]*Conn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()

	var msg *api.SubscribeLogsResponse
	for _, c := range conns {
		if !c.WantsLog(level) {
			continue
		}
		if msg == nil {
			msg = &api.SubscribeLogsResponse{Level: level, Message: []byte(line)}
		}
		_ = c.Send(msg)
	}
}

func (s *Server) Close() error {
	s.mu.Lock()
	ln := s.listener
	conns := make([]*Conn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()

	var errs []error
	if ln != nil {
		errs = append(errs, ln.Close())
	}
	for _, c := range conns {
		errs = append(errs, c.Close())
	}
	return errors.Join(errs...)
}
