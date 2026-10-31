package esphomedevice

import (
	"context"
	"math/rand/v2"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ygelfand/go-esphome-device/internal/wire"
)

// Home Assistant listens for device-initiated connections on this port.
const DefaultDialPort = 6054

const (
	dialBackoffMin = 5 * time.Second
	dialBackoffMax = 5 * time.Minute
	dialTimeout    = 10 * time.Second
	dialHello      = 15 * time.Second
	dialRecheck    = 5 * time.Second
	dialTick       = time.Second
)

type Dial struct {
	Port  int
	Delay time.Duration
	Load  func() (netip.Addr, bool)
	Save  func(netip.Addr) error
}

type dialer struct {
	s   *Server
	cfg Dial

	mu      sync.Mutex
	target  netip.Addr
	saved   bool
	targets int
	dialed  bool
	next    time.Time
	backoff time.Duration
}

func newDialer(s *Server, cfg Dial) *dialer {
	d := &dialer{s: s, cfg: cfg, backoff: dialBackoffMin, next: time.Now().Add(cfg.Delay)}
	if cfg.Load != nil {
		if a, ok := cfg.Load(); ok && a.IsValid() {
			d.target, d.saved = a, true
		}
	}
	return d
}

func (d *dialer) run(ctx context.Context, wg *sync.WaitGroup) {
	t := time.NewTicker(dialTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			addr, ok := d.due(now)
			if !ok {
				continue
			}
			nc, err := (&net.Dialer{Timeout: dialTimeout}).DialContext(ctx, "tcp", addr)
			if err != nil {
				d.s.logger().Debug("dialing home assistant failed", "addr", addr, "err", err)
				d.retry(time.Now())
				continue
			}
			d.s.logger().Debug("dialed home assistant", "addr", addr)
			wg.Go(func() { d.done(d.s.serveDialed(ctx, nc)) })
		}
	}
}

func (d *dialer) due(now time.Time) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.targets > 0 || d.dialed {
		d.next = time.Time{}
		return "", false
	}
	if d.next.IsZero() {
		d.next = now.Add(d.cfg.Delay)
		return "", false
	}
	if now.Before(d.next) {
		return "", false
	}
	psk := d.s.psk()
	if !d.target.IsValid() || psk == nil || psk.IsZero() {
		d.next = now.Add(dialRecheck)
		return "", false
	}
	d.dialed = true
	port := d.cfg.Port
	if port == 0 {
		port = DefaultDialPort
	}
	return net.JoinHostPort(d.target.String(), strconv.Itoa(port)), true
}

func (d *dialer) retry(now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dialed = false
	d.scheduleRetry(now)
}

func (d *dialer) scheduleRetry(now time.Time) {
	span := d.backoff / 5
	d.next = now.Add(d.backoff - span + rand.N(2*span+1))
	d.backoff = min(d.backoff*2, dialBackoffMax)
}

func (d *dialer) done(authenticated bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dialed = false
	if authenticated {
		d.backoff = dialBackoffMin
		return
	}
	d.scheduleRetry(time.Now())
}

func (d *dialer) arrived(peer net.Addr) {
	ap, err := netip.ParseAddrPort(peer.String())
	d.mu.Lock()
	d.targets++
	d.next, d.backoff = time.Time{}, dialBackoffMin
	if err != nil {
		d.mu.Unlock()
		d.s.logger().Warn("not remembering the dial target", "peer", peer.String(), "err", err)
		return
	}
	addr := ap.Addr().Unmap()
	if d.saved && addr == d.target {
		d.mu.Unlock()
		return
	}
	d.target = addr
	d.mu.Unlock()
	if d.cfg.Save == nil {
		return
	}
	if err := d.cfg.Save(addr); err != nil {
		d.s.logger().Warn("saving the dial target failed", "addr", addr, "err", err)
		return
	}
	d.mu.Lock()
	d.saved = d.target == addr
	d.mu.Unlock()
	d.s.logger().Info("remembered the dial target", "addr", addr)
}

func (d *dialer) left() {
	d.mu.Lock()
	d.targets--
	d.mu.Unlock()
}

func helloMAC(mac string) string {
	return strings.ToLower(strings.NewReplacer(":", "", "-", "").Replace(mac))
}

func (s *Server) serveDialed(ctx context.Context, nc net.Conn) bool {
	psk := s.psk()
	if psk == nil || psk.IsZero() {
		_ = nc.Close()
		return false
	}
	unheard := time.AfterFunc(dialHello, func() { _ = nc.Close() })
	defer unheard.Stop()
	if s.WriteTimeout > 0 {
		nc = bounded{Conn: nc, d: s.WriteTimeout}
	}
	n := wire.NewNoise(nc, psk[:], s.Info.Name, helloMAC(s.Info.MACAddress))
	if err := n.Announce(); err != nil {
		_ = nc.Close()
		return false
	}
	return s.serveTransport(ctx, nc, n, func() { unheard.Stop() })
}
