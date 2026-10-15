package room

import "testing"

func TestParseQuickPlayPayload(t *testing.T) {
	for _, tc := range []struct {
		data string
		ai   bool
	}{
		{"", false},
		{"null", false},
		{"{}", false},
		{`{"bot":"idle"}`, false},
		{`{"bot":"hard"}`, false},
		{`{"bot":1}`, false},
		{"not json", false},
		{`{"bot":"ai"}`, true},
	} {
		p := parseQuickPlayPayload([]byte(tc.data))
		if p.AIBot() != tc.ai {
			t.Errorf("parseQuickPlayPayload(%q).AIBot() = %v, want %v", tc.data, p.AIBot(), tc.ai)
		}
		if !tc.ai && p.Bot != "idle" {
			t.Errorf("parseQuickPlayPayload(%q).Bot = %q, want idle", tc.data, p.Bot)
		}
	}
}

func TestInternalEventsAreRejectedFromClients(t *testing.T) {
	for _, event := range []string{"__start_tutorial", "__register_client"} {
		if !isInternalEvent(event) {
			t.Errorf("%s must be internal", event)
		}
	}
	for _, event := range []string{"Move", "Shoot", "Ready", "StartMatch"} {
		if isInternalEvent(event) {
			t.Errorf("%s must reach the room", event)
		}
	}
}
