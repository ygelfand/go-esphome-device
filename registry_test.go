package esphomedevice

import (
	"testing"

	"github.com/ygelfand/go-esphome-device/api"
)

func TestRegistryPopulated(t *testing.T) {
	if len(messagesByID) < 100 {
		t.Fatalf("registry looks empty: %d messages", len(messagesByID))
	}
	if len(messagesByID) != len(messagesByName) {
		t.Errorf("id/name maps disagree: %d vs %d", len(messagesByID), len(messagesByName))
	}
}

func TestKnownIDs(t *testing.T) {
	tests := []struct {
		id     uint32
		name   string
		source api.APISourceType
	}{
		{1, "HelloRequest", api.APISourceType_SOURCE_CLIENT},
		{2, "HelloResponse", api.APISourceType_SOURCE_SERVER},
		{7, "PingRequest", api.APISourceType_SOURCE_BOTH},
		{9, "DeviceInfoRequest", api.APISourceType_SOURCE_CLIENT},
		{10, "DeviceInfoResponse", api.APISourceType_SOURCE_SERVER},
	}

	for _, tc := range tests {
		info, ok := LookupID(tc.id)
		if !ok {
			t.Errorf("id %d (%s) not registered", tc.id, tc.name)
			continue
		}
		if got := string(info.Name); got != tc.name {
			t.Errorf("id %d: name = %q, want %q", tc.id, got, tc.name)
		}
		if info.Source != tc.source {
			t.Errorf("id %d (%s): source = %v, want %v", tc.id, tc.name, info.Source, tc.source)
		}
	}
}

func TestLookupRoundTrip(t *testing.T) {
	info, ok := Lookup(&api.HelloResponse{})
	if !ok {
		t.Fatal("HelloResponse not registered")
	}
	if info.ID != 2 {
		t.Errorf("HelloResponse ID = %d, want 2", info.ID)
	}
	if !info.FromDevice() {
		t.Error("HelloResponse should be sent by the device")
	}
	if info.FromClient() {
		t.Error("HelloResponse should not be sent by the client")
	}
}

func TestNewMessage(t *testing.T) {
	m, ok := NewMessage(1)
	if !ok {
		t.Fatal("id 1 not registered")
	}
	if _, ok := m.(*api.HelloRequest); !ok {
		t.Errorf("NewMessage(1) = %T, want *api.HelloRequest", m)
	}

	if _, ok := NewMessage(0xffff); ok {
		t.Error("unknown id should not resolve")
	}
}

func TestVoiceAssistantMessagesPresent(t *testing.T) {
	var names []string
	for _, info := range Messages() {
		if n := string(info.Name); len(n) >= 14 && n[:14] == "VoiceAssistant" {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		t.Fatal("no VoiceAssistant messages found")
	}
	t.Logf("%d VoiceAssistant messages: %v", len(names), names)
}
