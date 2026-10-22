package room

import (
	"context"
	"time"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/match"
	"battle-squad/internal/game/matchmaker"
	"battle-squad/internal/shared/observability"
)

// Boss matches (mode boss:<bossId>, see match/boss.go). The matchmaker puts every human on team 1
// and leaves bot teammates to fill it to 4 (the varied bots of bots.go, at the humans' average
// level); the room adds the boss's side (match.NewBossSide) as team 2 and the MatchFound cards
// of the boss and its first minions.

// bossCards are the MatchFound cards of the boss's side: the boss (level = its recommended
// level) and its minions.
func bossCards(cfg gamedata.BossConfig, side []*match.BattlePlayerState) []MatchFoundPlayer {
	out := make([]MatchFoundPlayer, 0, len(side))
	for _, p := range side {
		out = append(out, MatchFoundPlayer{PlayerID: p.PlayerID, DisplayName: p.DisplayName, CharacterID: p.CharacterID, IsBot: true,
			Level: cfg.RecommendedLevel, Role: p.Role, BossID: p.BossID, MinionID: p.MinionID})
	}
	return out
}

// startBossMatch builds the boss match from the room players (team 1) and side (team 2) and
// runs it after startDelay (the MatchFound reveal); it counts for milestones and daily missions.
func (r *Room) startBossMatch(matchID string, gd *gamedata.Snapshot, cfg gamedata.BossConfig, side []*match.BattlePlayerState,
	botTierConfig matchmaker.BotTierConfig, startDelay time.Duration) {
	party, _, _ := gamedata.BossSpawns(gd.Data.Maps[cfg.MapID])
	players := make([]*match.BattlePlayerState, 0, len(r.State.Players)+len(side))
	for i, p := range r.State.Players {
		sp := party[i%len(party)]
		isBot := isBotID(p.PlayerID)
		stats, look := r.playerStats(p, isBot)
		players = append(players, newBattlePlayer(p, match.Vector2{X: sp.X, Y: sp.Y}, stats, look, isBot))
	}
	players = append(players, side...)

	r.match = match.NewMatchWithData(gd, matchID, r.ID, r.State.Mode, r.State.MapID, players, r.Clients, r.db, r.hub.redis,
		economy.NewRepository(), r.hub)
	r.match.SetupBoss(cfg)
	r.match.StartDelay = startDelay
	r.match.CountsForMilestones = true
	if botTierConfig != (matchmaker.BotTierConfig{}) {
		r.match.BotTier = botTierConfig
	}

	r.matchDone = r.match.MatchDone
	go r.match.Run()
	r.hub.SyncRoomState(context.Background(), &r.State)

	observability.Log.Info().Str("roomId", r.ID).Str("matchId", matchID).Str("bossId", cfg.BossID).
		Int("party", len(r.State.Players)).Msg("boss match started")
}
