package matchmaker

import (
	"sort"

	"battle-squad/internal/game/gamedata"
)

// planBossMatches forms the matches of one tick of a boss queue: first come, first served, no
// rating. Entries (parties of 1..4 and solos) are taken oldest first; each match starts with
// the oldest entry left and adds the next entries, in queue order, that still fit in
// BossPartySize humans. A party of 4 starts at once; a smaller one starts once its oldest entry
// has waited botFillAfter seconds, bot teammates filling the free places. Every human plays on
// team 1 (Teams[0]); team 2 is the boss's side, built by the room (Teams[1] stays empty).
func planBossMatches(entries []QueueEntry, now int64, cfg MatchmakingConfig) []MatchResult {
	queue := make([]QueueEntry, 0, len(entries))
	for _, e := range entries {
		if n := len(e.PlayerIDs); n >= 1 && n <= gamedata.BossPartySize {
			queue = append(queue, e)
		}
	}
	sort.SliceStable(queue, func(i, j int) bool {
		if queue[i].QueuedAt != queue[j].QueuedAt {
			return queue[i].QueuedAt < queue[j].QueuedAt
		}
		return queue[i].EntryID < queue[j].EntryID
	})

	used := map[string]bool{}
	var out []MatchResult
	for i, e := range queue {
		if used[e.EntryID] {
			continue
		}
		group, humans := []QueueEntry{e}, len(e.PlayerIDs)
		for _, o := range queue[i+1:] {
			if humans == gamedata.BossPartySize {
				break
			}
			if !used[o.EntryID] && humans+len(o.PlayerIDs) <= gamedata.BossPartySize {
				group = append(group, o)
				humans += len(o.PlayerIDs)
			}
		}
		if humans < gamedata.BossPartySize && now-e.QueuedAt < int64(cfg.BotFillAfter) {
			continue
		}
		for _, g := range group {
			used[g.EntryID] = true
		}
		bots := gamedata.BossPartySize - humans
		out = append(out, MatchResult{Teams: [2]MatchTeam{{Entries: group, Bots: bots}, {}}, HasBot: bots > 0})
	}
	return out
}
