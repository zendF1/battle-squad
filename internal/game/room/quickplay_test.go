package room

import (
	"encoding/json"
	"reflect"
	"testing"

	"battle-squad/internal/shared/model"
)

func TestParseQuickPlayPayload(t *testing.T) {
	for _, tc := range []struct {
		data      string
		ai        bool
		character string
		items     []string
	}{
		{"", false, "rookie", []string{}},
		{"null", false, "rookie", []string{}},
		{"{}", false, "rookie", []string{}},
		{`{"bot":"idle"}`, false, "rookie", []string{}},
		{`{"bot":"hard"}`, false, "rookie", []string{}},
		{`{"bot":1}`, false, "rookie", []string{}},
		{"not json", false, "rookie", []string{}},
		{`{"bot":"ai"}`, true, "rookie", []string{}},
		{`{"characterId":""}`, false, "rookie", []string{}},
		{`{"items":null}`, false, "rookie", []string{}},
		{`{"items":[]}`, false, "rookie", []string{}},
		{`{"bot":"ai","characterId":"tanko","items":["medkit","teleport"]}`, true, "tanko", []string{"medkit", "teleport"}},
		{`{"characterId":"spark","items":["medkit","medkit"]}`, false, "spark", []string{"medkit", "medkit"}},
	} {
		p := parseQuickPlayPayload([]byte(tc.data))
		if p.AIBot() != tc.ai {
			t.Errorf("parseQuickPlayPayload(%q).AIBot() = %v, want %v", tc.data, p.AIBot(), tc.ai)
		}
		if !tc.ai && p.Bot != "idle" {
			t.Errorf("parseQuickPlayPayload(%q).Bot = %q, want idle", tc.data, p.Bot)
		}
		if p.CharacterID != tc.character {
			t.Errorf("parseQuickPlayPayload(%q).CharacterID = %q, want %q", tc.data, p.CharacterID, tc.character)
		}
		if p.Items == nil || !reflect.DeepEqual(p.Items, tc.items) {
			t.Errorf("parseQuickPlayPayload(%q).Items = %#v, want %#v", tc.data, p.Items, tc.items)
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

func TestRoomErrorMessageLoadoutInvalid(t *testing.T) {
	appErr := model.ErrLoadoutInvalid
	appErr.Message = "You do not own item: medkit"
	msg, ok := roomErrorMessage(appErr)
	if !ok || msg.Event != "RoomError" {
		t.Fatalf("msg = %+v, ok = %v", msg, ok)
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(msg.Data, &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "LOADOUT_INVALID" || body.Error.Message != appErr.Message {
		t.Fatalf("payload = %s", msg.Data)
	}
}
