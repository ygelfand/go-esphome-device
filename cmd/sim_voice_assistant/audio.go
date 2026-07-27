package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/hajimehoshi/go-mp3"
)

// oto allows one context per process, so everything is converted to the voice
// pipeline's native format rather than opening a context per stream.
const (
	playRate     = 16000
	playChannels = 1
)

// soundEnabled is set once from -no-sound.
var soundEnabled = true

var (
	otoOnce sync.Once
	otoCtx  *oto.Context
	otoErr  error
)

// playAudio sniffs MP3 versus raw PCM. Announcements arrive as MP3; the voice pipeline
// path is logged so a format change is visible rather than silent.
func playAudio(log *slog.Logger, data []byte) error {
	if !soundEnabled || len(data) == 0 {
		return nil
	}
	if len(data) >= 3 && (string(data[:3]) == "ID3" || (data[0] == 0xFF && data[1]&0xE0 == 0xE0)) {
		log.Debug("playing mp3", "bytes", len(data))
		return playMP3(data)
	}
	log.Debug("playing pcm", "bytes", len(data))
	return playPCM(data)
}

func audioContext() (*oto.Context, error) {
	otoOnce.Do(func() {
		var ready chan struct{}
		otoCtx, ready, otoErr = oto.NewContext(&oto.NewContextOptions{
			SampleRate:   playRate,
			ChannelCount: playChannels,
			Format:       oto.FormatSignedInt16LE,
		})
		if otoErr == nil {
			<-ready
		}
	})
	return otoCtx, otoErr
}

// playPCM plays signed 16-bit little-endian mono samples at playRate.
func playPCM(pcm []byte) error {
	if !soundEnabled {
		return nil
	}
	ctx, err := audioContext()
	if err != nil {
		return err
	}

	p := ctx.NewPlayer(bytes.NewReader(pcm))
	p.Play()
	for p.IsPlaying() {
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

func playMP3(data []byte) error {
	d, err := mp3.NewDecoder(bytes.NewReader(data))
	if err != nil {
		return err
	}
	raw, err := io.ReadAll(d)
	if err != nil {
		return err
	}
	return playPCM(toMonoPlayRate(raw, d.SampleRate()))
}

// toMonoPlayRate downmixes 16-bit stereo and resamples by nearest neighbour, which is
// plenty for hearing whether the pipeline works.
func toMonoPlayRate(stereo []byte, rate int) []byte {
	frames := len(stereo) / 4
	mono := make([]int16, frames)
	for i := range frames {
		l := int16(binary.LittleEndian.Uint16(stereo[i*4:]))
		r := int16(binary.LittleEndian.Uint16(stereo[i*4+2:]))
		mono[i] = int16((int32(l) + int32(r)) / 2)
	}

	if rate == playRate {
		out := make([]byte, len(mono)*2)
		for i, s := range mono {
			binary.LittleEndian.PutUint16(out[i*2:], uint16(s))
		}
		return out
	}

	outFrames := frames * playRate / rate
	out := make([]byte, outFrames*2)
	for i := range outFrames {
		src := i * rate / playRate
		if src >= len(mono) {
			break
		}
		binary.LittleEndian.PutUint16(out[i*2:], uint16(mono[src]))
	}
	return out
}
