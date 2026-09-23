package esphomedevice

import (
	"bytes"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/ygelfand/go-esphome-device/api"
)

func TestFNV1MatchesESPHome(t *testing.T) {
	// Reference values from ESPHome's fnv1_hash (multiply then xor, 32-bit).
	tests := map[string]uint32{
		"":       2166136261,
		"a":      0x050c5d7e,
		"foobar": 0x31f0b262,
	}
	for in, want := range tests {
		if got := fnv1(in); got != want {
			t.Errorf("fnv1(%q) = %#08x, want %#08x", in, got, want)
		}
	}
}

func TestEntityKeysAreStableAndDistinct(t *testing.T) {
	a := &Select{Base: Base{ObjectID: "wake_word"}}
	b := &Number{Base: Base{ObjectID: "threshold"}}

	if a.Key() == b.Key() {
		t.Error("distinct object ids produced the same key")
	}
	if a.Key() != fnv1("wake_word") {
		t.Error("key is not derived from ObjectID")
	}
}

func TestDuplicateObjectIDIsRejected(t *testing.T) {
	e := NewEntities()
	if err := e.Add(&Select{Base: Base{ObjectID: "keep"}}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	err := e.Add(
		&Number{Base: Base{ObjectID: "dup"}},
		&Number{Base: Base{ObjectID: "dup"}},
	)
	if err == nil {
		t.Fatal("expected an error on duplicate object id")
	}

	// Nothing from the rejected call is registered, so a caller that carries on is not left with half
	// of what it asked for.
	if got := len(e.all()); got != 1 {
		t.Errorf("registered %d entities after a rejected Add, want 1", got)
	}
}

func testEntitySet() (*Entities, *Select, *Number, *Light, *BinarySensor) {
	sel := &Select{
		Base:    Base{ObjectID: "wake_word", Name: "Wake Word"},
		Options: []string{"hey_jarvis", "okay_nabu"},
	}
	sel.Set("hey_jarvis")

	num := &Number{
		Base: Base{ObjectID: "threshold", Name: "Threshold", Category: CategoryConfig},
		Min:  0, Max: 1, Step: 0.01,
	}
	num.Set(0.97)

	light := &Light{Base: Base{ObjectID: "ring", Name: "LED Ring"}}
	mute := &BinarySensor{Base: Base{ObjectID: "mute", Name: "Mute"}}

	e := NewEntities()
	e.Add(sel, num, light, mute)
	return e, sel, num, light, mute
}

func TestListEntities(t *testing.T) {
	ents, _, _, _, _ := testEntitySet()
	_, peer := startServer(t, ents)
	peer.hello()

	peer.send(&api.ListEntitiesRequest{})

	seen := map[string]proto.Message{}
	for {
		msg := peer.recv()
		if _, done := msg.(*api.ListEntitiesDoneResponse); done {
			break
		}
		switch m := msg.(type) {
		case *api.ListEntitiesSelectResponse:
			seen["select"] = m
		case *api.ListEntitiesNumberResponse:
			seen["number"] = m
		case *api.ListEntitiesLightResponse:
			seen["light"] = m
		case *api.ListEntitiesBinarySensorResponse:
			seen["binary_sensor"] = m
		default:
			t.Fatalf("unexpected %T in listing", m)
		}
	}

	for _, want := range []string{"select", "number", "light", "binary_sensor"} {
		if seen[want] == nil {
			t.Errorf("%s missing from listing", want)
		}
	}

	sel := seen["select"].(*api.ListEntitiesSelectResponse)
	if got := sel.GetOptions(); len(got) != 2 || got[0] != "hey_jarvis" {
		t.Errorf("select options = %v", got)
	}
	num := seen["number"].(*api.ListEntitiesNumberResponse)
	if num.GetMaxValue() != 1 || num.GetEntityCategory() != CategoryConfig {
		t.Errorf("number describe wrong: %+v", num)
	}
}

func TestSubscribeStatesDumpsCurrentValues(t *testing.T) {
	ents, _, _, _, _ := testEntitySet()
	_, peer := startServer(t, ents)
	peer.hello()

	peer.send(&api.SubscribeStatesRequest{})

	var sawSelect, sawNumber bool
	for range 4 {
		switch m := peer.recv().(type) {
		case *api.SelectStateResponse:
			sawSelect = true
			if m.GetState() != "hey_jarvis" {
				t.Errorf("select state = %q", m.GetState())
			}
		case *api.NumberStateResponse:
			sawNumber = true
			if m.GetState() != 0.97 {
				t.Errorf("number state = %v", m.GetState())
			}
		}
	}
	if !sawSelect || !sawNumber {
		t.Error("state dump missing entities")
	}
}

func TestSelectCommandInvokesCallback(t *testing.T) {
	ents, sel, _, _, _ := testEntitySet()
	got := make(chan string, 1)
	sel.OnCommand = func(v string) { got <- v }

	_, peer := startServer(t, ents)
	peer.hello()
	peer.send(&api.SelectCommandRequest{Key: sel.Key(), State: "okay_nabu"})

	if v := <-got; v != "okay_nabu" {
		t.Errorf("callback got %q", v)
	}
	// Callback owns the state, so nothing changed on its own.
	if sel.Get() != "hey_jarvis" {
		t.Errorf("state changed despite a callback being set: %q", sel.Get())
	}
}

func TestNumberCommandAppliesWithoutCallback(t *testing.T) {
	ents, _, num, _, _ := testEntitySet()
	_, peer := startServer(t, ents)
	peer.hello()
	peer.send(&api.SubscribeStatesRequest{})
	for range 4 {
		peer.recv()
	}

	peer.send(&api.NumberCommandRequest{Key: num.Key(), State: 0.5})

	msg, ok := peer.recv().(*api.NumberStateResponse)
	if !ok {
		t.Fatal("expected a NumberStateResponse after the command")
	}
	if msg.GetState() != 0.5 {
		t.Errorf("state = %v, want 0.5", msg.GetState())
	}
	if num.Get() != 0.5 {
		t.Errorf("entity value = %v, want 0.5", num.Get())
	}
}

// Light commands are partial: unflagged fields must survive untouched.
func TestLightCommandMergesPartialUpdate(t *testing.T) {
	ents, _, _, light, _ := testEntitySet()
	light.Set(LightState{On: true, Brightness: 0.8, Red: 1, Green: 0.5, Blue: 0})

	_, peer := startServer(t, ents)
	peer.hello()
	peer.send(&api.SubscribeStatesRequest{})
	for range 4 {
		peer.recv()
	}

	peer.send(&api.LightCommandRequest{
		Key:           light.Key(),
		HasBrightness: true,
		Brightness:    0.2,
	})

	// Wait on the resulting state push rather than polling.
	if _, ok := peer.recv().(*api.LightStateResponse); !ok {
		t.Fatal("expected a LightStateResponse after the command")
	}

	got := light.Get()
	if got.Brightness != 0.2 {
		t.Errorf("brightness = %v, want 0.2", got.Brightness)
	}
	if !got.On {
		t.Error("On was cleared by a command that did not set has_state")
	}
	if got.Red != 1 || got.Green != 0.5 {
		t.Errorf("colour was clobbered: %+v", got)
	}
}

func TestStateChangePublishesToSubscribers(t *testing.T) {
	ents, _, _, _, mute := testEntitySet()
	_, peer := startServer(t, ents)
	peer.hello()
	peer.send(&api.SubscribeStatesRequest{})
	for range 4 {
		peer.recv()
	}

	mute.Set(true)

	msg, ok := peer.recv().(*api.BinarySensorStateResponse)
	if !ok {
		t.Fatal("expected a BinarySensorStateResponse")
	}
	if !msg.GetState() {
		t.Error("state should be true")
	}
}

// A chained handler must still broadcast. The server binds by type, so a Chain that does not pass
// bindServer through leaves Home Assistant with only the states sent on connect: a value changed on
// the device then shows up nowhere until the integration reloads.
func TestChainedEntitiesStillPublish(t *testing.T) {
	ents, _, _, _, mute := testEntitySet()

	_, peer := startServer(t, Chain(ents, &VoiceSatellite{}))
	peer.hello()
	peer.send(&api.SubscribeStatesRequest{})
	for range 4 {
		peer.recv()
	}

	mute.Set(true)

	msg, ok := peer.recv().(*api.BinarySensorStateResponse)
	if !ok {
		t.Fatal("expected a BinarySensorStateResponse")
	}
	if !msg.GetState() {
		t.Error("state should be true")
	}
}

func TestButtonHasNoState(t *testing.T) {
	pressed := make(chan struct{}, 1)
	btn := &Button{
		Base:    Base{ObjectID: "restart", Name: "Restart"},
		OnPress: func() { pressed <- struct{}{} },
	}
	ents := NewEntities()
	ents.Add(btn)

	_, peer := startServer(t, ents)
	peer.hello()

	// A state dump must skip the button rather than sending a nil message.
	peer.send(&api.ListEntitiesRequest{})
	if _, ok := peer.recv().(*api.ListEntitiesButtonResponse); !ok {
		t.Fatal("expected the button in the listing")
	}
	if _, ok := peer.recv().(*api.ListEntitiesDoneResponse); !ok {
		t.Fatal("expected listing to end")
	}

	peer.send(&api.ButtonCommandRequest{Key: btn.Key()})
	<-pressed
}

func TestSubDeviceOnListing(t *testing.T) {
	sel := &Select{Base: Base{ObjectID: "mixing", DeviceID: 3}, Options: []string{"a"}}
	ents := NewEntities()
	ents.Add(sel)

	_, peer := startServer(t, ents)
	peer.hello()
	peer.send(&api.ListEntitiesRequest{})

	got, ok := peer.recv().(*api.ListEntitiesSelectResponse)
	if !ok {
		t.Fatal("expected the select in the listing")
	}
	if got.GetDeviceId() != 3 {
		t.Errorf("device id = %d, want 3", got.GetDeviceId())
	}
}

// State has to carry the sub-device too. Listed under one and updated under another, Home Assistant
// cannot match the update to the entity, and the value only appears when the integration reloads.
func TestSubDeviceOnState(t *testing.T) {
	sel := &Select{Base: Base{ObjectID: "mixing", DeviceID: 3}, Options: []string{"a", "b"}}
	sel.Set("a")

	ents := NewEntities()
	ents.Add(sel)

	_, peer := startServer(t, ents)
	peer.hello()
	peer.send(&api.SubscribeStatesRequest{})

	dump, ok := peer.recv().(*api.SelectStateResponse)
	if !ok {
		t.Fatal("expected the select in the state dump")
	}
	if dump.GetDeviceId() != 3 {
		t.Errorf("dumped device id = %d, want 3", dump.GetDeviceId())
	}

	sel.Set("b")
	pushed, ok := peer.recv().(*api.SelectStateResponse)
	if !ok {
		t.Fatal("expected a state push after the change")
	}
	if pushed.GetState() != "b" || pushed.GetDeviceId() != 3 {
		t.Errorf("pushed %q on device %d, want \"b\" on 3", pushed.GetState(), pushed.GetDeviceId())
	}
}

// A picture is bigger than a frame: the noise transport carries its length in sixteen bits, so a
// still has to arrive in pieces with only the last one saying done.
func TestCameraImageArrivesInPieces(t *testing.T) {
	want := make([]byte, chunk*2+1234)
	for i := range want {
		want[i] = byte(i)
	}

	cam := &Camera{
		Base:  Base{ObjectID: "camera", Name: "Camera"},
		Image: func() ([]byte, error) { return want, nil },
	}

	ents := NewEntities()
	if err := ents.Add(cam); err != nil {
		t.Fatal(err)
	}
	_, peer := startServer(t, ents)
	peer.hello()

	peer.send(&api.CameraImageRequest{Single: true})

	var got []byte
	for pieces := 0; ; pieces++ {
		if pieces > 8 {
			t.Fatal("the picture never finished")
		}
		m, ok := peer.recv().(*api.CameraImageResponse)
		if !ok {
			t.Fatalf("expected an image piece, got %T", m)
		}
		if m.GetKey() != cam.Key() {
			t.Errorf("piece %d has key %d, want %d", pieces, m.GetKey(), cam.Key())
		}
		got = append(got, m.GetData()...)
		if m.GetDone() {
			break
		}
	}

	if !bytes.Equal(got, want) {
		t.Errorf("the picture came back %d bytes, want %d", len(got), len(want))
	}
}

// Subscribing must not cost a picture, so a camera has no state to dump.
func TestCameraHasNoStateToDump(t *testing.T) {
	cam := &Camera{
		Base:  Base{ObjectID: "camera"},
		Image: func() ([]byte, error) { t.Fatal("subscribing asked for a picture"); return nil, nil },
	}
	if got := cam.state(); got != nil {
		t.Errorf("a camera has state %T", got)
	}
}

// The request names no camera, so a device with two has no way to say which answered.
func TestASecondCameraIsRefused(t *testing.T) {
	ents := NewEntities()
	if err := ents.Add(&Camera{Base: Base{ObjectID: "one"}}); err != nil {
		t.Fatal(err)
	}
	if err := ents.Add(&Camera{Base: Base{ObjectID: "two"}}); err == nil {
		t.Error("a second camera was accepted")
	}
}
