package esphomedevice

import (
	"sync"

	"google.golang.org/protobuf/proto"

	"github.com/ygelfand/go-esphome-device/api"
)

type EntityCategory = api.EntityCategory

const (
	CategoryNone       = api.EntityCategory_ENTITY_CATEGORY_NONE
	CategoryConfig     = api.EntityCategory_ENTITY_CATEGORY_CONFIG
	CategoryDiagnostic = api.EntityCategory_ENTITY_CATEGORY_DIAGNOSTIC
)

// Entity is one Home Assistant entity exposed by the device. Implementations live in
// this package; describe and state are unexported so the set stays closed.
type Entity interface {
	Key() uint32
	describe() proto.Message
	state() proto.Message
}

// Base carries the fields every entity domain shares.
type Base struct {
	// ObjectID is the stable identifier the entity key is derived from. Changing it
	// makes Home Assistant treat the entity as a new one.
	ObjectID string

	Name              string
	Icon              string
	Category          EntityCategory
	DisabledByDefault bool

	mu     sync.Mutex
	notify func(proto.Message)
}

func (b *Base) Key() uint32 { return fnv1(b.ObjectID) }

func (b *Base) setNotifier(fn func(proto.Message)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.notify = fn
}

func (b *Base) publish(msg proto.Message) {
	b.mu.Lock()
	fn := b.notify
	b.mu.Unlock()
	if fn != nil {
		fn(msg)
	}
}

// fnv1 matches ESPHome's object-id hash, so keys line up with a real device's.
func fnv1(s string) uint32 {
	const (
		offsetBasis uint32 = 2166136261
		prime       uint32 = 16777619
	)
	h := offsetBasis
	for i := range len(s) {
		h *= prime
		h ^= uint32(s[i])
	}
	return h
}
