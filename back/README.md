# MAL API Server

Go backend for syncing completed anime from MyAnimeList into PostgreSQL and exposing the result over a small REST API.

The service:
- stores grouped anime data in PostgreSQL
- refreshes data from MAL on demand
- caches anime details locally to reduce repeated MAL requests
- exposes read-only API endpoints for list and stats
- writes structured logs via `log/slog`

## What It Does

When a sync request is triggered, the server:

1. Resolves the current app `user_id` from the request.
2. Loads that user's OAuth token from PostgreSQL.
3. Rejects the request with `401` if the token is missing or expired.
4. Requests the authenticated user's completed anime list from MyAnimeList.
5. Fetches extra details for each title, including `media_type` and related anime.
6. Groups related titles into one logical record.
7. Splits grouped results into `series` and `movie`.
8. Saves the result into PostgreSQL under the current `user_id`.

The sync runs in the background. The HTTP request returns immediately after the job is scheduled.

## Requirements

- Go `1.26+`
- PostgreSQL
- A prepared schema from [`schema.sql`](./schema.sql)
- A MyAnimeList application with at least a client ID

## Auth Behavior

The backend generates and stores MAL tokens through a local OAuth flow using `go run . auth`.

Current flow:
- opens the MAL authorization URL
- waits for the local callback
- exchanges the code for an access token
- fetches the current MAL username from `/v2/users/@me`
- upserts a row in `users`
- upserts a row in `mal_tokens`

The normal HTTP server does not refresh tokens automatically.
If the stored token is expired, run `go run . auth` again to replace it.

Operational contract:
- `go run . auth` is the only supported writer for `users` and `mal_tokens`
- `go run .` only reads `users` and `mal_tokens`
- token refresh is a manual operator action, not a runtime background behavior

## Configuration

The server reads the following environment variables:

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `DATABASE_URL` | Yes | empty | PostgreSQL connection string used by the backend. Required for both `go run .` and `go run . auth`. |
| `MAL_CLIENT_ID` | Required for `go run . auth` | empty | MAL OAuth client ID used by the local OAuth flow. |
| `MAL_CLIENT_SECRET` | Optional | empty | MAL OAuth client secret. Some flows can work without it depending on app setup. |
| `MAL_REDIRECT_URI` | Required for `go run . auth` | empty | OAuth callback URL configured in your MAL app, e.g. `http://localhost:8085/callback`. |
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
- `cred.env`: MAL credentials and redirect URI
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
- grouped anime data is stored in PostgreSQL, not in local files

## Quick Start

From the `back/` directory:

`cred.env`

```bash
export MAL_CLIENT_ID="your_client_id"
export MAL_CLIENT_SECRET="your_client_secret"
export MAL_REDIRECT_URI="http://localhost:8085/callback"
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

Create or refresh a MAL token and user row:

```bash
go run . auth
```

The command starts a temporary callback listener, prints the MAL authorization URL, waits for the browser callback, then stores the user and token in PostgreSQL.

If the stored token later expires, run the same command again.

Start the server:

```bash
go run .
```

The server starts on `http://localhost:8080`.

Before calling `/api/sync/{user_id}`, ensure:
- the schema has been applied
- `go run . auth` has completed successfully
- the user has a row in `users`
- the user has a row in `mal_tokens`
- the stored token is still valid

## Development Commands

Run the server:

```bash
go run .
```

Run auth:

```bash
go run . auth
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
gofmt -w *.go
```

## API

Base path: `/api`

Canonical user-scoped routes:
- `GET /api/anime/{user_id}`
- `POST /api/sync/{user_id}`
- `GET /api/stats/{user_id}`

The backend expects an integer `user_id`.
This is the internal `users.id` from PostgreSQL, not the MAL username.

The code still accepts `X-User-ID` and `?user_id=` as a backward-compatible fallback, but path-based routes are now the primary format.

### `GET /api/anime/{user_id}`

Returns all grouped anime for the current user from PostgreSQL.

If you have not run a successful sync yet, this endpoint may return an empty array.

Example:

```bash
curl http://localhost:8080/api/anime/1
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
    "type": "series"
  },
  {
    "id": 199,
    "display_title": "Sen to Chihiro no Kamikakushi",
    "merged_titles": 1,
    "avg_score": 10,
    "watched_episodes_sum": 1,
    "synced_at": "2026-04-11T08:20:17Z",
    "type": "movie"
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

### `POST /api/sync/{user_id}`

Starts background synchronization with MyAnimeList for the provided app user.

Example:

```bash
curl -X POST http://localhost:8080/api/sync/1
```

Success response:

```json
{
  "success": true,
  "message": "Sync started in background"
}
```

Behavior:
- returns immediately
- actual sync continues in a goroutine
- does not stream progress over HTTP
- syncs are isolated by `user_id`
- the same `user_id` cannot run overlapping sync jobs inside one backend process

Possible error responses:
- `400 Bad Request`
  - `user_id` is missing
  - `user_id` is not a positive integer
- `401 Unauthorized`
  - no token row exists for that user
  - the stored token is expired
- `500 Internal Server Error`
  - token could not be loaded for another reason
  - database access failed

### `GET /api/stats/{user_id}`

Returns counts of grouped series and movies currently stored for the current user.

Example:

```bash
curl http://localhost:8080/api/stats/1
```

Response example:

```json
{
  "series_count": 152,
  "movies_count": 87,
  "total_count": 239
}
```

## Smoke Test

Start the server:

```bash
go run .
```

In another terminal:

```bash
curl http://localhost:8080/api/anime/1
curl http://localhost:8080/api/stats/1
curl -X POST http://localhost:8080/api/sync/1
```

If you do not know the user id after `go run . auth`, look it up:

```sql
SELECT id, username FROM users;
```

## Database

The backend expects an already prepared PostgreSQL schema.
Schema creation is part of the deployment contract, not part of application startup.
On startup, the app only opens the database connection and runs `Ping`.
If the schema is missing or broken, the error will surface on the first real query.

Expected tables:
- `users`
- `mal_tokens`
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

### User-Scoped Anime Table

`anime_entries`

| Column | Type | Required | Key | Meaning |
| --- | --- | --- | --- | --- |
| `user_id` | `INTEGER` | Yes | Composite PK, FK | Owning application user |
| `anime_id` | `INTEGER` | Yes | Composite PK | Canonical MAL ID for the grouped record |
| `anime_type` | `TEXT` | Yes | Indexed | Group classification: `series` or `movie` |
| `group_key` | `TEXT` | Yes | Indexed | Deterministic key built from grouped MAL IDs |
| `display_title` | `TEXT` | Yes | - | Title shown in API responses |
| `merged_titles` | `INTEGER` | Yes | - | Number of merged titles inside the group |
| `avg_score` | `DOUBLE PRECISION` | Yes | - | Average MAL score for the group |
| `watched_episodes_sum` | `INTEGER` | Yes | - | Sum of watched episodes across grouped entries |
| `synced_at` | `TIMESTAMPTZ` | Yes | - | UTC time of the last successful write |

### RLS

`anime_entries` uses PostgreSQL row-level security.

The backend sets:

```sql
SET LOCAL app.user_id = '<current user id>';
```

inside the transaction before reading or writing anime rows.

This means:
- API requests only see rows for the current `user_id`
- snapshot rewrites only affect rows for the current `user_id`
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
2. Deduplicates completed-list entries by MAL ID before detail fetch.
3. Fetches anime details through two parallel primary workers.
4. Sends transient primary failures into a separate retry queue processed by two parallel retry workers.
5. Caches resolved detail responses locally for 7 days.
6. Merges titles that are linked through `related_anime`.
7. Uses average score and total watched episodes across the merged set.
8. Treats isolated standalone movies as `movie`; other grouped items land in `series`.

If MAL is temporarily unstable and a stale cache entry exists, the backend may use cached details instead of failing immediately.

## Implementation Contracts

These rules are easy to forget, but they currently define the intended behavior of the backend. If one of them changes, the code, DB expectations, API expectations, and this README should be updated together.

- Only MAL entries with `status=completed` participate in sync. Other MAL list states are intentionally ignored.
- Group membership is built only from MAL IDs and `related_anime` links. Titles are never merged by text similarity.
- Anime details are fetched once per unique MAL ID during a sync run, even if the completed list contains multiple entries that collapse to the same ID.
- The current sync pipeline uses bounded concurrency: `2` primary detail workers and `2` retry workers. Retry runs in parallel with primary processing instead of waiting for the full primary pass to finish.
- Every persisted group must contain at least one positive MAL ID. The persisted `anime_id` is the smallest member ID, and `group_key` is the sorted member IDs joined with `:`.
- `display_title` is taken from the first completed-list entry encountered for the group. It is not normalized from MAL details and should be treated as a stable snapshot choice.
- `avg_score` is the arithmetic mean of merged MAL scores rounded to one decimal place. `watched_episodes_sum` is a plain sum across grouped entries.
- `anime_type='movie'` is reserved only for isolated one-item movie groups whose related MAL IDs are absent from the completed list. Any linked movie or mixed movie/non-movie group is persisted with `anime_type='series'`.
- A sync is all-or-nothing at the database level. If anime details still cannot be resolved after retry, the current user's DB snapshot is left unchanged.
- Background sync requests are not serialized globally. Different users can sync independently.
- The backend prevents overlapping sync runs for the same `user_id` inside one process.
- The HTTP server never refreshes or rewrites user tokens at runtime. If a token expires, the operator must run `go run . auth` again.
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

### `401 Unauthorized` on `/api/sync/{user_id}`

Most likely causes:
- no matching row exists in `mal_tokens` for the provided `user_id`
- `access_token` is expired

Fix:
- run `go run . auth` again for that user

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
- `main.go`: application bootstrap and auth command handling
- `api.go`: HTTP routes and handlers
- `auth.go`: MAL OAuth helpers and user/token persistence used by `go run . auth`
- `sync.go`: sync orchestration and grouping logic
- `mal_client.go`: MAL API client and retry behavior
- `cache.go`: local anime details cache
- `db.go`: PostgreSQL connection setup plus RLS-scoped reads and writes
- `logger.go`: structured logging setup
- `schema.sql`: PostgreSQL schema and RLS policies
