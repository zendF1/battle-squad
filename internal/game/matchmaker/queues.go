package matchmaker

import "slices"

// The matchmaking queues the matchmaker runs. A queue's matches play with the queue's id as
// their mode (MatchFound / MatchStarted mode, match history): ranked_2v2 changes rating,
// casual_2v2 does not. config_game_modes lobby rows join one of them (queue).
const (
	RankedQueue = "ranked_2v2"
	CasualQueue = "casual_2v2"
)

// Queues lists the queues the matchmaker runs, in tick order.
var Queues = []string{RankedQueue, CasualQueue}

// KnownQueue reports whether the matchmaker runs queue q.
func KnownQueue(q string) bool {
	return slices.Contains(Queues, q)
}

// IsRankedQueue reports whether queue q's matches change rating.
func IsRankedQueue(q string) bool {
	return q == RankedQueue
}

// queueKeyPrefix + queue id is a queue's sorted set (score = party rating, member = entry id).
const queueKeyPrefix = "matchmaking:queue:"

// legacyQueueKey is the ranked queue's sorted set before there were several queues; the
// leader moves what it holds into the ranked queue (see Queue.MigrateLegacy).
const legacyQueueKey = "matchmaking:queue:2v2"

func queueKey(queue string) string {
	return queueKeyPrefix + queue
}

// queueOf is the queue of an entry; an entry stored before entries had one is ranked.
func queueOf(e QueueEntry) string {
	if e.Queue == "" {
		return RankedQueue
	}
	return e.Queue
}
