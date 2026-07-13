# Game Server Design Reference

Tài liệu thiết kế chi tiết cho 2 thể loại game online multiplayer:
1. **Turn-Based Artillery** (Battle Squad - Go)
2. **Realtime 2D MMORPG** (Ninja School Online - Java)

---

## MỤC LỤC

- [Phần 1: Turn-Based Artillery Game](#phần-1-turn-based-artillery-game)
- [Phần 2: Realtime 2D MMORPG](#phần-2-realtime-2d-mmorpg)
- [Phần 3: So sánh tổng quan](#phần-3-so-sánh-tổng-quan)

---

# Phần 1: Turn-Based Artillery Game

**Đại diện:** Battle Squad (Go, WebSocket, PostgreSQL, Redis)

## 1.1 Gameplay Overview

### Mô tả
- Game bắn súng theo lượt 2D (artillery/worms-style)
- 2-4 player chia thành 2 team
- Mỗi lượt 20 giây: di chuyển + bắn/dùng item
- Đạn bay theo quỹ đạo vật lý (gravity + wind)
- Địa hình phá hủy được
- Kết thúc khi 1 team bị tiêu diệt hoàn toàn

### Core Mechanics
| Mechanic | Chi tiết |
|----------|----------|
| Turn duration | 20s countdown |
| Actions per turn | Move (tốn energy) + 1 Shoot/Item |
| Physics | Projectile trajectory: gravity + wind |
| Terrain | Destructible, pixel-level collision |
| Win condition | Team elimination |
| Rating | Elo system, K-factor configurable |

### Game Modes
- **PvP 1v1**: 2 player, casual
- **PvP 2v2**: 4 player, ranked matchmaking
- **Tutorial**: 1 player vs idle bot
- **Bot fill**: Khi matchmaking timeout, fill bot AI

---

## 1.2 Vấn đề cần giải quyết

### Vấn đề 1: Đồng bộ trạng thái giữa các client
**Bối cảnh:** 4 player nhìn cùng 1 trận đấu, mọi action phải hiển thị đồng nhất.

**Khó khăn:**
- Network latency khác nhau giữa các player
- Client có thể miss message (packet loss, slow connection)
- Thứ tự message phải đúng (shoot trước damage sau)

**Phương án:**
| # | Phương án | Ưu | Nhược |
|---|-----------|-----|-------|
| 1 | Lockstep (đợi tất cả confirm) | Hoàn hảo consistency | Lag = player chậm nhất |
| 2 | Server authoritative + broadcast | Server quyết định, client hiển thị | Client bị delay |
| 3 | Client prediction + server reconciliation | Mượt nhất | Phức tạp, visual rollback |

**Chốt: Phương án 2 - Server Authoritative**

Lý do: Turn-based game không cần prediction vì player đợi lượt. Server xử lý logic, broadcast kết quả, client chỉ hiển thị animation.

---

### Vấn đề 2: Xử lý concurrent actions trong cùng match
**Bối cảnh:** Nhiều goroutine có thể access match state: room goroutine, match goroutine, bot goroutine, timer goroutine.

**Khó khăn:**
- Go map không thread-safe → panic nếu concurrent read/write
- Mutex gây deadlock nếu lock ordering sai
- Fine-grained lock phức tạp, khó debug

**Phương án:**
| # | Phương án | Ưu | Nhược |
|---|-----------|-----|-------|
| 1 | sync.RWMutex trên mỗi field | Fine-grained control | Lock ordering hell, deadlock |
| 2 | Single goroutine + channel (Actor model) | Zero lock, zero race | Phải queue mọi thứ qua channel |
| 3 | Hybrid: coarse mutex + careful access | Đơn giản | Vẫn có risk miss lock |

**Chốt: Phương án 2 - Single Goroutine Event Loop (Actor Model)**

Lý do: Đơn giản nhất, Go idiom (share memory by communicating). Match.Run() là single goroutine duy nhất access state. Mọi input (player action, bot action, timer) đều gửi qua buffered channel.

---

### Vấn đề 3: Match lifecycle & cleanup
**Bối cảnh:** Match phải kết thúc đúng lúc, cleanup resources, không để zombie match.

**Khó khăn:**
- Player disconnect giữa chừng
- Server crash/restart
- Match stuck (bug trong logic)
- Goroutine leak

**Phương án:**
| # | Phương án | Ưu | Nhược |
|---|-----------|-----|-------|
| 1 | Context cancellation only | Simple | Không detect stuck match |
| 2 | Watchdog timer + context | Detect zombie | Extra goroutine |
| 3 | Heartbeat-based timeout | Precise | Overhead per player |

**Chốt: Phương án 2 - Watchdog + Context + Panic Recovery**

Implementation:
- `context.WithCancel` cho graceful shutdown
- Watchdog mỗi 30s: check last activity, check all-disconnected
- `defer recover()` bắt panic → end match as no-contest
- `sync.Once` trên MatchDone channel để tránh double-close

---

### Vấn đề 4: Matchmaking cho ranked mode
**Bối cảnh:** Xếp cặp player theo rating, thời gian chờ hợp lý, fill bot khi cần.

**Khó khăn:**
- Rating range phải mở rộng dần (đợi lâu → accept wider range)
- Multiple server nodes cùng matchmake → duplicate match
- Bot difficulty phải phù hợp với rating tier

**Phương án:**
| # | Phương án | Ưu | Nhược |
|---|-----------|-----|-------|
| 1 | Per-node matchmaking | Simple, no coordination | Duplicate matches |
| 2 | Redis-based distributed queue + leader | Single matcher, no dupe | Complexity |
| 3 | Dedicated matchmaking microservice | Clean separation | Extra infra |

**Chốt: Phương án 2 - Redis Leader Election + Rating-Based Matching**

Implementation:
- Leader node (elected via Redis SETNX) runs matchmaker tick
- Queue stored in Redis sorted set (score = rating)
- Rating range widens: start ±50, expand ±25 mỗi 5s
- After timeout → fill bot với difficulty theo tier
- Bot difficulty config hot-reloaded từ DB mỗi 30s

---

### Vấn đề 5: Slow/dead client handling
**Bối cảnh:** Client mất mạng hoặc quá chậm, send buffer đầy.

**Khó khăn:**
- Blocking send → block cả match goroutine cho tất cả player
- Drop message → client desync
- Không biết client chết hay chỉ chậm

**Phương án:**
| # | Phương án | Ưu | Nhược |
|---|-----------|-----|-------|
| 1 | Blocking send + timeout | Guaranteed delivery | 1 slow client block all |
| 2 | Non-blocking send, drop if full | Never block | Client miss events |
| 3 | Non-blocking + drop counter → kick | Balance | Aggressive disconnect |

**Chốt: Phương án 3 - Non-blocking + Kick After Threshold**

Implementation:
- Buffered channel (256 messages)
- Non-blocking send: `select { case ch <- msg: default: dropped++ }`
- Counter reset trên mỗi successful send
- Sau 10 consecutive drops → close channel, kill player in match
- Client có thể reconnect và nhận full state sync

---

## 1.3 Core Architecture

```
                    ┌──────────────────────────────────────────┐
                    │              GAME SERVER                  │
                    │                                          │
  WebSocket ───────►│  WS Server (gorilla/websocket)           │
  Connection        │    │                                     │
                    │    ▼                                     │
                    │  CompositeWSHandler                      │
                    │    ├─► LobbyHandler (ranked queue)       │
                    │    └─► RoomHandler (match routing)       │
                    │           │                              │
                    │           ▼                              │
                    │  Room Hub ─── [Room 1] [Room 2] ...      │
                    │    (sync.RWMutex)  │                     │
                    │                    ▼                     │
                    │              Match Engine                 │
                    │              (1 goroutine)                │
                    │                    │                     │
                    │    ┌───────────────┼──────────────┐      │
                    │    ▼               ▼              ▼      │
                    │  Physics      Bot Brain      Elo Calc    │
                    │  (terrain,    (SmartBot,     (K-factor,  │
                    │   collision)   BotBrain)      ratings)   │
                    └──────────────────────────────────────────┘
                              │                    │
                              ▼                    ▼
                         PostgreSQL              Redis
                         (persistent)         (sessions, queue,
                                               leaderboard)
```

### Component Ownership

| Component | Goroutine | Owns |
|-----------|-----------|------|
| WS Server | 1 per connection (read) + 1 per connection (write) | Socket I/O |
| Room Hub | Main goroutine (mutex-protected) | rooms map |
| Room | 1 goroutine per room | Room.State, Room.Clients (pre-match) |
| Match | 1 goroutine per match | Match.State, Match.Clients (during match) |
| Matchmaker | 1 goroutine (leader only) | Redis queue |
| Lobby | 1 goroutine per lobby | Lobby.State, Lobby.Clients |

---

## 1.4 Luồng chi tiết

### Luồng 1: Player Connect → Join Room → Match

```
Client                    WS Server              Room Hub           Room              Match
  │                          │                      │                │                 │
  │──── WS Connect ─────────►│                      │                │                 │
  │                          │── Auth (JWT) ────────│                │                 │
  │                          │── Create Client ─────│                │                 │
  │                          │── ReadPump() ────────│                │                 │
  │                          │── WritePump() ───────│                │                 │
  │                          │                      │                │                 │
  │──── CreateRoom ─────────►│─────────────────────►│                │                 │
  │                          │                      │── NewRoom() ──►│                 │
  │                          │                      │── go Run() ───►│                 │
  │◄──── RoomCreated ────────│                      │                │                 │
  │                          │                      │                │                 │
  │──── JoinRoom ───────────►│─────────────────────►│── FindRoom() ─►│                 │
  │                          │                      │                │── event chan ──►│
  │                          │                      │                │  (processJoin)  │
  │◄──── RoomUpdated ────────│                      │                │                 │
  │                          │                      │                │                 │
  │──── Ready ──────────────►│──────────────────────────────────────►│                 │
  │──── StartMatch ─────────►│──────────────────────────────────────►│                 │
  │                          │                      │                │── NewMatch() ──►│
  │                          │                      │                │── go Run() ────►│
  │◄──── MatchStarted ───────│                      │                │                 │
  │                          │                      │                │                 │
  │──── Move/Shoot ─────────►│──────────────────────────────────────►│── event chan ──►│
  │                          │                      │                │                 │── process
  │◄──── PlayerMoved ────────│                      │                │                 │── broadcast
  │◄──── ProjectileResult ───│                      │                │                 │
  │◄──── PlayerDamaged ──────│                      │                │                 │
  │                          │                      │                │                 │
  │◄──── MatchEnded ─────────│                      │                │◄── matchDone ───│
  │                          │                      │◄── destroy ────│                 │
```

### Luồng 2: Ranked Matchmaking

```
Player A (Lobby)         Matchmaker              Room Hub           Match
  │                          │                      │                │
  │── StartQueue ───────────►│                      │                │
  │                          │── Add to Redis ──────│                │
  │                          │   sorted set         │                │
  │                          │                      │                │
  │                          │── Tick (every 2s) ───│                │
  │                          │   Check pairs:       │                │
  │                          │   |ratingA-ratingB|  │                │
  │                          │   < allowed_range    │                │
  │                          │                      │                │
  │                          │── Match Found! ─────►│                │
  │                          │                      │── CreateBattle │
  │                          │                      │── RegisterClients
  │◄──── MatchFound ─────────│                      │── startRankedMatch
  │                          │                      │   go match.Run()
  │                          │                      │   go room.Run()
  │◄──── MatchStarted ───────│                      │                │
  │                          │                      │                │
  │  ... match plays out ... │                      │                │
  │                          │                      │                │
  │◄──── MatchEnded ─────────│                      │                │
  │◄──── ReturnToLobby ──────│                      │                │
  │                          │                      │                │
  │── StartQueue again ─────►│  (cycle repeats)     │                │
```

### Luồng 3: Match Turn Cycle (Core Game Loop)

```
Match Goroutine (single thread):

    ┌──────────────────────────────────────────────────────────┐
    │                    select { }                             │
    │                                                          │
    │  case ev := <-m.Events:     ◄── Player/Bot actions       │
    │      handleEvent(ev)             Move, Shoot, UseItem    │
    │                                                          │
    │  case <-ticker.C:           ◄── Every 1 second           │
    │      TurnTimeLeft--              Broadcast TurnTimerTick  │
    │      if TurnTimeLeft <= 0:       Force endTurn()          │
    │                                                          │
    │  case <-watchdog.C:         ◄── Every 30 seconds         │
    │      check all disconnected      End if zombie            │
    │      check last activity         End if stuck > 2min      │
    │                                                          │
    │  case <-m.ctx.Done():       ◄── Graceful shutdown        │
    │      return                                              │
    └──────────────────────────────────────────────────────────┘

    Turn Flow:
    ┌─────────────┐    ┌──────────────┐    ┌───────────────┐
    │ startTurn() │───►│ Wait action  │───►│  endTurn()    │
    │ reset energy│    │ or timeout   │    │ next player   │
    │ update wind │    │ (20s max)    │    │ check win     │
    │ broadcast   │    │              │    │ startTurn()   │
    └─────────────┘    └──────────────┘    └───────────────┘

    Shoot Flow (within a turn):
    ┌──────────┐   ┌────────────┐   ┌───────────┐   ┌──────────┐
    │ Validate │──►│ Simulate   │──►│ Calculate │──►│ Apply    │
    │ turn,    │   │ projectile │   │ explosion │   │ damage,  │
    │ energy,  │   │ trajectory │   │ radius,   │   │ destroy  │
    │ frozen   │   │ (physics)  │   │ falloff   │   │ terrain  │
    └──────────┘   └────────────┘   └───────────┘   └──────────┘
         │                                                │
         ▼                                                ▼
    ┌──────────────────────────────────────────────────────────┐
    │ Broadcast: ProjectileResult, PlayerDamaged, TerrainUpdate │
    │ Schedule: delayed endTurn (wait for animation)            │
    │ Check: win condition                                      │
    └──────────────────────────────────────────────────────────┘
```

### Luồng 4: Bot AI Decision

```
startTurn() detects bot's turn
    │
    ▼
go func() {                          ◄── Spawn goroutine
    sleep(2-6s random)               ◄── Feel natural
    │
    ▼
    BotBrain.DecideAction(state)     ◄── AI logic
    │
    ├── SmartBotBrain (ranked):
    │   ├── Evaluate all targets (distance, HP)
    │   ├── Calculate optimal angle
    │   ├── Add error margin (based on tier)
    │   └── Return ShootAction
    │
    └── BotBrain (tutorial):
        └── Return EndTurn (idle bot)
    │
    ▼
    m.Events <- matchEvent{Shoot}    ◄── Inject into event channel
}()                                      (processed by match goroutine)
```

---

## 1.5 Data Flow & State

### Match State (owned by Match goroutine)

```go
MatchState {
    MatchID         string
    RoomID          string
    Mode            string              // "pvp_1v1", "pvp_2v2"
    MapID           string
    Status          string              // "in_progress", "ended"
    TurnIndex       int
    CurrentPlayerID string
    TurnTimeLeft    int                 // countdown 20→0
    Wind            WindState           // direction + power
    Players         map[string]*BattlePlayerState {
        PlayerID    string
        DisplayName string
        TeamID      int
        HP, MaxHP   int
        Position    Vector2{X, Y float64}
        MoveEnergy  int                 // 100 per turn, 1 per 2px
        SkillEnergy int                 // +20 per turn, max 100
        IsAlive     bool
        IsBot       bool
        Items       []string
        Effects     []StatusEffect
    }
    TurnOrder       []string            // alternating teams
}
```

### Message Protocol (WebSocket JSON)

```json
// Client → Server
{"event": "Move", "data": {"targetX": 450.5}}
{"event": "Shoot", "data": {"angle": 45.0, "power": 80.0, "actionMode": "normal"}}
{"event": "UseItem", "data": {"itemId": "teleport", "targetX": 800, "targetY": 200}}
{"event": "EndTurn"}
{"event": "Reconnect"}

// Server → Client
{"event": "MatchStarted", "data": {<full MatchState>}}
{"event": "TurnStarted", "data": {"turnIndex":3, "currentPlayerId":"...", "wind":{...}}}
{"event": "TurnTimerTick", "data": {"timeLeft": 15}}
{"event": "PlayerMoved", "data": {"playerId":"...", "position":{x,y}, "moveEnergy":60}}
{"event": "ProjectileResult", "data": {"impacts":[...], "flightTime":1.2}}
{"event": "PlayerDamaged", "data": [{"playerId":"...", "damage":35, "hp":65, "isAlive":true}]}
{"event": "TerrainDestroyed", "data": {"x":400, "y":300, "radius":50}}
{"event": "MatchEnded", "data": {"winningTeam":1, "rewards":[...]}}
{"event": "MatchStateSync", "data": {<full MatchState>}}  // on reconnect
```

---

## 1.6 Kỹ thuật quan trọng

### Physics Engine (Server-side)
```
Projectile simulation:
  x(t) = x0 + vx*t + wind*t²
  y(t) = y0 + vy*t + 0.5*gravity*t²

Collision detection:
  - Pixel-level terrain mask (bool[][])
  - Circle-circle for player hitbox
  - Step through trajectory at 1px intervals

Terrain destruction:
  - Clear circular area from collision mask
  - Broadcast destroyed region to clients

Damage falloff:
  - Full damage at epicenter
  - Linear falloff to 0 at explosion radius edge
  - damage = baseDamage * (1 - distance/radius)
```

### Elo Rating System
```
Expected score: E = 1 / (1 + 10^((opponentRating - myRating) / 400))
New rating: R' = R + K * (actualScore - expectedScore)

actualScore: 1.0 (win), 0.0 (loss)
K-factor: configurable (default 32)
Bot modifier: 0.5x (less rating change vs bots)
Rating floor: 0 (can't go negative)
```

### Reconnection
```
1. Client reconnects with same JWT
2. Server finds room by player ID (Redis lookup)
3. Room routes to Match.ProcessEvent("Reconnect")
4. Match replaces client reference: m.Clients[playerID] = newClient
5. Match sends full MatchStateSync to reconnected client
6. Client rebuilds visual state from sync data
```

---

---

# Phần 2: Realtime 2D MMORPG

**Đại diện:** Ninja School Online (Java, TCP Socket, MySQL)

## 2.1 Gameplay Overview

### Mô tả
- Game MMORPG 2D side-scrolling realtime
- Hàng trăm player online cùng lúc trên 1 server
- 160+ map, mỗi map chia nhiều area/zone (10-20 area)
- Mỗi area chứa 20+ player và 30+ mob
- Combat realtime: đánh liên tục, không đợi lượt
- PvE (đánh mob, boss) + PvP (đấu player)

### Core Mechanics
| Mechanic | Chi tiết |
|----------|----------|
| Movement | Realtime, client gửi (x,y) liên tục |
| Combat | Realtime, attack mỗi 0.5-2s tùy speed |
| Mob AI | Timer-based attack, không di chuyển |
| Area capacity | ~20 player + ~30 mob per zone |
| Persistence | Character save to DB, load on login |
| Economy | Item drop, trade, shop, upgrade |

### Game World Structure
```
Server (1 process)
  └── 160 Maps (1 thread each)
       └── 10-20 Areas per map (logical partition)
            ├── Player list (max ~20)
            ├── Mob list (max ~50)
            ├── Item drops on ground
            └── NPC/quest givers
```

---

## 2.2 Vấn đề cần giải quyết

### Vấn đề 1: Thousands of concurrent connections
**Bối cảnh:** 5000-10000 player online cùng lúc, mỗi player 1 TCP connection.

**Khó khăn:**
- Mỗi connection cần đọc/ghi liên tục
- Memory: 10K connection × data structures = RAM pressure
- Context switching: quá nhiều thread = CPU overhead

**Phương án:**
| # | Phương án | Ưu | Nhược |
|---|-----------|-----|-------|
| 1 | Thread-per-connection (blocking I/O) | Đơn giản, dễ debug | 20K threads, RAM cao |
| 2 | NIO + Event loop (Netty) | Scalable, ít thread | Phức tạp, callback hell |
| 3 | Thread pool + selector | Balance | Medium complexity |

**Chốt: Phương án 1 - Thread-per-connection**

Lý do: Đơn giản nhất, JVM handle được 20K threads (10K player × 2 threads). RAM ~20GB cho 10K player là chấp nhận được. Game logic chạy trực tiếp trên network thread, không cần queue/dispatch overhead. Production đã chứng minh ổn định.

Tradeoff chấp nhận: RAM cao hơn NIO approach, nhưng code đơn giản hơn 10x → ít bug hơn.

---

### Vấn đề 2: Realtime movement sync cho 20 player/area
**Bối cảnh:** 20 player di chuyển liên tục, mỗi player gửi position update 5-10 lần/giây.

**Khó khăn:**
- 20 player × 10 updates/s = 200 messages/s per area
- Broadcast mỗi move cho 19 player khác = 200 × 19 = 3800 messages/s outbound per area
- Phải giữ smooth trên client

**Phương án:**
| # | Phương án | Ưu | Nhược |
|---|-----------|-----|-------|
| 1 | Broadcast mọi move ngay lập tức | Lowest latency | Bandwidth explosion |
| 2 | Batch updates mỗi tick (100ms) | Save bandwidth | 100ms latency |
| 3 | Immediate broadcast, no lock | Fast | Có thể race nhưng OK cho position |

**Chốt: Phương án 3 - Immediate Broadcast, No Synchronization**

Lý do: Movement KHÔNG cần consistency hoàn hảo. Nếu 2 player thấy position lệch 1-2 frame, không ảnh hưởng gameplay. Bỏ lock cho movement = zero contention, movement luôn mượt dù combat đang xảy ra.

Implementation:
```java
// moveMessage() - KHÔNG synchronized
public void moveMessage(User p, Message msg) {
    nj.x = msg.readShort();  // Direct write, no lock
    nj.y = msg.readShort();
    // Broadcast cho all players trong area
    for (User u : this.getUsers()) {  // CopyOnWriteArrayList - safe iteration
        if (u != p) u.sendMessage(moveMsg);
    }
}
```

---

### Vấn đề 3: Combat consistency - 20 player đánh cùng 1 mob
**Bối cảnh:** 20 player đánh boss cùng lúc, mỗi player attack 2 lần/giây = 40 attacks/s vào cùng mob.

**Khó khăn:**
- Mob HP phải chính xác (không negative, không double-kill reward)
- Ai giết mob cuối cùng phải nhận reward đúng
- Damage calculation cần đọc mob HP + player stats atomically

**Phương án:**
| # | Phương án | Ưu | Nhược |
|---|-----------|-----|-------|
| 1 | Fine-grained lock per mob | Parallelism cho different mobs | Lock ordering, deadlock risk |
| 2 | Coarse lock per area (synchronized Place) | Simple, no deadlock | 1 attack blocks all |
| 3 | Lock-free CAS trên mob HP | Maximum parallelism | Complex, retry storms |
| 4 | Queue attacks, process in tick | Consistent | Latency per attack |

**Chốt: Phương án 2 - Coarse-grained synchronized(Place)**

Lý do: Mỗi attack chỉ mất ~8ms trong synchronized block. 20 player attack cùng lúc = serialize 160ms worst case. Player không nhận thấy 160ms delay trong realtime combat. Đơn giản tuyệt đối, zero deadlock risk.

```java
// FightMob() - synchronized trên Place instance
public void FightMob(User p, Message msg) {
    // ... validate (outside lock) ...

    synchronized(this) {  // Lock TOÀN BỘ area
        // 1. Read mob HP
        // 2. Calculate damage
        // 3. Apply damage
        // 4. Check death
        // 5. Award XP/drops
        // 6. Broadcast result
    }  // ~8ms total, release
}
```

Key insight: **Coarse lock + fast execution = đơn giản VÀ hiệu quả.** Không cần fine-grained.

---

### Vấn đề 4: Mob AI cho 30+ mobs per area
**Bối cảnh:** Mỗi area có 30 mob, mỗi mob cần "sống" - attack player, respawn khi chết.

**Khó khăn:**
- 30 mob × pathfinding = expensive
- Mob attack timing phải feel natural
- Mob respawn sau X giây
- Không được block player actions

**Phương án:**
| # | Phương án | Ưu | Nhược |
|---|-----------|-----|-------|
| 1 | Full AI mỗi tick (A*, behavior tree) | Smart mobs | CPU expensive |
| 2 | Timer-based: chỉ attack, không move | Rất nhẹ | Mob đứng yên |
| 3 | Simple patrol + attack radius | Có movement | Medium CPU |

**Chốt: Phương án 2 - Timer-based Attack, No Movement**

Lý do: Mobile game 2D không cần mob di chuyển thông minh. Mob đứng tại vị trí spawn, attack player khi đến gần (range check). Respawn sau timer. Cực kỳ nhẹ: chỉ cần 1 timestamp check per mob per tick.

```java
// Trong Place.update() - chạy mỗi 1 giây
for (Mob mob : mobs) {
    if (!mob.isDie && System.currentTimeMillis() >= mob.timeFight) {
        mob.attackNearbyPlayer(this);  // Range check + damage
        mob.timeFight = System.currentTimeMillis() + mob.attackInterval;
    }
    if (mob.isDie && System.currentTimeMillis() >= mob.respawnTime) {
        mob.respawn();
    }
}
```

Chi phí: 30 mob × 2 timestamp comparisons = ~0.001ms per tick. Negligible.

---

### Vấn đề 5: Disconnection handling cho persistent world
**Bối cảnh:** Player disconnect bất ngờ, character phải được save và area phải cleanup.

**Khó khăn:**
- Phân biệt disconnect vs lag spike
- Save character state trước khi cleanup
- Thông báo cho party/guild members
- Remove từ area player list (concurrent modification)

**Phương án:**
| # | Phương án | Ưu | Nhược |
|---|-----------|-----|-------|
| 1 | Immediate cleanup on socket close | Fast | Lag spike = kick |
| 2 | Grace period (10s timeout) | Tolerant | Zombie characters |
| 3 | Heartbeat-based detection + grace | Balanced | Extra traffic |

**Chốt: Phương án 2 - Grace Period với DaemonThread**

Implementation:
- Mỗi message nhận được → update `lastTimeReceiveData`
- DaemonThread kiểm tra mỗi 60s: nếu idle > 10s → disconnect
- Disconnect flow:
  1. Close socket
  2. Interrupt cả 2 thread (read + send)
  3. Save character to DB
  4. Remove from area's CopyOnWriteArrayList
  5. Broadcast "player left" cho area

---

### Vấn đề 6: Bandwidth optimization
**Bối cảnh:** Mobile game, connection 3G/4G, bandwidth limited.

**Khó khăn:**
- 20 player × frequent updates = nhiều data
- Binary protocol vs text protocol
- Minimize redundant data

**Phương án:**
| # | Phương án | Ưu | Nhược |
|---|-----------|-----|-------|
| 1 | JSON text protocol | Debug easy | Large payload |
| 2 | Custom binary protocol | Minimal size | Hard to debug |
| 3 | Protobuf/MessagePack | Balanced | Dependency |

**Chốt: Phương án 2 - Custom Binary Protocol với XOR Encryption**

Implementation:
```
Message format:
  [1 byte: command ID]
  [2 bytes: data length]
  [N bytes: data (custom binary encoding)]

Encryption: XOR với fixed key per session
Compression: Không cần (messages already tiny: 10-50 bytes)
```

Kết quả: Movement message = 5 bytes (command + x + y). So với JSON {"x":400,"y":200} = 17 bytes. Tiết kiệm 70%.

---

## 2.3 Core Architecture

```
┌────────────────────────────────────────────────────────────────┐
│                         GAME SERVER                             │
│                                                                │
│  ┌──────────────┐                                              │
│  │ Server Thread │──── Accept connections ──────┐               │
│  │ (TCP :14444) │                               │               │
│  └──────────────┘                               ▼               │
│                                          ┌─────────────┐        │
│                                          │   Session    │×10000  │
│                                          │  (2 threads) │        │
│                                          │  Read + Send │        │
│                                          └──────┬──────┘        │
│                                                 │               │
│  ┌──────────────┐                               ▼               │
│  │ DaemonThread │◄─── monitors ──── PlayerManager               │
│  │ (timeout,    │                   (all sessions)              │
│  │  blacklist)  │                                              │
│  └──────────────┘                                              │
│                                                                │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │                    MAP SYSTEM                            │   │
│  │                                                         │   │
│  │  Map Thread ×160 (1 per map, 1-second tick)             │   │
│  │    │                                                    │   │
│  │    ├── Area[0]: Place instance                          │   │
│  │    │     ├── CopyOnWriteArrayList<User> (players)       │   │
│  │    │     ├── ArrayList<Mob> (mobs, sync'd access)       │   │
│  │    │     ├── CopyOnWriteArrayList<ItemMap> (drops)      │   │
│  │    │     └── synchronized update() every 1s             │   │
│  │    │                                                    │   │
│  │    ├── Area[1]: Place instance                          │   │
│  │    │     └── ... (same structure)                       │   │
│  │    │                                                    │   │
│  │    └── Area[N]: Place instance                          │   │
│  │                                                         │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐         │
│  │  Controller  │  │   Service    │  │   Manager    │         │
│  │ (msg router) │  │ (msg builder)│  │ (game logic) │         │
│  └──────────────┘  └──────────────┘  └──────────────┘         │
└────────────────────────────────────────────────────────────────┘
                              │
                              ▼
                           MySQL
                     (character data,
                      items, inventory)
```

### Thread Count Breakdown (10K players)

| Thread Type | Count | Purpose |
|-------------|-------|---------|
| Server accept | 1 | Accept new TCP connections |
| Session read | 10,000 | Blocking read from socket |
| Session send | 10,000 | Blocking write (queue-based) |
| Map | 160 | 1-second tick per map |
| DaemonThread | 1 | Timeout detection, cleanup |
| DB pool | 50 | Connection pool to MySQL |
| **Total** | **~20,212** | |

### Memory Budget (10K players)

| Resource | Per Unit | Total |
|----------|----------|-------|
| Thread stack | 512KB | 10GB |
| Session object | ~2KB | 20MB |
| Ninja (character) | ~10KB | 100MB |
| Map/Area data | ~1MB per map | 160MB |
| Mob instances | ~500B per mob | ~50MB |
| **Total** | | **~10.5GB** |

---

## 2.4 Luồng chi tiết

### Luồng 1: Player Login → Enter Map

```
Client                Session Thread         Controller        PlayerManager       Map/Place
  │                       │                      │                  │                │
  │── TCP Connect ───────►│                      │                  │                │
  │                       │── start Read thread  │                  │                │
  │                       │── start Send thread  │                  │                │
  │                       │                      │                  │                │
  │── Login Message ─────►│─────────────────────►│                  │                │
  │                       │                      │── validate ─────►│                │
  │                       │                      │   (user/pass)    │── checkDupe ──►│
  │                       │                      │                  │── addSession   │
  │                       │                      │◄── User object ──│                │
  │                       │                      │                  │                │
  │                       │                      │── loadNinja() ───│                │
  │                       │                      │   (from MySQL)   │                │
  │                       │                      │                  │                │
  │◄── Login Success ─────│◄─────────────────────│                  │                │
  │◄── Character Data ────│                      │                  │                │
  │                       │                      │                  │                │
  │                       │                      │── EnterMap() ────────────────────►│
  │                       │                      │                  │                │── add to
  │                       │                      │                  │                │   _users
  │◄── Map Data ──────────│                      │                  │                │
  │◄── Other Players ─────│                      │                  │                │
  │◄── Mob Positions ─────│                      │                  │                │
  │                       │                      │                  │                │
  │   (other players      │                      │                  │                │
  │    receive "player    │                      │                  │                │
  │    entered" msg)      │                      │                  │                │
```

### Luồng 2: Realtime Combat (Player attacks Mob)

```
Player Thread            Place (Area)              Mob              Other Players
  │                         │                      │                    │
  │── Attack Message ──────►│                      │                    │
  │   (mob_id, skill_id)    │                      │                    │
  │                         │                      │                    │
  │                    ┌────┤ synchronized(this) {  │                    │
  │                    │    │                      │                    │
  │   validate:        │    │── canAttack()?       │                    │
  │   - cooldown OK?   │    │── inRange()?         │                    │
  │   - mana enough?   │    │── mob.isDie?         │                    │
  │                    │    │                      │                    │
  │   calculate:       │    │── damage formula ────│                    │
  │   - base damage    │    │   ATK - DEF          │                    │
  │   - critical?      │    │   × crit modifier    │                    │
  │   - element bonus  │    │                      │                    │
  │                    │    │                      │                    │
  │   apply:           │    │── mob.HP -= damage ──►│ HP update         │
  │                    │    │                      │                    │
  │   check death:     │    │── if HP <= 0: ───────►│ isDie = true      │
  │                    │    │     award XP          │ set respawnTime   │
  │                    │    │     drop items        │                    │
  │                    │    │     update quest      │                    │
  │                    │    │                      │                    │
  │   broadcast:       │    │──────────────────────────────────────────►│
  │                    │    │  "Player X attacked   │                    │ display
  │                    │    │   Mob Y, damage Z"    │                    │ animation
  │                    │    │                      │                    │
  │                    └────┤ } // release lock     │                    │
  │                         │                      │                    │
  │◄── Attack Result ───────│                      │                    │
  │   (damage, mob HP,      │                      │                    │
  │    XP gained, drops)    │                      │                    │
  │                         │                      │                    │
  │   Total time in lock:   │                      │                    │
  │   ~8ms                  │                      │                    │
```

### Luồng 3: 1-Second Tick (Place.update)

```
Map Thread
  │
  │── Every 1000ms ──────────────────────────────────────────────────────
  │
  │   for each Area in map:
  │     │
  │     ▼
  │   Place.update()
  │     │
  │     ├── synchronized(this) {
  │     │
  │     ├── [1] Update Items (3643-3654)
  │     │     for each item on ground:
  │     │       if invisible && time expired → make visible
  │     │       if lifetime expired → remove
  │     │
  │     ├── [2] Update Players (3655-3676)
  │     │     for each player:
  │     │       if has fire debuff → apply DoT damage
  │     │       if has ice debuff → check duration
  │     │       if has wind debuff → check duration
  │     │       if buff expired → remove buff
  │     │
  │     ├── [3] Update Mobs (3684-3716)
  │     │     for each mob:
  │     │       if mob.isDie:
  │     │         if respawnTime reached → respawn()
  │     │       else:
  │     │         if fire debuff → apply damage
  │     │         if attackTimer reached → attackNearbyPlayer()
  │     │         reset attackTimer
  │     │
  │     ├── [4] Special Events (3718-3750)
  │     │     Cave mechanics
  │     │     Boss spawn timers
  │     │     Event/quest logic
  │     │
  │     └── } // release synchronized
  │
  │   Total update time per area: ~5-20ms
  │   Total per map (15 areas): ~75-300ms
  │   Sleep remaining: 1000ms - elapsed
  │
  │── Next tick ─────────────────────────────────────────────────────────
```

### Luồng 4: Player Movement Broadcast

```
Player A Thread          Place               Player B,C,D... Threads
  │                       │                        │
  │── Move(x=400,y=200)─►│                        │
  │                       │  NO LOCK!              │
  │                       │                        │
  │                       │── nj.x = 400           │
  │                       │── nj.y = 200           │
  │                       │                        │
  │                       │── for each user:       │
  │                       │     user.sendMessage() │
  │                       │          │             │
  │                       │          └────────────►│── queue.add(moveMsg)
  │                       │                        │   (send thread delivers)
  │                       │                        │
  │◄── (no response       │                        │◄── receive move packet
  │     needed for move)  │                        │    update Player A sprite
  │                       │                        │
  │  Latency: ~1-5ms     │                        │  Latency: ~10-50ms
  │  (local update)       │                        │  (network + queue)
```

### Luồng 5: Concurrent Attacks Serialization

```
Timeline (20 players attack simultaneously):

T=0ms:   Player1.FightMob() → acquire synchronized(Place) ✓
T=0ms:   Player2.FightMob() → BLOCKED (waiting for lock)
T=0ms:   Player3.FightMob() → BLOCKED
T=0ms:   Player4.moveMessage() → executes immediately (no lock needed!)
...
T=8ms:   Player1 done → release lock
T=8ms:   Player2.FightMob() → acquire lock ✓
T=8ms:   Player5.moveMessage() → executes immediately
...
T=16ms:  Player2 done → release lock
T=16ms:  Player3.FightMob() → acquire lock ✓
...
T=160ms: Player20 done → all attacks processed

Key insight:
- Attacks serialize: 160ms total for 20 attacks
- Movement NEVER blocks: always instant
- Players feel responsive because movement is most frequent action
- Attack delay (8-160ms) hidden by attack animation (~500ms)
```

---

## 2.5 Data Flow & State

### Session State (per connection)

```java
Session {
    Socket socket;
    Thread readThread, sendThread;
    BlockingQueue<Message> sendDatas;
    long lastTimeReceiveData;       // timeout detection
    boolean connected;
    boolean login;
    User user;                      // null until login
}
```

### Character State (Ninja)

```java
Ninja {
    // Identity
    int id;
    String name;
    byte gender;            // 0=male, 1=female
    byte classId;           // 1=sword, 2=shuriken, 3=kunai, 4=fan

    // Position
    volatile short x, y;
    short mapId;
    byte areaId;

    // Stats (volatile for cross-thread visibility)
    volatile int hp, maxHP;
    volatile int mp, maxMP;
    volatile int attack, defense, speed;
    volatile short level;
    long exp;

    // Equipment
    Item[] equipment;       // weapon, armor, etc.
    Item[] inventory;       // bag items

    // State
    boolean isDie;
    long timeDie;           // death timestamp for respawn
    byte pk;                // PK mode (0=peace, 1=hostile)

    // Effects
    byte fire, ice, wind;   // debuff counters (tick-based duration)
}
```

### Mob State

```java
Mob {
    int id;
    short x, y;
    int hp, maxHP;
    int attack, defense;
    byte level;
    volatile boolean isDie;
    long timeFight;         // next attack timestamp
    long respawnTime;       // when to respawn after death
    int[] dropItems;        // item IDs that can drop
    int expReward;
}
```

### Binary Protocol Format

```
┌──────────────────────────────────────────────────────┐
│  Packet Structure                                     │
├──────────┬──────────┬────────────────────────────────┤
│ Command  │ Length   │ Payload                         │
│ (1 byte) │ (2 byte) │ (variable)                     │
├──────────┼──────────┼────────────────────────────────┤
│   -23    │   0005   │ [chat message bytes]            │ Chat
│   -28    │   0004   │ [x:short][y:short]              │ Move
│   -30    │   000A   │ [sub_cmd][mob_id][skill_id]...  │ Attack
│   -24    │   0002   │ [mob_id:short]                  │ Pick item
└──────────┴──────────┴────────────────────────────────┘

XOR Encryption:
  encrypted[i] = raw[i] ^ KEY_BYTE
  KEY_BYTE = session-specific (assigned on connect)
```

---

## 2.6 Kỹ thuật quan trọng

### CopyOnWriteArrayList cho Player List
```
Tại sao dùng:
- Player join/leave area: infrequent (vài lần/phút)
- Iterate player list for broadcast: very frequent (hàng trăm lần/giây)
- CopyOnWriteArrayList: read = O(1) no lock, write = copy entire array

Tradeoff:
- Write (join/leave): expensive (copy array)
- Read (broadcast): zero lock, zero contention
- Perfect cho use case: nhiều read, ít write
```

### Volatile Fields cho Position/HP
```
Tại sao volatile thay vì synchronized:
- Position update: 1 writer (owning player thread), N readers (broadcast)
- HP update: 1 writer (attack synchronized block), N readers (UI display)
- volatile đảm bảo visibility across threads
- Không cần atomicity (single writer pattern)
- Cost: ~0 (CPU memory barrier instruction)
```

### Damage Formula
```
Base damage = ATK - DEF * 0.5
Critical hit: damage *= 2.0 × (1 + critBonus/100)
Element bonus: +20% if attacker element > target element
Level penalty: damage *= 0.8 if attacker 5+ levels below target
Final: clamp(damage, 1, mob.maxHP)
```

### Mob Respawn System
```
1. Mob killed → isDie = true, respawnTime = now + respawnDelay
2. Place.update() (every 1s) checks: if isDie && now >= respawnTime
3. Respawn: isDie = false, HP = maxHP, position = original spawn
4. Broadcast to area: "Mob X appeared"
5. respawnDelay: 30-120s depending on mob type/rarity
```

---

---

# Phần 3: So sánh tổng quan

## 3.1 Architecture Comparison

| Aspect | Turn-Based Artillery | Realtime MMORPG |
|--------|---------------------|-----------------|
| **Language** | Go | Java |
| **Protocol** | WebSocket (JSON) | TCP Socket (Binary) |
| **Threading** | Goroutines (lightweight) | OS Threads (heavy) |
| **Concurrency** | Channel-based (Actor model) | Lock-based (Synchronized) |
| **State ownership** | Single goroutine per match | Coarse lock per area |
| **Tick rate** | 1s (timer only) | 1s (full state update) |
| **Players per room** | 2-4 | 20+ |
| **Entities per room** | 4-6 (players + bots) | 50+ (players + mobs) |
| **Match duration** | 3-10 minutes | Persistent (hours) |
| **State persistence** | Rating only | Full character |
| **Bandwidth** | ~1KB/s per player | ~5KB/s per player |
| **Memory per player** | ~50KB | ~1.5MB (incl. thread stacks) |

## 3.2 Concurrency Model Comparison

```
TURN-BASED (Go - Actor Model):
════════════════════════════════════════════════════════════════
  Every component is a goroutine with its own event channel.
  ZERO shared state. ZERO locks. Communication via channels.

  [Player goroutine] ──channel──► [Room goroutine] ──channel──► [Match goroutine]

  Pros: No deadlock possible, no race conditions, simple reasoning
  Cons: Everything must be serialized through channels
  Best for: Small rooms (2-4 players), complex per-player logic


REALTIME MMORPG (Java - Lock Model):
════════════════════════════════════════════════════════════════
  Shared state (Place) accessed by multiple threads.
  Coarse synchronized blocks protect critical sections.

  [Player thread 1] ──┐
  [Player thread 2] ──┼── synchronized(Place) ──► [Shared State]
  [Player thread 3] ──┘
  [Map thread] ────────── synchronized(Place) ──► [Shared State]

  Pros: Immediate response (no channel overhead), familiar model
  Cons: Potential deadlock if lock ordering wrong, contention
  Best for: Large rooms (20+ players), high-frequency small updates
```

## 3.3 When to Use Which

| Scenario | Recommended Model | Reason |
|----------|-------------------|--------|
| Turn-based, 2-4 players | Actor (Go channels) | Simple, zero race |
| Realtime, 20+ players | Lock-based (synchronized) | Low latency, direct access |
| Realtime, 2-4 players | Either works | Actor simpler, lock faster |
| MMO open world | Lock-based + zone sharding | Need immediate response |
| Battle royale (100 players) | ECS + tick-based | Batch processing efficiency |
| Card game, board game | Actor model | Perfect fit for turns |

## 3.4 Scaling Strategies

### Turn-Based (Horizontal Scaling)
```
┌─────────┐   ┌─────────┐   ┌─────────┐
│ Node 1  │   │ Node 2  │   │ Node 3  │
│ 50 rooms│   │ 50 rooms│   │ 50 rooms│
└────┬────┘   └────┬────┘   └────┬────┘
     │              │              │
     └──────────────┼──────────────┘
                    │
              ┌─────┴─────┐
              │   Redis    │  (matchmaking queue,
              │            │   room registry,
              │            │   session routing)
              └─────┬─────┘
                    │
              ┌─────┴─────┐
              │ PostgreSQL │  (ratings, rewards)
              └───────────┘

Scaling: Add more nodes. Redis routes players to correct node.
Each room is independent — perfect horizontal scaling.
Bottleneck: Matchmaking (single leader via Redis lock).
```

### Realtime MMORPG (Vertical + Zone Sharding)
```
┌─────────────────────────────────────────────┐
│              SINGLE SERVER                    │
│                                             │
│  Map 1-50    Map 51-100    Map 101-160      │
│  (Thread×50) (Thread×50)  (Thread×60)       │
│                                             │
│  10,000 player threads                      │
│  160 map threads                            │
│  Total: ~20,000 threads                     │
│  RAM: ~16-32GB                              │
└─────────────────────────────────────────────┘

Scaling: Bigger server (vertical) OR channel-based sharding
  - Channel 1: Map 1-50 (server A)
  - Channel 2: Map 51-100 (server B)
  - Cross-channel: trade/chat via message bus

Bottleneck: Single area with 20 players all fighting.
Solution: Area capacity cap → overflow to next area instance.
```

## 3.5 Failure Handling

| Failure | Turn-Based | Realtime MMORPG |
|---------|-----------|-----------------|
| Player disconnect | Grace period → kill character in match | Save to DB → cleanup from area |
| Server crash | Match lost (short-lived) | Character safe in DB (last save) |
| Slow client | Drop msgs → kick after 10 drops | BlockingQueue backpressure |
| Bug in logic | Panic recovery → no-contest | Exception caught, session continues |
| DB unavailable | Match continues (in-memory) | Login blocked, game continues |

## 3.6 Key Takeaways

### Turn-Based Game Design Principles
1. **Server authoritative** - client chỉ render, server quyết định tất cả
2. **Actor model** - mỗi entity 1 goroutine, communicate via channels
3. **Short-lived matches** - không cần persistence phức tạp
4. **Reconnect = full state sync** - gửi lại toàn bộ MatchState
5. **Animation timing** - server delay endTurn để client kịp hiển thị

### Realtime MMORPG Design Principles
1. **Tách movement khỏi combat** - movement không lock, combat coarse lock
2. **Timer-based mob AI** - đơn giản nhưng hiệu quả, không cần pathfinding
3. **CopyOnWrite cho frequent iteration** - player list đọc nhiều ghi ít
4. **Volatile cho shared counters** - HP/position single writer, many readers
5. **1-second tick cho state reconciliation** - đảm bảo consistency định kỳ
6. **Binary protocol** - tiết kiệm bandwidth cho mobile
7. **Thread-per-connection** - đơn giản, JVM handle được

### Universal Principles (cả 2 loại)
1. **Đơn giản hơn tốt hơn** - coarse lock > fine-grained lock
2. **Broadcast chi phí O(N)** - giới hạn N (room size) để control
3. **Timeout detection** - luôn có watchdog/daemon phát hiện zombie
4. **Graceful degradation** - drop message tốt hơn block everything
5. **Rate limiting** - protect server khỏi spam client
6. **Kick slow clients** - 1 client chậm không được ảnh hưởng tất cả
