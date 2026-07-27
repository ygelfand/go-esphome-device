package main

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/jonchammer/audio-io/core"
	"github.com/jonchammer/audio-io/wave"
)

// sampleWAV is used when -speak is not given, so the simulator can say something
// without any setup.
//
//go:embed sample.wav
var sampleWAV []byte

// loadWAV reads any uncompressed WAVE file and returns 16 kHz mono signed 16-bit PCM,
// which is what the voice pipeline expects.
func loadWAVFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return loadWAV(f)
}

func loadWAV(src io.ReadSeeker) ([]byte, error) {
	r := wave.NewReader(src)
	header, err := r.Header()
	if err != nil {
		return nil, err
	}

	samples, err := normalized(r, header)
	if err != nil {
		return nil, err
	}

	mono := downmix(samples, int(header.ChannelCount()))
	mono = resample(mono, int(header.FrameRate()), playRate)

	pcm := make([]byte, len(mono)*2)
	for i, s := range mono {
		clamped := math.Max(-1, math.Min(1, s))
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(int16(clamped*math.MaxInt16)))
	}
	return pcm, nil
}

// normalized dequantizes whatever the file holds into float64 in -1..1.
func normalized(r *wave.Reader, header *wave.Header) ([]float64, error) {
	sampleType, err := header.SampleType()
	if err != nil {
		return nil, err
	}
	n := int(header.SampleCount())

	switch sampleType {
	case wave.SampleTypeUint8:
		d := make([]uint8, n)
		if _, err := r.ReadUint8(d); err != nil {
			return nil, err
		}
		return core.DequantizeUint8(d), nil
	case wave.SampleTypeInt16:
		d := make([]int16, n)
		if _, err := r.ReadInt16(d); err != nil {
			return nil, err
		}
		return core.DequantizeInt16(d), nil
	case wave.SampleTypeInt24:
		d := make([]int32, n)
		if _, err := r.ReadInt24(d); err != nil {
			return nil, err
		}
		return core.DequantizeInt24(d), nil
	case wave.SampleTypeInt32:
		d := make([]int32, n)
		if _, err := r.ReadInt32(d); err != nil {
			return nil, err
		}
		return core.DequantizeInt32(d), nil
	case wave.SampleTypeFloat32:
		d := make([]float32, n)
		if _, err := r.ReadFloat32(d); err != nil {
			return nil, err
		}
		return core.DequantizeFloat32(d), nil
	case wave.SampleTypeFloat64:
		d := make([]float64, n)
		if _, err := r.ReadFloat64(d); err != nil {
			return nil, err
		}
		return d, nil
	}
	return nil, fmt.Errorf("unsupported sample type %s", sampleType)
}

func downmix(interleaved []float64, channels int) []float64 {
	if channels <= 1 {
		return interleaved
	}
	frames := len(interleaved) / channels
	out := make([]float64, frames)
	for i := range frames {
		var sum float64
		for c := range channels {
			sum += interleaved[i*channels+c]
		}
		out[i] = sum / float64(channels)
	}
	return out
}

// resample is nearest neighbour, which is plenty for feeding speech to STT.
func resample(in []float64, from, to int) []float64 {
	if from == to || from == 0 {
		return in
	}
	out := make([]float64, len(in)*to/from)
	for i := range out {
		src := i * from / to
		if src >= len(in) {
			break
		}
		out[i] = in[src]
	}
	return out
}
