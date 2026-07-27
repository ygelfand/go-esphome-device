package main

import (
	"math"
	"testing"
)

// sample.wav is 32-bit IEEE float, mono, 24 kHz — a format go-audio/wav silently
// misreads, which is why decoding goes through audio-io.
func TestLoadSampleWAV(t *testing.T) {
	pcm, err := loadWAVFile("sample.wav")
	if err != nil {
		t.Fatalf("loadWAV: %v", err)
	}
	if len(pcm)%2 != 0 {
		t.Fatalf("odd byte count %d, not 16-bit samples", len(pcm))
	}

	// 130560 bytes of float32 mono at 24 kHz is 32640 frames, ~1.36s.
	const wantFrames = 32640 * playRate / 24000
	gotFrames := len(pcm) / 2
	if gotFrames != wantFrames {
		t.Errorf("frames = %d, want %d (%.2fs at %d Hz)",
			gotFrames, wantFrames, float64(wantFrames)/playRate, playRate)
	}

	if peak(pcm) < 1000 {
		t.Errorf("peak amplitude %d looks like silence — decode probably wrong", peak(pcm))
	}
}

func TestResampleRates(t *testing.T) {
	in := make([]float64, 2400)
	for i := range in {
		in[i] = math.Sin(float64(i))
	}

	for _, tc := range []struct{ from, want int }{
		{24000, 1600}, {16000, 2400}, {48000, 800}, {8000, 4800},
	} {
		if got := len(resample(in, tc.from, playRate)); got != tc.want {
			t.Errorf("resample from %d: len = %d, want %d", tc.from, got, tc.want)
		}
	}
}

func TestDownmixAveragesChannels(t *testing.T) {
	// Two frames of stereo: (1, -1) and (0.5, 0.5).
	got := downmix([]float64{1, -1, 0.5, 0.5}, 2)
	if len(got) != 2 {
		t.Fatalf("frames = %d, want 2", len(got))
	}
	if got[0] != 0 || got[1] != 0.5 {
		t.Errorf("got %v, want [0 0.5]", got)
	}
}

func peak(pcm []byte) int {
	var m int
	for i := 0; i+1 < len(pcm); i += 2 {
		v := int(int16(uint16(pcm[i]) | uint16(pcm[i+1])<<8))
		if v < 0 {
			v = -v
		}
		m = max(m, v)
	}
	return m
}
