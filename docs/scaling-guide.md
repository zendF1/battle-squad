# Scaling Guide

## Architecture Overview

Battle Squad server gom 3 process doc lap:
- **API Server** (:8080) — REST API, stateless, scale tu do
- **Game Server** (:8081) — WebSocket, stateful (rooms/matches in memory), can sticky sessions
- **Worker** — Background jobs, chay 1 instance

## Single Node (Development)

```bash
docker-compose up -d postgres redis
go run cmd/api/main.go
go run cmd/game/main.go
go run cmd/worker/main.go
```

## Multi-Node (Production)

### Game Server Scaling

Game server su dung actor model — moi room/match la 1 goroutine. Room chi ton tai tren node tao no.

**Yeu cau:**
- Moi node can `NODE_ID` duy nhat (dung cho matchmaker leader election)
- WebSocket connections phai sticky — cung client luon ket noi cung node
- Nginx ip_hash dam bao sticky sessions

**Deploy:**
```bash
docker-compose up -d
```

Docker Compose mac dinh chay 2 game nodes (`game1`, `game2`) phia sau Nginx.

**Them node:** Copy service block trong `docker-compose.yml` (vd: `game3`) va them vao `deploy/nginx.conf` upstream:
```nginx
upstream game_servers {
    ip_hash;
    server game1:8081;
    server game2:8081;
    server game3:8081;  # new node
}
```

### Matchmaker Leader Election

Chi 1 game node chay matchmaker tai moi thoi diem. Su dung Redis lock (`matchmaking:leader`) voi TTL 10s. Node giu lock se chay matching tick. Neu node chet, lock tu expire va node khac lay lai.

Khong can cau hinh gi — tu dong hoat dong khi co nhieu nodes.

### Connection Pool Tuning

| Env Var | Default | Mo ta |
|---------|---------|-------|
| `DB_MAX_CONNS` | 50 | Max PostgreSQL connections per process |
| `DB_MIN_CONNS` | 10 | Min idle connections |
| `REDIS_POOL_SIZE` | 100 | Max Redis connections per process |
| `REDIS_MIN_IDLE` | 20 | Min idle Redis connections |

**Luu y:** Moi process co pool rieng. 3 game nodes x 50 DB conns = 150 connections tong. PostgreSQL mac dinh max 100 connections — can tang `max_connections` trong `postgresql.conf` hoac dung PgBouncer.

### Capacity Estimates

| Config | Rooms dong thoi |
|--------|-----------------|
| 1 node, 4GB RAM, DB pool 50 | ~300 rooms |
| 2 nodes, 8GB RAM, DB pool 50 | ~600 rooms |
| 4 nodes, 16GB RAM, DB pool 100 | ~1200 rooms |

Bottleneck chinh: PostgreSQL connections va RAM (terrain mask ~180KB per match sau bitset optimization).

### What Scales / What Doesn't

| Component | Scale? | Ghi chu |
|-----------|--------|---------|
| API Server | Horizontal | Stateless, them instances tuy y |
| Game Server | Horizontal | Sticky sessions required, moi node can NODE_ID |
| Worker | Single | 1 instance, chay cron jobs |
| PostgreSQL | Vertical | Connection pooling, hoac dung PgBouncer |
| Redis | Vertical | Single instance du cho hau het cases |
