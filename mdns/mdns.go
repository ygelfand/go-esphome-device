// Package mdns advertises a device over mDNS so Home Assistant discovers it.
//
// Import it only if you want advertisement; nothing in the core library depends on it.
// Home Assistant can always be pointed at a host directly, and multicast is unreliable
// enough on Wi-Fi that nothing should require it.
package mdns

import (
	"fmt"
	"net"
	"strings"

	"github.com/libp2p/zeroconf/v2"
)

const (
	Service = "_esphomelib._tcp"
	Domain  = "local."

	// NoiseEncryption is the value Home Assistant looks for to know the API speaks
	// Noise. The key it appears under depends on provisioning state.
	NoiseEncryption = "Noise_NNpsk0_25519_ChaChaPoly_SHA256"
)

// Config describes the advertisement. Name must match the device name reported over the
// API, since Home Assistant correlates the two.
type Config struct {
	Name         string
	FriendlyName string
	Port         int
	MACAddress   string
	Version      string
	Platform     string
	Board        string
	Network      string

	// Encrypted selects between the api_encryption and api_encryption_supported TXT
	// keys, which is how Home Assistant decides whether to prompt for a key.
	Encrypted bool

	// Provisionable advertises that an unprovisioned device accepts a zero-PSK Noise
	// connection for provisioning. Only meaningful when Encrypted is false.
	Provisionable bool

	// IPs are the addresses to advertise. Left empty, the host's interface addresses
	// are used — never a name lookup, which would stall trying to resolve our own
	// unpublished .local name.
	IPs []net.IP
}

type Advertiser struct {
	server *zeroconf.Server
}

func Advertise(cfg Config) (*Advertiser, error) {
	if cfg.Name == "" {
		return nil, fmt.Errorf("mdns: Name is required")
	}
	if cfg.Port == 0 {
		return nil, fmt.Errorf("mdns: Port is required")
	}

	ips := cfg.IPs
	if len(ips) == 0 {
		var err error
		if ips, err = interfaceIPs(); err != nil {
			return nil, err
		}
	}

	srv, err := zeroconf.RegisterProxy(
		cfg.Name,
		Service,
		Domain,
		cfg.Port,
		cfg.Name,
		ipStrings(ips),
		txtRecords(cfg),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("mdns: register: %w", err)
	}
	return &Advertiser{server: srv}, nil
}

func ipStrings(ips []net.IP) []string {
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	return out
}

func (a *Advertiser) Close() error {
	if a == nil || a.server == nil {
		return nil
	}
	a.server.Shutdown()
	return nil
}

// interfaceIPs returns the host's unicast addresses, preferring routable ones and
// falling back to loopback so a local test still advertises something.
func interfaceIPs() ([]net.IP, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, fmt.Errorf("mdns: interface addresses: %w", err)
	}

	var routable, loopback []net.IP
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLinkLocalUnicast() {
			continue
		}
		if ipnet.IP.IsLoopback() {
			loopback = append(loopback, ipnet.IP)
			continue
		}
		routable = append(routable, ipnet.IP)
	}

	if len(routable) > 0 {
		return routable, nil
	}
	if len(loopback) > 0 {
		return loopback, nil
	}
	return nil, fmt.Errorf("mdns: no usable interface addresses")
}

func txtRecords(cfg Config) []string {
	var txt []string
	add := func(k, v string) {
		if v != "" {
			txt = append(txt, k+"="+v)
		}
	}

	add("friendly_name", cfg.FriendlyName)
	add("version", cfg.Version)
	add("mac", strings.ToLower(strings.NewReplacer(":", "", "-", "").Replace(cfg.MACAddress)))
	add("platform", cfg.Platform)
	add("board", cfg.Board)
	add("network", cfg.Network)

	if cfg.Encrypted {
		add("api_encryption", NoiseEncryption)
	} else {
		add("api_encryption_supported", NoiseEncryption)
		if cfg.Provisionable {
			add("api_provisioning", "zero-psk")
		}
	}
	return txt
}
