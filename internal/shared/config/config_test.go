package config

import "testing"

func TestDevFlagsIgnoredInProduction(t *testing.T) {
	for _, tc := range []struct {
		env  string
		want bool
	}{
		{"development", true},
		{"staging", true},
		{"production", false},
		{"Production", false},
		{"PRODUCTION", false},
		{"prod", false},
		{"PROD", false},
		{" prod ", false},
	} {
		t.Run(tc.env, func(t *testing.T) {
			t.Setenv("APP_ENV", tc.env)
			t.Setenv("DEV_UNLOCK_ALL", "true")
			t.Setenv("IAP_MOCK_ENABLED", "true")
			cfg := LoadConfig()
			if cfg.DevUnlockAll != tc.want || cfg.IAPMockEnabled != tc.want {
				t.Fatalf("APP_ENV=%q: DevUnlockAll=%v IAPMockEnabled=%v, want %v", tc.env, cfg.DevUnlockAll, cfg.IAPMockEnabled, tc.want)
			}
		})
	}
}

// Production refuses the development JWT secret and short ones; development accepts them.
func TestCheckProduction(t *testing.T) {
	for _, tc := range []struct {
		env, secret string
		ok          bool
	}{
		{"development", DevJWTSecret, true},
		{"production", DevJWTSecret, false},
		{"prod", "short", false},
		{"production", "0123456789abcdef0123456789abcdef", true},
	} {
		c := &Config{Env: tc.env, JWTSecret: tc.secret}
		if err := c.CheckProduction(); (err == nil) != tc.ok {
			t.Errorf("%s %q: %v", tc.env, tc.secret, err)
		}
	}
}
