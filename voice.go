package esphomedevice

import (
	"context"
	"errors"
	"sync"

	"google.golang.org/protobuf/proto"

	"github.com/ygelfand/go-esphome-device/api"
)

// VoiceFeature flags go in DeviceInfoResponse.voice_assistant_feature_flags and tell
// Home Assistant what the satellite can do.
//
// The enum lives in ESPHome's C++ (voice_assistant.h), not api.proto, so these are
// transcribed from there rather than generated.
type VoiceFeature uint32

const (
	FeatureVoiceAssistant    VoiceFeature = 1 << 0
	FeatureSpeaker           VoiceFeature = 1 << 1
	FeatureAPIAudio          VoiceFeature = 1 << 2
	FeatureTimers            VoiceFeature = 1 << 3
	FeatureAnnounce          VoiceFeature = 1 << 4
	FeatureStartConversation VoiceFeature = 1 << 5
	FeatureMultiChannelAudio VoiceFeature = 1 << 6
)

// DefaultVoiceFeatures matches what ESPHome always sets. The rest are conditional there:
// Speaker when a speaker is configured, Timers when timers are, MultiChannelAudio when a
// second mic source exists, and Announce together with StartConversation when a
// media_player is present — ESPHome sets that pair as a unit.
const DefaultVoiceFeatures = FeatureVoiceAssistant | FeatureAPIAudio

type (
	VoiceEvent      = api.VoiceAssistantEvent
	VoiceTimerEvent = api.VoiceAssistantTimerEvent
)

// WakeWord is one model the device can listen for.
type WakeWord struct {
	ID               string
	Phrase           string
	TrainedLanguages []string
}

// ExternalWakeWord is a model Home Assistant is offering, served from its
// custom_wake_words directory. The device downloads URL and should check the result
// against Hash and Size before using it.
type ExternalWakeWord struct {
	ID               string
	Phrase           string
	TrainedLanguages []string
	ModelType        string
	Size             uint32
	Hash             string
	URL              string
}

// PipelineEvent is a stage transition reported by Home Assistant's pipeline, with the
// key/value pairs that stage carried (transcript text, TTS url, error details).
type PipelineEvent struct {
	Type VoiceEvent
	Data map[string]string
}

// Announce is Home Assistant asking the device to speak something outside a turn.
type Announce struct {
	MediaID            string
	Text               string
	PreannounceMediaID string
	StartConversation  bool
}

type TimerEvent struct {
	Type         VoiceTimerEvent
	TimerID      string
	Name         string
	TotalSeconds uint32
	SecondsLeft  uint32
	IsActive     bool
}

var (
	ErrNoSubscriber   = errors.New("esphomedevice: no voice assistant subscriber")
	ErrTurnInProgress = errors.New("esphomedevice: a voice turn is already running")
)

// VoiceSatellite implements the device half of the voice_assistant flow. The device
// initiates every turn, so nothing here is driven by Home Assistant polling — which is
// what makes a pre-flight arbitration check invisible to the protocol.
//
// Use it as a Handler, usually via Chain alongside Entities.
type VoiceSatellite struct {
	// AvailableWakeWords and ActiveWakeWords are reported to Home Assistant and shown
	// in its UI. Changing them at runtime requires no reconnect.
	AvailableWakeWords []WakeWord
	ActiveWakeWords    []string
	MaxActiveWakeWords uint32

	// OnPipelineEvent reports stage transitions: STT text, TTS urls, errors.
	OnPipelineEvent func(PipelineEvent)

	// OnTTSAudio receives audio to play. Only used when Home Assistant negotiated
	// audio over the API, which it signals in SubscribeVoiceAssistantRequest.
	OnTTSAudio func(data []byte, end bool)

	// OnStartRequestAccepted fires with Home Assistant's answer to StartTurn. A
	// non-zero port means it wants the legacy UDP audio path instead.
	OnStartRequestAccepted func(port uint32, err bool)

	OnAnnounce func(Announce)
	OnTimer    func(TimerEvent)

	// OnSubscribed fires when Home Assistant starts or stops listening for voice turns. Until it
	// has subscribed, a wake word cannot be served at all, so this is what readiness means for a
	// satellite — and it is worth showing, since the device otherwise looks broken rather than
	// unattached.
	OnSubscribed func(subscribed bool)

	// OnSetActiveWakeWords fires when Home Assistant changes the selection.
	OnSetActiveWakeWords func(ids []string)

	// OnExternalWakeWords fires when Home Assistant offers models from its
	// custom_wake_words directory. Returning the ones the device has adopted adds them
	// to the advertised list; return nil to ignore the offer.
	//
	// It runs before the configuration reply is sent, so a slow download belongs in a
	// goroutine — adopt on the next request instead of blocking this one.
	OnExternalWakeWords func([]ExternalWakeWord) []WakeWord

	mu       sync.Mutex
	conn     *Conn
	apiAudio bool
	turnOpen bool
}

// Subscribed reports whether Home Assistant is listening for voice turns.
func (v *VoiceSatellite) Subscribed() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.conn != nil
}

// APIAudio reports whether audio travels over the API connection rather than the legacy
// UDP path.
func (v *VoiceSatellite) APIAudio() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.apiAudio
}

// StartTurn tells Home Assistant a wake word fired and a turn is beginning. The device
// decides when to call this, so any pre-flight work — such as winning an arbitration
// claim — happens before it and is invisible to Home Assistant.
func (v *VoiceSatellite) StartTurn(wakeWordPhrase string, settings *api.VoiceAssistantAudioSettings) error {
	v.mu.Lock()
	conn := v.conn
	if conn == nil {
		v.mu.Unlock()
		return ErrNoSubscriber
	}
	if v.turnOpen {
		v.mu.Unlock()
		return ErrTurnInProgress
	}
	v.turnOpen = true
	v.mu.Unlock()

	if settings == nil {
		settings = &api.VoiceAssistantAudioSettings{VolumeMultiplier: 1}
	}
	return conn.Send(&api.VoiceAssistantRequest{
		Start:          true,
		WakeWordPhrase: wakeWordPhrase,
		AudioSettings:  settings,
	})
}

// StopTurn ends the current turn early.
func (v *VoiceSatellite) StopTurn() error {
	v.mu.Lock()
	conn := v.conn
	v.turnOpen = false
	v.mu.Unlock()

	if conn == nil {
		return ErrNoSubscriber
	}
	return conn.Send(&api.VoiceAssistantRequest{Start: false})
}

// SendAudio streams captured audio for the active turn.
func (v *VoiceSatellite) SendAudio(data []byte) error {
	v.mu.Lock()
	conn := v.conn
	v.mu.Unlock()
	if conn == nil {
		return ErrNoSubscriber
	}
	return conn.Send(&api.VoiceAssistantAudio{Data: data})
}

// EndAudio marks the end of captured audio for the turn.
func (v *VoiceSatellite) EndAudio() error {
	v.mu.Lock()
	conn := v.conn
	v.mu.Unlock()
	if conn == nil {
		return ErrNoSubscriber
	}
	return conn.Send(&api.VoiceAssistantAudio{End: true})
}

// AnnounceFinished reports the outcome of an Announce back to Home Assistant.
func (v *VoiceSatellite) AnnounceFinished(success bool) error {
	v.mu.Lock()
	conn := v.conn
	v.mu.Unlock()
	if conn == nil {
		return ErrNoSubscriber
	}
	return conn.Send(&api.VoiceAssistantAnnounceFinished{Success: success})
}

func (v *VoiceSatellite) Handle(ctx context.Context, c *Conn, msg proto.Message) error {
	switch m := msg.(type) {
	case *api.SubscribeVoiceAssistantRequest:
		v.mu.Lock()
		was := v.conn != nil
		if m.GetSubscribe() {
			v.conn = c
			v.apiAudio = m.GetFlags()&uint32(api.VoiceAssistantSubscribeFlag_VOICE_ASSISTANT_SUBSCRIBE_API_AUDIO) != 0
		} else if v.conn == c {
			v.conn = nil
			v.turnOpen = false
		}
		now, notify := v.conn != nil, v.OnSubscribed
		v.mu.Unlock()

		if now != was && notify != nil {
			notify(now)
		}
		return nil

	case *api.VoiceAssistantResponse:
		if v.OnStartRequestAccepted != nil {
			v.OnStartRequestAccepted(m.GetPort(), m.GetError())
		}
		if m.GetError() {
			v.mu.Lock()
			v.turnOpen = false
			v.mu.Unlock()
		}
		return nil

	case *api.VoiceAssistantEventResponse:
		ev := PipelineEvent{Type: m.GetEventType(), Data: map[string]string{}}
		for _, d := range m.GetData() {
			ev.Data[d.GetName()] = d.GetValue()
		}
		switch ev.Type {
		case api.VoiceAssistantEvent_VOICE_ASSISTANT_RUN_END,
			api.VoiceAssistantEvent_VOICE_ASSISTANT_ERROR:
			v.mu.Lock()
			v.turnOpen = false
			v.mu.Unlock()
		}
		if v.OnPipelineEvent != nil {
			v.OnPipelineEvent(ev)
		}
		return nil

	case *api.VoiceAssistantAudio:
		if v.OnTTSAudio != nil {
			v.OnTTSAudio(m.GetData(), m.GetEnd())
		}
		return nil

	case *api.VoiceAssistantConfigurationRequest:
		cfg := v.configuration(externalWakeWords(m))
		c.log.Info("voice configuration requested",
			"available", len(cfg.GetAvailableWakeWords()),
			"active", cfg.GetActiveWakeWords(),
			"max_active", cfg.GetMaxActiveWakeWords(),
			"offered_by_ha", len(m.GetExternalWakeWords()))
		return c.Send(cfg)

	case *api.VoiceAssistantSetConfiguration:
		v.ActiveWakeWords = m.GetActiveWakeWords()
		if v.OnSetActiveWakeWords != nil {
			v.OnSetActiveWakeWords(m.GetActiveWakeWords())
		}
		return nil

	case *api.VoiceAssistantAnnounceRequest:
		if v.OnAnnounce != nil {
			v.OnAnnounce(Announce{
				MediaID:            m.GetMediaId(),
				Text:               m.GetText(),
				PreannounceMediaID: m.GetPreannounceMediaId(),
				StartConversation:  m.GetStartConversation(),
			})
		}
		return nil

	case *api.VoiceAssistantTimerEventResponse:
		if v.OnTimer != nil {
			v.OnTimer(TimerEvent{
				Type:         m.GetEventType(),
				TimerID:      m.GetTimerId(),
				Name:         m.GetName(),
				TotalSeconds: m.GetTotalSeconds(),
				SecondsLeft:  m.GetSecondsLeft(),
				IsActive:     m.GetIsActive(),
			})
		}
		return nil
	}
	return nil
}

func externalWakeWords(m *api.VoiceAssistantConfigurationRequest) []ExternalWakeWord {
	offered := m.GetExternalWakeWords()
	if len(offered) == 0 {
		return nil
	}

	out := make([]ExternalWakeWord, 0, len(offered))
	for _, w := range offered {
		out = append(out, ExternalWakeWord{
			ID:               w.GetId(),
			Phrase:           w.GetWakeWord(),
			TrainedLanguages: w.GetTrainedLanguages(),
			ModelType:        w.GetModelType(),
			Size:             w.GetModelSize(),
			Hash:             w.GetModelHash(),
			URL:              w.GetUrl(),
		})
	}
	return out
}

func (v *VoiceSatellite) configuration(offered []ExternalWakeWord) *api.VoiceAssistantConfigurationResponse {
	maxActive := v.MaxActiveWakeWords
	if maxActive == 0 {
		maxActive = 1
	}

	words := v.AvailableWakeWords
	if len(offered) > 0 && v.OnExternalWakeWords != nil {
		if adopted := v.OnExternalWakeWords(offered); len(adopted) > 0 {
			// Copy rather than append onto the caller's slice.
			words = make([]WakeWord, 0, len(v.AvailableWakeWords)+len(adopted))
			words = append(words, v.AvailableWakeWords...)
			words = append(words, adopted...)
		}
	}

	available := make([]*api.VoiceAssistantWakeWord, 0, len(words))
	for _, w := range words {
		available = append(available, &api.VoiceAssistantWakeWord{
			Id:               w.ID,
			WakeWord:         w.Phrase,
			TrainedLanguages: w.TrainedLanguages,
		})
	}

	return &api.VoiceAssistantConfigurationResponse{
		AvailableWakeWords: available,
		ActiveWakeWords:    v.ActiveWakeWords,
		MaxActiveWakeWords: maxActive,
	}
}
