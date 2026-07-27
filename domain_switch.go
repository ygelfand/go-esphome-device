package esphomedevice

import (
	"sync"

	"google.golang.org/protobuf/proto"

	"github.com/ygelfand/go-esphome-device/api"
)

// Switch is a boolean Home Assistant can both read and set, for state the device owns but the
// user can change from either end — a microphone mute being the obvious one.
type Switch struct {
	Base
	DeviceClass string

	// Assumed tells Home Assistant the device cannot confirm the state, so it should offer
	// separate on and off controls rather than a toggle.
	Assumed bool

	// OnCommand runs when Home Assistant sets the switch. If nil the new value is applied
	// directly; if set, the callback owns whether to call Set.
	OnCommand func(value bool)

	mu    sync.RWMutex
	value bool
}

func (s *Switch) Set(v bool) {
	s.mu.Lock()
	changed := s.value != v
	s.value = v
	s.mu.Unlock()
	if changed {
		s.publish(s.state())
	}
}

func (s *Switch) Get() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.value
}

func (s *Switch) command(v bool) {
	if s.OnCommand != nil {
		s.OnCommand(v)
		return
	}
	s.Set(v)
}

func (s *Switch) describe() proto.Message {
	return &api.ListEntitiesSwitchResponse{
		ObjectId:          s.ObjectID,
		Key:               s.Key(),
		Name:              s.Name,
		Icon:              s.Icon,
		DeviceClass:       s.DeviceClass,
		AssumedState:      s.Assumed,
		EntityCategory:    s.Category,
		DisabledByDefault: s.DisabledByDefault,
	}
}

func (s *Switch) state() proto.Message {
	return &api.SwitchStateResponse{Key: s.Key(), State: s.Get()}
}
