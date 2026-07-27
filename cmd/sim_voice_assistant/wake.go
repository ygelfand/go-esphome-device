package main

import (
	"bufio"
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	esphome "github.com/ygelfand/go-esphome-device"
	"golang.org/x/term"
)

const (
	wakePhrase = "Hey Jarvis"

	// silence is a sentinel speakPath meaning "send silence, not speech".
	silence = "\x00silence"
)

// console drives the device by hand, since there is no microphone. On a terminal it
// reads single keys with echo off, so keystrokes do not litter the log; piped input
// falls back to lines.
func console(log *slog.Logger, sat *esphome.VoiceSatellite, speakPath string, quit func()) {
	fd := int(os.Stdin.Fd())

	if !term.IsTerminal(fd) {
		for sc := bufio.NewScanner(os.Stdin); sc.Scan(); {
			if !dispatchKey(sat, speakPath, strings.ToLower(strings.TrimSpace(sc.Text())), quit) {
				return
			}
		}
		return
	}

	old, err := term.MakeRaw(fd)
	if err != nil {
		log.Warn("keyboard unavailable", "err", err)
		return
	}
	defer func() { _ = term.Restore(fd, old) }()
	setRawTerminal(true)

	printHint(speakPath)

	buf := make([]byte, 1)
	for {
		if _, err := os.Stdin.Read(buf); err != nil {
			return
		}
		key := strings.ToLower(string(buf[:1]))
		if buf[0] == 3 { // raw mode disables ISIG, so ctrl-c arrives as a byte
			key = "q"
		}
		if !dispatchKey(sat, speakPath, key, quit) {
			return
		}
	}
}

// dispatchKey reports whether to keep reading.
func dispatchKey(sat *esphome.VoiceSatellite, speakPath, key string, quit func()) bool {
	switch key {
	case "w", "", "\r", "\n":
		fireWake(sat, silence)
	case "s":
		fireWake(sat, speakPath)
	case "q":
		status("quitting")
		quit()
		return false
	case "?", "h":
		printHint(speakPath)
	}
	return true
}

func printHint(speakPath string) {
	speak := "[s] wake + speech (built-in)"
	if speakPath != "" {
		speak = "[s] wake + speech (" + filepath.Base(speakPath) + ")"
	}
	status(fmt.Sprintf("[w] wake + silence   %s   [q] quit", speak))
}

func fireWake(sat *esphome.VoiceSatellite, speakPath string) {
	if !sat.Subscribed() {
		status("✗ wake ignored — Home Assistant is not subscribed")
		return
	}

	audio, err := utterance(speakPath)
	if err != nil {
		status("✗ " + err.Error())
		return
	}

	if err := sat.StartTurn(wakePhrase, nil); err != nil {
		status("✗ " + err.Error())
		return
	}

	const chunk = 1024
	for off := 0; off < len(audio); off += chunk {
		end := min(off+chunk, len(audio))
		if err := sat.SendAudio(audio[off:end]); err != nil {
			status("✗ " + err.Error())
			return
		}
	}
	if err := sat.EndAudio(); err != nil {
		status("✗ " + err.Error())
		return
	}

	secs := float64(len(audio)) / (playRate * 2)
	if speakPath == silence {
		status(fmt.Sprintf("▸ wake sent + %.1fs silence — expect no speech detected", secs))
		return
	}
	status(fmt.Sprintf("▸ wake sent + %.1fs speech", secs))
}

// utterance returns 16 kHz mono PCM: the speech file, or a second of silence.
func utterance(speakPath string) ([]byte, error) {
	switch speakPath {
	case silence:
		return make([]byte, playRate*2), nil
	case "":
		return loadWAV(bytes.NewReader(sampleWAV))
	}
	return loadWAVFile(speakPath)
}
