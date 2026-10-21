package gamedata

import "testing"

// The physics rows are read by their game_settings key physics.<name> (they were looked up as
// a bare <name> and never found, so the defaults always applied); a bare row is the fallback.
func TestPhysicsSettingKeys(t *testing.T) {
	s := map[string]string{"physics.gravity": "445", "gravity": "1", "wind_scale": "67"}
	if v, ok := physicsSetting(s, "gravity"); !ok || v != "445" {
		t.Fatalf("gravity %q %v", v, ok)
	}
	if v, ok := physicsSetting(s, "wind_scale"); !ok || v != "67" {
		t.Fatalf("bare wind_scale %q %v", v, ok)
	}
	if _, ok := physicsSetting(s, "time_step"); ok {
		t.Fatal("missing row found")
	}
}
