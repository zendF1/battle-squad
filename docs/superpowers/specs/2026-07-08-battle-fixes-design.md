# Battle Fixes & Features — Design Spec

**Date:** 2026-07-08

## Overview

Four changes to the battle/match system: cancel shot UI, fall death, single-use items, and teleport item.

---

## 1. Cancel Shot Icon (X)

### Behavior
- When player starts drag-to-shoot, an X icon appears near the current player character on the game canvas.
- The icon is a semi-transparent circle (~30px radius) with an X drawn inside.
- If the user drags their touch point into the X icon's hitbox, the shot is cancelled: trajectory hidden, drag state reset, no shot fired.
- The X icon disappears when the drag ends (whether cancelled or fired).

### Implementation

**Client only — no server changes.**

**`battle_game.dart`:**
- Add `CancelZoneComponent` — a Flame `PositionComponent` rendered near the active player.
- Shows/hides via `showCancelZone(Vector2 playerPos)` / `hideCancelZone()`.
- Renders: semi-transparent dark circle + white X icon.

**`match_screen.dart`:**
- `_onDragStart`: call `_game.showCancelZone(playerScreenPos)`.
- `_onDragUpdate`: check if current touch position is within cancel zone hitbox. If yes, change icon to "active" state (e.g., red tint). Update a `_isInCancelZone` flag.
- `_onDragEnd`: if `_isInCancelZone`, cancel shot (hide trajectory, do not send Shoot). Otherwise fire normally.
- On cancel or fire: call `_game.hideCancelZone()`.

---

## 2. Fall Death (No Ground → Die)

### Behavior
- After terrain destruction, if a player falls below the map boundary (y > 900), they die immediately (HP = 0, isAlive = false).
- Dead players cannot be controlled (existing `isAlive` checks cover turn logic).

### Implementation

**Server (`internal/game/match/match.go`):**
- After processing terrain destruction and applying gravity/fall to players, check each player's Y position.
- If `player.Position.Y > 900` (world height): set `player.HP = 0`, `player.IsAlive = false`.
- Emit `PlayerDamagedEvent` with damage = remaining HP, type = "fall".
- Check win condition after fall deaths.

**Client (`player_component.dart`):**
- In `update()`: if `position.y > 900` and still rendering, mark as dead locally for immediate visual feedback (stop rendering, stop gravity).
- Server state sync will confirm via `PlayerDamagedEvent`.

---

## 3. Single-Use Items

### Behavior
- Each item can only be used once per match. After use, it is removed from the player's available items.

### Implementation

**Server (`internal/game/match/match.go`):**
- After processing `UseItem`, remove the used item from `player.Items` slice.
- The `ItemUsedEvent` response should include the updated items list (or the removed itemId).

**Client (`match_provider.dart`):**
- On `ItemUsedEvent`: remove the used item from `BattlePlayerState.items`.
- `item_skill_bar.dart`: UI automatically reflects fewer items since it reads from state.
- If the removed item was the active selection, reset `actionMode` to `'weapon'`.

---

## 4. Teleport Item

### Behavior
- Player selects teleport item → action mode switches to `'item'` with `itemId: 'teleport'`.
- Player drag-to-shoots as normal (trajectory preview shown).
- Projectile flies with normal physics (gravity + wind).
- **No terrain destruction, no damage.**
- Where the projectile hits ground = teleport destination. Player instantly moves there.
- If projectile flies out of map bounds (no ground hit) → no teleport, player stays, turn ends.
- Item is consumed on use (single-use rule applies).

### Implementation

**Server (`internal/game/match/match.go`):**
- In shoot handler, when `actionMode == 'item'` and `itemId == 'teleport'`:
  - Run projectile physics simulation as normal.
  - Find the landing point (first terrain collision).
  - If valid landing: update `player.Position` to landing point. Emit `PlayerMovedEvent`.
  - If no landing (out of bounds): do nothing, just end the action.
  - Do NOT call terrain destruction or damage calculation.
  - Remove item from player (single-use).
  - Emit `ItemUsedEvent` with teleport result.

**Client:**
- `match_provider.dart`: on `ItemUsedEvent` for teleport, update player position from event data.
- `player_component.dart`: snap position (no gravity fall needed — teleporting).
- `match_screen.dart`: trajectory preview works as normal during drag. After shot, animate projectile path as usual but no explosion effect.

---

## Files Changed

| File | Changes |
|------|---------|
| `match_screen.dart` | Cancel zone logic in drag handlers |
| `battle_game.dart` | Add CancelZoneComponent, show/hide methods |
| `player_component.dart` | Fall death check (y > 900), teleport snap |
| `match_provider.dart` | Remove item on use, handle teleport position update |
| `item_skill_bar.dart` | Reset selection when item removed |
| `match.go` (server) | Fall death check, teleport logic, remove item on use |
| New: `cancel_zone_component.dart` | CancelZone Flame component |
