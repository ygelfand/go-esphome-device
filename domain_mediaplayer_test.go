package esphomedevice

import (
	"testing"

	"github.com/ygelfand/go-esphome-device/api"
)

// Home Assistant raises KeyError on MEDIA_PLAYER_STATE_NONE, so the zero value must
// never reach the wire.
func TestMediaPlayerZeroValueReportsIdle(t *testing.T) {
	p := &MediaPlayer{Base: Base{ObjectID: "media"}}

	msg, ok := p.state().(*api.MediaPlayerStateResponse)
	if !ok {
		t.Fatal("expected MediaPlayerStateResponse")
	}
	if msg.GetState() != MediaPlayerIdle {
		t.Errorf("state = %v, want IDLE", msg.GetState())
	}
}

func TestMediaPlayerExplicitStatesPassThrough(t *testing.T) {
	p := &MediaPlayer{Base: Base{ObjectID: "media"}}
	for _, want := range []MediaPlayerState{MediaPlayerPlaying, MediaPlayerPaused, MediaPlayerAnnouncing} {
		p.SetState(want)
		msg := p.state().(*api.MediaPlayerStateResponse)
		if msg.GetState() != want {
			t.Errorf("state = %v, want %v", msg.GetState(), want)
		}
	}
}
