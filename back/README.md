# MAL API Server

Go backend for syncing completed anime from MyAnimeList into PostgreSQL and exposing the result over a small REST API.

The service:
- stores a global anime catalog, relations, and user snapshots in PostgreSQL
- refreshes data from MAL on demand
- caches anime details locally to reduce repeated MAL requests
- exposes read-only API endpoints for grouped list, nested franchise data, and stats
- writes structured logs via `log/slog`

## What It Does

When a sync request is triggered, the server:

1. Resolves the target user either from the signed session cookie or from a public MAL username.
2. For signed-in sync, loads that user's OAuth token from PostgreSQL.
3. For public sync, uses `MAL_CLIENT_ID` and the public MAL username.
4. Requests the target user's completed anime list from MyAnimeList.
5. Clears the target user's stored snapshot if the completed list is empty.
6. Reuses or hydrates global catalog rows and relation edges for the completed titles and their related franchise nodes.
7. Builds grouped `series` / `movie` records for the target user from the local relation graph.
8. Exposes each grouped record together with a nested `franchise` array assembled from the stored catalog and relation data.

The sync runs in the background. The HTTP request returns immediately with a `job_id`, and progress is available through a status endpoint or Server-Sent Events.

## Requirements

- Go `1.26+`
- PostgreSQL
- A prepared schema from [`schema.sql`](./schema.sql)
- A MyAnimeList application with at least a client ID

## Auth Behavior

The backend generates and stores MAL tokens through the HTTP OAuth flow used by the frontend.

Current flow:
- `GET /api/auth/mal/start` creates a short-lived OAuth state cookie and redirects to MAL
- MAL redirects back to `GET /api/auth/mal/callback`
- the backend validates the state cookie and exchanges the code for an access token
- fetches the current MAL username from `/v2/users/@me`
- upserts a row in `users`
- upserts a row in `mal_tokens`
- creates a signed `HttpOnly` app session cookie
- redirects back to the frontend

The normal HTTP server does not refresh tokens automatically.
If the stored token is expired, sign in with MAL again from the frontend.

Operational contract:
- `go run .` is the only application entrypoint
- browser OAuth is the supported writer for `users` and `mal_tokens`
- token refresh is a manual operator action, not a runtime background behavior

## Configuration

The server reads the following environment variables:

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `DATABASE_URL` | Yes | empty | PostgreSQL connection string used by the backend. |
| `MAL_CLIENT_ID` | Yes, for auth | empty | MAL OAuth client ID used by the browser OAuth flow. |
| `MAL_CLIENT_SECRET` | Optional | empty | MAL OAuth client secret. Some flows can work without it depending on app setup. |
| `MAL_REDIRECT_URI` | Yes, for auth | empty | OAuth callback URL configured in your MAL app, e.g. `http://localhost:8080/api/auth/mal/callback`. |
| `MAL_FRONTEND_URL` | Recommended | `/` | Where the backend redirects after successful MAL auth, e.g. `http://localhost:5173/`. |
| `MAL_SESSION_SECRET` | Recommended | dev fallback | Secret used to sign session and OAuth state cookies. Set this outside local throwaway runs. |
| `PORT` | No | `8080` | HTTP server port. |
| `MAL_DATA_DIR` | No | empty | Optional base directory for runtime files such as the anime details cache. When empty, relative file names are resolved from the current working directory. |
| `CORS_ALLOWED_ORIGINS` | No | empty | Comma-separated list of browser origins allowed to call the API. When empty, CORS middleware is disabled entirely. |
| `LOG_LEVEL` | No | `info` | Log level: `debug`, `info`, `warn`, `error`. |
| `LOG_FORMAT` | No | `text` | Log format: `text` or `json`. |

## Env Files

The backend loads environment files from the current working directory in this order:

1. `cred.env`
2. `paths.env`

Rules:
- `paths.env` can override values from `cred.env`
- real process environment variables still win over both files
- missing files are ignored
- files are parsed via `godotenv`, so both `KEY=value` and `export KEY=value` formats are supported
- values are read literally; shell expressions such as `$(pwd)` are not expanded
- env files are parsed into startup config; the backend does not rewrite process environment variables at runtime
- runtime path resolution depends on the current working directory you use to launch the app

Suggested split:
- `cred.env`: MAL credentials, redirect URI, frontend URL, and session secret
- `paths.env`: local runtime overrides such as `DATABASE_URL`, `PORT`, `MAL_DATA_DIR`, and `CORS_ALLOWED_ORIGINS`

## Runtime Files

By default, the app stores runtime files relative to the current working directory.
If `MAL_DATA_DIR` is set, the anime details cache uses that directory as its base path.

| File | Purpose |
| --- | --- |
| `.mal_anime_details_cache.json` | Cache of MAL anime details used during sync |

Notes:
- `.mal_token.json` is no longer used by the backend
- tokens are stored in PostgreSQL in `mal_tokens`
- anime catalog, relations, raw user items, and grouped snapshots are stored in PostgreSQL

## Quick Start

From the `back/` directory:

`cred.env`

```bash
export MAL_CLIENT_ID="your_client_id"
export MAL_CLIENT_SECRET="your_client_secret"
export MAL_REDIRECT_URI="http://localhost:8080/api/auth/mal/callback"
export MAL_FRONTEND_URL="http://localhost:5173/"
export MAL_SESSION_SECRET="replace_with_a_long_random_string"
```

`paths.env`

```bash
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/mal?sslmode=disable"
export PORT="8080"
export MAL_DATA_DIR="/absolute/path/to/back"
export CORS_ALLOWED_ORIGINS="http://localhost:5173"
export LOG_LEVEL="info"
export LOG_FORMAT="text"
```

Apply the schema:

```bash
psql "$DATABASE_URL" -f schema.sql
```

Start the server:

```bash
go run .
```

The server starts on `http://localhost:8080`.

Before calling `/api/sync`, ensure:
- the schema has been applied
- the user has signed in through the frontend
- the browser has a valid app session cookie
- the user has a row in `users` and `mal_tokens`
- the stored token is still valid

Before calling `/api/public/sync`, ensure:
- the schema has been applied
- `MAL_CLIENT_ID` is configured
- the target MAL list is public

## Development Commands

Run the server:

```bash
go run .
```

Run tests:

```bash
go test ./...
```

Run race tests:

```bash
go test -race ./...
```

Format code:

```bash
gofmt -w ./*.go
```

## API

Base path: `/api`

Canonical routes:
- `GET /api/me`
- `GET /api/anime`
- `POST /api/sync`
- `GET /api/sync/jobs/{job_id}`
- `GET /api/sync/jobs/{job_id}/events`
- `GET /api/stats`
- `GET /api/auth/mal/start`
- `GET /api/auth/mal/callback`
- `POST /api/auth/logout`
- `POST /api/public/sync`
- `GET /api/public/anime/{username}`
- `GET /api/public/stats/{username}`

The private anime, stats, and sync routes expect a valid signed session cookie.
The frontend obtains that cookie by sending the browser through `GET /api/auth/mal/start`.

There are no public `user_id`, `X-User-ID`, or `?user_id=` API fallbacks.

Public routes do not require a user session. Public sync reads an open MAL list by username using the configured `MAL_CLIENT_ID` and stores the result under a local `users` row for that username. It cannot read private MAL lists and it never updates a user's MAL account.

### `GET /api/me`

Returns the current signed-in MAL user.

### `GET /api/anime`

Returns all grouped anime for the current user from PostgreSQL.
Each item includes both the grouped summary fields and a nested `franchise` array built from `group_member_ids` plus the stored `anime_relations` graph.

If you have not run a successful sync yet, this endpoint may return an empty array.

Example:

```bash
curl --cookie "mal_session=..." http://localhost:8080/api/anime
```

Response example:

```json
[
  {
    "id": 5114,
    "display_title": "Fullmetal Alchemist: Brotherhood",
    "merged_titles": 2,
    "avg_score": 9.5,
    "watched_episodes_sum": 64,
    "synced_at": "2026-04-11T08:20:17Z",
    "type": "series",
    "franchise": [
      {
        "id": 5114,
        "title": "Fullmetal Alchemist: Brotherhood",
        "media_type": "tv",
        "start_date": "2009-04-05",
        "image_medium_url": "https://cdn.example/fmab-medium.jpg",
        "image_large_url": "https://cdn.example/fmab-large.jpg",
        "in_user_list": true,
        "user_score": 10,
        "watched_episodes": 64
      },
      {
        "id": 121,
        "title": "Fullmetal Alchemist",
        "media_type": "tv",
        "start_date": "2003-10-04",
        "image_medium_url": "https://cdn.example/fma-medium.jpg",
        "image_large_url": "https://cdn.example/fma-large.jpg",
        "relation_type": "alternative_version",
        "relation_type_formatted": "Alternative version",
        "in_user_list": false
      }
    ]
  }
]
```

Field meanings:
- `id`: canonical MAL ID for the grouped record
- `display_title`: title chosen as the display name for the group
- `merged_titles`: number of distinct titles merged into the group
- `avg_score`: average MAL list score within the group
- `watched_episodes_sum`: total watched episodes across grouped entries
- `synced_at`: UTC time when the group was last written to DB
- `type`: `series` or `movie`
- `franchise`: full franchise snapshot for the group

`franchise` item meanings:
- `id`: MAL anime id
- `title`: title stored in `anime_catalog`
- `media_type`: MAL media type such as `tv` or `movie`
- `start_date`: start date stored in `anime_catalog`, when known
- `image_medium_url` / `image_large_url`: poster URLs stored in `anime_catalog`
- `relation_type` / `relation_type_formatted`: relation label from `anime_relations` when the item was pulled in via a stored edge
- `in_user_list`: whether this title exists in the current user's completed list snapshot
- `user_score`: current user's MAL score for this title when `in_user_list=true`
- `watched_episodes`: current user's watched episode count when `in_user_list=true`

### `POST /api/sync`

Starts background synchronization with MyAnimeList for the signed-in user.

Example:

```bash
curl --cookie "mal_session=..." -X POST http://localhost:8080/api/sync
```

Success response:

```json
{
  "success": true,
  "message": "Sync started in background",
  "job_id": "Bz5v..."
}
```

Behavior:
- returns immediately
- actual sync continues in a goroutine
- returns `job_id` for progress tracking
- syncs are isolated by the resolved internal `user_id`
- the same resolved user cannot run overlapping sync jobs inside one backend process

Possible error responses:
- `401 Unauthorized`
  - session cookie is missing or invalid
  - no token row exists for that user
  - the stored token is expired
- `500 Internal Server Error`
  - token could not be loaded for another reason
  - database access failed

### `POST /api/public/sync`

Starts background synchronization for a public MAL list without browser OAuth.

Example:

```bash
curl -X POST http://localhost:8080/api/public/sync \
  -H "Content-Type: application/json" \
  -d '{"username":"some-mal-username"}'
```

Success response:

```json
{
  "success": true,
  "message": "Public sync started in background",
  "job_id": "Bz5v..."
}
```

Behavior:
- requires `MAL_CLIENT_ID`
- creates or reuses a local `users` row keyed by MAL username
- fetches `GET /users/{username}/animelist` from MAL with `X-MAL-CLIENT-ID`
- only works when the MAL list is public
- shares the same snapshot tables and grouping pipeline as authenticated sync
- returns `job_id` for progress tracking

Possible error responses:
- `400 Bad Request`
  - request body is not valid JSON
  - `username` is missing or empty
- `500 Internal Server Error`
  - `MAL_CLIENT_ID` is not configured
  - the local user row could not be saved

### `GET /api/sync/jobs/{job_id}`

Returns the latest in-memory sync job snapshot. Jobs are not persisted to the database.

Example response:

```json
{
  "id": "Bz5v...",
  "mode": "public",
  "username": "some-mal-username",
  "status": "running",
  "phase": "hydrating_catalog",
  "current": 42,
  "total": 180,
  "message": "Syncing anime details",
  "started_at": "2026-04-24T00:20:00Z"
}
```

### `GET /api/sync/jobs/{job_id}/events`

Server-Sent Events stream for sync progress. The server sends a JSON job snapshot when a meaningful phase changes, plus throttled updates during catalog hydration, and a final `completed` or `failed` snapshot.

### `GET /api/public/anime/{username}`

Returns the latest locally stored grouped anime snapshot for a MAL username.
This endpoint does not call MAL directly; run `POST /api/public/sync` first.

Example:

```bash
curl http://localhost:8080/api/public/anime/some-mal-username
```

Possible error responses:
- `404 Not Found`
  - no local public snapshot exists for that username yet

### `GET /api/stats`

Returns counts of grouped series and movies currently stored for the current user.

Example:

```bash
curl --cookie "mal_session=..." http://localhost:8080/api/stats
```

Response example:

```json
{
  "series_count": 152,
  "movies_count": 87,
  "total_count": 239
}
```

### `GET /api/public/stats/{username}`

Returns counts for the latest locally stored public snapshot for a MAL username.

Example:

```bash
curl http://localhost:8080/api/public/stats/some-mal-username
```

## Smoke Test

Start the server:

```bash
go run .
```

Open the frontend and sign in with MAL. For raw API smoke tests, reuse the browser session cookie:

```bash
curl --cookie "mal_session=..." http://localhost:8080/api/me
curl --cookie "mal_session=..." http://localhost:8080/api/anime
curl --cookie "mal_session=..." http://localhost:8080/api/stats
curl --cookie "mal_session=..." -X POST http://localhost:8080/api/sync
```

## Database

The backend expects an already prepared PostgreSQL schema.
Schema creation is part of the deployment contract, not part of application startup.
On startup, the app only opens the database connection and runs `Ping`.
If the schema is missing or broken, the error will surface on the first real query.

Expected tables:
- `users`
- `mal_tokens`
- `anime_catalog`
- `anime_relations`
- `user_anime_items`
- `anime_entries`

### Control Plane Tables

`users`

| Column | Type | Required | Key | Meaning |
| --- | --- | --- | --- | --- |
| `id` | `INTEGER` | Yes | Primary key | Internal application user id |
| `username` | `TEXT` | Yes | Unique key | Username fetched from MAL `/users/@me` |
| `created_at` | `TIMESTAMPTZ` | Yes | - | Row creation time |
| `updated_at` | `TIMESTAMPTZ` | Yes | - | Row update time |

`mal_tokens`

| Column | Type | Required | Key | Meaning |
| --- | --- | --- | --- | --- |
| `user_id` | `INTEGER` | Yes | Primary key, FK | Points to `users.id` |
| `access_token` | `TEXT` | Yes | - | Current MAL access token |
| `refresh_token` | `TEXT` | No | - | MAL refresh token when available |
| `token_type` | `TEXT` | Yes | - | Usually `Bearer` |
| `expires_at` | `TIMESTAMPTZ` | Yes | - | Token expiry timestamp |
| `created_at` | `TIMESTAMPTZ` | Yes | - | Row creation time |
| `updated_at` | `TIMESTAMPTZ` | Yes | - | Row update time |

### Global Anime Tables

`anime_catalog`

One row per MAL anime id, shared by all users.
Stores title, media type, start date, poster URLs, and freshness metadata for detail hydration.

`anime_relations`

Directed relation edges between entries in `anime_catalog`.
Stores `relation_type` and `relation_type_formatted` exactly so the API can surface them later in `franchise`.

### User-Scoped Anime Tables

`user_anime_items`

Raw completed-list snapshot for one user.
Stores `anime_id`, original MAL list title, score, watched episodes, and sync timestamp.

`anime_entries`

Grouped read model for one user.
Stores the summary shown in the list view plus `group_member_ids`, which seed franchise expansion on the read path.

| Column | Type | Required | Key | Meaning |
| --- | --- | --- | --- | --- |
| `user_id` | `INTEGER` | Yes | Composite PK, FK | Owning application user |
| `anime_id` | `INTEGER` | Yes | Composite PK | Canonical MAL ID for the grouped record |
| `anime_type` | `TEXT` | Yes | Indexed | Group classification: `series` or `movie` |
| `group_key` | `TEXT` | Yes | Indexed | Deterministic key built from grouped MAL IDs |
| `display_title` | `TEXT` | Yes | - | Title shown in API responses |
| `merged_titles` | `INTEGER` | Yes | - | Number of merged titles inside the group |
| `avg_score` | `DOUBLE PRECISION` | Yes | - | Average MAL score for the group |
| `group_member_ids` | `INTEGER[]` | Yes | - | Seed MAL IDs used to expand the franchise on read |
| `watched_episodes_sum` | `INTEGER` | Yes | - | Sum of watched episodes across grouped entries |
| `synced_at` | `TIMESTAMPTZ` | Yes | - | UTC time of the last successful write |

### RLS

`user_anime_items` and `anime_entries` use PostgreSQL row-level security.

The backend sets:

```sql
SET LOCAL app.user_id = '<current user id>';
```

inside the transaction before reading or writing anime rows.

This means:
- API requests only see rows for the current `user_id`
- raw snapshot rewrites only affect rows for the current `user_id`
- grouped snapshot rewrites only affect rows for the current `user_id`
- manual SQL queries in `psql` may look empty until you set `app.user_id`

To inspect data manually:

```sql
BEGIN;
SET LOCAL app.user_id = '1';

SELECT user_id, anime_id, anime_type, display_title
FROM anime_entries
ORDER BY anime_type, anime_id;

COMMIT;
```

### Schema File

The authoritative schema lives in [`schema.sql`](./schema.sql).

Apply it with:

```bash
psql "$DATABASE_URL" -f schema.sql
```

## Sync Details

The sync process currently operates like this:

1. Reads all completed anime from MAL using paginated requests.
2. If the completed list is empty, clears `user_anime_items` and `anime_entries` for that user and stops.
3. Deduplicates completed-list entries by MAL ID and upserts stub rows into `anime_catalog`.
4. Rewrites `user_anime_items` for the current user.
5. Traverses the franchise graph from each seed id with a hard cap of `40` nodes per traversal.
6. Processes independent seed franchises in parallel inside one sync run, while a shared resolver deduplicates in-flight MAL detail fetches for the same `anime_id`.
7. Reuses fresh `anime_catalog` rows when possible; otherwise fetches MAL details, updates `anime_catalog`, and rewrites outgoing rows in `anime_relations`.
8. Builds grouped user views from the local `anime_relations` graph and the current user's `user_anime_items`.
9. Persists grouped summaries to `anime_entries`, including `group_member_ids` for later read-path expansion.
10. On `GET /api/anime`, expands `group_member_ids` back through `anime_relations` and returns the nested `franchise` array.

The local JSON cache is still used as a best-effort detail cache to avoid repeated MAL requests.

## Implementation Contracts

These rules are easy to forget, but they currently define the intended behavior of the backend. If one of them changes, the code, DB expectations, API expectations, and this README should be updated together.

- Only MAL entries with `status=completed` participate in sync. Other MAL list states are intentionally ignored.
- Group membership is built only from MAL IDs and stored relation edges. Titles are never merged by text similarity.
- `anime_catalog` and `anime_relations` are global tables; `user_anime_items` and `anime_entries` are user-scoped tables.
- Anime details are fetched from MAL only for catalog rows that are new or stale. Fresh resolved catalog rows are reused.
- Franchise traversal is transitively expanded through `anime_relations`, with `maxNodesPerFranchise=40` as a hard safety cap on both sync-time hydration and read-time franchise expansion.
- Independent seed franchises may hydrate in parallel during one sync run, but MAL detail work for the same `anime_id` is deduplicated through a shared in-flight resolver.
- Lowering `maxNodesPerFranchise` bounds future traversals only. It does not purge previously persisted rows from the global `anime_catalog` / `anime_relations` graph.
- Every persisted group must contain at least one positive MAL ID. The persisted `anime_id` is the smallest member ID, and `group_key` is the sorted member IDs joined with `:`.
- `display_title` is taken from the first completed-list entry encountered for the group. It is not normalized from MAL details and should be treated as a stable snapshot choice.
- `avg_score` is the arithmetic mean of merged MAL scores rounded to one decimal place. `watched_episodes_sum` is a plain sum across grouped entries.
- `anime_type='movie'` is reserved only for one-item grouped views whose user-owned member is a movie. Other grouped views land in `series`.
- An empty completed list clears both `user_anime_items` and `anime_entries` for the current user.
- During a non-empty sync, `user_anime_items` is rewritten before grouped snapshot rebuild; `anime_entries` is rewritten after graph hydration and grouping succeed.
- `GET /api/anime` returns grouped summaries plus nested `franchise` data assembled on the read path from `group_member_ids`, `anime_catalog`, `anime_relations`, and `user_anime_items`.
- Background sync requests are not serialized globally. Different users can sync independently.
- The backend prevents overlapping sync runs for the same `user_id` inside one process.
- Sync job status is kept in memory only. Finished jobs are pruned after a short retention window and do not survive backend restarts.
- Server-Sent Events are the primary progress channel; `GET /api/sync/jobs/{job_id}` remains as a fallback and debugging endpoint.
- The HTTP server never refreshes tokens in the background. If a token expires, the user must sign in with MAL again.
- The details cache is best-effort infrastructure, not the source of truth. Cache load/save failures only log warnings; successful sync data still comes from MAL responses or usable cached details.
- Successful sync writes are snapshot rewrites for a single user, not incremental updates.
- Schema creation and migrations are outside application startup. The backend only checks that PostgreSQL is reachable during startup.
- Runtime path resolution depends on the current working directory because env files and default relative paths are resolved from there.

## Logging

The backend uses structured logging via the standard library `log/slog`.

Examples:

```bash
LOG_LEVEL=debug go run .
LOG_FORMAT=json LOG_LEVEL=info go run .
```

What to expect:
- `info`: startup, sync lifecycle, DB writes, auth completion
- `warn`: recoverable problems such as stale cache fallback or missing token for a user
- `error`: failed syncs and request handling failures
- `debug`: verbose MAL request and cache activity, including primary/retry worker logs during sync

Each log entry includes a `component` field so it is easier to filter logs by subsystem.

## CORS

The server reads allowed browser origins from `CORS_ALLOWED_ORIGINS`.

Set a custom list with a comma-separated value:

```bash
CORS_ALLOWED_ORIGINS="http://localhost:5173,https://app.example.com" go run .
```

If `CORS_ALLOWED_ORIGINS` is empty, the backend does not attach CORS headers at all.
This is the preferred production setup when your frontend is served through the same `nginx` host and all browser requests go through `/api`.

When CORS is enabled, the server allows credentials and the following methods:
- `GET`
- `POST`
- `PUT`
- `DELETE`
- `OPTIONS`

## Nginx Reverse Proxy

For production, the simplest setup is:

- serve the frontend from `nginx`
- proxy `/api/` to the Go backend on `127.0.0.1:8080`
- keep `CORS_ALLOWED_ORIGINS` empty, because browser traffic is same-origin

Example server block:

```nginx
server {
    listen 80;
    server_name app.example.com;

    root /var/www/mal-front/dist;
    index index.html;

    location /api/ {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location / {
        try_files $uri $uri/ /index.html;
    }
}
```

Notes:
- use `proxy_pass http://127.0.0.1:8080;` without a trailing slash so `/api/...` stays `/api/...`
- `try_files ... /index.html` is important for SPA routing
- if you later add HTTPS, keep the same locations and terminate TLS in `nginx`

Typical deploy steps:

1. Build the frontend into `/var/www/mal-front/dist`
2. Run the Go backend on `127.0.0.1:8080`
3. Put the config into `/etc/nginx/sites-available/mal`
4. Symlink it into `/etc/nginx/sites-enabled/`
5. Check config with `nginx -t`
6. Reload with `systemctl reload nginx`

## Troubleshooting

### `401 Unauthorized` on `/api/sync`

Most likely causes:
- browser session cookie is missing, expired, or invalid
- no matching row exists in `mal_tokens` for the resolved user
- `access_token` is expired

Fix:
- sign in with MAL again from the frontend

Useful checks:

```sql
SELECT id, username FROM users;
SELECT user_id, expires_at FROM mal_tokens;
```

### Sync finished, API sees rows, but `SELECT` in PostgreSQL looks empty

Most likely cause:
- RLS is active and your SQL session has no `app.user_id`

Use:

```sql
BEGIN;
SET LOCAL app.user_id = '1';

SELECT user_id, anime_id, anime_type, display_title
FROM anime_entries
ORDER BY anime_type, anime_id;

COMMIT;
```

### Sync starts but data looks stale

Possible causes:
- MAL details cache is still valid
- MAL returned transient errors and stale cache was used
- no completed anime changed since the last sync

Use `LOG_LEVEL=debug` to see detailed request and cache behavior.

### Runtime files are created in an unexpected directory

Check:
- `MAL_DATA_DIR`
- the working directory you used to launch the app

## Project Files

Main backend files:
- `main.go`: thin application entrypoint
- `api.go`: HTTP routes and handlers
- `auth.go`: MAL OAuth HTTP handlers plus user/token persistence
- `session.go`: signed session and OAuth state cookie helpers
- `sync_jobs.go`: in-memory sync job registry, progress snapshots, and SSE helpers
- `sync.go`: sync orchestration, shared catalog resolver, and grouping logic
- `mal_client.go`: MAL API client and retry behavior
- `cache.go`: local anime details cache
- `db.go`: PostgreSQL connection setup plus RLS-scoped reads and writes
- `logger.go`: structured logging setup
- `*_test.go`: backend test suite kept alongside the application code in `back/`
- `schema.sql`: PostgreSQL schema and RLS policies
