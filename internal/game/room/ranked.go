package room

import (
	"context"
	"errors"
	"math"
	"time"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/game/battlestats"
	"battle-squad/internal/game/match"
	"battle-squad/internal/game/matchmaker"
	"battle-squad/internal/shared/observability"
	"battle-squad/internal/shared/ranked"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RankedMode is the mode of matchmade games; only it changes rating.
const RankedMode = match.RankedMode

// rankedProfile is what a MatchFound card shows of a human.
type rankedProfile struct {
	DisplayName string
	Level       int
	TitleID     string
}

// loadRankedProfiles reads the display name, account level and equipped title of
// the humans. A missing profile keeps the queue entry's name and level 1.
func loadRankedProfiles(ctx context.Context, pool *pgxpool.Pool, playerIDs []string) map[string]rankedProfile {
	out := make(map[string]rankedProfile, len(playerIDs))
	if pool == nil || len(playerIDs) == 0 {
		return out
	}
	rows, err := pool.Query(ctx, `SELECT player_id, display_name, level FROM player_profiles WHERE player_id = ANY($1)`, playerIDs)
	if err != nil {
		observability.Log.Warn().Err(err).Msg("ranked: failed to load profiles for MatchFound")
		return out
	}
	for rows.Next() {
		var id string
		var p rankedProfile
		if err := rows.Scan(&id, &p.DisplayName, &p.Level); err == nil {
			out[id] = p
		}
	}
	rows.Close()
	for _, id := range playerIDs {
		t, err := battlestats.LoadEquippedTitle(ctx, pool, id)
		if err != nil {
			observability.Log.Warn().Err(err).Str("playerId", id).Msg("ranked: failed to load equipped title for MatchFound")
			continue
		}
		if t != nil {
			p := out[id]
			p.TitleID = t.ID
			out[id] = p
		}
	}
	return out
}

// rankedRoster is a matchmade game laid out for the room: the room players
// (team 1 then team 2, humans before bots), each human's lobby, the MatchFound
// teams (ids still empty) and each team's average rating for Elo, a bot
// counting as the humans' average rating.
type rankedRoster struct {
	Players      []RoomPlayer
	LobbyMapping map[string]string
	Found        MatchFound
	TeamRatings  map[int]int
	BotRating    int
}

// buildRankedRoster lays out the match. newBotID makes each bot's player id
// ("bot_" + suffix).
func buildRankedRoster(result matchmaker.MatchResult, profiles map[string]rankedProfile, newBotID func() string) rankedRoster {
	ratings := result.HumanRatings()
	botRating := result.BotRating()

	levelSum, humans := 0, 0
	for pid := range ratings {
		levelSum += max(profiles[pid].Level, 1)
		humans++
	}
	botLevel := 1
	if humans > 0 {
		botLevel = int(math.Round(float64(levelSum) / float64(humans)))
	}

	ro := rankedRoster{
		LobbyMapping: map[string]string{},
		Found:        MatchFound{MapID: result.MapID, Mode: RankedMode},
		TeamRatings:  map[int]int{},
		BotRating:    botRating,
	}
	card := func(p RoomPlayer, isBot bool, level, rating int, titleID string) MatchFoundPlayer {
		tier, div := ranked.TierAndDivision(rating)
		return MatchFoundPlayer{
			PlayerID: p.PlayerID, DisplayName: p.DisplayName, CharacterID: p.CharacterID, IsBot: isBot,
			Level: level, Rating: rating, Tier: tier, Division: div, TitleID: titleID,
		}
	}

	for i, team := range result.Teams {
		teamID := i + 1
		ft := MatchFoundTeam{TeamID: teamID, Players: []MatchFoundPlayer{}}
		sum := 0
		for _, entry := range team.Entries {
			for _, pid := range entry.PlayerIDs {
				prof := profiles[pid]
				name := prof.DisplayName
				if name == "" {
					name = entry.PlayerNames[pid]
				}
				if name == "" {
					name = "Player_" + pid[:min(6, len(pid))]
				}
				charID := entry.PlayerChars[pid]
				if charID == "" {
					charID = "rookie"
				}
				items := entry.PlayerItems[pid]
				if items == nil {
					items = []string{}
				}
				p := RoomPlayer{PlayerID: pid, DisplayName: name, TeamID: teamID, CharacterID: charID, Items: items, IsReady: true}
				ro.Players = append(ro.Players, p)
				ro.LobbyMapping[pid] = entry.LobbyID
				ft.Players = append(ft.Players, card(p, false, max(prof.Level, 1), ratings[pid], prof.TitleID))
				sum += ratings[pid]
			}
		}
		for b := 0; b < team.Bots; b++ {
			id := newBotID()
			p := RoomPlayer{PlayerID: id, DisplayName: "Bot " + id[len(id)-4:], TeamID: teamID, CharacterID: "rookie", Items: []string{}, IsReady: true}
			ro.Players = append(ro.Players, p)
			ft.Players = append(ft.Players, card(p, true, botLevel, botRating, ""))
			sum += botRating
		}
		if n := len(ft.Players); n > 0 {
			ro.TeamRatings[teamID] = int(math.Round(float64(sum) / float64(n)))
		}
		ro.Found.Teams = append(ro.Found.Teams, ft)
	}
	if len(ro.Players) > 0 {
		ro.Players[0].IsHost = true
	}
	return ro
}

// reserveRankedItems reserves every human's items for the match like a custom
// room start, so the items spent are deducted at the end. A player whose
// items cannot be reserved (no longer owned, over a use limit) plays without
// items; a database failure starts the match with no items for anyone.
func (r *Room) reserveRankedItems(matchID string) {
	for attempt := 0; attempt <= len(r.State.Players); attempt++ {
		err := r.reservePlayerItems(matchID)
		if err == nil {
			return
		}
		var loadoutErr *LoadoutError
		if !errors.As(err, &loadoutErr) || loadoutErr.PlayerID == "" {
			observability.Log.Error().Err(err).Str("roomId", r.ID).Msg("ranked: failed to reserve items; playing without items")
			break
		}
		observability.Log.Info().Str("roomId", r.ID).Str("playerId", loadoutErr.PlayerID).Str("reason", loadoutErr.Reason).
			Msg("ranked: items not reservable; player plays without items")
		for i := range r.State.Players {
			if r.State.Players[i].PlayerID == loadoutErr.PlayerID {
				r.State.Players[i].Items = []string{}
			}
		}
	}
	for i := range r.State.Players {
		r.State.Players[i].Items = []string{}
	}
}

// startRankedMatch builds the match from the room players and runs it after
// startDelay (the MatchFound reveal); it counts for milestones and changes
// rating by Elo.
func (r *Room) startRankedMatch(matchID string, botTierConfig matchmaker.BotTierConfig, eloParams match.EloParams, teamRatings map[int]int, startDelay time.Duration) {
	team1Spawns, team2Spawns := r.teamSpawns()
	team1Idx, team2Idx := 0, 0

	matchPlayers := []*match.BattlePlayerState{}
	for _, p := range r.State.Players {
		var spawnPos match.Vector2
		if p.TeamID == 2 {
			sp := team2Spawns[team2Idx%len(team2Spawns)]
			spawnPos = match.Vector2{X: sp.X, Y: sp.Y}
			team2Idx++
		} else {
			sp := team1Spawns[team1Idx%len(team1Spawns)]
			spawnPos = match.Vector2{X: sp.X, Y: sp.Y}
			team1Idx++
		}

		isBot := isBotID(p.PlayerID)
		stats, look := baseStats(p.CharacterID), cosmetics{}
		if !isBot {
			stats, look = r.getActualStats(p.PlayerID, p.CharacterID)
		}
		matchPlayers = append(matchPlayers, newBattlePlayer(p, spawnPos, stats, look, isBot))
	}

	r.match = match.NewMatch(
		matchID,
		r.ID,
		r.State.Mode,
		r.State.MapID,
		matchPlayers,
		r.Clients,
		r.db,
		r.hub.redis,
		economy.NewRepository(),
		r.hub,
	)
	r.match.TeamRatings = teamRatings
	r.match.EloParams = eloParams
	r.match.StartDelay = startDelay
	// Matchmade matches (with or without bots) count for milestones
	r.match.CountsForMilestones = true
	// Zero config means the tier is missing from bot_difficulty: keep the default
	// rather than a bot with no aim error
	if botTierConfig != (matchmaker.BotTierConfig{}) {
		r.match.BotTier = botTierConfig
	}

	r.matchDone = r.match.MatchDone
	go r.match.Run()
	r.hub.SyncRoomState(context.Background(), &r.State)

	observability.Log.Info().
		Str("roomId", r.ID).
		Str("matchId", matchID).
		Int("team1Rating", teamRatings[1]).
		Int("team2Rating", teamRatings[2]).
		Msg("ranked match started")
}

func isBotID(playerID string) bool {
	return len(playerID) >= 4 && playerID[:4] == "bot_"
}
