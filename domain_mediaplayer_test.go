package esphomedevice

import (
	"testing"

	"github.com/ygelfand/go-esphome-device/api"
)

// The flags are Home Assistant's MediaPlayerEntityFeature values, and iota makes the unused bit
// easy to misplace, so the ones we rely on are pinned.
func TestMediaPlayerFeatureValues(t *testing.T) {
	cases := []struct {
		feature MediaPlayerFeature
		want    uint32
	}{
		{MediaPlayerFeaturePause, 1},
		{MediaPlayerFeatureVolumeSet, 4},
		{MediaPlayerFeatureVolumeMute, 8},
		{MediaPlayerFeatureTurnOn, 128},
		{MediaPlayerFeaturePlayMedia, 512},
		{MediaPlayerFeatureVolumeStep, 1024},
		{MediaPlayerFeatureStop, 4096},
		{MediaPlayerFeaturePlay, 16384},
		{MediaPlayerFeatureAnnounce, 1048576},
	}
	for _, c := range cases {
		if uint32(c.feature) != c.want {
			t.Errorf("feature = %d, want %d", c.feature, c.want)
		}
	}
}

// A player that sends no flags gets no controls in Home Assistant, and SupportsPause has to reach
// the flags as well as its own field.
func TestMediaPlayerDescribeCarriesFeatures(t *testing.T) {
	p := &MediaPlayer{
		Base:          Base{ObjectID: "media"},
		SupportsPause: true,
		Features:      MediaPlayerFeatureVolumeSet | MediaPlayerFeatureVolumeMute,
	}

	msg, ok := p.describe().(*api.ListEntitiesMediaPlayerResponse)
	if !ok {
		t.Fatal("expected ListEntitiesMediaPlayerResponse")
	}
	want := uint32(MediaPlayerFeatureVolumeSet | MediaPlayerFeatureVolumeMute | MediaPlayerFeaturePause)
	if msg.GetFeatureFlags() != want {
		t.Errorf("feature_flags = %d, want %d", msg.GetFeatureFlags(), want)
	}
	if !msg.GetSupportsPause() {
		t.Error("supports_pause = false, want true")
	}
}

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
