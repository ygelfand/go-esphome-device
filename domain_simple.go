package esphomedevice

import (
	"sync"

	"google.golang.org/protobuf/proto"

	"github.com/ygelfand/go-esphome-device/api"
)

type BinarySensor struct {
	Base
	DeviceClass string

	mu    sync.RWMutex
	value bool
}

func (b *BinarySensor) Set(v bool) {
	b.mu.Lock()
	b.value = v
	b.mu.Unlock()
	b.publish(b.state())
}

func (b *BinarySensor) Get() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.value
}

func (b *BinarySensor) describe() proto.Message {
	return &api.ListEntitiesBinarySensorResponse{
		ObjectId:          b.ObjectID,
		Key:               b.Key(),
		Name:              b.Name,
		Icon:              b.Icon,
		DeviceClass:       b.DeviceClass,
		EntityCategory:    b.Category,
		DisabledByDefault: b.DisabledByDefault,
		DeviceId:          b.DeviceID,
	}
}

func (b *BinarySensor) state() proto.Message {
	return &api.BinarySensorStateResponse{Key: b.Key(), State: b.Get(), DeviceId: b.DeviceID}
}

// TextSensor is a string Home Assistant reads. With no unit or state class its changes appear in the
// logbook, unlike a numeric sensor's. Home Assistant truncates a state at 255 characters.
type TextSensor struct {
	Base
	DeviceClass string

	mu    sync.RWMutex
	value string
}

func (t *TextSensor) Set(v string) {
	t.mu.Lock()
	t.value = v
	t.mu.Unlock()
	t.publish(t.state())
}

func (t *TextSensor) Get() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.value
}

func (t *TextSensor) describe() proto.Message {
	return &api.ListEntitiesTextSensorResponse{
		ObjectId:          t.ObjectID,
		Key:               t.Key(),
		Name:              t.Name,
		Icon:              t.Icon,
		DeviceClass:       t.DeviceClass,
		EntityCategory:    t.Category,
		DisabledByDefault: t.DisabledByDefault,
		DeviceId:          t.DeviceID,
	}
}

func (t *TextSensor) state() proto.Message {
	return &api.TextSensorStateResponse{Key: t.Key(), State: t.Get(), DeviceId: t.DeviceID}
}

// Select is a fixed list of options, used for things like the active wake word.
type Select struct {
	Base
	Options []string

	// OnCommand runs when Home Assistant picks an option. If nil the new value is
	// applied directly; if set, the callback owns whether to call Set.
	OnCommand func(value string)

	mu    sync.RWMutex
	value string
}

func (s *Select) Set(v string) {
	s.mu.Lock()
	s.value = v
	s.mu.Unlock()
	s.publish(s.state())
}

func (s *Select) Get() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.value
}

func (s *Select) command(v string) {
	if s.OnCommand != nil {
		s.OnCommand(v)
		return
	}
	s.Set(v)
}

func (s *Select) describe() proto.Message {
	return &api.ListEntitiesSelectResponse{
		ObjectId:          s.ObjectID,
		Key:               s.Key(),
		Name:              s.Name,
		Icon:              s.Icon,
		Options:           s.Options,
		EntityCategory:    s.Category,
		DisabledByDefault: s.DisabledByDefault,
		DeviceId:          s.DeviceID,
	}
}

func (s *Select) state() proto.Message {
	return &api.SelectStateResponse{Key: s.Key(), State: s.Get(), DeviceId: s.DeviceID}
}

// Number is a tunable scalar, used for thresholds, gain and volume.
// How Home Assistant presents a number. Auto lets it decide, which gives a slider for a short
// range; Box asks for a typed value.
const (
	NumberAuto   = api.NumberMode_NUMBER_MODE_AUTO
	NumberBox    = api.NumberMode_NUMBER_MODE_BOX
	NumberSlider = api.NumberMode_NUMBER_MODE_SLIDER
)

type Number struct {
	Base
	Min, Max, Step float64
	Unit           string
	DeviceClass    string
	Mode           api.NumberMode

	OnCommand func(value float32)

	mu    sync.RWMutex
	value float32
}

func (n *Number) Set(v float32) {
	n.mu.Lock()
	n.value = v
	n.mu.Unlock()
	n.publish(n.state())
}

func (n *Number) Get() float32 {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.value
}

func (n *Number) command(v float32) {
	if n.OnCommand != nil {
		n.OnCommand(v)
		return
	}
	n.Set(v)
}

func (n *Number) describe() proto.Message {
	return &api.ListEntitiesNumberResponse{
		ObjectId:          n.ObjectID,
		Key:               n.Key(),
		Name:              n.Name,
		Icon:              n.Icon,
		MinValue:          float32(n.Min),
		MaxValue:          float32(n.Max),
		Step:              float32(n.Step),
		UnitOfMeasurement: n.Unit,
		DeviceClass:       n.DeviceClass,
		Mode:              n.Mode,
		EntityCategory:    n.Category,
		DisabledByDefault: n.DisabledByDefault,
		DeviceId:          n.DeviceID,
	}
}

func (n *Number) state() proto.Message {
	return &api.NumberStateResponse{Key: n.Key(), State: n.Get(), DeviceId: n.DeviceID}
}

// Button is stateless; Home Assistant only ever presses it.
type Button struct {
	Base
	DeviceClass string
	OnPress     func()
}

func (b *Button) command() {
	if b.OnPress != nil {
		b.OnPress()
	}
}

func (b *Button) describe() proto.Message {
	return &api.ListEntitiesButtonResponse{
		ObjectId:          b.ObjectID,
		Key:               b.Key(),
		Name:              b.Name,
		Icon:              b.Icon,
		DeviceClass:       b.DeviceClass,
		EntityCategory:    b.Category,
		DisabledByDefault: b.DisabledByDefault,
		DeviceId:          b.DeviceID,
	}
}

// Buttons have no state, but the interface needs something to send on a state dump.
// ESPHome omits them entirely, so this is never actually transmitted.
func (b *Button) state() proto.Message { return nil }
