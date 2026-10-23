package gamedata

// Elements (ngũ hành, migration 054_elements): every character and boss has one (config_characters
// .element, config_bosses.element; minions carry their boss's). Each element beats one other
// (config_elements.beats): metal → wood → earth → water → fire → metal (Kim khắc Mộc, Mộc khắc
// Thổ, Thổ khắc Thủy, Thủy khắc Hỏa, Hỏa khắc Kim). The damage multipliers are rules.elements
// (ElementRules); the match applies them in elementFactor (internal/game/match/element.go).

// The element ids.
const (
	ElementMetal = "metal"
	ElementWood  = "wood"
	ElementWater = "water"
	ElementFire  = "fire"
	ElementEarth = "earth"
)

// ElementConfig is a config_elements row: an element, its Vietnamese name, the element it beats
// and its display order.
type ElementConfig struct {
	ElementID string
	Name      string
	Beats     string
	SortOrder int
}

// DefaultElements are the elements 054_elements seeds (the YAML fallback plays with them too).
func DefaultElements() []ElementConfig {
	return []ElementConfig{
		{ElementID: ElementMetal, Name: "Kim", Beats: ElementWood, SortOrder: 1},
		{ElementID: ElementWood, Name: "Mộc", Beats: ElementEarth, SortOrder: 2},
		{ElementID: ElementWater, Name: "Thủy", Beats: ElementFire, SortOrder: 3},
		{ElementID: ElementFire, Name: "Hỏa", Beats: ElementMetal, SortOrder: 4},
		{ElementID: ElementEarth, Name: "Thổ", Beats: ElementWater, SortOrder: 5},
	}
}

// ElementBeats reports whether element a beats element b (both set, a's config_elements row
// names b).
func (s *Snapshot) ElementBeats(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	for _, e := range s.Elements {
		if e.ElementID == a {
			return e.Beats == b
		}
	}
	return false
}

// Element is the snapshot's element id.
func (s *Snapshot) Element(id string) (ElementConfig, bool) {
	for _, e := range s.Elements {
		if e.ElementID == id {
			return e, true
		}
	}
	return ElementConfig{}, false
}

// ValidElement reports whether id is "" (none) or an element of the snapshot.
func (s *Snapshot) ValidElement(id string) bool {
	if id == "" {
		return true
	}
	_, ok := s.Element(id)
	return ok
}
