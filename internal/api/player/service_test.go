package player

import (
	"errors"
	"testing"

	"battle-squad/internal/shared/model"
)

func TestNormalizeDisplayName(t *testing.T) {
	cases := []struct {
		in, want string
		err      *model.AppError
	}{
		{"Alice", "Alice", nil},
		{"  Alice  ", "Alice", nil},
		{"\tBob\n", "Bob", nil},
		{"   ", "", &model.ErrDisplayNameEmpty},
		{"", "", &model.ErrDisplayNameEmpty},
		{"abcdefghijklmnopqrst", "abcdefghijklmnopqrst", nil},       // 20 chars
		{"abcdefghijklmnopqrstu", "", &model.ErrDisplayNameTooLong}, // 21 chars
		{"Nguyễn Văn Thành Đạt", "Nguyễn Văn Thành Đạt", nil},       // 20 runes, more than 20 bytes
		{"  Nguyễn Văn Thành Đạt  ", "Nguyễn Văn Thành Đạt", nil},   // trimmed before counting
		{"Nguyễn Văn Thành Đạtx", "", &model.ErrDisplayNameTooLong}, // 21 runes
	}
	for _, c := range cases {
		got, err := normalizeDisplayName(c.in)
		if c.err != nil {
			var appErr model.AppError
			if !errors.As(err, &appErr) || appErr.Message != c.err.Message {
				t.Fatalf("%q: err %v, want %v", c.in, err, *c.err)
			}
			if appErr.Code != "PLAYER_UPDATE_FAILED" || appErr.Status != 400 {
				t.Fatalf("%q: code %s status %d", c.in, appErr.Code, appErr.Status)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Fatalf("%q: got %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}
