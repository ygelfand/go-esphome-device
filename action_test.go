package esphomedevice

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/ygelfand/go-esphome-device/api"
)

func TestActionAnswersWithJSON(t *testing.T) {
	action := &Action{
		Name:    "turn_audio",
		Args:    []Arg{{Name: "id", Type: ArgString}, {Name: "page", Type: ArgInt}},
		Answers: true,
		Run: func(c Call) (any, error) {
			return map[string]any{"id": c.String("id"), "page": c.Int("page")}, nil
		},
	}

	ents := NewEntities()
	if err := ents.AddActions(action); err != nil {
		t.Fatal(err)
	}

	_, peer := startServer(t, ents)
	peer.hello()

	peer.send(&api.ListEntitiesRequest{})
	listed, ok := peer.recv().(*api.ListEntitiesServicesResponse)
	if !ok {
		t.Fatal("expected the action in the listing")
	}
	if listed.GetName() != "turn_audio" {
		t.Errorf("name = %q, want turn_audio", listed.GetName())
	}
	if listed.GetSupportsResponse() != api.SupportsResponseType_SUPPORTS_RESPONSE_OPTIONAL {
		t.Errorf("supports_response = %v, want optional", listed.GetSupportsResponse())
	}
	if len(listed.GetArgs()) != 2 || listed.GetArgs()[1].GetType() != api.ServiceArgType_SERVICE_ARG_TYPE_INT {
		t.Errorf("args = %v", listed.GetArgs())
	}
	if _, done := peer.recv().(*api.ListEntitiesDoneResponse); !done {
		t.Fatal("expected listing to end")
	}

	peer.send(&api.ExecuteServiceRequest{
		Key:            listed.GetKey(),
		CallId:         7,
		ReturnResponse: true,
		Args: []*api.ExecuteServiceArgument{
			{String_: "abc123"},
			{Int_: 2},
		},
	})

	reply, ok := peer.recv().(*api.ExecuteServiceResponse)
	if !ok {
		t.Fatal("expected an answer")
	}
	if reply.GetCallId() != 7 || !reply.GetSuccess() {
		t.Fatalf("call_id = %d, success = %v", reply.GetCallId(), reply.GetSuccess())
	}

	var got struct {
		ID   string `json:"id"`
		Page int    `json:"page"`
	}
	if err := json.Unmarshal(reply.GetResponseData(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "abc123" || got.Page != 2 {
		t.Errorf("answered %+v, want abc123 page 2", got)
	}
}

// A caller that did not ask for an answer gets none, and the action still runs.
func TestActionWithoutReturnResponseSaysNothing(t *testing.T) {
	ran := make(chan string, 1)
	action := &Action{
		Name: "install_wake_model",
		Args: []Arg{{Name: "url", Type: ArgString}},
		Run: func(c Call) (any, error) {
			ran <- c.String("url")
			return nil, nil
		},
	}

	ents := NewEntities()
	if err := ents.AddActions(action); err != nil {
		t.Fatal(err)
	}

	_, peer := startServer(t, ents)
	peer.hello()

	peer.send(&api.ExecuteServiceRequest{
		Key:  action.key(),
		Args: []*api.ExecuteServiceArgument{{String_: "http://example/model.tflite"}},
	})

	if got := <-ran; got != "http://example/model.tflite" {
		t.Errorf("ran with %q", got)
	}

	// Nothing should come back, so a ping is what proves the connection is still good and idle.
	peer.send(&api.PingRequest{})
	if _, ok := peer.recv().(*api.PingResponse); !ok {
		t.Error("expected only the ping answer")
	}
}

func TestActionErrorReachesTheCaller(t *testing.T) {
	action := &Action{
		Name:    "turn_audio",
		Answers: true,
		Run:     func(Call) (any, error) { return nil, errors.New("no such turn") },
	}

	ents := NewEntities()
	if err := ents.AddActions(action); err != nil {
		t.Fatal(err)
	}

	_, peer := startServer(t, ents)
	peer.hello()
	peer.send(&api.ExecuteServiceRequest{Key: action.key(), CallId: 3, ReturnResponse: true})

	reply, ok := peer.recv().(*api.ExecuteServiceResponse)
	if !ok {
		t.Fatal("expected an answer")
	}
	if reply.GetSuccess() {
		t.Error("success is true on a failed action")
	}
	if reply.GetErrorMessage() != "no such turn" {
		t.Errorf("error = %q", reply.GetErrorMessage())
	}
}

func TestDuplicateActionNameRegistersNothing(t *testing.T) {
	ents := NewEntities()
	run := func(Call) (any, error) { return nil, nil }

	err := ents.AddActions(
		&Action{Name: "turn_audio", Run: run},
		&Action{Name: "turn_audio", Run: run},
	)
	if err == nil {
		t.Fatal("expected a duplicate to be refused")
	}
	if len(ents.allActions()) != 0 {
		t.Errorf("registered %d actions, want none", len(ents.allActions()))
	}
}

func TestActionWithoutRunIsRefused(t *testing.T) {
	ents := NewEntities()
	if err := ents.AddActions(&Action{Name: "turn_audio"}); err == nil {
		t.Fatal("expected an action with no Run to be refused")
	}
}
