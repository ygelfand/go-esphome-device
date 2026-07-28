// Package esphomedevice implements the device side of the ESPHome native API, so a Go
// program can present itself to Home Assistant as an ESPHome device.
package esphomedevice

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// API version we advertise. Major mismatches make Home Assistant disconnect; minor
// mismatches only warn.
const (
	APIVersionMajor = 1
	APIVersionMinor = 12
)

const DefaultPort = 6053

// Info is what a device reports in response to DeviceInfoRequest. Name is also the
// hostname Home Assistant identifies the device by, so it must be stable.
type Info struct {
	Name          string
	FriendlyName  string
	MACAddress    string
	Manufacturer  string
	Model         string
	Version       string
	SuggestedArea string

	// VoiceFeatures advertises voice satellite capability. Leave zero on devices that
	// aren't satellites; Home Assistant won't offer voice for them.
	VoiceFeatures VoiceFeature

	// Devices are sub-devices entities can be assigned to, by Base.DeviceID. Home Assistant shows
	// each as its own device page under this one.
	Devices []Device
}

// Device is a sub-device. ID must be non-zero and unique, since zero means the device itself.
type Device struct {
	ID     uint32
	Name   string
	AreaID uint32
}

func (i Info) validate() error {
	if i.Name == "" {
		return errors.New("esphomedevice: Info.Name is required")
	}
	if len(i.Name) > 31 {
		return fmt.Errorf("esphomedevice: Info.Name %q exceeds 31 bytes", i.Name)
	}
	return nil
}

// PSK is the 32-byte Noise pre-shared key. Possession of it is the only identity the
// protocol has, so it must be unique per device. All-zeros is reserved to mark a device
// unprovisioned — see Unprovisioned.
type PSK [32]byte

// Unprovisioned returns the reserved all-zeros key. The connection is still Noise, but
// anyone can complete the handshake: passive-sniffing protection, no authentication.
func Unprovisioned() *PSK { return &PSK{} }

// IsZero reports whether this is the reserved unprovisioned key.
func (k PSK) IsZero() bool {
	var acc byte
	for _, b := range k {
		acc |= b
	}
	return acc == 0
}

func GeneratePSK() (PSK, error) {
	var k PSK
	_, err := rand.Read(k[:])
	return k, err
}

// ParsePSK accepts the base64 form Home Assistant asks for.
func ParsePSK(s string) (PSK, error) {
	var k PSK
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return k, fmt.Errorf("esphomedevice: psk is not valid base64: %w", err)
	}
	if len(raw) != 32 {
		return k, fmt.Errorf("esphomedevice: psk must decode to 32 bytes, got %d", len(raw))
	}
	copy(k[:], raw)
	return k, nil
}

func (k PSK) String() string { return base64.StdEncoding.EncodeToString(k[:]) }
