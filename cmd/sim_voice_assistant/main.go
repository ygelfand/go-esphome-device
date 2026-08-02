// Command sim_voice_assistant runs a simulated ESPHome voice satellite so the library
// can be exercised against a real Home Assistant with no hardware involved.
package main

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"
	"github.com/ygelfand/go-esphome-device/mdns"
)

func main() {
	var (
		addr      = flag.String("addr", ":6053", "listen address")
		name      = flag.String("name", "sim-voice-assistant", "device name (also the hostname HA sees)")
		key       = flag.String("key", "", "base64 Noise PSK; generated and printed if unset")
		zeroPSK   = flag.Bool("zero-psk", false, "run unprovisioned so HA can push a key (needs aioesphomeapi 45.6.0+)")
		plaintext = flag.Bool("plaintext", false, "no encryption; readable in a capture, removed in ESPHome 2027.2.0")
		mac       = flag.String("mac", "", "MAC address; derived from -name if unset")
		speak     = flag.String("speak", "", "16-bit mono 16kHz WAV spoken after the wake word")
		noSound   = flag.Bool("no-sound", false, "do not play audio Home Assistant sends back")
		noMDNS    = flag.Bool("no-mdns", false, "do not advertise over mDNS")
		genKey    = flag.Bool("genkey", false, "print a new PSK and exit")
		verbose   = flag.Bool("v", false, "debug logging")
	)
	flag.Parse()

	if *genKey {
		k, err := esphome.GeneratePSK()
		if err != nil {
			fatal(err)
		}
		fmt.Println(k)
		return
	}

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(crlfWriter{os.Stderr}, &slog.HandlerOptions{Level: level}))

	// Default to a real key: it matches production, works on every Home Assistant
	// version, and unlike plaintext has no removal date.
	var psk *esphome.PSK
	switch {
	case *plaintext:
		psk = nil
	case *zeroPSK:
		psk = esphome.Unprovisioned()
	case *key != "":
		k, err := esphome.ParsePSK(*key)
		if err != nil {
			fatal(err)
		}
		psk = &k
	default:
		k, err := esphome.GeneratePSK()
		if err != nil {
			fatal(err)
		}
		psk = &k
		fmt.Fprintf(os.Stderr, "generated encryption key: %s\n"+
			"pass -key to keep it across restarts, otherwise HA needs re-pairing\n\n", k)
	}

	soundEnabled = !*noSound

	if *mac == "" {
		*mac = macFromName(*name)
	}

	srv := &esphome.Server{
		Addr: *addr,
		Info: esphome.Info{
			Name:         *name,
			FriendlyName: "Simulated Device",
			MACAddress:   *mac,
			Manufacturer: "go-esphome-device",
			Model:        "sim_voice_assistant",
			Version:      "dev",
			// Announce and StartConversation go together, and both require a
			// media_player, which this simulator exposes.
			VoiceFeatures: esphome.DefaultVoiceFeatures |
				esphome.FeatureSpeaker |
				esphome.FeatureTimers |
				esphome.FeatureAnnounce |
				esphome.FeatureStartConversation,
		},
		PSK:    psk,
		Logger: log,
	}

	ents, player := buildEntities(log)
	sat := buildSatellite(log, player)

	srv.Handler = esphome.Chain(ents, sat)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdown, cancel := context.WithCancel(ctx)
	defer cancel()
	go console(log, sat, *speak, cancel)

	if !*noMDNS {
		port, err := portOf(*addr)
		if err != nil {
			fatal(err)
		}
		ad, err := mdns.Advertise(mdns.Config{
			Name:          *name,
			FriendlyName:  srv.Info.FriendlyName,
			Port:          port,
			MACAddress:    srv.Info.MACAddress,
			Version:       srv.Info.Version,
			Platform:      "host",
			Network:       "wifi",
			Encrypted:     psk != nil && !psk.IsZero(),
			Provisionable: psk != nil && psk.IsZero(),
		})
		if err != nil {
			fatal(err)
		}
		defer func() { _ = ad.Close() }()
		log.Info("advertising", "service", mdns.Service, "name", *name, "port", port)
	}

	if err := srv.ListenAndServe(shutdown); err != nil {
		fatal(err)
	}
}

func fetchAndPlay(log *slog.Logger, rawURL string) error {
	data, _, err := fetchMedia(log, rawURL)
	if err != nil {
		return err
	}
	return playAudio(log, data)
}

func fetchMedia(log *slog.Logger, rawURL string) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%s: %s", resp.Status, shortURL(rawURL))
	}

	ctype := resp.Header.Get("Content-Type")
	log.Info("fetched media", "status", resp.StatusCode, "bytes", len(body),
		"type", ctype, "url", shortURL(rawURL))
	return body, ctype, nil
}

// shortURL drops the query, which carries a long signed token.
func shortURL(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil {
		return u.Scheme + "://" + u.Host + u.Path
	}
	return rawURL
}

// macFromName keeps each -name on its own MAC, since Home Assistant uses it as the
// unique_id and a collision makes discovery silently abort as already_configured.
// 02:… marks it locally administered.
func macFromName(name string) string {
	sum := sha256.Sum256([]byte(name))
	return fmt.Sprintf("02:%02x:%02x:%02x:%02x:%02x", sum[0], sum[1], sum[2], sum[3], sum[4])
}

func portOf(addr string) (int, error) {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(p)
}

func buildEntities(log *slog.Logger) (*esphome.Entities, *esphome.MediaPlayer) {
	ring := &esphome.Light{
		Base:    esphome.Base{ObjectID: "ring", Name: "LED Ring"},
		Effects: []string{"None", "Pulse", "Spin"},
	}
	ring.OnCommand = func(s esphome.LightState) {
		log.Info("light", "on", s.On, "brightness", s.Brightness,
			"rgb", fmt.Sprintf("%.2f/%.2f/%.2f", s.Red, s.Green, s.Blue), "effect", s.Effect)
		ring.Set(s)
	}

	player := &esphome.MediaPlayer{
		Base:          esphome.Base{ObjectID: "media", Name: "Media Player"},
		SupportsPause: true,
	}
	player.OnCommand = func(c esphome.MediaCommand) {
		if c.HasMediaURL {
			go func() {
				if err := fetchAndPlay(log, c.MediaURL); err != nil {
					log.Error("fetch media", "err", err)
				}
			}()
			player.SetState(esphome.MediaPlayerPlaying)
		}
		if c.HasVolume {
			player.SetVolume(c.Volume)
		}
		if c.HasCommand {
			log.Info("media command", "command", c.Command.String())
		}
	}

	// No wake word select: Home Assistant builds one for satellites from
	// VoiceAssistantConfigurationResponse, and exposing our own collides with it.
	threshold := &esphome.Number{
		Base: esphome.Base{ObjectID: "threshold", Name: "Wake Threshold", Category: esphome.CategoryConfig},
		Min:  0, Max: 1, Step: 0.01,
	}
	threshold.Set(0.97)

	gain := &esphome.Number{
		Base: esphome.Base{ObjectID: "mic_gain", Name: "Mic Gain", Category: esphome.CategoryConfig},
		Min:  0, Max: 48, Step: 1, Unit: "dB",
	}
	gain.Set(24)

	mute := &esphome.BinarySensor{
		Base: esphome.Base{ObjectID: "mute", Name: "Mic Muted"},
	}

	restart := &esphome.Button{
		Base:    esphome.Base{ObjectID: "restart", Name: "Restart"},
		OnPress: func() { log.Info("restart pressed") },
	}

	firmware := &esphome.Update{
		Base:        esphome.Base{ObjectID: "firmware", Name: "Firmware", Category: esphome.CategoryConfig},
		DeviceClass: "firmware",
	}
	firmware.Set(esphome.UpdateState{
		CurrentVersion: "dev",
		LatestVersion:  "dev",
		Title:          "sim_voice_assistant",
	})

	ents := esphome.NewEntities()
	if err := ents.Add(ring, player, threshold, gain, mute, restart, firmware); err != nil {
		fatal(err)
	}

	// Toggle the mute sensor so state pushes are visible in Home Assistant.
	go func() {
		for range time.Tick(30 * time.Second) {
			mute.Set(!mute.Get())
		}
	}()

	return ents, player
}

func buildSatellite(log *slog.Logger, player *esphome.MediaPlayer) *esphome.VoiceSatellite {
	v := &esphome.VoiceSatellite{
		AvailableWakeWords: []esphome.WakeWord{
			{ID: "hey_jarvis", Phrase: "Hey Jarvis", TrainedLanguages: []string{"en"}},
			{ID: "okay_nabu", Phrase: "Okay Nabu", TrainedLanguages: []string{"en"}},
			{ID: "alexa", Phrase: "Alexa", TrainedLanguages: []string{"en"}},
		},
		ActiveWakeWords:    []string{"hey_jarvis"},
		MaxActiveWakeWords: 1,
	}

	v.OnPipelineEvent = func(e esphome.PipelineEvent) {
		log.Info("pipeline", "event", e.Type.String(), "data", e.Data)
	}
	v.OnTTSAudio = func(data []byte, end bool) {
		log.Debug("tts audio", "bytes", len(data), "end", end)
	}
	v.OnStartRequestAccepted = func(port uint32, err bool) {
		log.Info("turn accepted", "udp_port", port, "error", err)
	}
	// Announcements carry a URL, not audio. Home Assistant's satellite connection test
	// checks that the device actually fetches it, so a real client must GET it.
	v.OnAnnounce = func(a esphome.Announce) {
		log.Info("announce", "text", a.Text, "start_conversation", a.StartConversation)

		// Playback must not block the connection's read loop.
		go func() {
			ok := true
			player.SetState(esphome.MediaPlayerAnnouncing)
			for _, u := range []string{a.PreannounceMediaID, a.MediaID} {
				if u == "" {
					continue
				}
				if err := fetchAndPlay(log, u); err != nil {
					log.Error("fetch announcement media", "err", err)
					ok = false
				}
			}
			player.SetState(esphome.MediaPlayerIdle)
			_ = v.AnnounceFinished(ok)
		}()
	}
	v.OnTimer = func(e esphome.TimerEvent) {
		log.Info("timer", "event", e.Type.String(), "name", e.Name, "left", e.SecondsLeft)
	}
	v.OnSetActiveWakeWords = func(ids []string) {
		log.Info("active wake words changed", "ids", ids)
	}

	return v
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "sim_voice_assistant:", err)
	os.Exit(1)
}
