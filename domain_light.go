package esphomedevice

import (
	"sync"

	"google.golang.org/protobuf/proto"

	"github.com/ygelfand/go-esphome-device/api"
)

type ColorMode = api.ColorMode

const (
	ColorModeOnOff      = api.ColorMode_COLOR_MODE_ON_OFF
	ColorModeBrightness = api.ColorMode_COLOR_MODE_BRIGHTNESS
	ColorModeRGB        = api.ColorMode_COLOR_MODE_RGB
)

// LightState is the full light state. Colour channels and brightness are 0..1.
type LightState struct {
	On         bool
	Brightness float32
	ColorMode  ColorMode
	Red        float32
	Green      float32
	Blue       float32
	Effect     string

	// TransitionLength is what Home Assistant asked for, in milliseconds. Only set on
	// commands; the device decides whether to honour it.
	TransitionLength uint32
}

// Light is an RGB light. For EchoLocal this is the LED ring, which the device also
// drives from satellite state — see the precedence note on OnCommand.
type Light struct {
	Base
	SupportedColorModes []ColorMode
	Effects             []string

	// OnCommand runs when Home Assistant changes the light. If nil the command is
	// applied directly. When the device drives the ring itself, the callback is where
	// precedence between satellite state and user control gets resolved.
	OnCommand func(LightState)

	mu    sync.RWMutex
	value LightState
}

func (l *Light) Set(s LightState) {
	l.mu.Lock()
	l.value = s
	l.mu.Unlock()
	l.publish(l.state())
}

func (l *Light) Get() LightState {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.value
}

// command folds a partial command onto current state. Every field is optional and
// flagged by its has_* companion, so unflagged fields must be left alone.
func (l *Light) command(m *api.LightCommandRequest) {
	next := l.Get()

	if m.GetHasState() {
		next.On = m.GetState()
	}
	if m.GetHasBrightness() {
		next.Brightness = m.GetBrightness()
	}
	if m.GetHasColorMode() {
		next.ColorMode = m.GetColorMode()
	}
	if m.GetHasRgb() {
		next.Red, next.Green, next.Blue = m.GetRed(), m.GetGreen(), m.GetBlue()
	}
	if m.GetHasEffect() {
		next.Effect = m.GetEffect()
	}
	if m.GetHasTransitionLength() {
		next.TransitionLength = m.GetTransitionLength()
	}

	if l.OnCommand != nil {
		l.OnCommand(next)
		return
	}
	l.Set(next)
}

func (l *Light) describe() proto.Message {
	modes := l.SupportedColorModes
	if len(modes) == 0 {
		modes = []ColorMode{ColorModeRGB}
	}
	return &api.ListEntitiesLightResponse{
		ObjectId:            l.ObjectID,
		Key:                 l.Key(),
		Name:                l.Name,
		Icon:                l.Icon,
		SupportedColorModes: modes,
		Effects:             l.Effects,
		EntityCategory:      l.Category,
		DisabledByDefault:   l.DisabledByDefault,
		DeviceId:            l.DeviceID,
	}
}

func (l *Light) state() proto.Message {
	s := l.Get()
	mode := s.ColorMode
	if mode == api.ColorMode_COLOR_MODE_UNKNOWN {
		mode = ColorModeRGB
	}
	return &api.LightStateResponse{
		Key:             l.Key(),
		DeviceId:        l.DeviceID,
		State:           s.On,
		Brightness:      s.Brightness,
		ColorMode:       mode,
		ColorBrightness: s.Brightness,
		Red:             s.Red,
		Green:           s.Green,
		Blue:            s.Blue,
		Effect:          s.Effect,
	}
}
