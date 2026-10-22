package matchmaker

import (
	"math"
	"sort"
)

// TeamSlots is how many players a 2v2 team has (every queue).
const TeamSlots = 2

// maxCandidates bounds how many in-range entries one entry's search looks at
// (the closest by rating), so a long queue stays cheap.
const maxCandidates = 24

// MatchTeam is one side of a matched game: the queue entries whose players
// play on it and how many bots fill its remaining slots.
type MatchTeam struct {
	Entries []QueueEntry
	Bots    int
}

// Humans is how many players the team's entries bring.
func (t MatchTeam) Humans() int {
	n := 0
	for _, e := range t.Entries {
		n += len(e.PlayerIDs)
	}
	return n
}

// MatchResult is a formed 2v2 game of a queue (its mode): Teams[0] plays as
// team 1, Teams[1] as team 2; each team has TeamSlots players (humans + bots).
type MatchResult struct {
	Queue  string
	Teams  [2]MatchTeam
	MapID  string
	HasBot bool
}

// Mode is the match's mode: its queue (ranked_2v2 when unset).
func (r MatchResult) Mode() string {
	if r.Queue == "" {
		return RankedQueue
	}
	return r.Queue
}

// Entries lists every queue entry of the match.
func (r MatchResult) Entries() []QueueEntry {
	var out []QueueEntry
	for _, t := range r.Teams {
		out = append(out, t.Entries...)
	}
	return out
}

// HumanRatings maps every human of the match to their own rating.
func (r MatchResult) HumanRatings() map[string]int {
	out := map[string]int{}
	for _, e := range r.Entries() {
		for _, pid := range e.PlayerIDs {
			out[pid] = e.PlayerRatings[pid]
		}
	}
	return out
}

// BotRating is the rating a bot counts as: the average of the match's humans
// (0 without humans).
func (r MatchResult) BotRating() int {
	return avgRating(r.HumanRatings())
}

// MaxHumanRating is the highest rating among the match's humans.
func (r MatchResult) MaxHumanRating() int {
	best := 0
	for _, v := range r.HumanRatings() {
		if v > best {
			best = v
		}
	}
	return best
}

func avgRating(ratings map[string]int) int {
	if len(ratings) == 0 {
		return 0
	}
	sum := 0
	for _, v := range ratings {
		sum += v
	}
	return int(math.Round(float64(sum) / float64(len(ratings))))
}

// planMatches forms the matches of one tick from the entries of one queue (every
// queue plans the same way; the caller sets the results' Queue).
//
// Entries are served oldest first. Two entries may share a match when their
// party ratings differ by at most the smaller of their allowed ranges
// (calculateAllowedRange), and every pair of entries in a match must be.
// A full match (4 humans) is party vs party, party vs 2 solos, or 4 solos
// split into the pairing whose team averages are closest. When an entry has
// no full match and has waited botFillAfter seconds, bots fill the slots the
// in-range humans leave free, preferring the most humans:
//
//   - party + solo in range: party vs solo + bot
//   - solo + party in range: solo + bot vs party
//   - solo + 2 solos: two of them teammates vs the third + bot (closest averages)
//   - solo + 1 solo: opponents, each with a bot (1+bot vs 1+bot)
//   - alone: party vs 2 bots, or solo + bot vs 2 bots
//
// A bot counts as the average rating of the match's humans.
func planMatches(entries []QueueEntry, now int64, cfg MatchmakingConfig) []MatchResult {
	queue := make([]QueueEntry, 0, len(entries))
	for _, e := range entries {
		if n := len(e.PlayerIDs); n >= 1 && n <= TeamSlots {
			queue = append(queue, e)
		}
	}
	sort.SliceStable(queue, func(i, j int) bool {
		if queue[i].QueuedAt != queue[j].QueuedAt {
			return queue[i].QueuedAt < queue[j].QueuedAt
		}
		return queue[i].EntryID < queue[j].EntryID
	})

	rangeOf := func(e QueueEntry) int {
		wait := now - e.QueuedAt
		if wait < 0 {
			wait = 0
		}
		return calculateAllowedRange(int(wait), cfg)
	}
	compatible := func(a, b QueueEntry) bool {
		diff := a.Rating - b.Rating
		if diff < 0 {
			diff = -diff
		}
		return diff <= min(rangeOf(a), rangeOf(b))
	}

	used := map[string]bool{}
	var out []MatchResult
	for _, e := range queue {
		if used[e.EntryID] {
			continue
		}
		var cands []QueueEntry
		for _, o := range queue {
			if o.EntryID != e.EntryID && !used[o.EntryID] && compatible(e, o) {
				cands = append(cands, o)
			}
		}
		if len(cands) > maxCandidates {
			sort.SliceStable(cands, func(i, j int) bool {
				return absInt(cands[i].Rating-e.Rating) < absInt(cands[j].Rating-e.Rating)
			})
			cands = cands[:maxCandidates]
		}

		res, ok := bestFullMatch(e, cands, compatible)
		if !ok {
			if now-e.QueuedAt < int64(cfg.BotFillAfter) {
				continue
			}
			res = bestBotFill(e, cands, compatible)
		}
		for _, m := range res.Entries() {
			used[m.EntryID] = true
		}
		out = append(out, res)
	}
	return out
}

// plan is a candidate match with its imbalance (lower is better).
type plan struct {
	res  MatchResult
	cost float64
}

func better(p plan, best *plan) bool {
	return best == nil || p.cost < best.cost
}

// newPlan builds a match from two teams (bots fill the free slots) and costs it
// as the gap between the team averages, a bot counting as the humans' average.
func newPlan(team1, team2 []QueueEntry) plan {
	res := MatchResult{Teams: [2]MatchTeam{{Entries: team1}, {Entries: team2}}}
	for i := range res.Teams {
		res.Teams[i].Bots = TeamSlots - res.Teams[i].Humans()
		if res.Teams[i].Bots > 0 {
			res.HasBot = true
		}
	}
	bot := float64(res.BotRating())
	avg := func(t MatchTeam) float64 {
		sum := float64(t.Bots) * bot
		for _, e := range t.Entries {
			for _, pid := range e.PlayerIDs {
				sum += float64(e.PlayerRatings[pid])
			}
		}
		return sum / TeamSlots
	}
	return plan{res: res, cost: math.Abs(avg(res.Teams[0]) - avg(res.Teams[1]))}
}

// allCompatible reports whether every pair of the entries may share a match.
func allCompatible(compatible func(a, b QueueEntry) bool, es ...QueueEntry) bool {
	for i := range es {
		for j := i + 1; j < len(es); j++ {
			if !compatible(es[i], es[j]) {
				return false
			}
		}
	}
	return true
}

// bestFullMatch finds e's most balanced 4-human match among the candidates
// (all already in range of e).
func bestFullMatch(e QueueEntry, cands []QueueEntry, compatible func(a, b QueueEntry) bool) (MatchResult, bool) {
	var parties, solos []QueueEntry
	for _, c := range cands {
		if len(c.PlayerIDs) == TeamSlots {
			parties = append(parties, c)
		} else {
			solos = append(solos, c)
		}
	}
	var best *plan
	consider := func(p plan) {
		if better(p, best) {
			best = &p
		}
	}

	if len(e.PlayerIDs) == TeamSlots {
		for _, p := range parties {
			consider(newPlan([]QueueEntry{e}, []QueueEntry{p}))
		}
		for i := range solos {
			for j := i + 1; j < len(solos); j++ {
				if compatible(solos[i], solos[j]) {
					consider(newPlan([]QueueEntry{e}, []QueueEntry{solos[i], solos[j]}))
				}
			}
		}
	} else {
		for _, p := range parties {
			for _, s := range solos {
				if compatible(p, s) {
					consider(newPlan([]QueueEntry{e, s}, []QueueEntry{p}))
				}
			}
		}
		for i := range solos {
			for j := i + 1; j < len(solos); j++ {
				for k := j + 1; k < len(solos); k++ {
					a, b, c := solos[i], solos[j], solos[k]
					if !allCompatible(compatible, a, b, c) {
						continue
					}
					consider(newPlan([]QueueEntry{e, a}, []QueueEntry{b, c}))
					consider(newPlan([]QueueEntry{e, b}, []QueueEntry{a, c}))
					consider(newPlan([]QueueEntry{e, c}, []QueueEntry{a, b}))
				}
			}
		}
	}
	if best == nil {
		return MatchResult{}, false
	}
	return best.res, true
}

// bestBotFill builds e's match with bots after its wait ran out, using the most
// in-range humans it can (see planMatches).
func bestBotFill(e QueueEntry, cands []QueueEntry, compatible func(a, b QueueEntry) bool) MatchResult {
	var parties, solos []QueueEntry
	for _, c := range cands {
		if len(c.PlayerIDs) == TeamSlots {
			parties = append(parties, c)
		} else {
			solos = append(solos, c)
		}
	}
	var best *plan
	consider := func(p plan) {
		if better(p, best) {
			best = &p
		}
	}

	if len(e.PlayerIDs) == TeamSlots {
		for _, s := range solos {
			consider(newPlan([]QueueEntry{e}, []QueueEntry{s}))
		}
		if best == nil {
			consider(newPlan([]QueueEntry{e}, nil))
		}
		return best.res
	}

	// 3 humans: a party, or two more solos
	for _, p := range parties {
		consider(newPlan([]QueueEntry{e}, []QueueEntry{p}))
	}
	for i := range solos {
		for j := i + 1; j < len(solos); j++ {
			a, b := solos[i], solos[j]
			if !compatible(a, b) {
				continue
			}
			consider(newPlan([]QueueEntry{e, a}, []QueueEntry{b}))
			consider(newPlan([]QueueEntry{e, b}, []QueueEntry{a}))
			consider(newPlan([]QueueEntry{a, b}, []QueueEntry{e}))
		}
	}
	if best != nil {
		return best.res
	}
	// 2 humans: opponents, each with a bot
	for _, s := range solos {
		consider(newPlan([]QueueEntry{e}, []QueueEntry{s}))
	}
	if best != nil {
		return best.res
	}
	return newPlan([]QueueEntry{e}, nil).res
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
