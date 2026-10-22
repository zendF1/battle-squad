package matchmaker

import (
	"slices"

	"battle-squad/internal/game/gamedata"
)

// The matchmaking queues the matchmaker runs. A queue's matches play with the queue's id as
// their mode (MatchFound / MatchStarted mode, match history): ranked_2v2 changes rating,
// casual_2v2 does not. config_game_modes lobby rows join one of them (queue).
const (
	RankedQueue = "ranked_2v2"
	CasualQueue = "casual_2v2"
)

// Queues lists the 2v2 queues the matchmaker runs, in tick order. Each enabled boss adds its
// queue boss:<bossId> (gamedata.BossQueue, AllQueues).
var Queues = []string{RankedQueue, CasualQueue}

// AllQueues are Queues, then the queues of the current game data's enabled bosses.
func AllQueues() []string {
	return append(append([]string(nil), Queues...), gamedata.Current().BossQueues()...)
}

// KnownQueue reports whether the matchmaker runs queue q: a 2v2 queue or the queue of an
// enabled boss of the current game data.
func KnownQueue(q string) bool {
	if slices.Contains(Queues, q) {
		return true
	}
	_, ok := gamedata.Current().BossOfQueue(q)
	return ok
}

// IsBossQueue reports whether q is a boss queue (boss:<bossId>), known or not.
func IsBossQueue(q string) bool {
	_, ok := gamedata.BossIDOfQueue(q)
	return ok
}

// MaxPartySize is the most players one lobby of queue q may bring: BossPartySize (4) for a boss
// queue, TeamSlots (2) for a 2v2 queue.
func MaxPartySize(q string) int {
	if IsBossQueue(q) {
		return gamedata.BossPartySize
	}
	return TeamSlots
}

// PartySize is the most members a lobby of queue q holds: the party size of the queue's lobby
// mode (config_game_modes.party_size), at most MaxPartySize.
func PartySize(q string) int {
	return gamedata.Current().LobbyPartySize(q, MaxPartySize(q))
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
