# GET /gamedata: contract

Every gameplay rule, balance number and Vietnamese display text the client needs, in one public
payload, so the client hardcodes none of them. It loads it on launch, caches it, and refreshes it
with a conditional GET. The client keeps only presentation: colours, layout, art per id, and
animation feel.

Server: `internal/api/gamedata` (handler → service → repository, types in `model.go`). Every
value comes from the DB (phase S2): the config tables and `game_settings`, edited in the admin
dashboard. The payload is built from one game data snapshot (`internal/game/gamedata`), the same
snapshot the matches started from it play with, by one function (`apigamedata.Build`), so the
client and the server agree on every number. Phase S2 only added fields (marked **S2**).

## 1. Request and response

```
GET /gamedata
If-None-Match: "13f9f9a13a6a316c"      (optional: the ETag of the cached copy)

GET /gamedata?version=13f9f9a13a6a316c (S2: exactly the game data of that version)
```

- Public: no `Authorization` header. The client calls it before login.
- `200`: body `{"success": true, "data": <GameData>}`, headers `ETag: "<data.version>"` (with
  the double quotes) and `Cache-Control: no-cache`.
- `304 Not Modified`: no body, same `ETag`. The server sends it when `If-None-Match` contains
  the current ETag (a single value, a comma list, or `*`).
- `500`: `{"success": false, "error": {"code": "INTERNAL_SERVER_ERROR", ...}}` when the DB
  cannot be read. Keep using the cache.
- **S2** `?version=<v>`: the payload of version `v` as it was published (also after an admin
  changed the config since), with `ETag: "<v>"` and the same `304` rule. `404
  GAMEDATA_VERSION_NOT_FOUND` when no server published `v` (or it was pruned: payloads stay at
  least 30 days, the newest 50 always).

## 2. Version and ETag rules

- `version` is 16 lowercase hex chars: the first 8 bytes of the SHA-256 of the JSON of `data`
  with `version` set to `""`. The ETag is the same string in double quotes.
- The API serves the payload of the game data snapshot it has loaded (built once per snapshot).
  An admin save (a config row, a `game_settings` row, a text) bumps the stored revision and
  notifies cmd/game and cmd/api, which reload within a moment (a revision poll every 10 s is the
  fallback); the next response then has the new content and version. A save that does not
  change the content keeps the version.
- The same content always gives the same version, on any API instance. Different content gives
  a different version. Versions are not ordered: compare them only for equality.
- **S2** `MatchStarted` and `MatchStateSync` carry `gamedataVersion` (string): the `version`
  of the game data that match plays with, from its start to its end (an admin save during the
  match does not change it; new matches get the new data). It is computed by the same builder
  from the same snapshot, so it always equals a `version` the API serves. When it differs from
  the cached `version`, fetch `GET /gamedata?version=<gamedataVersion>` before the player aims
  or acts, and play the match with it (a plain `GET /gamedata` may already return newer data
  than the match uses). `TurnStarted` carries `turnTimeLeft` (seconds, number): the turn's
  length, `timings.turnSec` of the match's game data.

### Client caching (phase C)

1. On launch, if `user://gamedata.json` exists, send `If-None-Match: "<its version>"`.
2. `304`: keep the cache. `200`: replace the cache with `data` (it carries its own `version`).
3. Network error or `5xx`: use the cache. With no cache (first launch offline), show a retry
   screen.

### Compatibility

- Ignore unknown fields. The server may add fields; it does not rename or remove any without a
  new contract version.
- New ids can appear at any time: characters, skills, items, effects, tiers, modes, themes, or a
  skill `effectType`. Use a generic fallback look for an id with no art; never crash.
- Arrays are in display order. JSON objects used as maps (`guideArcLength`, `byTierWord`,
  `params`, `values`) have no order.
- Numbers are JSON numbers: a whole value prints without a decimal (`445`, not `445.0`). Read
  every number as a float, and as an int where the type below says int.
- Text is UTF-8 Vietnamese. `""` means "none" (no marker, no banner, no queue).

## 3. Units and conventions

- Distances in px, y grows downward (map top is y 0). Angles in degrees: 0 points right, 90 up,
  180 left. The shot direction is `(cos a, −sin a)` in screen space, so 270 points straight down.
- Times in seconds (fields ending `Sec`, plus the `params` noted below). Percentages 0..100
  (`Percent`, `Pct`). Ratios 0..1 (`bonus`, `sellRate`, `minPart`, `decay`).
- "Power" is the slingshot power, 0..100.

## 4. GameData fields

`data` has these top-level keys, in this order: `version`, `physics`, `aim`, `aimAid`, `combat`,
`terrain`, `movement`, `timings`, `skillEnergy`, `skillRanks`, `weapons`, `skills`,
`characters`, `defaultCharacterId`, `items`, `itemRules`, `statusEffects`, `rank`, `levels`,
`equipment`, `economy`, `matchmaking`, `modes`, `mapThemes`, `gunKinds` (S2), `bosses` (boss mode).

The **Source** column says where the admin edits the value: a `game_settings` row (`rules.<group>`
rows are JSON objects, the field is the JSON key) or a config table column. A value that is
missing or not valid (wrong type, out of the range the dashboard enforces) falls back to the
default in parentheses.

### 4.1 `physics`: the shell flight (`SimulateProjectile`, `match/shooting.go`)

| Field | Type | Unit | Meaning | Source |
|---|---|---|---|---|
| `gravity` | number | px/s² | downward acceleration | `game_settings physics.gravity` (default 445) |
| `speedPerPower` | number | px/s per power point | launch speed = power × this | `physics.projectile_speed_multiplier` (9.6) |
| `windScale` | number | px/s² per wind unit | horizontal wind acceleration = wind direction (−1, 0, 1) × wind power × windScale × weapon `windInfluence` / `projectileWeight` | `physics.wind_scale` (67) |
| `timeStep` | number | s | integration step | `physics.time_step` (0.02) |
| `pathRecordStep` | number | s | how often the server records path points | `physics.path_record_step` (0.05) |
| `maxFlightSec` | number | s | a flight stops after this | `physics.max_flight_seconds` (4.5) |
| `playerHitRadius` | number | px | a shell hits an enemy within this of its body centre | `physics.player_hit_radius` (24) |
| `collisionStepPx` | number | px | each step is checked for hits every this many px | `rules.physics` `collisionStepPx` (4) |
| `shotOriginLift` | number | px | a shot starts this far above the feet (the body centre; also the hit centre) | `rules.physics` `shotOriginLift` (20, the body centre) |
| `outOfBounds` | `{left, right, below}` numbers | px | the flight ends at x < −left, x > mapWidth + right or y > mapHeight + below | `rules.physics` `outOfBoundsLeft` / `outOfBoundsRight` / `outOfBoundsBelow` (100, 100, 50) |

`game_settings` keys: a `physics.<name>` row is read first, then a bare `<name>` row. A
missing or unparsable row gives the default. The parser is `gamedata.PhysicsFromSettings`, the
same one the match uses (also for `match.*`, `move.*`, `fall.*` and `combat.defense_k`).

### 4.2 `aim`

| Field | Type | Meaning | Source |
|---|---|---|---|
| `powerMin`, `powerMax` | number | the Shoot power range; the server rejects power > `powerMax` | `rules.aim` `powerMax` (100; `powerMin` is 0) |
| `angleMin`, `angleMax` | number (deg) | the slingshot's angle range. Client only: the server accepts any finite angle | `rules.aim` `angleMin` / `angleMax` (0..180) |
| `noAimAngle`, `noAimPower` | number | what to send with a skill whose `aimKind` is `none` (and the power of a `line` skill); the server still validates them | `rules.aim` `noAimAngle` / `noAimPower` (90 / 50) |

### 4.3 `aimAid`: the client's aiming guide (client-only tuning, served by the server)

| Field | Type | Meaning |
|---|---|---|
| `guideArcLength` | object tier → number (px) | how much of the exact flight the guide arc draws, measured along the path, by the local player's rank tier |
| `defaultTier` | string | the tier to use when the player is unranked or their tier is not in `guideArcLength` |
| `hitTolerance` | `{max, minPart, farPx, highPx}` | the guide shows "Trúng!" when the exact path passes within `playerHitRadius + r` of an enemy's body centre, with r = `max` × min(1, `minPart` + (1 − `minPart`) × max(D / `farPx`, H / `highPx`)) px. D is the horizontal distance to the flight's end; H is the apex height above the origin |

Source: `rules.aim_aid` (`guideArcLength` object tier → px; `hitToleranceMax`,
`hitToleranceMinPart`, `hitToleranceFarPx`, `hitToleranceHighPx`). `defaultTier` is the lowest
rank tier.

### 4.4 `combat`

| Field | Type | Meaning | Source |
|---|---|---|---|
| `defenseK` | number | damage taken = raw × defenseK / (defenseK + defense) | `game_settings combat.defense_k` (600) |
| `powerShotFactor` | number | a shot or skill armed with `power_shot` deals this × damage | `config_items.value` of `power_shot` (1.5) |
| `lavaDamage` | int | HP lost at the start of a turn on lava (ignores defense) | `rules.combat` `lavaDamage` (75) |
| `critMultiplier` | number | **S2** a critical hit deals this × the damage (`damage` events with `critical`) | `rules.combat` `critMultiplier` (1.5) |

### 4.5 `terrain`

| Field | Type | Meaning | Source |
|---|---|---|---|
| `playerHeight` | int px | the free space a player needs above the ground | `rules.terrain` `playerHeight` (40) |
| `maxClimbPerStep` | int px | the most a walking player climbs per 1 px step sideways | `rules.terrain` `maxClimbPerStep` (3) |
| `craterPxPerTerrainDamage` | number | crater radius = weapon `terrainDamage` × this (see `weapons[].craterRadius`) | `rules.terrain` `craterPxPerTerrainDamage` (0.6) |
| `bowlDepth` | number | a crater's floor lies `bowlDepth` × r under the impact point | `rules.terrain` `bowlDepth` (0.72) |
| `bowlCeiling` | number | a crater carves up to `bowlCeiling` × r above the impact point | `rules.terrain` `bowlCeiling` (1.5) |
| `maxTerrainDamageBonusPercent` | int % | the shooter's terrain damage bonus grows its craters by at most this % | `rules.terrain` `maxTerrainDamageBonusPercent` (50) |
| `fallDamageThresholdPx` | number px | falls up to this deal no damage | `game_settings fall.damage_threshold` (150) |
| `fallDamagePerPx` | number HP/px | HP lost per px of fall beyond the threshold (ignores defense) | `game_settings fall.damage_per_pixel` (3.75) |

Migration 041 corrected the `fall.*` rows (seeded 30 / 0.5 but never read before S2) to the
150 / 3.75 the match played with.

### 4.6 `movement`

| Field | Type | Meaning | Source |
|---|---|---|---|
| `pxPerEnergy` | number | a walk reaches at most moveEnergy × this px from the start | `game_settings move.step_pixels` / `move.energy_cost_per_2px` (2 / 1 = 2) |
| `energyPerPx` | number | a walk costs round(px walked × this) move energy | `move.energy_cost_per_2px` / `move.step_pixels` (1 / 2 = 0.5) |
| `defaultMoveEnergy` | int | base move energy of a character with no `mobility` (the match sends `maxMoveEnergy` per player) | `rules.movement` `defaultMoveEnergy` (100) |
| `netMoveEnergyCap` | int | a netted player has at most this much move energy | `config_items.value` of `spider_net` (50) |

### 4.7 `timings`

| Field | Type | Meaning | Source |
|---|---|---|---|
| `turnSec` | int s | a turn's length (TurnTimeLeft at its start) | `game_settings match.turn_time_seconds` (20) |
| `actionIntervalSec` | number s | the server drops a player's actions sent closer together than this | `rules.timings` `actionIntervalSec` (0.15) |
| `botFollowUpDelaySec` | number s | a bot waits this long after a heal or move before its next action | `rules.timings` `botFollowUpDelaySec` (1.5) |
| `skipTurnDelaySec` | number s | the server ends a turn skipped by stun or freeze after this | `rules.timings` `skipTurnDelaySec` (2) |

### 4.8 `skillEnergy` and `skillRanks`

`skillEnergy` `{max, perTurn, onHit}` (ints): a skill can be used at `max` energy. A player
gains `perTurn` at the end of each of its own turns, and `onHit` once per enemy action that takes
its HP or shield. Source: `rules.skill_energy` `max` / `perTurn` / `onHit` (100 / 20 / 10).

`skillRanks` `{byTierWord, default, max}`: a player's skill rank comes from its equipped weapon
item id. Take the part after the last `_` (e.g. `rookie_weapon_lv20` → `lv20`,
`crafted_weapon_gold` → `gold`) and look it up in `byTierWord` (object word → int). No weapon,
or a word not in the map, gives `default` (0). The rank is at most `max` (4). Inside a match, use
the `skillRank` the server sends. Source: `rules.skill_ranks` (`byTierWord`, `default`; `max` is the
highest value).

### 4.9 `weapons[]` (config_weapons, ordered by `weaponId`)

| Field | Type | Meaning |
|---|---|---|
| `weaponId` | string | id (`characters[].weaponId` points here) |
| `name` | string | config name (English today; not shown in the UI) |
| `damage` | int | base damage |
| `explosionRadius` | int px | damage area of the blast |
| `terrainDamage` | int | crater size points |
| `craterRadius` | number px | the crater a normal shot carves on normal ground = `terrainDamage` × `terrain.craterPxPerTerrainDamage`, or `explosionRadius` when `terrainDamage` ≤ 0. Exact float as the server computes it (e.g. 14.399999999999999); round it for display |
| `projectileWeight` | number | wind divisor |
| `windInfluence` | number | wind multiplier |
| `multiHit` | int | config value |

### 4.10 `skills[]` (config_skills, in character order)

| Field | Type | Meaning |
|---|---|---|
| `skillId` | string | id (ShotResolved `skillId`; `characters[].skillId`) |
| `characterId` | string | its character |
| `name` | string | Vietnamese button name ("Mưa tên lửa") |
| `description` | string | Vietnamese description **template** (see below) |
| `effectType` | string | `missile_rain`, `bomb_chain`, `laser`, `bloom` (or a future one) |
| `statusEffectId` | string | the effect it puts on the enemies it hits (`statusEffects[].effectId`), `""` none |
| `aimKind` | string | `arc`: the slingshot with its arcs. `line`: a straight line to the map edge in any direction; power is ignored, send `aim.noAimPower`. `none`: fired on tap with `aim.noAimAngle` / `aim.noAimPower`. Unknown effect types are `arc` |
| `cooldownTurn`, `projectileCount` | int | config values (informational) |
| `damageMultiplier` | number | scales the whole skill's damage |
| `params` | object name → number | the effect type's tuning numbers (below); `{}` for an unknown type |
| `ranks` | array of `{rank, values}` | one entry per rank 0..`skillRanks.max`, in rank order. `values` is an object name → number |

**The description template.** Replace every `{key}` in `description` with
`ranks[r].values[key]`, where r is the player's skill rank. Print a whole number without a
decimal (`12`, not `12.0`); print any other number in its shortest decimal form. The server
rounds percent values to whole numbers. `%` is a literal character. Leave a `{key}` that has no
value as it is. Rank 0 of `triple_shot` gives: "Bắn pháo hiệu, 12 tên lửa rơi quanh chỗ đáp
(+4 mỗi cấp súng), mỗi quả 25% sát thương; địch trúng bị bỏng: mất 10% máu tối đa đầu lượt
sau."

**`values` per effect type** (every rank carries all of its keys):

| effectType | keys |
|---|---|
| `missile_rain` | `count` missiles (12 + 4r), `countPerRank`, `spread` px (90 + 15r), `damagePct` per missile (25), `capPct` per target for the whole rain (200), `burnPct` max HP lost per burn tick (10) |
| `bomb_chain` | `count` bombs (2 + r), `countPerRank`, `eachPct` per bomb (round((180 + 15r) / (3 + r)): 60 49 42 38 34), `capPct` per target (120 + 15r) |
| `laser` | `firstPct` on the first enemy (150 + 10r), `decayPct` less per next enemy (20) |
| `bloom` | `selfPct` Flora's heal (40 + 5r), `allyPct` a teammate's (30 + 5r), `perRankPct` (5), `shieldCapPct` max shield as % of max HP (50) |

**`params` per effect type.** These are the skill's `config_skills.params` (JSON object; a missing
or invalid key keeps the effect type's default), plus `burnMaxHpPercent` (missile_rain) and
`shieldCapPercent` (bloom) from `rules.combat` and the derived `seedTime` (bloom). The
description template is `config_skills.description_template`. Times are in s,
speeds in px/s, damage factors are × the skill's base damage, blast and crater are × the
weapon's:

- `missile_rain`: `baseCount`, `countPerRank`, `spreadBase`, `spreadPerRank` (px), `focus`,
  `delay` (s after the flare lands), `interval` (s between missiles), `startY` (px, above the
  map), `dropSpeed`, `damage`, `blast`, `crater`, `cap`, `burnTurns`, `burnMaxHpPercent`.
- `bomb_chain`: `baseCount`, `countPerRank`, `launchGap` (s), `totalBase`, `totalPerRank`,
  `capBase`, `capPerRank`, `blast`, `fallSpeed` (× free fall), `hitGap` (s), `rootTurns`.
- `laser`: `halfWidth` (px; it hits an enemy within halfWidth + `physics.playerHitRadius` of
  the beam), `firstBase`, `firstPerRank`, `decay`, `chargeTime` (s before the beam), `beamTime`
  (s shown), `sweepSpeed` (px/s of the beam front), `stunTurns`.
- `bloom`: `selfBase`, `allyBase`, `perRank` (%), `riseTime`, `crossTime`, `fallTime`,
  `seedTime` (s; the heal lands then), `skyY` (px), `fallSpeed`, `linger` (s), `shieldCapPercent`.

Replays come from the server timeline (ShotResolved). The client uses `params` only for the
aim aid (laser `halfWidth`) and for fallbacks; the beam event's own `t` / `duration` win.

### 4.11 `characters[]` (config_characters, in display order) and `defaultCharacterId`

| Field | Type | Meaning |
|---|---|---|
| `characterId` | string | id |
| `name` | string | display name ("Tanko") |
| `role` | string | role id (`balanced`, `demolisher`, `striker`, `sustain`) |
| `roleName` | string | Vietnamese role label ("Phá địa hình") |
| `playstyle` | string | Vietnamese play style paragraph ("Lối chơi") |
| `weaponId` | string | its starter weapon (`weapons[]`) |
| `skillId` | string | its skill (`skills[]`) |
| `gunKind` | string | the gun art kind (`bazooka`, `cannon`, `shock_rifle`, `seed_launcher`); `""` lets the client fall back to its generic gun |
| `hp`, `damage`, `mobility`, `defense`, `skillPower`, `terrainDamage`, `difficulty` | int | config base stats. A player's own stats still come from `/player/characters` |

`defaultCharacterId` (string): the character every player owns and plays when none is picked
(`rules.player` `defaultCharacterId`, "rookie").

### 4.12 `items[]` (config_items, in loadout order) and `itemRules`

| Field | Type | Meaning |
|---|---|---|
| `itemId` | string | id |
| `name` | string | Vietnamese name (`display_name`, else the config name) |
| `type`, `targetType` | string | config values |
| `value` | number | config value: medkit HP healed (450), wind_stopper turns (2), drill_bomb pierce px (50), freeze_bomb turns (1), power_shot damage factor (1.5, = `combat.powerShotFactor`), spider_net move energy cap (50, = `movement.netMoveEnergyCap`). Not used for air_strike (see `itemRules.airStrike`) or teleport |
| `maxUsePerMatch`, `cooldown` | int | config values |
| `instant` | bool | true: send `UseItem {itemId}`; it acts at once and the turn goes on. false: it arms the next shot, sent as `Shoot {angle, power, actionMode: "item", itemId}` |
| `aimKind` | string | `none` (instant items), `arc` (a slingshot shot; teleport too), `targetX` (aimed at a map column: send `Shoot.targetX`; the server ignores angle and power but validates them) |

`instant` / `aimKind` come from the match code (`match.InstantItem`, `match.TargetXItem`). An
item id the match does not handle shows as `instant: true, aimKind: "none"`; the server will
reject it.

`itemRules`: `maxLoadout` (int; a player brings at most this many items, `rules.items`
`maxLoadout` 3) and `airStrike {angle, power}` (numbers; the strike falls from (targetX, 0) at
this angle and power whatever the Shoot carries, `rules.items` `airStrikeAngle` /
`airStrikePower` 270 / 80).

### 4.13 `statusEffects[]` (config_status_effects in order, then any effect the match knows without a row)

| Field | Type | Meaning |
|---|---|---|
| `effectId` | string | id as in `players[].statusEffects[].effectId` and ShotResolved `status` events |
| `name` | string | Vietnamese label when it lands ("Choáng"); the id itself when there is no row |
| `marker` | string | short text above the player's head while it is active ("x1.5", "Khoan"); `""` none |
| `banner` | string | the notice while it skips the turn or blocks moving ("Choáng — mất lượt"); `""` none |
| `bad` | bool | the bloom (Cleanse) removes it |
| `skipsTurn` | bool | the whole turn is skipped (the server ends it after `timings.skipTurnDelaySec`) |
| `blocksMove` | bool | the player cannot walk or teleport |

`bad`, `skipsTurn` and `blocksMove` are the `config_status_effects` columns `bad`,
`skips_turn`, `blocks_move` (an effect that skips the turn also blocks moving). When two
turn-skipping effects are active, the server counts the skip as the first in this order (stun
before freeze). An effect lasts while `durationTurn > 0` and loses one turn at the end of its target's
own turn.

### 4.14 `rank`

| Field | Type | Meaning |
|---|---|---|
| `tiers` | array of `{tier, name, minRating, divisions, divisionSize}` | lowest first: id, Vietnamese name ("Bạc"), first rating of the tier, number of divisions (3; master 1), rating points per division (0 when there is one division) |
| `defaultRating` | int | the rating of a player with no rank row (`game_settings elo.defaultRating`, 1000) |
| `rankedMode` | string | the ranked mode / queue id (`ranked_2v2`) |

A rating's tier is the last tier whose `minRating` ≤ rating (the first tier for any rating
below it). Its division is clamp(`divisions` − floor((rating − `minRating`) / `divisionSize`),
1, `divisions`), or 1 when `divisionSize` is 0. Division 3 is the lowest and 1 the highest;
show them as "III", "II", "I". The tiers, names and ranges are `config_rank_tiers` (`name`,
`min_rating`, `divisions`, `division_size`); the dashboard keeps them valid (from 0, each above
the previous). The tier ids are fixed.

### 4.15 `levels`

- `character.levels`: an array of `{level, expRequired}` (ints) in level order, from
  `{1, 0}`. `expRequired` is the **total** EXP a character needs to reach that level. A
  character's level is the highest entry with `expRequired` ≤ its EXP. `character.maxLevel` is
  the last level (80). Source: `game_settings character_levels`, the curve `/player/characters`
  uses.
- `player.expPerLevel` (int): a player at level N needs N × `expPerLevel` EXP, counted from 0 at
  level N, to reach N + 1 (`rules.player` `expPerLevel`, 100).

### 4.16 `equipment`

| Field | Type | Meaning | Source |
|---|---|---|---|
| `upgradeStepPerLevel` | number | a piece's stat multiplier = 1 + upgradeLevel × this + the `bonus` of every milestone reached | `rules.equipment` `upgradeStepPerLevel` (0.02) |
| `maxUpgradeLevel` | int | highest +level | `rules.equipment` `maxUpgradeLevel` (16) |
| `milestones` | array of `{level, bonus}` | the bonus added once a piece reaches the level | `game_settings equipment.milestone_{6,10,14,16}_bonus` |
| `maxStoneLevel` | int | highest stone level; stones below it merge into the next | `rules.equipment` `maxStoneLevel` (12) |
| `mergeMinCount`, `mergeMaxCount` | int | a merge takes this many stones (or gems) of one level | `rules.equipment` `mergeMinCount` / `mergeMaxCount` (2 / 4) |
| `mergeRatePerItem` | number % | merge success chance per stone | `equipment.merge_rate_per_item` (25) |
| `sellRate` | number | selling pays back floor(buy price × this) | `equipment.sell_rate` (0.25) |
| `weaponUpgradeCostMultiplier` | number | weapons need upgrade_cost × this stone power | `equipment.weapon_upgrade_cost_multiplier` (3) |
| `tiers` | array of `{tier, name, kind, idSuffix, skillRank}` | the item tiers in order: id, Vietnamese name ("Sắt"), `normal` (shop items whose ids end in `idSuffix`, `lv10`…`lv40`) or `crafted` (ids end in the tier itself; `idSuffix` `""`), and the skill rank a weapon of the tier gives | `config_equipment_tiers` + `rules.skill_ranks` `byTierWord` |

| `slots` | array of `{id, name}` | **S2** the equipment slots in display order, with their Vietnamese name (`helmet` "Mũ", `armor` "Áo", `gloves` "Găng tay", `pants` "Quần", `boots` "Giày", `weapon` "Vũ khí") | `config_equipment_slots` |
| `typeWords` | array of `{word, name, gunKind}` | **S2** the type part of the catalog's "<Tier> <Type>" item names, lower case (`"shock rifle"`), its Vietnamese word ("Súng điện") and, for a gun, its gun kind (`gunKinds[].id`, `"shock_rifle"`; `""` for armour). Show "<name> <tier name>" ("Súng điện Thép") | `config_item_type_words` |

The per-item numbers (upgrade rates, stone powers, careful-upgrade costs) stay in the equipment
catalog endpoints.

### 4.17 `economy`, `matchmaking`, `modes`, `mapThemes`

- `economy.coinsPerGem` (int): the base gem → coin rate. A coin pack shows a bonus only when it
  beats this rate (client-only tuning, `rules.economy` `coinsPerGem` 100).
- `matchmaking`: `queue` (the ranked queue id), `slots` (players per match, 4), `teamSize` (2),
  `botFillAfterSec` (bots fill the free slots after this wait), `matchStartDelaySec` (s between
  MatchFound and MatchStarted; the reveal must fit), `inviteTimeoutSec` (s a party invite,
  `InviteToLobby`, waits for its answer before `InviteExpired`; the same number the `LobbyInvite`
  / `InviteSent` `expiresInSec` carries, default 30, migration 042) and `tips` (Vietnamese
  strings shown while searching, in order). `botFillAfterSec`, `matchStartDelaySec` and
  `inviteTimeoutSec` come from `game_settings matchmaking` (admin: Matchmaking page), `slots`
  and `teamSize` from Go `matchmaker.TeamSlots`, `tips` from `config_tips`.
- `modes.categories[]` `{categoryId, name}`: the sections (tabs) of the mode select screen, in
  order (`pvp` "PvP", `boss` "Boss"). A category with no `modes.lobby` entry shows the "Sắp ra
  mắt" placeholder. Source `config_mode_categories` (admin: Texts → Mode Categories).
- `modes.lobby[]` `{modeId, name, description, queue, bot, category, icon, partySize, ranked}`:
  the enabled mode choices of the mode select screen, in order (group them by `category`).
  A non-empty `queue` joins that matchmaking queue (`CreateLobby {queue}` / `SetLobbyQueue
  {queue}`, then `StartQueue`); the server runs `ranked_2v2`, `casual_2v2` and `boss:<bossId>`
  of every enabled boss (§4.18; those modes have `category` `boss`, `icon` `boss`, `partySize`
  4). A boss mode whose boss is disabled or missing is left out. A non-empty `bot`
  sends QuickPlay with `bot: <value>`. Neither means practice.
  - `category` (string): a `modes.categories[].categoryId`.
  - `icon` (string): an icon key the client maps to its own icon (`swords`, `trophy`, `robot`,
    `target`); `""` or an unknown key → the generic icon.
  - `partySize` (int, ≥ 1): the most lobby members; 2 shows the teammate slot and the party flow,
    4 the boss party's 3 "+" slots, 1 means solo (bot, practice). The server enforces it
    (`LOBBY_FULL`; switching a bigger lobby to a smaller mode is `PARTY_TOO_LARGE`).
  - `ranked` (bool): its matches change rating / rank points (only the `ranked_2v2` queue): show
    the rank line and the result's rating row only then.
  - Source `config_game_modes` (kind `lobby`, `enabled` true; admin: Texts → Game Modes, which
    rejects a queue the server does not run, a `ranked` flag that does not match the queue, a
    party bigger than the queue allows (2 for 2v2, 4 for a boss), or an unknown category).
- `modes.match[]` `{modeId, name}`: a match's mode (MatchStarted `mode`, match history) and its
  Vietnamese name. A matchmade match's mode is its queue: `ranked_2v2` "Đấu hạng 2vs2",
  `casual_2v2` "Đấu thường 2vs2", `boss:<bossId>` the boss's name (`boss:steel_robot` "Rô-bốt
  Thép").
- `mapThemes`: `default` (string) and `themes` (array of theme ids the server accepts, from Go
  `gamedata.MapThemes`). The art per theme stays in the client; an unknown theme draws as
  `default`.
- **S2** `gunKinds[]` `{id, name}`: every gun art kind (`characters[].gunKind`; `blaster` is the
  generic gun of crafted and shared weapons) with its Vietnamese name, in display order
  (`bazooka` "Bazooka", `cannon` "Đại pháo", `shock_rifle` "Súng điện", `seed_launcher` "Súng hạt",
  `blaster` "Súng"). Source `config_gun_kinds`.

### 4.18 `bosses[]` (config_bosses, enabled only, in display order; boss mode, migration 046)

Everything the Boss tab and the arena need to draw and describe a boss:

| Field | Type | Meaning |
|---|---|---|
| `bossId` | string | id (`steel_robot`, `lava_crab`, `ice_witch`); the boss's match player id is `boss_<bossId>` |
| `name`, `description` | string | Vietnamese texts |
| `recommendedLevel` | int | shown on the card ("Lv đề xuất") |
| `modeId` | string | the `modes.lobby` entry whose queue fights it (`""` when none) |
| `queue` | string | `boss:<bossId>`: `CreateLobby {queue}` / `SetLobbyQueue {queue}`, and the match mode |
| `mapId` | string | its arena (`GET /maps/{id}`; the match terrain still comes in `MatchStarted`) |
| `hp` | int | the boss's max HP (the big HP bar) |
| `bodySize` | number | × a player's body: the boss is drawn `bodySize` × a player's 40 px height; it is hit within `physics.playerHitRadius × bodySize` of a centre `physics.shotOriginLift × bodySize` above its feet |
| `turnLimit` | int | boss turns before it escapes (the party loses) |
| `shieldedDamageFactor` | number 0..1 | while a minion lives the boss takes this × the damage (show the shield; `MatchState.bossShielded`) |
| `minions[]` | `{minionId, name, hp, bodySize}` | its minion types (a minion's match player id is `minion_<minionId>_<n>`) |
| `skills[]` | `{skillId, name, description, kind}` | its skills (`ShotResolved.skillId` of the boss's turn); `kind` `rain`, `beam` or `summon` |
| `dailyBonusRuns` | int | runs a day with the big reward table (`GET /boss/runs` has what is left) |

```json
{"bossId": "steel_robot", "name": "Rô-bốt Thép",
 "description": "Người khổng lồ cơ khí canh giữ xưởng sắt bỏ hoang. Nã pháo dồn dập, bắn tia laze xuyên đất và gọi drone che chắn.",
 "recommendedLevel": 10, "modeId": "boss_steel_robot", "queue": "boss:steel_robot", "mapId": "arena_steel_robot",
 "hp": 12000, "bodySize": 3, "turnLimit": 15, "shieldedDamageFactor": 0.1,
 "minions": [{"minionId": "drone", "name": "Drone", "hp": 650, "bodySize": 0.7}],
 "skills": [
   {"skillId": "shell_barrage", "name": "Mưa đạn pháo", "description": "Mười quả đạn pháo dội xuống quanh một người chơi.", "kind": "rain"},
   {"skillId": "laser_beam", "name": "Tia laze nghiền", "description": "Tia laze xuyên địa hình bắn thẳng vào người yếu nhất, trúng mọi người trên đường.", "kind": "beam"},
   {"skillId": "drone_wave", "name": "Gọi drone", "description": "Thả hai drone. Còn drone thì Rô-bốt Thép được che chắn.", "kind": "summon"}],
 "dailyBonusRuns": 3}
```

The skill numbers, minion stats beyond HP / body, reward tables and waves stay on the server
(admin Bosses page). Source: `config_bosses` (and `config_game_modes` for `modeId`).

## 5. Not in this payload

- Maps and brick types: `GET /maps`, `GET /maps/{id}` (the boss arenas included).
- A player's boss runs left today: `GET /boss/runs`.
- A player's own stats, stat multipliers and reset cost: `GET /player/characters`.
- The equipment catalog with per-item prices, rates and stone powers; the shop, IAP, missions,
  milestones and titles: their own endpoints.

## 6. DB tables behind it (migration 040_gamedata_texts)

- `config_characters`: + `role_name`, `playstyle`, `gun_kind`, `sort_order`.
- `config_items`: + `display_name`, `sort_order`.
- `config_skills`: + `description_template`. When it is empty, the old `description` is sent.
- New tables: `config_status_effects (effect_id, name, marker, banner, sort_order)`,
  `config_rank_tiers (tier, name, sort_order)`,
  `config_equipment_tiers (tier, name, kind, id_suffix, sort_order)`,
  `config_game_modes (mode_id, kind lobby|match, name, description, queue, bot, sort_order)`,
  `config_tips (tip_id, text, sort_order)`.
- The texts come from the Godot client at 935c5af. The migration fills a column only while it is
  empty and inserts rows with `ON CONFLICT DO NOTHING`, so reruns keep admin edits.
  `configs/*.yaml` carries the same texts for the admin seed of a fresh DB.

## 6a. Mode select (migration 044_mode_select)

- `config_game_modes`: + `category` (a `config_mode_categories` id), `icon`, `party_size` (int,
  default 1), `ranked` (bool), `enabled` (bool, default true; false → not in `modes.lobby`).
  Added with their seed values once, so reruns keep admin edits; the old default names change
  only while unedited (`ranked` "Xếp hạng 2v2" → "Đấu hạng 2vs2", `bot` "Đấu với máy" → "Đấu
  máy"); practice moves last (sort 4). New rows `casual` (lobby, queue `casual_2v2`, sort 1) and
  `casual_2v2` (match).
- New table `config_mode_categories (category_id, name, sort_order)`: `pvp` "PvP" 1, `boss`
  "Boss" 2.

## 6c. Boss mode (migrations 045_boss_arenas, 046_boss_mode)

- 045 (generated by `go run ./cmd/mapgen`): the arenas `arena_steel_robot`, `arena_lava_crab`,
  `arena_ice_witch` in `config_maps` (`ON CONFLICT DO NOTHING`).
- 046: `config_bosses (boss_id, name, description, recommended_level, enabled, sort_order, map_id,
  hp, defense, damage, body_size, turn_limit, shielded_damage_factor, out_of_map, skills, minions,
  waves, rewards, daily_bonus_runs)` seeded with the three bosses; lobby modes `boss_<bossId>`
  (queue `boss:<bossId>`, category `boss`, icon `boss`, party 4) and match modes `boss:<bossId>`;
  `player_boss_runs`. Seed rows `ON CONFLICT DO NOTHING`, so reruns keep admin edits.

## 6b. DB behind it since S2 (migration 041_gamedata_rules)

- `game_settings` JSON rows `rules.physics`, `rules.aim`, `rules.aim_aid`, `rules.combat`,
  `rules.terrain`, `rules.movement`, `rules.timings`, `rules.skill_energy`, `rules.skill_ranks`,
  `rules.items`, `rules.player`, `rules.equipment`, `rules.economy` (seeded with the values above).
  `rules.bots` (migration 043: bot level spread, gun upgrade, items, medkit threshold, teleport
  margins) is server-only and not in the payload.
- The scalar rows `match.turn_time_seconds`, `move.step_pixels`, `move.energy_cost_per_2px`,
  `fall.damage_threshold`, `fall.damage_per_pixel` now drive the match (the dead fall / move seed
  values were corrected to what the match played with).
- `config_skills.params` (JSONB), `config_status_effects.bad / skips_turn / blocks_move`,
  `config_rank_tiers.min_rating / divisions / division_size`.
- New tables `config_gun_kinds (gun_kind, name, sort_order)`, `config_equipment_slots (slot, name,
  sort_order)`, `config_item_type_words (word, name, gun_kind, sort_order)` (texts from the
  Godot client at 60a712b).
- `gamedata_revision` (bumped by a trigger on every config write; the servers reload on it) and
  `gamedata_versions` (every published payload, for `?version=`).
- Idempotent: one-time fills only in the run that adds their column, seed rows `ON CONFLICT DO
  NOTHING`, so reruns keep admin edits.

## 7. Example (the dev DB, 2026-10-22, after S2)

The full response (`200`, `ETag: "4eedfeb2e6a5a751"`):

```json
{
  "success": true,
  "data": {
    "version": "4eedfeb2e6a5a751",
    "physics": {
      "gravity": 445,
      "speedPerPower": 9.6,
      "windScale": 67,
      "timeStep": 0.02,
      "pathRecordStep": 0.05,
      "maxFlightSec": 4.5,
      "playerHitRadius": 24,
      "collisionStepPx": 4,
      "shotOriginLift": 20,
      "outOfBounds": {"left": 100, "right": 100, "below": 50}
    },
    "aim": {
      "powerMin": 0,
      "powerMax": 100,
      "angleMin": 0,
      "angleMax": 180,
      "noAimAngle": 90,
      "noAimPower": 50
    },
    "aimAid": {
      "guideArcLength": {"bronze": 700, "diamond": 500, "gold": 600, "master": 450, "platinum": 550, "silver": 650},
      "defaultTier": "bronze",
      "hitTolerance": {"max": 20, "minPart": 0.2, "farPx": 1200, "highPx": 700}
    },
    "combat": {"defenseK": 600, "powerShotFactor": 1.5, "lavaDamage": 75, "critMultiplier": 1.5},
    "terrain": {
      "playerHeight": 40,
      "maxClimbPerStep": 3,
      "craterPxPerTerrainDamage": 0.6,
      "bowlDepth": 0.72,
      "bowlCeiling": 1.5,
      "maxTerrainDamageBonusPercent": 50,
      "fallDamageThresholdPx": 150,
      "fallDamagePerPx": 3.75
    },
    "movement": {"pxPerEnergy": 2, "energyPerPx": 0.5, "defaultMoveEnergy": 100, "netMoveEnergyCap": 50},
    "timings": {"turnSec": 20, "actionIntervalSec": 0.15, "botFollowUpDelaySec": 1.5, "skipTurnDelaySec": 2},
    "skillEnergy": {"max": 100, "perTurn": 20, "onHit": 10},
    "skillRanks": {
      "byTierWord": {
        "diamond": 4,
        "dragon": 4,
        "gold": 3,
        "iron": 1,
        "lv10": 1,
        "lv20": 2,
        "lv30": 3,
        "lv40": 4,
        "mithril": 3,
        "silver": 2,
        "steel": 2,
        "titan": 4
      },
      "default": 0,
      "max": 4
    },
    "weapons": [
      {
        "weaponId": "basic_rocket",
        "name": "Basic Rocket",
        "damage": 480,
        "explosionRadius": 40,
        "terrainDamage": 20,
        "craterRadius": 12,
        "projectileWeight": 1,
        "windInfluence": 1,
        "multiHit": 1
      },
      {
        "weaponId": "cannon_ball",
        "name": "Cannon Ball",
        "damage": 420,
        "explosionRadius": 60,
        "terrainDamage": 30,
        "craterRadius": 18,
        "projectileWeight": 1.5,
        "windInfluence": 0.5,
        "multiHit": 1
      },
      {
        "weaponId": "electric_orb",
        "name": "Electric Orb",
        "damage": 620,
        "explosionRadius": 30,
        "terrainDamage": 20,
        "craterRadius": 12,
        "projectileWeight": 0.8,
        "windInfluence": 1.3,
        "multiHit": 1
      },
      {
        "weaponId": "seed_bomb",
        "name": "Seed Bomb",
        "damage": 380,
        "explosionRadius": 40,
        "terrainDamage": 24,
        "craterRadius": 14.399999999999999,
        "projectileWeight": 0.9,
        "windInfluence": 1,
        "multiHit": 1
      }
    ],
    "skills": [
      {
        "skillId": "triple_shot",
        "characterId": "rookie",
        "name": "Mưa tên lửa",
        "description": "Bắn pháo hiệu, {count} tên lửa rơi quanh chỗ đáp (+{countPerRank} mỗi cấp súng), mỗi quả {damagePct}% sát thương; địch trúng bị bỏng: mất {burnPct}% máu tối đa đầu lượt sau.",
        "effectType": "missile_rain",
        "statusEffectId": "burn",
        "aimKind": "arc",
        "cooldownTurn": 2,
        "projectileCount": 12,
        "damageMultiplier": 1,
        "params": {
          "baseCount": 12,
          "blast": 0.7,
          "burnMaxHpPercent": 10,
          "burnTurns": 1,
          "cap": 2,
          "countPerRank": 4,
          "crater": 1,
          "damage": 0.25,
          "delay": 0.5,
          "dropSpeed": 1500,
          "focus": 2,
          "interval": 0.05,
          "spreadBase": 90,
          "spreadPerRank": 15,
          "startY": -60
        },
        "ranks": [
          {
            "rank": 0,
            "values": {
              "burnPct": 10,
              "capPct": 200,
              "count": 12,
              "countPerRank": 4,
              "damagePct": 25,
              "spread": 90
            }
          },
          {
            "rank": 1,
            "values": {
              "burnPct": 10,
              "capPct": 200,
              "count": 16,
              "countPerRank": 4,
              "damagePct": 25,
              "spread": 105
            }
          },
          {
            "rank": 2,
            "values": {
              "burnPct": 10,
              "capPct": 200,
              "count": 20,
              "countPerRank": 4,
              "damagePct": 25,
              "spread": 120
            }
          },
          {
            "rank": 3,
            "values": {
              "burnPct": 10,
              "capPct": 200,
              "count": 24,
              "countPerRank": 4,
              "damagePct": 25,
              "spread": 135
            }
          },
          {
            "rank": 4,
            "values": {
              "burnPct": 10,
              "capPct": 200,
              "count": 28,
              "countPerRank": 4,
              "damagePct": 25,
              "spread": 150
            }
          }
        ]
      },
      {
        "skillId": "heavy_bomb",
        "characterId": "tanko",
        "name": "Bom liên hoàn",
        "description": "Bắn {count} quả bom lớn liên tiếp cùng đường bay (+{countPerRank} mỗi cấp súng), đào sâu một hố; mỗi quả {eachPct}% sát thương, mỗi địch nhận tối đa {capPct}%; địch trúng bị cầm chân lượt sau.",
        "effectType": "bomb_chain",
        "statusEffectId": "root",
        "aimKind": "arc",
        "cooldownTurn": 3,
        "projectileCount": 2,
        "damageMultiplier": 1,
        "params": {
          "baseCount": 2,
          "blast": 1.3,
          "capBase": 1.2,
          "capPerRank": 0.15,
          "countPerRank": 1,
          "fallSpeed": 3,
          "hitGap": 0.08,
          "launchGap": 0.1,
          "rootTurns": 1,
          "totalBase": 1.8,
          "totalPerRank": 0.15
        },
        "ranks": [
          {"rank": 0, "values": {"capPct": 120, "count": 2, "countPerRank": 1, "eachPct": 60}},
          {"rank": 1, "values": {"capPct": 135, "count": 3, "countPerRank": 1, "eachPct": 49}},
          {"rank": 2, "values": {"capPct": 150, "count": 4, "countPerRank": 1, "eachPct": 42}},
          {"rank": 3, "values": {"capPct": 165, "count": 5, "countPerRank": 1, "eachPct": 38}},
          {"rank": 4, "values": {"capPct": 180, "count": 6, "countPerRank": 1, "eachPct": 34}}
        ]
      },
      {
        "skillId": "shock_field",
        "characterId": "spark",
        "name": "Tia laze",
        "description": "Tia thẳng xuyên địa hình tới mép bản đồ, trúng mọi địch trên đường: {firstPct}% sát thương, mỗi địch sau giảm {decayPct}%; địch trúng bị choáng, mất lượt sau.",
        "effectType": "laser",
        "statusEffectId": "stun",
        "aimKind": "line",
        "cooldownTurn": 3,
        "projectileCount": 1,
        "damageMultiplier": 1,
        "params": {
          "beamTime": 0.6,
          "chargeTime": 0.3,
          "decay": 0.8,
          "firstBase": 1.5,
          "firstPerRank": 0.1,
          "halfWidth": 10,
          "stunTurns": 1,
          "sweepSpeed": 12000
        },
        "ranks": [
          {"rank": 0, "values": {"decayPct": 20, "firstPct": 150}},
          {"rank": 1, "values": {"decayPct": 20, "firstPct": 160}},
          {"rank": 2, "values": {"decayPct": 20, "firstPct": 170}},
          {"rank": 3, "values": {"decayPct": 20, "firstPct": 180}},
          {"rank": 4, "values": {"decayPct": 20, "firstPct": 190}}
        ]
      },
      {
        "skillId": "healing_bloom",
        "characterId": "flora",
        "name": "Hoa hồi xuân",
        "description": "Hồi máu cả đội: Flora {selfPct}%, đồng đội {allyPct}% máu tối đa (+{perRankPct}% mỗi cấp súng); máu dư thành khiên (tối đa {shieldCapPct}% máu); giải mọi hiệu ứng xấu.",
        "effectType": "bloom",
        "statusEffectId": "",
        "aimKind": "none",
        "cooldownTurn": 3,
        "projectileCount": 1,
        "damageMultiplier": 1,
        "params": {
          "allyBase": 30,
          "crossTime": 0.2,
          "fallSpeed": 400,
          "fallTime": 0.65,
          "linger": 0.9,
          "perRank": 5,
          "riseTime": 0.55,
          "seedTime": 1.4,
          "selfBase": 40,
          "shieldCapPercent": 50,
          "skyY": -140
        },
        "ranks": [
          {"rank": 0, "values": {"allyPct": 30, "perRankPct": 5, "selfPct": 40, "shieldCapPct": 50}},
          {"rank": 1, "values": {"allyPct": 35, "perRankPct": 5, "selfPct": 45, "shieldCapPct": 50}},
          {"rank": 2, "values": {"allyPct": 40, "perRankPct": 5, "selfPct": 50, "shieldCapPct": 50}},
          {"rank": 3, "values": {"allyPct": 45, "perRankPct": 5, "selfPct": 55, "shieldCapPct": 50}},
          {"rank": 4, "values": {"allyPct": 50, "perRankPct": 5, "selfPct": 60, "shieldCapPct": 50}}
        ]
      }
    ],
    "characters": [
      {
        "characterId": "rookie",
        "name": "Rookie",
        "role": "balanced",
        "roleName": "Cân bằng",
        "playstyle": "Không nổi bật hẳn mặt nào nhưng cũng chẳng có điểm yếu rõ ràng: máu, sát thương và tầm di chuyển đều vừa phải, đạn chịu gió bình thường. Hợp để tập căn góc và lực; khi đầy năng lượng, Mưa tên lửa trút xuống cả một vùng quanh pháo hiệu, khó né và đốt máu đối thủ ở lượt sau. Đổi lại, Rookie đánh không đau bằng Spark và không lì bằng Flora.",
        "weaponId": "basic_rocket",
        "skillId": "triple_shot",
        "gunKind": "bazooka",
        "hp": 1500,
        "damage": 480,
        "mobility": 100,
        "defense": 50,
        "skillPower": 50,
        "terrainDamage": 20,
        "difficulty": 1
      },
      {
        "characterId": "tanko",
        "name": "Tanko",
        "role": "demolisher",
        "roleName": "Phá địa hình",
        "playstyle": "Chuyên phá địa hình: đại pháo khoét hố sâu hơn hẳn mọi người, phá chỗ nấp và đào đất ngay dưới chân đối thủ. Đạn nặng ít bị gió đẩy nên vẫn bắn chuẩn khi trời gió lớn; để dành Bom liên hoàn bắn thẳng vào địch: từng quả đào sâu thêm, địch rơi xuống đáy hố, trúng tiếp và bị cầm chân. Bù lại mỗi phát gây ít sát thương và Tanko đi được ngắn nhất, khó thoát khỏi chỗ đứng xấu.",
        "weaponId": "cannon_ball",
        "skillId": "heavy_bomb",
        "gunKind": "cannon",
        "hp": 1650,
        "damage": 420,
        "mobility": 80,
        "defense": 60,
        "skillPower": 65,
        "terrainDamage": 30,
        "difficulty": 2
      },
      {
        "characterId": "spark",
        "name": "Spark",
        "role": "striker",
        "roleName": "Sát thương",
        "playstyle": "Đánh đau nhất: chỉ vài phát trúng là hạ được đối thủ, còn Tia laze xuyên địa hình, trúng mọi địch trên đường và làm họ mất lượt sau. Spark chạy xa nhất nên dễ đổi chỗ tìm góc bắn hoặc né khỏi tầm ngắm. Điểm yếu là máu và phòng thủ mỏng nhất, đạn nhẹ bị gió đẩy mạnh nên phải tính gió thật kỹ.",
        "weaponId": "electric_orb",
        "skillId": "shock_field",
        "gunKind": "shock_rifle",
        "hp": 1250,
        "damage": 620,
        "mobility": 120,
        "defense": 30,
        "skillPower": 80,
        "terrainDamage": 20,
        "difficulty": 2
      },
      {
        "characterId": "flora",
        "name": "Flora",
        "role": "sustain",
        "roleName": "Bền bỉ",
        "playstyle": "Lì đòn nhất: máu và phòng thủ cao nhất, lại có Hoa hồi xuân hồi máu cả đội, máu dư thành khiên, nên càng đánh lâu càng có lợi. Hãy đứng vững, bắn chắc tay và kéo trận dài để đối thủ cạn máu trước. Bù lại sát thương thấp và đạn gần như không khoét đất, nên khó kết thúc trận nhanh.",
        "weaponId": "seed_bomb",
        "skillId": "healing_bloom",
        "gunKind": "seed_launcher",
        "hp": 1900,
        "damage": 380,
        "mobility": 100,
        "defense": 90,
        "skillPower": 85,
        "terrainDamage": 24,
        "difficulty": 1
      }
    ],
    "defaultCharacterId": "rookie",
    "items": [
      {
        "itemId": "medkit",
        "name": "Hộp cứu thương",
        "type": "heal",
        "targetType": "self",
        "value": 450,
        "maxUsePerMatch": 1,
        "cooldown": 0,
        "instant": true,
        "aimKind": "none"
      },
      {
        "itemId": "teleport",
        "name": "Dịch chuyển",
        "type": "teleport",
        "targetType": "position",
        "value": 0,
        "maxUsePerMatch": 1,
        "cooldown": 0,
        "instant": false,
        "aimKind": "arc"
      },
      {
        "itemId": "power_shot",
        "name": "Tăng lực",
        "type": "attack_boost",
        "targetType": "self",
        "value": 1.5,
        "maxUsePerMatch": 1,
        "cooldown": 0,
        "instant": false,
        "aimKind": "arc"
      },
      {
        "itemId": "drill_bomb",
        "name": "Bom khoan",
        "type": "terrain_pierce",
        "targetType": "shoot",
        "value": 50,
        "maxUsePerMatch": 1,
        "cooldown": 0,
        "instant": false,
        "aimKind": "arc"
      },
      {
        "itemId": "spider_net",
        "name": "Lưới nhện",
        "type": "move_debuff",
        "targetType": "shoot",
        "value": 50,
        "maxUsePerMatch": 1,
        "cooldown": 0,
        "instant": false,
        "aimKind": "arc"
      },
      {
        "itemId": "freeze_bomb",
        "name": "Bom băng",
        "type": "freeze",
        "targetType": "shoot",
        "value": 1,
        "maxUsePerMatch": 1,
        "cooldown": 0,
        "instant": false,
        "aimKind": "arc"
      },
      {
        "itemId": "air_strike",
        "name": "Không kích",
        "type": "air_attack",
        "targetType": "position",
        "value": 30,
        "maxUsePerMatch": 1,
        "cooldown": 0,
        "instant": false,
        "aimKind": "targetX"
      },
      {
        "itemId": "wind_stopper",
        "name": "Dừng gió",
        "type": "wind_cancel",
        "targetType": "self",
        "value": 2,
        "maxUsePerMatch": 1,
        "cooldown": 0,
        "instant": true,
        "aimKind": "none"
      }
    ],
    "itemRules": {"maxLoadout": 3, "airStrike": {"angle": 270, "power": 80}},
    "statusEffects": [
      {
        "effectId": "burn",
        "name": "Bỏng",
        "marker": "",
        "banner": "",
        "bad": true,
        "skipsTurn": false,
        "blocksMove": false
      },
      {
        "effectId": "root",
        "name": "Bị cầm chân",
        "marker": "",
        "banner": "Bị cầm chân",
        "bad": true,
        "skipsTurn": false,
        "blocksMove": true
      },
      {
        "effectId": "stun",
        "name": "Choáng",
        "marker": "",
        "banner": "Choáng — mất lượt",
        "bad": true,
        "skipsTurn": true,
        "blocksMove": true
      },
      {
        "effectId": "freeze",
        "name": "Bị đóng băng",
        "marker": "",
        "banner": "Đóng băng — mất lượt",
        "bad": true,
        "skipsTurn": true,
        "blocksMove": true
      },
      {
        "effectId": "net",
        "name": "Bị lưới",
        "marker": "",
        "banner": "",
        "bad": true,
        "skipsTurn": false,
        "blocksMove": false
      },
      {
        "effectId": "power_shot",
        "name": "Tăng lực",
        "marker": "x1.5",
        "banner": "",
        "bad": false,
        "skipsTurn": false,
        "blocksMove": false
      },
      {
        "effectId": "drill_bomb",
        "name": "Bom khoan",
        "marker": "Khoan",
        "banner": "",
        "bad": false,
        "skipsTurn": false,
        "blocksMove": false
      },
      {
        "effectId": "spider_net",
        "name": "Lưới nhện",
        "marker": "Đạn lưới",
        "banner": "",
        "bad": false,
        "skipsTurn": false,
        "blocksMove": false
      },
      {
        "effectId": "freeze_bomb",
        "name": "Bom băng",
        "marker": "Đạn băng",
        "banner": "",
        "bad": false,
        "skipsTurn": false,
        "blocksMove": false
      },
      {
        "effectId": "air_strike",
        "name": "Không kích",
        "marker": "Không kích",
        "banner": "",
        "bad": false,
        "skipsTurn": false,
        "blocksMove": false
      },
      {
        "effectId": "wind_stop",
        "name": "Dừng gió",
        "marker": "",
        "banner": "",
        "bad": false,
        "skipsTurn": false,
        "blocksMove": false
      }
    ],
    "rank": {
      "tiers": [
        {"tier": "bronze", "name": "Đồng", "minRating": 0, "divisions": 3, "divisionSize": 333},
        {"tier": "silver", "name": "Bạc", "minRating": 1000, "divisions": 3, "divisionSize": 66},
        {"tier": "gold", "name": "Vàng", "minRating": 1200, "divisions": 3, "divisionSize": 100},
        {
          "tier": "platinum",
          "name": "Bạch kim",
          "minRating": 1500,
          "divisions": 3,
          "divisionSize": 100
        },
        {
          "tier": "diamond",
          "name": "Kim cương",
          "minRating": 1800,
          "divisions": 3,
          "divisionSize": 133
        },
        {"tier": "master", "name": "Cao thủ", "minRating": 2200, "divisions": 1, "divisionSize": 0}
      ],
      "defaultRating": 1000,
      "rankedMode": "ranked_2v2"
    },
    "levels": {
      "character": {
        "levels": [
          {"level": 1, "expRequired": 0},
          {"level": 2, "expRequired": 200},
          {"level": 3, "expRequired": 400},
          {"level": 4, "expRequired": 700},
          {"level": 5, "expRequired": 1100},
          {"level": 6, "expRequired": 1600},
          {"level": 7, "expRequired": 2200},
          {"level": 8, "expRequired": 3000},
          {"level": 9, "expRequired": 4000},
          {"level": 10, "expRequired": 5200},
          {"level": 11, "expRequired": 6425},
          {"level": 12, "expRequired": 7675},
          {"level": 13, "expRequired": 8950},
          {"level": 14, "expRequired": 10250},
          {"level": 15, "expRequired": 11575},
          {"level": 16, "expRequired": 12925},
          {"level": 17, "expRequired": 14300},
          {"level": 18, "expRequired": 15700},
          {"level": 19, "expRequired": 17125},
          {"level": 20, "expRequired": 18575},
          {"level": 21, "expRequired": 20050},
          {"level": 22, "expRequired": 21550},
          {"level": 23, "expRequired": 23075},
          {"level": 24, "expRequired": 24625},
          {"level": 25, "expRequired": 26200},
          {"level": 26, "expRequired": 27800},
          {"level": 27, "expRequired": 29425},
          {"level": 28, "expRequired": 31075},
          {"level": 29, "expRequired": 32750},
          {"level": 30, "expRequired": 34450},
          {"level": 31, "expRequired": 36175},
          {"level": 32, "expRequired": 37925},
          {"level": 33, "expRequired": 39700},
          {"level": 34, "expRequired": 41500},
          {"level": 35, "expRequired": 43325},
          {"level": 36, "expRequired": 45175},
          {"level": 37, "expRequired": 47050},
          {"level": 38, "expRequired": 48950},
          {"level": 39, "expRequired": 50875},
          {"level": 40, "expRequired": 52825},
          {"level": 41, "expRequired": 54800},
          {"level": 42, "expRequired": 56800},
          {"level": 43, "expRequired": 58825},
          {"level": 44, "expRequired": 60875},
          {"level": 45, "expRequired": 62950},
          {"level": 46, "expRequired": 65050},
          {"level": 47, "expRequired": 67175},
          {"level": 48, "expRequired": 69325},
          {"level": 49, "expRequired": 71500},
          {"level": 50, "expRequired": 73700},
          {"level": 51, "expRequired": 75925},
          {"level": 52, "expRequired": 78175},
          {"level": 53, "expRequired": 80450},
          {"level": 54, "expRequired": 82750},
          {"level": 55, "expRequired": 85075},
          {"level": 56, "expRequired": 87425},
          {"level": 57, "expRequired": 89800},
          {"level": 58, "expRequired": 92200},
          {"level": 59, "expRequired": 94625},
          {"level": 60, "expRequired": 97075},
          {"level": 61, "expRequired": 99550},
          {"level": 62, "expRequired": 102050},
          {"level": 63, "expRequired": 104575},
          {"level": 64, "expRequired": 107125},
          {"level": 65, "expRequired": 109700},
          {"level": 66, "expRequired": 112300},
          {"level": 67, "expRequired": 114925},
          {"level": 68, "expRequired": 117575},
          {"level": 69, "expRequired": 120250},
          {"level": 70, "expRequired": 122950},
          {"level": 71, "expRequired": 125675},
          {"level": 72, "expRequired": 128425},
          {"level": 73, "expRequired": 131200},
          {"level": 74, "expRequired": 134000},
          {"level": 75, "expRequired": 136825},
          {"level": 76, "expRequired": 139675},
          {"level": 77, "expRequired": 142550},
          {"level": 78, "expRequired": 145450},
          {"level": 79, "expRequired": 148375},
          {"level": 80, "expRequired": 151325}
        ],
        "maxLevel": 80
      },
      "player": {"expPerLevel": 100}
    },
    "equipment": {
      "upgradeStepPerLevel": 0.02,
      "maxUpgradeLevel": 16,
      "milestones": [
        {"level": 6, "bonus": 0.1},
        {"level": 10, "bonus": 0.2},
        {"level": 14, "bonus": 0.4},
        {"level": 16, "bonus": 1}
      ],
      "maxStoneLevel": 12,
      "mergeMinCount": 2,
      "mergeMaxCount": 4,
      "mergeRatePerItem": 25,
      "sellRate": 0.25,
      "weaponUpgradeCostMultiplier": 3,
      "tiers": [
        {"tier": "iron", "name": "Sắt", "kind": "normal", "idSuffix": "lv10", "skillRank": 1},
        {"tier": "steel", "name": "Thép", "kind": "normal", "idSuffix": "lv20", "skillRank": 2},
        {"tier": "mithril", "name": "Mithril", "kind": "normal", "idSuffix": "lv30", "skillRank": 3},
        {"tier": "dragon", "name": "Rồng", "kind": "normal", "idSuffix": "lv40", "skillRank": 4},
        {"tier": "silver", "name": "Bạc", "kind": "crafted", "idSuffix": "", "skillRank": 2},
        {"tier": "gold", "name": "Vàng", "kind": "crafted", "idSuffix": "", "skillRank": 3},
        {"tier": "titan", "name": "Titan", "kind": "crafted", "idSuffix": "", "skillRank": 4},
        {"tier": "diamond", "name": "Kim cương", "kind": "crafted", "idSuffix": "", "skillRank": 4}
      ],
      "slots": [
        {"id": "helmet", "name": "Mũ"},
        {"id": "armor", "name": "Áo"},
        {"id": "gloves", "name": "Găng tay"},
        {"id": "pants", "name": "Quần"},
        {"id": "boots", "name": "Giày"},
        {"id": "weapon", "name": "Vũ khí"}
      ],
      "typeWords": [
        {"word": "sword", "name": "Kiếm", "gunKind": ""},
        {"word": "vest", "name": "Áo giáp", "gunKind": ""},
        {"word": "cap", "name": "Mũ", "gunKind": ""},
        {"word": "pants", "name": "Quần", "gunKind": ""},
        {"word": "boots", "name": "Giày", "gunKind": ""},
        {"word": "gloves", "name": "Găng", "gunKind": ""},
        {"word": "bazooka", "name": "Bazooka", "gunKind": "bazooka"},
        {"word": "cannon", "name": "Đại pháo", "gunKind": "cannon"},
        {"word": "shock rifle", "name": "Súng điện", "gunKind": "shock_rifle"},
        {"word": "seed launcher", "name": "Súng hạt", "gunKind": "seed_launcher"},
        {"word": "blaster", "name": "Súng", "gunKind": "blaster"}
      ]
    },
    "economy": {"coinsPerGem": 100},
    "matchmaking": {
      "queue": "ranked_2v2",
      "slots": 4,
      "teamSize": 2,
      "botFillAfterSec": 20,
      "matchStartDelaySec": 3,
      "inviteTimeoutSec": 30,
      "tips": [
        "Gió mạnh đẩy đạn rất xa: nhìn mũi tên gió trước khi bắn.",
        "Bom liên hoàn của Tanko đào hố sâu, địch trúng bị cầm chân lượt sau.",
        "Tia laze của Spark xuyên địa hình và làm choáng mọi địch trên đường bắn.",
        "Hoa hồi xuân của Flora hồi máu cả đội và giải mọi hiệu ứng xấu.",
        "Mưa tên lửa của Rookie làm địch bỏng, mất máu đầu lượt sau.",
        "Bắn vào chân địch: hố sâu làm chúng rơi xuống và mất máu.",
        "Bom băng đóng băng địch một lượt, cả đội tha hồ dồn sát thương.",
        "Dịch chuyển giúp thoát khỏi hố sâu hoặc chiếm chỗ cao.",
        "Bị địch bắn trúng cũng nạp thêm năng lượng kỹ năng.",
        "Thắng trận xếp hạng để lên điểm, thua sẽ bị trừ điểm.",
        "Rời trận giữa chừng tính là thua, kể cả khi đồng đội thắng.",
        "Khiên chịu sát thương trước máu: máu hồi dư của Flora thành khiên."
      ]
    },
    "modes": {
      "categories": [
        {"categoryId": "pvp", "name": "PvP"},
        {"categoryId": "boss", "name": "Boss"}
      ],
      "lobby": [
        {
          "modeId": "casual",
          "name": "Đấu thường 2vs2",
          "description": "Ghép 2v2, không tính điểm hạng",
          "queue": "casual_2v2",
          "bot": "",
          "category": "pvp",
          "icon": "swords",
          "partySize": 2,
          "ranked": false
        },
        {
          "modeId": "ranked",
          "name": "Đấu hạng 2vs2",
          "description": "Ghép 2v2, tính điểm hạng",
          "queue": "ranked_2v2",
          "bot": "",
          "category": "pvp",
          "icon": "trophy",
          "partySize": 2,
          "ranked": true
        },
        {
          "modeId": "bot",
          "name": "Đấu máy",
          "description": "Đấu 1v1 với máy biết bắn",
          "queue": "",
          "bot": "ai",
          "category": "pvp",
          "icon": "robot",
          "partySize": 1,
          "ranked": false
        },
        {
          "modeId": "practice",
          "name": "Luyện tập",
          "description": "Tập bắn với bia đứng yên",
          "queue": "",
          "bot": "",
          "category": "pvp",
          "icon": "target",
          "partySize": 1,
          "ranked": false
        }
      ],
      "match": [
        {"modeId": "pvp_1v1", "name": "Đấu 1v1"},
        {"modeId": "pvp_2v2", "name": "Đấu 2v2"},
        {"modeId": "ranked_2v2", "name": "Đấu hạng 2vs2"},
        {"modeId": "casual_2v2", "name": "Đấu thường 2vs2"}
      ]
    },
    "mapThemes": {
      "default": "grassland",
      "themes": [
        "grassland",
        "ice",
        "beach",
        "graveyard",
        "desert",
        "sky",
        "temple",
        "steel",
        "city_night",
        "stone",
        "cave"
      ]
    },
    "gunKinds": [
      {"id": "bazooka", "name": "Bazooka"},
      {"id": "cannon", "name": "Đại pháo"},
      {"id": "shock_rifle", "name": "Súng điện"},
      {"id": "seed_launcher", "name": "Súng hạt"},
      {"id": "blaster", "name": "Súng"}
    ]
  }
}
```
