package auth

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"battle-squad/internal/shared/auth"
	"battle-squad/internal/shared/database"
)

// fakeAccounts satisfies accountLookup without a database.
type fakeAccounts struct{}

func (fakeAccounts) FindAccountByID(_ context.Context, accountID string) (*Account, error) {
	return &Account{AccountID: accountID, Status: "active", Role: "player"}, nil
}

func (fakeAccounts) GetPlayerProfileByAccountID(_ context.Context, _ string) (string, string, int, error) {
	return "player-1", "Bob", 3, nil
}

// fakeRedis answers GET/SET/DEL from a map without a server (the hook never calls next).
// SET fails when failSet is true.
type fakeRedis struct {
	data    map[string]string
	failSet bool
}

func (f *fakeRedis) DialHook(next redis.DialHook) redis.DialHook { return next }

func (f *fakeRedis) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func (f *fakeRedis) ProcessHook(redis.ProcessHook) redis.ProcessHook {
	return func(_ context.Context, cmd redis.Cmder) error {
		key := fmt.Sprint(cmd.Args()[1])
		switch c := cmd.(type) {
		case *redis.StringCmd: // GET
			if v, ok := f.data[key]; ok {
				c.SetVal(v)
			} else {
				c.SetErr(redis.Nil)
			}
		case *redis.StatusCmd: // SET
			if f.failSet {
				c.SetErr(errors.New("set failed"))
			} else {
				f.data[key] = fmt.Sprint(cmd.Args()[2])
				c.SetVal("OK")
			}
		case *redis.IntCmd: // DEL
			if _, ok := f.data[key]; ok {
				delete(f.data, key)
				c.SetVal(1)
			}
		default:
			cmd.SetErr(fmt.Errorf("unexpected command %v", cmd.Args()))
		}
		return cmd.Err()
	}
}

// newRotationService returns a Service holding one valid session for the returned refresh token.
func newRotationService(t *testing.T, failSet bool) (*Service, *fakeRedis, string) {
	t.Helper()
	fr := &fakeRedis{data: map[string]string{}, failSet: failSet}
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	client.AddHook(fr)
	jwtRefresh := auth.NewJWTManager("test-refresh-secret", time.Hour)
	svc := &Service{
		accounts:   fakeAccounts{},
		redis:      &database.RedisClient{Client: client},
		jwtAccess:  auth.NewJWTManager("test-access-secret", time.Hour),
		jwtRefresh: jwtRefresh,
	}
	old, err := jwtRefresh.Generate("acc-1", "player-1", "player")
	if err != nil {
		t.Fatal(err)
	}
	fr.data["session:"+old] = "acc-1:player-1"
	return svc, fr, old
}

func TestRefreshKeepsOldSessionWhenSavingNewFails(t *testing.T) {
	svc, fr, old := newRotationService(t, true)
	_, err := svc.RefreshToken(context.Background(), old)
	if err == nil || errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("err = %v, want a server failure", err)
	}
	if _, ok := fr.data["session:"+old]; !ok {
		t.Fatal("old session revoked although the new one was not saved: the player gets logged out on retry")
	}
}

func TestRefreshRotatesSession(t *testing.T) {
	svc, fr, old := newRotationService(t, false)
	resp, err := svc.RefreshToken(context.Background(), old)
	if err != nil {
		t.Fatal(err)
	}
	if resp.RefreshToken == old {
		t.Fatal("new refresh token equals the old one")
	}
	if _, ok := fr.data["session:"+old]; ok {
		t.Fatal("old session still valid after rotation")
	}
	if _, ok := fr.data["session:"+resp.RefreshToken]; !ok {
		t.Fatal("new session not saved")
	}
}
