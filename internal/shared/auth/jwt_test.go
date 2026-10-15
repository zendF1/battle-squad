package auth

import (
	"testing"
	"time"
)

func TestGenerateTokensAreUniqueWithinOneSecond(t *testing.T) {
	m := NewJWTManager("secret", time.Hour)
	a, err := m.Generate("acc-1", "player-1", "player")
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Generate("acc-1", "player-1", "player")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two tokens with the same claims in the same second are identical")
	}
	if _, err := m.Verify(a); err != nil {
		t.Fatalf("token with jti does not verify: %v", err)
	}
}
