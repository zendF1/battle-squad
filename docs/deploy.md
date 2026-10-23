# Fresh production deploy

One migration path for every environment: production runs exactly what dev and CI run.
`cmd/migrate` alone turns an empty Postgres into the complete, current game: the schema, all the
reference game data (characters, weapons, skills, items, maps, bosses, modes, elements, shop,
missions, rules, ...) and the first admin dashboard login. No manual SQL, no admin seed button.

## What cmd/migrate does

The binary embeds `migrations/*.up.sql` and `configs/*.yaml` (`migrations.FS`, `configs.FS`), so it
runs on its own (no repo checkout next to it). Every run, on one connection holding a
Postgres advisory lock (two runs never interleave):

1. **Schema migrations**: every `migrations/*.up.sql` not yet in `schema_migrations`
   (filename, checksum, applied_at), in filename order, each in its own transaction with its
   tracking row. A database migrated before the tracking table existed reruns every file once
   (they are idempotent) and records them all. An applied file whose content changed is a hard
   error; `-allow-changed` records the new checksum without rerunning it.
2. **Reference-data seed** (`internal/shared/migrate/seed.go`): `configs/*.yaml` and the Go
   defaults (game_settings, rules, equipment, titles, milestones), missing rows only
   (`ON CONFLICT DO NOTHING`): admin edits are never overwritten. One transaction; when nothing
   is missing it rolls back, so `gamedata_revision` does not move.
3. **Dev fixtures**: none. Migrations and seed hold reference data only, the same in every
   environment; there are no test accounts or gift codes to load.
4. **Admin bootstrap**: while `admin_users` is empty and both `ADMIN_BOOTSTRAP_EMAIL` and
   `ADMIN_BOOTSTRAP_PASSWORD` are set, it creates one admin (bcrypt, password >= 12 chars). It
   never changes an existing admin. There is no default password. Production with no admin and
   the variables unset: a warning, and nobody can sign in to cmd/admin.

A second run is a no-op. `migrate -status` lists every file, `migrate -dry-run` lists what a run
would do (pending files, missing seed rows, missing admin) without changing anything.

## Environment variables

| Variable | migrate | api | game | admin | worker | Notes |
|---|---|---|---|---|---|---|
| `APP_ENV` | x | x | x | x | x | `production` (or `prod`) in production |
| `POSTGRES_DSN` | x | x | x | x | x | `postgres://user:pass@host:5432/db?sslmode=require` |
| `DB_MAX_CONNS` / `DB_MIN_CONNS` | x | x | x | x | x | pool size, default 50 / 10 |
| `REDIS_ADDR` / `REDIS_PASSWORD` / `REDIS_DB` |  | x | x | x | x | one Redis shared by every server |
| `REDIS_POOL_SIZE` / `REDIS_MIN_IDLE` |  | x | x | x | x | default 100 / 20 |
| `JWT_SECRET` |  | x | x |  |  | the same value on api and game; >= 32 chars; api and game refuse the dev default in production |
| `API_PORT` |  | x |  |  |  | default 8080 |
| `GAME_PORT` |  |  | x |  |  | default 8081 |
| `NODE_ID` |  |  | x | x |  | unique per game node (`game-1`, `game-2`); admin defaults to `node-admin-1` |
| `ADMIN_PORT` |  |  |  | x |  | default 9000 |
| `ADMIN_BOOTSTRAP_EMAIL` / `ADMIN_BOOTSTRAP_PASSWORD` | x |  |  |  |  | first admin only; remove them after the first run |
| `APP_VERSION` / `PROTOCOL_VERSION` |  | x | x |  |  | client version policy |
| `IAP_MOCK_ENABLED` / `DEV_UNLOCK_ALL` |  | x | x |  |  | test only, ignored in production |

`.env.example` lists them all. The servers also read a `.env` in their working directory.

**Redis**: Redis 7, one instance (or DB number) shared by api, game, admin and worker: the
matchmaking queues, rooms, sessions of the admin dashboard, the game data reload channel
(`gamedata:changed`). Nothing has to be created in it. Persistence (AOF) is recommended so a
restart keeps queued players and admin sessions, but every server tolerates an empty Redis.

**Admin dashboard**: every page needs a sign-in in production (and elsewhere as soon as one
admin exists). Serve it over HTTPS (behind a proxy setting `X-Forwarded-Proto: https`) so the
session cookie is `Secure`, and keep it off the public internet if you can.

## Steps

```bash
# 0. Secrets
export APP_ENV=production
export POSTGRES_DSN='postgres://battlesquad:<password>@db:5432/battlesquad?sslmode=require'
export REDIS_ADDR=redis:6379 REDIS_PASSWORD=<redis password>
export JWT_SECRET=$(openssl rand -hex 32)

# 1. Migrate (empty DB -> complete game data + first admin)
ADMIN_BOOTSTRAP_EMAIL=ops@yourdomain.com ADMIN_BOOTSTRAP_PASSWORD='<12+ chars>' ./migrate
./migrate -status            # 0 pending

# 2. Servers, in this order
./api                        # :8080, GET /healthz, GET /gamedata
./game                       # :8081 /ws (one per node, unique NODE_ID; nginx in deploy/nginx.conf)
./admin                      # :9000, sign in with the bootstrapped admin
./worker
```

Order: **migrate -> api -> game -> admin -> worker**. Run `migrate` before every deploy of the
servers (it is a no-op when nothing changed); the servers never write the schema.

With Docker: `Dockerfile.migrate` (the migrate binary alone), `Dockerfile.api`, `.game`,
`.admin`, `.worker`. `docker-compose.yml` runs `migrate` once after Postgres is healthy and
starts api / game / admin / worker only when it exited 0:

```bash
APP_ENV=production JWT_SECRET=$(openssl rand -hex 32) \
ADMIN_BOOTSTRAP_EMAIL=ops@yourdomain.com ADMIN_BOOTSTRAP_PASSWORD='<12+ chars>' \
docker compose up -d --build
docker compose logs migrate   # "database is up to date ... admin=created"
```

(The compose file's Postgres credentials are the local ones: change them for a real deploy.)

### Checks after the deploy

```bash
curl -s https://api.example.com/healthz
curl -s https://api.example.com/gamedata | jq '.data.characters | length'   # 5
curl -s -X POST https://api.example.com/auth/guest-login -H 'Content-Type: application/json' \
  -d '{"deviceInstallId": "deploy-check-1"}'                               # accessToken
```

## Adding a migration

1. Add `migrations/NNN_name.up.sql` (next number) and `NNN_name.down.sql`. It is embedded and
   picked up by filename order; nothing to register.
2. Keep it idempotent (`IF NOT EXISTS`, `ON CONFLICT DO NOTHING`, updates guarded by the old
   value) and never `DROP TABLE` in an up file: a DB migrated before the tracking table reruns
   every file once.
3. Never edit an applied file: add a new one (cmd/migrate refuses a changed checksum).
4. New reference rows also go to `configs/*.yaml` / the seed's Go defaults when those tables are
   seeded from there, with the CURRENT values, so a fresh DB and an upgraded one agree
   (`internal/shared/migrate` and `internal/admin` integration tests compare them).
5. `TEST_POSTGRES_DSN=... go test ./...` with a throwaway Postgres.
