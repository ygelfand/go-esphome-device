package esphomedevice

import (
	"sync"

	"google.golang.org/protobuf/proto"

	"github.com/ygelfand/go-esphome-device/api"
)

// Sensor is a number Home Assistant reads. Unlike a TextSensor it is recorded and graphed, and with a
// device class Home Assistant will convert it for display — a size reported in kB can be shown in MB
// without the device knowing.
//
// The protocol carries the state as a float32, so a value is exact to 24 bits: report a size in the
// unit it will be read in rather than in bytes, or a large one lands on the nearest representable
// multiple.
type Sensor struct {
	Base

	// Unit is what the number is in, as Home Assistant spells it: "kB", "%", "dB".
	Unit string

	// DeviceClass is what kind of quantity it is, which is what lets Home Assistant convert it.
	DeviceClass string

	// StateClass says how it accumulates. Measurement is the usual answer for anything that goes up
	// and down; the default is none, which keeps it out of long-term statistics.
	StateClass SensorStateClass

	// Decimals is how many places Home Assistant shows.
	Decimals int32

	mu    sync.RWMutex
	value float32
	known bool
}

// SensorStateClass is how a sensor's value accumulates over time.
type SensorStateClass int32

const (
	StateClassNone SensorStateClass = iota
	StateClassMeasurement
	StateClassTotalIncreasing
	StateClassTotal
)

// Set publishes a value.
func (s *Sensor) Set(v float32) {
	s.mu.Lock()
	s.value, s.known = v, true
	s.mu.Unlock()
	s.publish(s.state())
}

func (s *Sensor) Get() float32 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.value
}

func (s *Sensor) describe() proto.Message {
	return &api.ListEntitiesSensorResponse{
		ObjectId:          s.ObjectID,
		Key:               s.Key(),
		Name:              s.Name,
		Icon:              s.Icon,
		UnitOfMeasurement: s.Unit,
		AccuracyDecimals:  s.Decimals,
		DeviceClass:       s.DeviceClass,
		StateClass:        api.SensorStateClass(s.StateClass),
		EntityCategory:    s.Category,
		DisabledByDefault: s.DisabledByDefault,
		DeviceId:          s.DeviceID,
	}
}

// state reports the value, saying so when there has never been one: a sensor that has not measured
// yet is unknown in Home Assistant rather than zero.
func (s *Sensor) state() proto.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return &api.SensorStateResponse{
		Key:          s.Key(),
		State:        s.value,
		MissingState: !s.known,
		DeviceId:     s.DeviceID,
	}
}
