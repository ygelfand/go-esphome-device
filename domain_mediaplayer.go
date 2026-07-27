package esphomedevice

import (
	"sync"

	"google.golang.org/protobuf/proto"

	"github.com/ygelfand/go-esphome-device/api"
)

type (
	MediaPlayerState   = api.MediaPlayerState
	MediaPlayerCommand = api.MediaPlayerCommand
)

const (
	MediaPlayerIdle       = api.MediaPlayerState_MEDIA_PLAYER_STATE_IDLE
	MediaPlayerPlaying    = api.MediaPlayerState_MEDIA_PLAYER_STATE_PLAYING
	MediaPlayerPaused     = api.MediaPlayerState_MEDIA_PLAYER_STATE_PAUSED
	MediaPlayerAnnouncing = api.MediaPlayerState_MEDIA_PLAYER_STATE_ANNOUNCING
)

// MediaCommand is one media_player command. Only the fields Home Assistant set are
// populated, indicated by the Has* flags.
type MediaCommand struct {
	HasCommand bool
	Command    MediaPlayerCommand

	HasVolume bool
	Volume    float32

	HasMediaURL bool
	MediaURL    string

	// Announcement marks TTS and alerts, which should duck or interrupt rather than
	// replace whatever is playing.
	HasAnnouncement bool
	Announcement    bool
}

type MediaPlayer struct {
	Base
	SupportsPause bool

	OnCommand func(MediaCommand)

	mu     sync.RWMutex
	state_ MediaPlayerState
	volume float32
	muted  bool
}

func (p *MediaPlayer) SetState(s MediaPlayerState) {
	p.mu.Lock()
	changed := p.state_ != s
	p.state_ = s
	p.mu.Unlock()
	if changed {
		p.publish(p.state())
	}
}

func (p *MediaPlayer) SetVolume(v float32) {
	p.mu.Lock()
	changed := p.volume != v
	p.volume = v
	p.mu.Unlock()
	if changed {
		p.publish(p.state())
	}
}

func (p *MediaPlayer) SetMuted(m bool) {
	p.mu.Lock()
	changed := p.muted != m
	p.muted = m
	p.mu.Unlock()
	if changed {
		p.publish(p.state())
	}
}

func (p *MediaPlayer) State() MediaPlayerState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state_
}

func (p *MediaPlayer) Volume() float32 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.volume
}

func (p *MediaPlayer) Muted() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.muted
}

func (p *MediaPlayer) command(m *api.MediaPlayerCommandRequest) {
	cmd := MediaCommand{
		HasCommand:      m.GetHasCommand(),
		Command:         m.GetCommand(),
		HasVolume:       m.GetHasVolume(),
		Volume:          m.GetVolume(),
		HasMediaURL:     m.GetHasMediaUrl(),
		MediaURL:        m.GetMediaUrl(),
		HasAnnouncement: m.GetHasAnnouncement(),
		Announcement:    m.GetAnnouncement(),
	}

	if p.OnCommand != nil {
		p.OnCommand(cmd)
		return
	}

	if cmd.HasVolume {
		p.SetVolume(cmd.Volume)
	}
	if cmd.HasCommand {
		switch cmd.Command {
		case api.MediaPlayerCommand_MEDIA_PLAYER_COMMAND_PLAY:
			p.SetState(MediaPlayerPlaying)
		case api.MediaPlayerCommand_MEDIA_PLAYER_COMMAND_PAUSE:
			p.SetState(MediaPlayerPaused)
		case api.MediaPlayerCommand_MEDIA_PLAYER_COMMAND_STOP:
			p.SetState(MediaPlayerIdle)
		case api.MediaPlayerCommand_MEDIA_PLAYER_COMMAND_MUTE:
			p.SetMuted(true)
		case api.MediaPlayerCommand_MEDIA_PLAYER_COMMAND_UNMUTE:
			p.SetMuted(false)
		}
	}
}

func (p *MediaPlayer) describe() proto.Message {
	return &api.ListEntitiesMediaPlayerResponse{
		ObjectId:          p.ObjectID,
		Key:               p.Key(),
		Name:              p.Name,
		Icon:              p.Icon,
		SupportsPause:     p.SupportsPause,
		EntityCategory:    p.Category,
		DisabledByDefault: p.DisabledByDefault,
	}
}

func (p *MediaPlayer) state() proto.Message {
	// Home Assistant has no mapping for MEDIA_PLAYER_STATE_NONE and raises on it, so the
	// zero value reports as idle.
	st := p.State()
	if st == api.MediaPlayerState_MEDIA_PLAYER_STATE_NONE {
		st = MediaPlayerIdle
	}
	return &api.MediaPlayerStateResponse{
		Key:    p.Key(),
		State:  st,
		Volume: p.Volume(),
		Muted:  p.Muted(),
	}
}
