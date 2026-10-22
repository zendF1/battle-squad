package match

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
)

// TerrainSnapshot is the authoritative terrain sent to clients on match start and reconnect.
// Masks are row-major (index = y*Width + x), run-length encoded as alternating air/solid run
// lengths (starting with air) written as unsigned varints, then base64 (std) encoded.
type TerrainSnapshot struct {
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	Version      uint64 `json:"version"` // number of crater operations (Terrain.Carve) already applied
	Solid        string `json:"solid"`
	Destructible string `json:"destructible"`
}

func (t *Terrain) Snapshot() TerrainSnapshot {
	n := t.Width * t.Height
	return TerrainSnapshot{
		Width:        t.Width,
		Height:       t.Height,
		Version:      t.Version,
		Solid:        encodeMaskRLE(t.Mask, n),
		Destructible: encodeMaskRLE(t.DestructibleMask, n),
	}
}

func encodeMaskRLE(mask []uint64, n int) string {
	buf := make([]byte, 0, 1024)
	var tmp [binary.MaxVarintLen64]byte

	current := false
	run := uint64(0)
	for i := 0; i < n; i++ {
		bit := mask[i/64]&(uint64(1)<<(i%64)) != 0
		if bit != current {
			buf = append(buf, tmp[:binary.PutUvarint(tmp[:], run)]...)
			current = bit
			run = 0
		}
		run++
	}
	buf = append(buf, tmp[:binary.PutUvarint(tmp[:], run)]...)
	return base64.StdEncoding.EncodeToString(buf)
}

// matchSnapshot is the MatchStarted / MatchStateSync payload: match state plus terrain and
// the version of the game data the match plays with (the GET /gamedata version of that same
// snapshot; a client with another cached version refetches it before it lets the player act).
type matchSnapshot struct {
	MatchState
	Terrain         TerrainSnapshot `json:"terrain"`
	GamedataVersion string          `json:"gamedataVersion"`
}

func (m *Match) snapshotPayload() []byte {
	payload, _ := json.Marshal(m.snapshot())
	return payload
}

func (m *Match) snapshot() matchSnapshot {
	return matchSnapshot{MatchState: m.State, Terrain: m.Terrain.Snapshot(), GamedataVersion: m.gd.Version}
}

// matchStateSync is the MatchStateSync payload: the snapshot plus, for the requesting player,
// the seq of the last Move the match loop took from them this turn (0 if none; see noteMoveSeq),
// and the current player's armed skill or shot item (omitted for the weapon), so a rejoining
// client shows that player's use badge.
type matchStateSync struct {
	matchSnapshot
	LastMoveSeq    int             `json:"lastMoveSeq"`
	SelectedAction *ActionSelected `json:"selectedAction,omitempty"`
}

func (m *Match) syncPayload(playerID string) []byte {
	payload, _ := json.Marshal(matchStateSync{
		matchSnapshot:  m.snapshot(),
		LastMoveSeq:    m.lastMoveSeq[playerID],
		SelectedAction: m.syncSelection(),
	})
	return payload
}
