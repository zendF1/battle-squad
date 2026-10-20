package milestone

import (
	"sort"
	"strconv"
	"strings"
)

// TierOrder ranks the rank tier ids (bronze 1 … master 6); unknown is 0. rank_peak stores it.
func TierOrder(tier string) int {
	switch tier {
	case "bronze":
		return 1
	case "silver":
		return 2
	case "gold":
		return 3
	case "platinum":
		return 4
	case "diamond":
		return 5
	case "master":
		return 6
	}
	return 0
}

// setLevel is the level of a set target ("set_level:6" → 6, true).
func setLevel(target string) (int, bool) {
	if !strings.HasPrefix(target, SetTargetPrefix) {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(target, SetTargetPrefix))
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// knownTarget reports whether Progress can compute target.
func knownTarget(target string) bool {
	switch target {
	case TargetCharLevel, TargetUpgradePeak, TargetWeaponPeak, TargetMatches, TargetWins, TargetCharWins, TargetRankPeak:
		return true
	}
	_, ok := setLevel(target)
	return ok
}

// Supported: the server can compute the milestone's progress and grant every reward. Others
// are not served and cannot be claimed (e.g. a title item whose id is not an active
// config_titles row: Repository.Milestones leaves its Name empty).
func Supported(m Milestone) bool {
	if !knownTarget(m.Target) || m.Required <= 0 {
		return false
	}
	for _, it := range m.Rewards.Items {
		if !supportedRewardKinds[it.Kind] {
			return false
		}
		if it.Kind == RewardKindTitle {
			if it.ID == "" || it.Name == "" {
				return false
			}
			continue
		}
		if it.Qty <= 0 {
			return false
		}
		if it.Kind == RewardKindStone && (it.Level < 1 || it.Level > 12) {
			return false
		}
		if it.Kind == RewardKindItem && it.ID == "" {
			return false
		}
	}
	return true
}

// Progress is the player's value for target.
func Progress(target string, p PlayerProgress) int {
	switch target {
	case TargetCharLevel:
		best := 0
		for _, lv := range p.CharLevels {
			best = max(best, lv)
		}
		return best
	case TargetUpgradePeak, TargetWeaponPeak:
		best := p.Counters[target]
		for _, pc := range p.Pieces {
			if target == TargetUpgradePeak || pc.Slot == "weapon" {
				best = max(best, pc.Level)
			}
		}
		return best
	case TargetMatches, TargetWins, TargetRankPeak:
		return p.Counters[target]
	case TargetCharWins:
		best := 0
		for k, v := range p.Counters {
			if strings.HasPrefix(k, CounterCharWinsPrefix) {
				best = max(best, v)
			}
		}
		return best
	}
	if level, ok := setLevel(target); ok {
		return SetProgress(p.Pieces, level)
	}
	return 0
}

// SetProgress is the most slots one character wears at >= level (0..SlotCount).
func SetProgress(pieces []Piece, level int) int {
	slots := map[string]map[string]bool{}
	for _, pc := range pieces {
		if pc.EquippedOn == "" || pc.Level < level {
			continue
		}
		if slots[pc.EquippedOn] == nil {
			slots[pc.EquippedOn] = map[string]bool{}
		}
		slots[pc.EquippedOn][pc.Slot] = true
	}
	best := 0
	for _, s := range slots {
		best = max(best, len(s))
	}
	return min(best, SlotCount)
}

// State of a milestone at progress.
func State(m Milestone, progress int) string {
	switch {
	case m.Claimed:
		return StateClaimed
	case progress >= m.Required:
		return StateClaimable
	}
	return StateLocked
}

// BuildList groups the supported milestones into tracks (TrackOrder, then any other track by
// name), nodes by sort order then id, and counts the claimable ones.
func BuildList(ms []Milestone, p PlayerProgress) ListResponse {
	byTrack := map[string][]Node{}
	claimable := 0
	for _, m := range ms {
		if !Supported(m) {
			continue
		}
		progress := Progress(m.Target, p)
		state := State(m, progress)
		if state == StateClaimable {
			claimable++
		}
		byTrack[m.Track] = append(byTrack[m.Track], Node{
			ID: m.ID, Track: m.Track, Target: m.Target, Required: m.Required, Progress: progress,
			State: state, Rewards: normalized(m.Rewards), Description: m.Description,
		})
	}
	order := map[string]int{}
	for i, t := range TrackOrder {
		order[t] = i
	}
	names := make([]string, 0, len(byTrack))
	for t := range byTrack {
		names = append(names, t)
	}
	sort.Slice(names, func(i, j int) bool {
		oi, iok := order[names[i]]
		oj, jok := order[names[j]]
		if iok != jok {
			return iok
		}
		if iok && oi != oj {
			return oi < oj
		}
		return names[i] < names[j]
	})
	sortKey := map[string]int{}
	for _, m := range ms {
		sortKey[m.ID] = m.SortOrder
	}
	out := ListResponse{Tracks: []Track{}, ClaimableCount: claimable}
	for _, t := range names {
		nodes := byTrack[t]
		sort.SliceStable(nodes, func(i, j int) bool {
			if sortKey[nodes[i].ID] != sortKey[nodes[j].ID] {
				return sortKey[nodes[i].ID] < sortKey[nodes[j].ID]
			}
			return nodes[i].ID < nodes[j].ID
		})
		out.Tracks = append(out.Tracks, Track{Track: t, Nodes: nodes})
	}
	return out
}

func normalized(r Rewards) Rewards {
	if r.Items == nil {
		r.Items = []RewardItem{}
	}
	return r
}

// SumRewards adds b to a, merging stones of the same level and items of the same id.
func SumRewards(a, b Rewards) Rewards {
	out := Rewards{Coin: a.Coin + b.Coin, Gem: a.Gem + b.Gem, Items: append([]RewardItem{}, a.Items...)}
	for _, it := range b.Items {
		merged := false
		for i := range out.Items {
			o := &out.Items[i]
			if o.Kind == it.Kind && o.Level == it.Level && o.ID == it.ID {
				o.Qty += it.Qty
				merged = true
				break
			}
		}
		if !merged {
			out.Items = append(out.Items, it)
		}
	}
	return out
}
