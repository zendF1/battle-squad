# Battle Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix battle bugs: add cancel-shot icon, fall-death when no ground, single-use items via shoot, and teleport item via projectile.

**Architecture:** Four independent changes across server (Go) and client (Flutter). Server changes: fall-death check after terrain destruction, teleport-via-shoot in processShoot, item consumption on shoot. Client changes: cancel-zone UI component, lift actionMode state to match_screen, fall-death visual, teleport animation.

**Tech Stack:** Go (server match engine), Flutter/Flame (client game engine), Riverpod (state), WebSocket (communication)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `battle-squad/internal/game/match/match.go` | Add fall-death check (y >= Height), add teleport-via-shoot in processShoot, consume item on shoot |
| `battle-squad-v1/lib/features/match/game/cancel_zone_component.dart` | New Flame component: X icon for cancelling shots |
| `battle-squad-v1/lib/features/match/game/battle_game.dart` | Add cancel zone show/hide methods |
| `battle-squad-v1/lib/features/match/match_screen.dart` | Lift actionMode state, cancel zone in drag handlers, pass actionMode to shoot |
| `battle-squad-v1/lib/features/match/hud/match_hud.dart` | Accept actionMode/activeItemId from parent instead of internal state |
| `battle-squad-v1/lib/features/match/game/player_component.dart` | Fall-death when y > world height |

---

### Task 1: Server — Fall Death When No Ground

Players who fall below the map (y >= terrain height) after terrain destruction should die immediately.

**Files:**
- Modify: `battle-squad/internal/game/match/match.go:833-865` (weapon shoot fall handling)
- Modify: `battle-squad/internal/game/match/match.go:673-701` (skill shoot fall handling)

- [ ] **Step 1: Add fall-death check in weapon shoot fall handling**

In `match.go`, the terrain collapse section (around line 833-865) calls `GetLandingY` which returns `t.Height` (900) when no ground exists. Currently it only applies fall damage. Add a check: if `landY >= float64(t.Height)`, kill the player.

Find the fall handling block in `processShoot` (weapon mode) around line 833:

```go
// Handle terrain collapse check (falling players)
for _, p := range m.State.Players {
    if !p.IsAlive {
        continue
    }

    landY := m.Terrain.GetLandingY(p.Position.X, p.Position.Y)
    if landY > p.Position.Y {
        // Player fell down
        fallDistance := landY - p.Position.Y
        p.Position.Y = landY

        fallDamage := CalculateFallDamage(fallDistance)
        if fallDamage > 0 {
```

Replace with:

```go
// Handle terrain collapse check (falling players)
for _, p := range m.State.Players {
    if !p.IsAlive {
        continue
    }

    landY := m.Terrain.GetLandingY(p.Position.X, p.Position.Y)
    if landY >= float64(m.Terrain.Height) {
        // Fell off the map — instant death
        remainingHP := p.HP
        p.HP = 0
        p.IsAlive = false
        p.Position.Y = landY

        player.KillCount++

        damagedPlayers = append(damagedPlayers, map[string]interface{}{
            "playerId": p.PlayerID,
            "damage":   remainingHP,
            "hp":       0,
            "isAlive":  false,
            "isKilled": true,
            "type":     "fall",
        })
        continue
    }

    if landY > p.Position.Y {
        // Player fell down
        fallDistance := landY - p.Position.Y
        p.Position.Y = landY

        fallDamage := CalculateFallDamage(fallDistance)
        if fallDamage > 0 {
```

- [ ] **Step 2: Apply same fix in skill shoot fall handling**

Find the identical terrain collapse block in the skill mode section (around line 673-701) and apply the same change:

```go
// Handle terrain collapse / fall damage
for _, p := range m.State.Players {
    if !p.IsAlive {
        continue
    }
    landY := m.Terrain.GetLandingY(p.Position.X, p.Position.Y)
    if landY >= float64(m.Terrain.Height) {
        // Fell off the map — instant death
        remainingHP := p.HP
        p.HP = 0
        p.IsAlive = false
        p.Position.Y = landY

        player.KillCount++

        damagedPlayers = append(damagedPlayers, map[string]interface{}{
            "playerId": p.PlayerID,
            "damage":   remainingHP,
            "hp":       0,
            "isAlive":  false,
            "isKilled": true,
            "type":     "fall",
        })
        continue
    }

    if landY > p.Position.Y {
        fallDistance := landY - p.Position.Y
        p.Position.Y = landY
        fallDamage := CalculateFallDamage(fallDistance)
        if fallDamage > 0 {
```

- [ ] **Step 3: Verify server compiles**

Run: `cd /Users/inspius/Desktop/Porojet/github.com/battle-squad && go build ./...`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add internal/game/match/match.go
git commit -m "fix: instant death when player falls off map (no ground)"
```

---

### Task 2: Client — Fall Death Visual

When a player falls below the world (y > 900), mark them dead visually instead of clamping to the bottom.

**Files:**
- Modify: `battle-squad-v1/lib/features/match/game/player_component.dart:93-99`

- [ ] **Step 1: Update fall-off-map handling in player_component.dart**

Current code at line 93-99 clamps position to terrain height. Change to mark player dead:

```dart
// Fell off map bottom
if (position.y >= terrainData.height) {
  position.y = terrainData.height.toDouble();
  _fallVelocity = 0;
  _isFalling = false;
}
```

Replace with:

```dart
// Fell off map bottom — dead
if (position.y >= terrainData.height) {
  position.y = terrainData.height.toDouble() + 100; // move off-screen
  _fallVelocity = 0;
  _isFalling = false;
  isAlive = false;
}
```

- [ ] **Step 2: Verify Flutter analyzes clean**

Run: `cd /Users/inspius/Desktop/Porojet/github.com/battle-squad-v1 && flutter analyze`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add lib/features/match/game/player_component.dart
git commit -m "fix: player dies visually when falling off map"
```

---

### Task 3: Client — Lift ActionMode State to MatchScreen

Currently `_actionMode` and `_activeItemId` live in `_MatchHudState`, but `_MatchScreenState` needs them for drag-to-shoot. Lift the state up.

**Files:**
- Modify: `battle-squad-v1/lib/features/match/match_screen.dart`
- Modify: `battle-squad-v1/lib/features/match/hud/match_hud.dart`

- [ ] **Step 1: Add actionMode/activeItemId state to _MatchScreenState**

In `match_screen.dart`, add to the state class (after `_isDragging`):

```dart
// Action mode state (lifted from HUD)
String _actionMode = 'weapon';
String? _activeItemId;
```

- [ ] **Step 2: Pass state and callbacks to MatchHud**

In `match_screen.dart`, update the MatchHud constructor call:

```dart
MatchHud(
  matchData: matchData,
  dragAngle: _isDragging ? _dragAngle : null,
  dragPower: _isDragging ? _dragPower : null,
  actionMode: _actionMode,
  activeItemId: _activeItemId,
  onActionModeChanged: (mode) => setState(() => _actionMode = mode),
  onActiveItemChanged: (id) => setState(() => _activeItemId = id),
  onShoot: (angle, power, mode, itemId) {
    notifier.shoot(
      angle: angle,
      power: power,
      actionMode: mode,
      itemId: itemId,
    );
  },
  onMove: (direction, targetX) {
    notifier.move(direction: direction, targetX: targetX);
  },
  onEndTurn: notifier.endTurn,
),
```

- [ ] **Step 3: Update MatchHud to accept external state**

In `match_hud.dart`, change `MatchHud` to accept `actionMode`, `activeItemId`, `onActionModeChanged`, `onActiveItemChanged` as constructor params:

```dart
class MatchHud extends ConsumerStatefulWidget {
  final MatchData matchData;
  final void Function(double angle, int power, String mode, String? itemId) onShoot;
  final void Function(String direction, double? targetX) onMove;
  final VoidCallback onEndTurn;
  final double? dragAngle;
  final int? dragPower;
  final String actionMode;
  final String? activeItemId;
  final ValueChanged<String> onActionModeChanged;
  final ValueChanged<String?> onActiveItemChanged;

  const MatchHud({
    super.key,
    required this.matchData,
    required this.onShoot,
    required this.onMove,
    required this.onEndTurn,
    this.dragAngle,
    this.dragPower,
    required this.actionMode,
    this.activeItemId,
    required this.onActionModeChanged,
    required this.onActiveItemChanged,
  });
```

- [ ] **Step 4: Remove internal state from _MatchHudState, use widget props**

In `_MatchHudState`, remove `_actionMode` and `_activeItemId` fields. Replace all references:
- `_actionMode` → `widget.actionMode`
- `_activeItemId` → `widget.activeItemId`
- `setState(() => _actionMode = ...)` → `widget.onActionModeChanged(...)`
- `setState(() => _activeItemId = ...)` → `widget.onActiveItemChanged(...)`

Update the ItemSkillBar usage:

```dart
ItemSkillBar(
  items: myItems,
  skillCooldown: mySkillCooldown,
  activeItemId: widget.activeItemId,
  actionMode: widget.actionMode,
  onActionModeChanged: widget.onActionModeChanged,
  onActiveItemChanged: widget.onActiveItemChanged,
),
```

- [ ] **Step 5: Update _onDragEnd to use actionMode state**

In `match_screen.dart`, update `_onDragEnd`:

```dart
void _onDragEnd(
    MatchData matchData, MatchNotifier notifier, String? myPlayerId) {
  if (!_isDragging || _dragPower < 5) {
    // Too weak, cancel
    _game?.hideTrajectory();
    setState(() => _isDragging = false);
    return;
  }

  // Fire with dragged angle/power using current action mode
  notifier.shoot(
    angle: _dragAngle,
    power: _dragPower,
    actionMode: _actionMode,
    itemId: _activeItemId,
  );

  _game?.hideTrajectory();
  setState(() {
    _isDragging = false;
    _dragStart = null;
    // Reset to weapon mode after using an item
    if (_actionMode == 'item') {
      _actionMode = 'weapon';
      _activeItemId = null;
    }
  });
}
```

- [ ] **Step 6: Verify Flutter analyzes clean**

Run: `cd /Users/inspius/Desktop/Porojet/github.com/battle-squad-v1 && flutter analyze`
Expected: No errors

- [ ] **Step 7: Commit**

```bash
git add lib/features/match/match_screen.dart lib/features/match/hud/match_hud.dart
git commit -m "refactor: lift actionMode state from HUD to MatchScreen for drag-to-shoot"
```

---

### Task 4: Client — Cancel Shot Icon (X)

Add a cancel zone near the player character during drag-to-shoot.

**Files:**
- Create: `battle-squad-v1/lib/features/match/game/cancel_zone_component.dart`
- Modify: `battle-squad-v1/lib/features/match/game/battle_game.dart`
- Modify: `battle-squad-v1/lib/features/match/match_screen.dart`

- [ ] **Step 1: Create CancelZoneComponent**

Create `cancel_zone_component.dart`:

```dart
import 'dart:math';
import 'package:flame/components.dart';
import 'package:flutter/material.dart';

class CancelZoneComponent extends PositionComponent {
  static const double radius = 24;
  bool isHighlighted = false;

  CancelZoneComponent()
      : super(
          size: Vector2.all(radius * 2),
          anchor: Anchor.center,
        );

  bool containsPoint(Vector2 point) {
    return position.distanceTo(point) <= radius;
  }

  void show(Vector2 playerPos) {
    // Position above and slightly left of the player
    position = playerPos + Vector2(0, -60);
    isHighlighted = false;
  }

  void hide() {
    position = Vector2(-200, -200); // off-screen
    isHighlighted = false;
  }

  @override
  void render(Canvas canvas) {
    final bgColor = isHighlighted
        ? const Color(0xCCFF1744) // bright red when active
        : const Color(0x88000000); // semi-transparent dark

    // Circle background
    final bgPaint = Paint()..color = bgColor;
    canvas.drawCircle(Offset(radius, radius), radius, bgPaint);

    // Border
    final borderPaint = Paint()
      ..color = isHighlighted ? const Color(0xFFFF1744) : const Color(0xAAFFFFFF)
      ..style = PaintingStyle.stroke
      ..strokeWidth = 2;
    canvas.drawCircle(Offset(radius, radius), radius, borderPaint);

    // X icon
    final xPaint = Paint()
      ..color = Colors.white
      ..style = PaintingStyle.stroke
      ..strokeWidth = 3
      ..strokeCap = StrokeCap.round;

    const inset = 10.0;
    canvas.drawLine(
      const Offset(inset, inset),
      Offset(radius * 2 - inset, radius * 2 - inset),
      xPaint,
    );
    canvas.drawLine(
      Offset(radius * 2 - inset, inset),
      Offset(inset, radius * 2 - inset),
      xPaint,
    );
  }
}
```

- [ ] **Step 2: Add cancel zone to BattleGame**

In `battle_game.dart`, add import and field:

```dart
import 'package:battle_squad_v1/features/match/game/cancel_zone_component.dart';
```

Add field:

```dart
late CancelZoneComponent _cancelZoneComponent;
```

In `onLoad()`, add after trajectory component:

```dart
_cancelZoneComponent = CancelZoneComponent();
_cancelZoneComponent.hide();
world.add(_cancelZoneComponent);
```

Add methods:

```dart
/// Show cancel zone near the active player.
void showCancelZone(String playerId) {
  final comp = playerComponents[playerId];
  if (comp == null) return;
  _cancelZoneComponent.show(comp.position.clone());
}

/// Hide cancel zone.
void hideCancelZone() {
  _cancelZoneComponent.hide();
}

/// Check if a world-space point is inside the cancel zone.
bool isInCancelZone(Vector2 worldPoint) {
  return _cancelZoneComponent.containsPoint(worldPoint);
}

/// Update cancel zone highlight state.
void setCancelZoneHighlight(bool highlighted) {
  _cancelZoneComponent.isHighlighted = highlighted;
}

/// Convert screen coordinates to world coordinates.
Vector2 screenToWorld(Offset screenPos) {
  return camera.viewfinder.globalToLocal(Vector2(screenPos.dx, screenPos.dy));
}
```

- [ ] **Step 3: Add cancel zone logic to match_screen drag handlers**

In `match_screen.dart`, add a field:

```dart
bool _isInCancelZone = false;
```

Update `_onDragStart`:

```dart
void _onDragStart(DragStartDetails details) {
  _dragStart = details.localPosition;
  setState(() {
    _isDragging = true;
    _isInCancelZone = false;
  });

  final myPlayerId = ref.read(authProvider).playerId;
  if (myPlayerId != null) {
    _game?.showCancelZone(myPlayerId);
  }
}
```

Update `_onDragUpdate` — add cancel zone check after trajectory update:

```dart
void _onDragUpdate(
    DragUpdateDetails details, MatchData matchData, String? myPlayerId) {
  if (_dragStart == null || myPlayerId == null) return;

  final dx = details.localPosition.dx - _dragStart!.dx;
  final dy = details.localPosition.dy - _dragStart!.dy;
  final distance = sqrt(dx * dx + dy * dy);

  var angleDeg = atan2(-(-dy), -dx) * 180 / pi;
  angleDeg = angleDeg.clamp(0, 180);
  final power = (distance / 2).clamp(0, 100).round();

  setState(() {
    _dragAngle = angleDeg;
    _dragPower = power;
  });

  _game?.showTrajectory(
    playerId: myPlayerId,
    angleDeg: angleDeg,
    power: power.toDouble(),
    windDirection: matchData.state.wind.direction,
    windPower: matchData.state.wind.power,
  );

  // Check cancel zone
  final game = _game;
  if (game != null) {
    final worldPoint = game.screenToWorld(details.localPosition);
    final inZone = game.isInCancelZone(worldPoint);
    game.setCancelZoneHighlight(inZone);
    _isInCancelZone = inZone;
  }
}
```

Update `_onDragEnd` — cancel if in cancel zone:

```dart
void _onDragEnd(
    MatchData matchData, MatchNotifier notifier, String? myPlayerId) {
  _game?.hideCancelZone();

  if (!_isDragging || _dragPower < 5 || _isInCancelZone) {
    // Too weak or cancelled
    _game?.hideTrajectory();
    setState(() {
      _isDragging = false;
      _isInCancelZone = false;
    });
    return;
  }

  notifier.shoot(
    angle: _dragAngle,
    power: _dragPower,
    actionMode: _actionMode,
    itemId: _activeItemId,
  );

  _game?.hideTrajectory();
  setState(() {
    _isDragging = false;
    _dragStart = null;
    _isInCancelZone = false;
    if (_actionMode == 'item') {
      _actionMode = 'weapon';
      _activeItemId = null;
    }
  });
}
```

- [ ] **Step 4: Verify Flutter analyzes clean**

Run: `cd /Users/inspius/Desktop/Porojet/github.com/battle-squad-v1 && flutter analyze`
Expected: No errors

- [ ] **Step 5: Commit**

```bash
git add lib/features/match/game/cancel_zone_component.dart lib/features/match/game/battle_game.dart lib/features/match/match_screen.dart
git commit -m "feat: add cancel shot icon (X) during drag-to-shoot"
```

---

### Task 5: Server — Teleport via Shoot & Item Consumption

When `actionMode == "item"` and `itemId == "teleport"`, simulate projectile, teleport player to landing point, no explosion/damage. Consume the item.

**Files:**
- Modify: `battle-squad/internal/game/match/match.go:740-745` (before weapon shoot logic)

- [ ] **Step 1: Add teleport-via-shoot handling in processShoot**

In `match.go`, after the skill mode block ends (line 739 `// ── End skill mode ──`) and before the weapon shoot logic (line 741), add:

```go
// ── Item mode: teleport ─────────────────────────────────────────────────
if action.ActionMode == "item" && action.ItemID != nil && *action.ItemID == "teleport" {
    // Verify player has the item
    itemIdx := -1
    for i, it := range player.Items {
        if it == "teleport" {
            itemIdx = i
            break
        }
    }
    if itemIdx == -1 {
        return
    }

    // Simulate projectile to find landing point
    result := SimulateProjectile(
        player.PlayerID,
        player.TeamID,
        player.Position,
        action.Angle,
        action.Power,
        weaponConfig,
        m.State.Wind,
        m.Terrain,
        m.State.Players,
        false, // no drill mode
    )
    result.TerrainDestroyed = false
    result.ExplosionRadius = 0

    player.ShotsFired++

    // Broadcast projectile path (so client can animate the flight)
    payloadResult, _ := json.Marshal(result)
    m.broadcast(ws.Message{Event: "ProjectileResult", Data: payloadResult})

    // Teleport if projectile hit terrain (has explosion point within map)
    if result.ExplosionPoint != nil && result.ExplosionPoint.Y < float64(m.Terrain.Height) {
        landY := m.Terrain.GetLandingY(result.ExplosionPoint.X, result.ExplosionPoint.Y)
        if landY < float64(m.Terrain.Height) {
            player.Position.X = result.ExplosionPoint.X
            player.Position.Y = landY

            posPayload, _ := json.Marshal(map[string]interface{}{
                "playerId":   player.PlayerID,
                "position":   player.Position,
                "moveEnergy": player.MoveEnergy,
            })
            m.broadcast(ws.Message{Event: "PlayerMoved", Data: posPayload})
        }
    }

    // Consume item
    player.Items = append(player.Items[:itemIdx], player.Items[itemIdx+1:]...)

    // Broadcast ItemUsed with updated player state
    itemPayload, _ := json.Marshal(map[string]interface{}{
        "playerId": player.PlayerID,
        "itemId":   "teleport",
        "players":  m.State.Players,
        "wind":     m.State.Wind,
    })
    m.broadcast(ws.Message{Event: "ItemUsed", Data: itemPayload})

    // Schedule end turn
    m.State.TurnTimeLeft = 999
    m.checkWinCondition(ctx)
    if m.State.Status == "in_progress" {
        flightTime := 0.0
        if len(result.Path) > 0 {
            flightTime = result.Path[len(result.Path)-1].Time
        }
        m.scheduleEndTurn(ctx, flightTime)
    }
    return
}
// ── End item mode ───────────────────────────────────────────────────────
```

- [ ] **Step 2: Verify server compiles**

Run: `cd /Users/inspius/Desktop/Porojet/github.com/battle-squad && go build ./...`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add internal/game/match/match.go
git commit -m "feat: teleport item via shoot — projectile finds landing point"
```

---

### Task 6: Client — Teleport Animation (No Explosion)

When a teleport projectile result comes back, animate the flight but skip the explosion.

**Files:**
- Modify: `battle-squad-v1/lib/features/match/game/battle_game.dart`
- Modify: `battle-squad-v1/lib/features/match/hud/item_skill_bar.dart`

- [ ] **Step 1: Skip explosion for zero-radius projectiles**

In `battle_game.dart`, update `_handleExplosion` to skip when `explosionRadius <= 0`:

```dart
void _handleExplosion(ProjectileResult result) {
  // Teleport or other no-damage projectiles: skip explosion
  if (result.explosionRadius <= 0) return;

  final ep = result.explosionPoint;
  final center = ep != null
      ? Vector2(ep.x, ep.y)
      : result.path.isNotEmpty
          ? Vector2(
              result.path.last.position.x,
              result.path.last.position.y,
            )
          : Vector2(_gameWidth / 2, _gameHeight / 2);

  if (result.terrainDestroyed) {
    _terrainComponent.onTerrainDestroyed(
      center.x,
      center.y,
      result.explosionRadius,
    );
  }

  final explosion = ExplosionComponent(
    center: center,
    radius: result.explosionRadius.clamp(10, 120),
    onComplete: () {},
  );
  world.add(explosion);
}
```

- [ ] **Step 2: Add teleport icon to item_skill_bar.dart**

In `item_skill_bar.dart`, update `_iconForItem`:

```dart
IconData _iconForItem(String itemId) {
  return switch (itemId) {
    'shield' => Icons.shield,
    'heal' || 'potion' || 'medkit' => Icons.favorite,
    'bomb' => Icons.local_fire_department,
    'rope' => Icons.cable,
    'teleport' => Icons.swap_horiz,
    _ => Icons.inventory_2,
  };
}
```

- [ ] **Step 3: Verify Flutter analyzes clean**

Run: `cd /Users/inspius/Desktop/Porojet/github.com/battle-squad-v1 && flutter analyze`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add lib/features/match/game/battle_game.dart lib/features/match/hud/item_skill_bar.dart
git commit -m "feat: teleport animation — skip explosion, add teleport icon"
```

---

### Task 7: Client — Item Removal on ItemUsedEvent

When `ItemUsedEvent` is received, the full players map (including updated items list) is already sent. The current handler already replaces the players map, so items are automatically updated. Verify this works and handle actionMode reset.

**Files:**
- Modify: `battle-squad-v1/lib/features/match/match_screen.dart`

- [ ] **Step 1: Reset actionMode when active item is consumed**

In `match_screen.dart`, in the `build` method, after `_syncPlayersToGame(matchData)`, add a check to reset actionMode if the active item was consumed:

```dart
// Reset action mode if active item was consumed
if (_activeItemId != null && myPlayerId != null) {
  final myPlayer = matchData.state.players[myPlayerId];
  if (myPlayer != null && !myPlayer.items.contains(_activeItemId)) {
    _actionMode = 'weapon';
    _activeItemId = null;
  }
}
```

- [ ] **Step 2: Verify Flutter analyzes clean**

Run: `cd /Users/inspius/Desktop/Porojet/github.com/battle-squad-v1 && flutter analyze`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add lib/features/match/match_screen.dart
git commit -m "fix: reset action mode when item is consumed"
```

---

### Task 8: Integration Verification

- [ ] **Step 1: Build server**

Run: `cd /Users/inspius/Desktop/Porojet/github.com/battle-squad && go build ./...`
Expected: No errors

- [ ] **Step 2: Analyze Flutter client**

Run: `cd /Users/inspius/Desktop/Porojet/github.com/battle-squad-v1 && flutter analyze`
Expected: No issues

- [ ] **Step 3: Run server tests**

Run: `cd /Users/inspius/Desktop/Porojet/github.com/battle-squad && go test ./internal/game/match/... -v`
Expected: All tests pass

- [ ] **Step 4: Run Flutter tests**

Run: `cd /Users/inspius/Desktop/Porojet/github.com/battle-squad-v1 && flutter test`
Expected: All tests pass
