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

// MediaPlayerFeature is what the player tells Home Assistant it can do, sent as feature_flags.
// Home Assistant computes the entity's supported features from this, so a player that sends none
// gets no controls in the UI. The bits are Home Assistant's MediaPlayerEntityFeature values.
type MediaPlayerFeature uint32

const (
	MediaPlayerFeaturePause MediaPlayerFeature = 1 << iota
	MediaPlayerFeatureSeek
	MediaPlayerFeatureVolumeSet
	MediaPlayerFeatureVolumeMute
	MediaPlayerFeaturePreviousTrack
	MediaPlayerFeatureNextTrack
	_ // Home Assistant leaves this bit unused.
	MediaPlayerFeatureTurnOn
	MediaPlayerFeatureTurnOff
	MediaPlayerFeaturePlayMedia
	MediaPlayerFeatureVolumeStep
	MediaPlayerFeatureSelectSource
	MediaPlayerFeatureStop
	MediaPlayerFeatureClearPlaylist
	MediaPlayerFeaturePlay
	MediaPlayerFeatureShuffleSet
	MediaPlayerFeatureSelectSound
	MediaPlayerFeatureBrowseMedia
	MediaPlayerFeatureRepeatSet
	MediaPlayerFeatureGrouping
	MediaPlayerFeatureAnnounce
	MediaPlayerFeatureEnqueue
)

// The commands Home Assistant can send.
const (
	MediaPlayerPlay       = api.MediaPlayerCommand_MEDIA_PLAYER_COMMAND_PLAY
	MediaPlayerPause      = api.MediaPlayerCommand_MEDIA_PLAYER_COMMAND_PAUSE
	MediaPlayerStop       = api.MediaPlayerCommand_MEDIA_PLAYER_COMMAND_STOP
	MediaPlayerMute       = api.MediaPlayerCommand_MEDIA_PLAYER_COMMAND_MUTE
	MediaPlayerUnmute     = api.MediaPlayerCommand_MEDIA_PLAYER_COMMAND_UNMUTE
	MediaPlayerVolumeUp   = api.MediaPlayerCommand_MEDIA_PLAYER_COMMAND_VOLUME_UP
	MediaPlayerVolumeDown = api.MediaPlayerCommand_MEDIA_PLAYER_COMMAND_VOLUME_DOWN
	MediaPlayerToggle     = api.MediaPlayerCommand_MEDIA_PLAYER_COMMAND_TOGGLE
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

// MediaFormat is audio the device can play. Home Assistant converts a source to the first format
// matching the purpose, with ffmpeg, so advertising something simple avoids needing a decoder.
type MediaFormat struct {
	// Format is a container or codec name as ffmpeg knows it, such as "wav" or "flac".
	Format      string
	SampleRate  uint32
	Channels    uint32
	SampleBytes uint32

	// Announcement marks this as the format for announcements rather than ordinary media.
	Announcement bool
}

type MediaPlayer struct {
	Base
	SupportsPause bool

	// SupportedFormats is what Home Assistant should convert to. With none, it sends whatever the
	// source happens to be.
	SupportedFormats []MediaFormat

	// Features is what Home Assistant offers in the UI. Pause is added from SupportsPause, which
	// older versions read instead of the flags.
	Features MediaPlayerFeature

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
		DeviceId:          p.DeviceID,
		FeatureFlags:      uint32(p.features()),
		SupportedFormats:  p.formats(),
	}
}

func (p *MediaPlayer) formats() []*api.MediaPlayerSupportedFormat {
	out := make([]*api.MediaPlayerSupportedFormat, 0, len(p.SupportedFormats))
	for _, f := range p.SupportedFormats {
		purpose := api.MediaPlayerFormatPurpose_MEDIA_PLAYER_FORMAT_PURPOSE_DEFAULT
		if f.Announcement {
			purpose = api.MediaPlayerFormatPurpose_MEDIA_PLAYER_FORMAT_PURPOSE_ANNOUNCEMENT
		}
		out = append(out, &api.MediaPlayerSupportedFormat{
			Format:      f.Format,
			SampleRate:  f.SampleRate,
			NumChannels: f.Channels,
			SampleBytes: f.SampleBytes,
			Purpose:     purpose,
		})
	}
	return out
}

func (p *MediaPlayer) features() MediaPlayerFeature {
	f := p.Features
	if p.SupportsPause {
		f |= MediaPlayerFeaturePause
	}
	return f
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
