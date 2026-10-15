package match

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"battle-squad/internal/game/ws"
)

// reservationFakes counts calls to a match's settle hooks.
type reservationFakes struct {
	rewards     atomic.Int32
	releases    atomic.Int32
	releaseCtxs chan error // ctx.Err() seen by each release
	rewardsErr  error
}

func fakeReservations(m *Match, rewardsErr error) *reservationFakes {
	f := &reservationFakes{releaseCtxs: make(chan error, 8), rewardsErr: rewardsErr}
	m.processRewards = func(ctx context.Context, stats map[string]*PlayerStats, usedItems map[string][]string) (map[string]RewardResult, error) {
		f.rewards.Add(1)
		return nil, f.rewardsErr
	}
	m.releaseReservations = func(ctx context.Context) error {
		f.releases.Add(1)
		f.releaseCtxs <- ctx.Err()
		return nil
	}
	return f
}

// runMatch starts the match loop and returns a channel closed when Run returns.
func runMatch(t *testing.T, m *Match) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		m.Run()
		close(done)
	}()
	return done
}

func waitRun(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("match loop did not exit")
	}
}

func TestStoppedMatchReleasesReservations(t *testing.T) {
	m, _ := newFlowMatch(t)
	f := fakeReservations(m, nil)

	done := runMatch(t, m)
	m.Stop()
	waitRun(t, done)

	if n := f.releases.Load(); n != 1 {
		t.Fatalf("releases = %d, want 1 for a match stopped before ending", n)
	}
	if err := <-f.releaseCtxs; err != nil {
		t.Fatalf("release ran on a cancelled context: %v", err)
	}
	if n := f.rewards.Load(); n != 0 {
		t.Fatalf("rewards = %d, want 0", n)
	}
}

func TestEndedMatchDoesNotReleaseAgain(t *testing.T) {
	m, clients := newFlowMatch(t)
	f := fakeReservations(m, nil)

	done := runMatch(t, m)
	m.ProcessEvent(context.Background(), clients["p1"], ws.Message{Event: "Leave"})
	waitRun(t, done)

	if n := f.rewards.Load(); n != 1 {
		t.Fatalf("rewards = %d, want 1", n)
	}
	if n := f.releases.Load(); n != 0 {
		t.Fatalf("releases = %d after rewards settled the match, want 0", n)
	}
}

func TestSecondLeaveAfterEndDoesNotPayAgain(t *testing.T) {
	m, clients := newFlowMatch(t)
	f := fakeReservations(m, nil)

	send(m, clients["p1"], "Leave", nil)
	send(m, clients["p2"], "Leave", nil)
	m.releaseUnsettledReservations()

	if n := f.rewards.Load(); n != 1 {
		t.Fatalf("rewards = %d, want 1", n)
	}
	if n := f.releases.Load(); n != 0 {
		t.Fatalf("releases = %d, want 0", n)
	}
}

func TestFailedRewardsReleaseOnExit(t *testing.T) {
	m, clients := newFlowMatch(t)
	f := fakeReservations(m, errors.New("db down"))

	done := runMatch(t, m)
	m.ProcessEvent(context.Background(), clients["p1"], ws.Message{Event: "Leave"})
	waitRun(t, done)

	if n := f.releases.Load(); n != 1 {
		t.Fatalf("releases = %d after rolled back rewards, want 1", n)
	}
}

func TestNoContestReleasesOnce(t *testing.T) {
	m, _ := newFlowMatch(t)
	f := fakeReservations(m, nil)

	m.endAsNoContest()
	m.releaseUnsettledReservations()

	if n := f.releases.Load(); n != 1 {
		t.Fatalf("releases = %d, want 1", n)
	}
}

// LeaveAndStop must never lose the Leave to the stop: the match always ends
// through rewards, never through a bare cancel.
func TestLeaveAndStopSettlesThroughRewards(t *testing.T) {
	for i := 0; i < 20; i++ {
		m, clients := newFlowMatch(t)
		f := fakeReservations(m, nil)

		done := runMatch(t, m)
		m.LeaveAndStop(context.Background(), clients["p1"])
		waitRun(t, done)

		if r, rel := f.rewards.Load(), f.releases.Load(); r != 1 || rel != 0 {
			t.Fatalf("run %d: rewards = %d, releases = %d, want 1 and 0", i, r, rel)
		}
	}
}
