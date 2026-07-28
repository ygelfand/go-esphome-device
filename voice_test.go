package esphomedevice

import (
	"testing"
	"time"

	"github.com/ygelfand/go-esphome-device/api"
)

func subscribedSatellite(t *testing.T, v *VoiceSatellite) *testPeer {
	t.Helper()

	_, peer := startServer(t, v)
	peer.hello()
	peer.send(&api.SubscribeVoiceAssistantRequest{
		Subscribe: true,
		Flags:     uint32(api.VoiceAssistantSubscribeFlag_VOICE_ASSISTANT_SUBSCRIBE_API_AUDIO),
	})

	deadline := time.Now().Add(2 * time.Second)
	for !v.Subscribed() {
		if time.Now().After(deadline) {
			t.Fatal("subscription never registered")
		}
		time.Sleep(2 * time.Millisecond)
	}
	return peer
}

func TestVoiceSubscribeNegotiatesAPIAudio(t *testing.T) {
	v := &VoiceSatellite{}
	subscribedSatellite(t, v)

	if !v.APIAudio() {
		t.Error("expected api audio to be negotiated from the subscribe flags")
	}
}

func TestVoiceStartTurnRequiresSubscriber(t *testing.T) {
	v := &VoiceSatellite{}
	if err := v.StartTurn("hey jarvis", nil); err != ErrNoSubscriber {
		t.Errorf("err = %v, want ErrNoSubscriber", err)
	}
}

// The device initiates turns, which is what lets an arbitration claim happen first
// without Home Assistant noticing.
func TestVoiceStartTurnSendsWakePhrase(t *testing.T) {
	v := &VoiceSatellite{}
	peer := subscribedSatellite(t, v)

	if err := v.StartTurn("hey jarvis", nil); err != nil {
		t.Fatalf("start turn: %v", err)
	}

	msg, ok := peer.recv().(*api.VoiceAssistantRequest)
	if !ok {
		t.Fatal("expected a VoiceAssistantRequest")
	}
	if !msg.GetStart() {
		t.Error("start should be true")
	}
	if msg.GetWakeWordPhrase() != "hey jarvis" {
		t.Errorf("wake_word_phrase = %q", msg.GetWakeWordPhrase())
	}
	if msg.GetAudioSettings().GetVolumeMultiplier() != 1 {
		t.Error("default audio settings should set a volume multiplier")
	}
}

func TestVoiceRejectsConcurrentTurns(t *testing.T) {
	v := &VoiceSatellite{}
	peer := subscribedSatellite(t, v)

	if err := v.StartTurn("hey jarvis", nil); err != nil {
		t.Fatalf("first turn: %v", err)
	}
	peer.recv()

	if err := v.StartTurn("hey jarvis", nil); err != ErrTurnInProgress {
		t.Errorf("err = %v, want ErrTurnInProgress", err)
	}
}

func TestVoiceRunEndClearsTurn(t *testing.T) {
	v := &VoiceSatellite{}
	peer := subscribedSatellite(t, v)

	if err := v.StartTurn("hey jarvis", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	peer.recv()

	events := make(chan PipelineEvent, 4)
	v.OnPipelineEvent = func(e PipelineEvent) { events <- e }

	peer.send(&api.VoiceAssistantEventResponse{
		EventType: api.VoiceAssistantEvent_VOICE_ASSISTANT_RUN_END,
	})
	<-events

	if err := v.StartTurn("hey jarvis", nil); err != nil {
		t.Errorf("a new turn should be allowed after RUN_END, got %v", err)
	}
}

func TestVoicePipelineEventCarriesData(t *testing.T) {
	v := &VoiceSatellite{}
	events := make(chan PipelineEvent, 4)
	v.OnPipelineEvent = func(e PipelineEvent) { events <- e }

	peer := subscribedSatellite(t, v)
	peer.send(&api.VoiceAssistantEventResponse{
		EventType: api.VoiceAssistantEvent_VOICE_ASSISTANT_STT_END,
		Data: []*api.VoiceAssistantEventData{
			{Name: "text", Value: "turn on the lights"},
		},
	})

	ev := <-events
	if ev.Type != api.VoiceAssistantEvent_VOICE_ASSISTANT_STT_END {
		t.Errorf("type = %v", ev.Type)
	}
	if ev.Data["text"] != "turn on the lights" {
		t.Errorf("data = %v", ev.Data)
	}
}

func TestVoiceAudioBothDirections(t *testing.T) {
	v := &VoiceSatellite{}
	got := make(chan []byte, 4)
	v.OnTTSAudio = func(data []byte, end bool) {
		if !end {
			got <- data
		}
	}

	peer := subscribedSatellite(t, v)

	// device -> HA
	if err := v.SendAudio([]byte{1, 2, 3}); err != nil {
		t.Fatalf("send audio: %v", err)
	}
	msg, ok := peer.recv().(*api.VoiceAssistantAudio)
	if !ok {
		t.Fatal("expected VoiceAssistantAudio")
	}
	if len(msg.GetData()) != 3 {
		t.Errorf("data = %v", msg.GetData())
	}

	// HA -> device
	peer.send(&api.VoiceAssistantAudio{Data: []byte{9, 9}})
	if data := <-got; len(data) != 2 {
		t.Errorf("tts audio = %v", data)
	}
}

func TestVoiceConfigurationReportsWakeWords(t *testing.T) {
	v := &VoiceSatellite{
		AvailableWakeWords: []WakeWord{
			{ID: "hey_jarvis", Phrase: "Hey Jarvis", TrainedLanguages: []string{"en"}},
			{ID: "okay_nabu", Phrase: "Okay Nabu", TrainedLanguages: []string{"en"}},
		},
		ActiveWakeWords: []string{"hey_jarvis"},
	}

	peer := subscribedSatellite(t, v)
	peer.send(&api.VoiceAssistantConfigurationRequest{})

	resp, ok := peer.recv().(*api.VoiceAssistantConfigurationResponse)
	if !ok {
		t.Fatal("expected VoiceAssistantConfigurationResponse")
	}
	if len(resp.GetAvailableWakeWords()) != 2 {
		t.Errorf("available = %d, want 2", len(resp.GetAvailableWakeWords()))
	}
	if got := resp.GetAvailableWakeWords()[0]; got.GetWakeWord() != "Hey Jarvis" {
		t.Errorf("first wake word = %q", got.GetWakeWord())
	}
	if len(resp.GetActiveWakeWords()) != 1 {
		t.Errorf("active = %v", resp.GetActiveWakeWords())
	}
	if resp.GetMaxActiveWakeWords() != 1 {
		t.Errorf("max_active = %d, want a default of 1", resp.GetMaxActiveWakeWords())
	}
}

// Home Assistant offers models from its custom_wake_words directory on the config
// request; a device downloads them and can then advertise them as its own.
func TestVoiceExternalWakeWordsOffered(t *testing.T) {
	v := &VoiceSatellite{
		AvailableWakeWords: []WakeWord{{ID: "hey_jarvis", Phrase: "Hey Jarvis"}},
	}

	got := make(chan []ExternalWakeWord, 1)
	v.OnExternalWakeWords = func(offered []ExternalWakeWord) []WakeWord {
		got <- offered
		return []WakeWord{{ID: "hey_biscuit", Phrase: "Hey Biscuit"}}
	}

	peer := subscribedSatellite(t, v)
	peer.send(&api.VoiceAssistantConfigurationRequest{
		ExternalWakeWords: []*api.VoiceAssistantExternalWakeWord{{
			Id:        "hey_biscuit",
			WakeWord:  "Hey Biscuit",
			ModelType: "micro",
			ModelSize: 62112,
			ModelHash: "abc123",
			Url:       "http://ha.local:8123/api/esphome/wake_words/hey_biscuit.json",
		}},
	})

	offered := <-got
	if len(offered) != 1 {
		t.Fatalf("offered %d wake words, want 1", len(offered))
	}
	if offered[0].URL == "" || offered[0].Hash != "abc123" || offered[0].Size != 62112 {
		t.Errorf("offer lost fields: %+v", offered[0])
	}

	resp, ok := peer.recv().(*api.VoiceAssistantConfigurationResponse)
	if !ok {
		t.Fatal("expected VoiceAssistantConfigurationResponse")
	}
	if len(resp.GetAvailableWakeWords()) != 2 {
		t.Fatalf("available = %d, want 2 (built-in plus adopted)", len(resp.GetAvailableWakeWords()))
	}

	// Adopting must not mutate the configured list.
	if len(v.AvailableWakeWords) != 1 {
		t.Errorf("AvailableWakeWords mutated: %+v", v.AvailableWakeWords)
	}
}

func TestVoiceExternalWakeWordsIgnoredWhenUnset(t *testing.T) {
	v := &VoiceSatellite{
		AvailableWakeWords: []WakeWord{{ID: "hey_jarvis", Phrase: "Hey Jarvis"}},
	}

	peer := subscribedSatellite(t, v)
	peer.send(&api.VoiceAssistantConfigurationRequest{
		ExternalWakeWords: []*api.VoiceAssistantExternalWakeWord{{Id: "hey_biscuit"}},
	})

	resp, ok := peer.recv().(*api.VoiceAssistantConfigurationResponse)
	if !ok {
		t.Fatal("expected VoiceAssistantConfigurationResponse")
	}
	if len(resp.GetAvailableWakeWords()) != 1 {
		t.Errorf("available = %d, want 1", len(resp.GetAvailableWakeWords()))
	}
}

func TestVoiceSetConfiguration(t *testing.T) {
	v := &VoiceSatellite{ActiveWakeWords: []string{"hey_jarvis"}}
	got := make(chan []string, 1)
	v.OnSetActiveWakeWords = func(ids []string) { got <- ids }

	peer := subscribedSatellite(t, v)
	peer.send(&api.VoiceAssistantSetConfiguration{ActiveWakeWords: []string{"okay_nabu"}})

	ids := <-got
	if len(ids) != 1 || ids[0] != "okay_nabu" {
		t.Errorf("ids = %v", ids)
	}
}

func TestVoiceAnnounceRoundTrip(t *testing.T) {
	v := &VoiceSatellite{}
	got := make(chan Announce, 1)
	v.OnAnnounce = func(a Announce) { got <- a }

	peer := subscribedSatellite(t, v)
	peer.send(&api.VoiceAssistantAnnounceRequest{
		Text:              "dinner is ready",
		StartConversation: true,
	})

	a := <-got
	if a.Text != "dinner is ready" || !a.StartConversation {
		t.Errorf("announce = %+v", a)
	}

	if err := v.AnnounceFinished(true); err != nil {
		t.Fatalf("announce finished: %v", err)
	}
	fin, ok := peer.recv().(*api.VoiceAssistantAnnounceFinished)
	if !ok {
		t.Fatal("expected VoiceAssistantAnnounceFinished")
	}
	if !fin.GetSuccess() {
		t.Error("success should be true")
	}
}

func TestVoiceTimerEvents(t *testing.T) {
	v := &VoiceSatellite{}
	got := make(chan TimerEvent, 1)
	v.OnTimer = func(e TimerEvent) { got <- e }

	peer := subscribedSatellite(t, v)
	peer.send(&api.VoiceAssistantTimerEventResponse{
		EventType:    api.VoiceAssistantTimerEvent_VOICE_ASSISTANT_TIMER_FINISHED,
		TimerId:      "t1",
		Name:         "pasta",
		TotalSeconds: 600,
		IsActive:     false,
	})

	e := <-got
	if e.TimerID != "t1" || e.Name != "pasta" || e.TotalSeconds != 600 {
		t.Errorf("timer = %+v", e)
	}
}

func TestVoiceUnsubscribeStopsTurns(t *testing.T) {
	v := &VoiceSatellite{}
	peer := subscribedSatellite(t, v)

	peer.send(&api.SubscribeVoiceAssistantRequest{Subscribe: false})

	deadline := time.Now().Add(2 * time.Second)
	for v.Subscribed() {
		if time.Now().After(deadline) {
			t.Fatal("still subscribed after unsubscribe")
		}
		time.Sleep(2 * time.Millisecond)
	}
	if err := v.StartTurn("hey jarvis", nil); err != ErrNoSubscriber {
		t.Errorf("err = %v, want ErrNoSubscriber", err)
	}
}

// A satellite cannot serve a wake word until Home Assistant has subscribed, so the transition is
// reported rather than left to be polled.
func TestSubscribedFiresOnChange(t *testing.T) {
	v := &VoiceSatellite{}
	got := make(chan bool, 4)
	v.OnSubscribed = func(subscribed bool) { got <- subscribed }

	ents := NewEntities()
	_, peer := startServer(t, Chain(ents, v))
	peer.hello()

	peer.send(&api.SubscribeVoiceAssistantRequest{Subscribe: true})
	if !<-got {
		t.Error("expected a subscribe")
	}

	// Repeating it is not a change, so nothing more should arrive before the unsubscribe.
	peer.send(&api.SubscribeVoiceAssistantRequest{Subscribe: true})
	peer.send(&api.SubscribeVoiceAssistantRequest{Subscribe: false})
	if <-got {
		t.Error("expected an unsubscribe")
	}
	if v.Subscribed() {
		t.Error("still subscribed after unsubscribing")
	}
}
