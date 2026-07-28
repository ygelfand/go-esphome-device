package esphomedevice

import (
	"slices"

	"google.golang.org/protobuf/proto"

	"github.com/ygelfand/go-esphome-device/api"
)

// Event is something that happens rather than something with a state, such as a button press.
// Home Assistant records each occurrence and its type, so automations can trigger on them.
type Event struct {
	Base
	DeviceClass string

	// Types are the event types this entity can emit. Home Assistant rejects any type not
	// declared here.
	Types []string
}

// Trigger reports that an event of this type happened. Unknown types are ignored, since Home
// Assistant would reject them anyway.
func (e *Event) Trigger(eventType string) {
	if !slices.Contains(e.Types, eventType) {
		return
	}
	e.publish(&api.EventResponse{Key: e.Key(), EventType: eventType})
}

func (e *Event) describe() proto.Message {
	return &api.ListEntitiesEventResponse{
		ObjectId:          e.ObjectID,
		Key:               e.Key(),
		Name:              e.Name,
		Icon:              e.Icon,
		DeviceClass:       e.DeviceClass,
		EventTypes:        e.Types,
		EntityCategory:    e.Category,
		DisabledByDefault: e.DisabledByDefault,
		DeviceId:          e.DeviceID,
	}
}

// state returns nil: an event has no state to restore, only occurrences.
func (e *Event) state() proto.Message { return nil }
