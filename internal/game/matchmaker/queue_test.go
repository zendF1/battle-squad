package matchmaker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"battle-squad/internal/shared/database"

	"github.com/redis/go-redis/v9"
)

func TestQueues(t *testing.T) {
	for _, q := range []string{RankedQueue, CasualQueue} {
		if !KnownQueue(q) {
			t.Fatalf("%s unknown", q)
		}
	}
	for _, q := range []string{"", "2v2", "boss_4p"} {
		if KnownQueue(q) {
			t.Fatalf("%q known", q)
		}
	}
	if !IsRankedQueue(RankedQueue) || IsRankedQueue(CasualQueue) || IsRankedQueue("") {
		t.Fatal("only ranked_2v2 is ranked")
	}
	if queueKey(RankedQueue) != "matchmaking:queue:ranked_2v2" || queueKey(CasualQueue) != "matchmaking:queue:casual_2v2" {
		t.Fatal("queue keys")
	}
	if (MatchResult{}).Mode() != RankedQueue || (MatchResult{Queue: CasualQueue}).Mode() != CasualQueue {
		t.Fatal("MatchResult.Mode")
	}
	if queueOf(QueueEntry{}) != RankedQueue || queueOf(QueueEntry{Queue: CasualQueue}) != CasualQueue {
		t.Fatal("queueOf")
	}
}

// A casual queue plans like ranked: the same entries make the same matches (party/solo
// pairing and bot fill).
func TestCasualPlansLikeRanked(t *testing.T) {
	cfg := DefaultMatchmakingConfig()
	entries := []QueueEntry{solo("a", 1000, 30), solo("b", 1020, 5), party("c", 990, "d", 1000, 1), solo("e", 2000, 25)}
	ranked := layouts(planMatches(entries, testNow, cfg))
	casual := make([]QueueEntry, len(entries))
	for i, e := range entries {
		e.Queue = CasualQueue
		casual[i] = e
	}
	got := layouts(planMatches(casual, testNow, cfg))
	if !slices.Equal(got, ranked) || !slices.Equal(got, []string{"a+b vs c+d", "e+bot vs bot+bot"}) {
		t.Fatalf("casual %v ranked %v", got, ranked)
	}
}

// testQueue is a Queue on Redis DB 7 at TEST_REDIS_ADDR (skipped without it).
func testQueue(t *testing.T) *Queue {
	t.Helper()
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("TEST_REDIS_ADDR not set")
	}
	r, err := database.NewRedisClient(addr, "", 7, 4, 0)
	if err != nil {
		t.Skipf("redis: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return NewQueue(r)
}

func entryIDs(t *testing.T, q *Queue, queue string) []string {
	t.Helper()
	entries, err := q.GetAllEntries(context.Background(), queue)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, e := range entries {
		if e.Queue != queue {
			t.Fatalf("entry %s of %s says queue %q", e.EntryID, queue, e.Queue)
		}
		ids = append(ids, e.EntryID)
	}
	return ids
}

// Each queue has its own sorted set: entries are listed, cancelled and claimed in their
// own queue; the old ranked key's entries move into the ranked queue.
func TestQueuesAreSeparateRedis(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()
	tag := fmt.Sprintf("it_%d_", time.Now().UnixNano())
	r := QueueEntry{EntryID: tag + "r", Queue: RankedQueue, PlayerIDs: []string{tag + "pr"}, Rating: 1000, QueuedAt: time.Now().Unix()}
	c := QueueEntry{EntryID: tag + "c", Queue: CasualQueue, PlayerIDs: []string{tag + "pc"}, Rating: 1000, QueuedAt: time.Now().Unix()}
	for _, e := range []QueueEntry{r, c} {
		if err := q.Enqueue(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, e := range []QueueEntry{r, c} {
			q.Dequeue(context.Background(), e) //nolint:errcheck
		}
	})

	if ids := entryIDs(t, q, RankedQueue); !slices.Contains(ids, r.EntryID) || slices.Contains(ids, c.EntryID) {
		t.Fatalf("ranked queue %v", ids)
	}
	if ids := entryIDs(t, q, CasualQueue); !slices.Contains(ids, c.EntryID) || slices.Contains(ids, r.EntryID) {
		t.Fatalf("casual queue %v", ids)
	}

	got, err := q.CancelByPlayer(ctx, tag+"pc")
	if err != nil || got == nil || got.EntryID != c.EntryID {
		t.Fatalf("cancel casual: %+v %v", got, err)
	}
	if slices.Contains(entryIDs(t, q, CasualQueue), c.EntryID) {
		t.Fatal("cancelled casual entry still queued")
	}
	if ok, err := q.Claim(ctx, []QueueEntry{r}); !ok || err != nil {
		t.Fatalf("claim ranked: %v %v", ok, err)
	}
	if slices.Contains(entryIDs(t, q, RankedQueue), r.EntryID) {
		t.Fatal("claimed ranked entry still queued")
	}

	// An entry the old code queued (old key, no queue in its JSON) becomes a ranked one
	old := QueueEntry{EntryID: tag + "old", PlayerIDs: []string{tag + "po"}, Rating: 1100, QueuedAt: time.Now().Unix()}
	raw, _ := json.Marshal(old)
	q.redis.Client.Set(ctx, entryKeyPrefix+old.EntryID, raw, entryTTL)
	q.redis.Client.ZAdd(ctx, legacyQueueKey, redis.Z{Score: 1100, Member: old.EntryID})
	t.Cleanup(func() { q.Dequeue(context.Background(), old) }) //nolint:errcheck
	if err := q.MigrateLegacy(ctx); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(entryIDs(t, q, RankedQueue), old.EntryID) {
		t.Fatal("legacy entry not in the ranked queue")
	}
	if n, _ := q.redis.Client.ZScore(ctx, legacyQueueKey, old.EntryID).Result(); n != 0 {
		t.Fatal("legacy entry still in the old key")
	}
}
