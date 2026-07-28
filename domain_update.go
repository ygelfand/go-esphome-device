package esphomedevice

import (
	"sync"

	"google.golang.org/protobuf/proto"

	"github.com/ygelfand/go-esphome-device/api"
)

// UpdateState is the firmware state reported to Home Assistant. An update is offered
// when LatestVersion differs from CurrentVersion.
type UpdateState struct {
	CurrentVersion string
	LatestVersion  string
	Title          string
	ReleaseSummary string
	ReleaseURL     string

	InProgress bool
	Progress   float32
}

// Update is a firmware update entity. ESPHome devices expose one even when they never
// self-update, and Home Assistant's satellite setup wizard expects it.
type Update struct {
	Base
	DeviceClass string

	// OnCommand runs when Home Assistant asks to install or check for an update.
	OnCommand func(api.UpdateCommand)

	mu    sync.RWMutex
	value UpdateState
}

func (u *Update) Set(s UpdateState) {
	u.mu.Lock()
	u.value = s
	u.mu.Unlock()
	u.publish(u.state())
}

func (u *Update) Get() UpdateState {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.value
}

func (u *Update) command(m *api.UpdateCommandRequest) {
	if u.OnCommand != nil {
		u.OnCommand(m.GetCommand())
	}
}

func (u *Update) describe() proto.Message {
	return &api.ListEntitiesUpdateResponse{
		ObjectId:          u.ObjectID,
		Key:               u.Key(),
		Name:              u.Name,
		Icon:              u.Icon,
		DeviceClass:       u.DeviceClass,
		EntityCategory:    u.Category,
		DisabledByDefault: u.DisabledByDefault,
	}
}

func (u *Update) state() proto.Message {
	s := u.Get()
	return &api.UpdateStateResponse{
		Key:            u.Key(),
		DeviceId:       u.DeviceID,
		CurrentVersion: s.CurrentVersion,
		LatestVersion:  s.LatestVersion,
		Title:          s.Title,
		ReleaseSummary: s.ReleaseSummary,
		ReleaseUrl:     s.ReleaseURL,
		InProgress:     s.InProgress,
		HasProgress:    s.InProgress,
		Progress:       s.Progress,
	}
}
