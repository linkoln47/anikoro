# MAL API Server

Go backend for syncing completed anime from MyAnimeList into a local SQLite database and exposing the result over a small REST API.

The service:
- stores grouped anime data in SQLite
- refreshes data from MAL on demand
- caches anime details locally to reduce repeated MAL requests
- exposes read-only API endpoints for list and stats
- writes structured logs via `log/slog`

## What It Does

When `/api/sync` is triggered, the server:

1. Loads an OAuth token from disk.
2. Requests the authenticated user's completed anime list from MyAnimeList.
3. Fetches extra details for each title, including `media_type` and related anime.
4. Groups related titles into one logical record.
5. Splits grouped results into `series` and `movie`.
6. Saves the result into SQLite.

The sync runs in the background. The HTTP request returns immediately after the job is scheduled.

## Requirements

- Go `1.26+`
- A MyAnimeList application with at least a client ID
- A prepared SQLite database file with the tables described in [Database](#database)
- A valid token file at runtime if you want `/api/sync` to work

## Token Behavior

The backend can now generate `.mal_token.json` through a local OAuth flow using `go run . auth`.

If the access token is expired but the file contains a valid `refresh_token`, the server can refresh it automatically as long as `MAL_CLIENT_ID` is set, and `MAL_CLIENT_SECRET` is set when required by your MAL app configuration.

## Configuration

The server reads the following environment variables:

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `MAL_CLIENT_ID` | Recommended | empty | MAL OAuth client ID. Needed for token refresh and future OAuth flow usage. |
| `MAL_CLIENT_SECRET` | Optional | empty | MAL OAuth client secret. Some flows can work without it depending on app setup. |
| `MAL_REDIRECT_URI` | Required for `go run . auth` | empty | OAuth callback URL configured in your MAL app, e.g. `http://localhost:8085/callback`. |
| `PORT` | No | `8080` | HTTP server port. |
| `MAL_DATA_DIR` | No | empty | Optional base directory for runtime files such as DB, token, and cache. When empty, relative file names are resolved from the current working directory. |
| `MAL_DB_PATH` | No | `mal.db` or `<MAL_DATA_DIR>/mal.db` | Explicit path to the SQLite database file. Overrides default DB location only. |
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
- running the backend from `back/` is part of the project contract

Suggested split:
- `cred.env`: MAL credentials and redirect URI
- `paths.env`: local runtime overrides such as `PORT`, `MAL_DATA_DIR`, and `CORS_ALLOWED_ORIGINS`

## Runtime Files

By default, the app stores runtime files relative to the current working directory.
If `MAL_DATA_DIR` is set, the DB, token, and details cache use that directory as their base path.

| File | Purpose |
| --- | --- |
| `mal.db` | SQLite database with grouped anime data |
| `.mal_token.json` | Stored MAL access and refresh tokens |
| `.mal_anime_details_cache.json` | Cache of MAL anime details used during sync |

`MAL_DB_PATH` only changes the database file location. Token and details cache still follow `MAL_DATA_DIR`.

## Token File Format

The server expects `.mal_token.json` to be valid JSON matching the internal token structure. A typical file looks like this:

```json
{
  "access_token": "your-access-token",
  "refresh_token": "your-refresh-token",
  "token_type": "Bearer",
  "expires_in": 3600,
  "expires_at": "2026-04-11T12:00:00Z"
}
```

Notes:
- `access_token` must be present or the file is treated as invalid.
- `expires_at` should be an RFC3339 timestamp.
- if `refresh_token` is present, the backend can try to refresh the token automatically

## Quick Start

From the `back/` directory:

```bash
go run .
```

The server starts on `http://localhost:8080`.

Example files:

`cred.env`

```bash
export MAL_CLIENT_ID="your_client_id"
export MAL_CLIENT_SECRET="your_client_secret"
export MAL_REDIRECT_URI="http://localhost:8085/callback"
```

`paths.env`

```bash
export PORT="8080"
export MAL_DATA_DIR="/absolute/path/to/back"
export CORS_ALLOWED_ORIGINS="http://localhost:5173"
export LOG_LEVEL="info"
export LOG_FORMAT="text"
```

Create or refresh the token file:

```bash
go run . auth
```

The command starts a temporary callback listener, prints the MAL authorization URL, waits for the browser callback, and then writes `.mal_token.json` into `MAL_DATA_DIR` when set, otherwise into the current working directory.

Before running the server for the first time, create `mal.db` with the schema from the [Database](#database) section, or point `MAL_DB_PATH` at an existing prepared database.

Before calling `/api/sync`, ensure `.mal_token.json` exists in `MAL_DATA_DIR` when set, otherwise in the current working directory.

## Development Commands

Run the server:

```bash
go run .
```

Run tests:

```bash
go test ./...
```

Format code:

```bash
gofmt -w *.go
```

## API

Base path: `/api`

### `GET /api/anime`

Returns all grouped anime from both database tables.

If you have not run a successful sync yet, this endpoint may return an empty array.

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

### `POST /api/sync`

Starts background synchronization with MyAnimeList.

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

Possible error responses:
- `401 Unauthorized`
  - no token file
  - expired token without refresh support
  - refresh attempt failed
- `500 Internal Server Error`
  - token could not be loaded or validated for another reason

### `GET /api/stats`

Returns counts of grouped series and movies currently stored in the database.

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
curl http://localhost:8080/api/anime
curl http://localhost:8080/api/stats
curl -X POST http://localhost:8080/api/sync
```

## Database

The backend expects an already prepared SQLite database file.
Schema creation is part of the deployment contract, not part of application startup.
On startup, the app only checks that the expected tables and columns exist.

Expected tables:

- `series_table`
- `movie_table`

Expected keys and constraints:

- `id` is the primary key in each table
- `group_key` is a deterministic unique business key in each table
- all data columns are required (`NOT NULL`)

### `series_table`

| Column | Type | Required | Key | Meaning |
| --- | --- | --- | --- | --- |
| `id` | `INTEGER` | Yes | Primary key | Canonical MAL ID for the grouped record |
| `group_key` | `TEXT` | Yes | Unique key | Deterministic key built from grouped MAL IDs |
| `display_title` | `TEXT` | Yes | - | Title shown in API responses |
| `merged_titles` | `INTEGER` | Yes | - | Number of merged titles inside the group |
| `avg_score` | `REAL` | Yes | - | Average MAL score for the group |
| `watched_episodes_sum` | `INTEGER` | Yes | - | Sum of watched episodes across grouped entries |
| `synced_at` | `TEXT` | Yes | - | RFC3339 UTC timestamp of the last successful write |

### `movie_table`

| Column | Type | Required | Key | Meaning |
| --- | --- | --- | --- | --- |
| `id` | `INTEGER` | Yes | Primary key | Canonical MAL ID for the grouped record |
| `group_key` | `TEXT` | Yes | Unique key | Deterministic key built from grouped MAL IDs |
| `display_title` | `TEXT` | Yes | - | Title shown in API responses |
| `merged_titles` | `INTEGER` | Yes | - | Number of merged titles inside the group |
| `avg_score` | `REAL` | Yes | - | Average MAL score for the group |
| `watched_episodes_sum` | `INTEGER` | Yes | - | Sum of watched episodes across grouped entries |
| `synced_at` | `TEXT` | Yes | - | RFC3339 UTC timestamp of the last successful write |

### SQL Schema

Create `series_table`:

```sql
CREATE TABLE IF NOT EXISTS series_table (
    id INTEGER PRIMARY KEY,
    group_key TEXT NOT NULL,
    display_title TEXT NOT NULL,
    merged_titles INTEGER NOT NULL,
    avg_score REAL NOT NULL,
    watched_episodes_sum INTEGER NOT NULL,
    synced_at TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS series_table_group_key_idx
    ON series_table (group_key);
```

Create `movie_table`:

```sql
CREATE TABLE IF NOT EXISTS movie_table (
    id INTEGER PRIMARY KEY,
    group_key TEXT NOT NULL,
    display_title TEXT NOT NULL,
    merged_titles INTEGER NOT NULL,
    avg_score REAL NOT NULL,
    watched_episodes_sum INTEGER NOT NULL,
    synced_at TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS movie_table_group_key_idx
    ON movie_table (group_key);
```

If you want to initialize a new database from the SQLite CLI:

```bash
sqlite3 mal.db
```

Then run both SQL blocks above and exit with `.quit`.

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
- Every persisted group must contain at least one positive MAL ID. The persisted `id` is the smallest member ID, and `group_key` is the sorted member IDs joined with `:`.
- `display_title` is taken from the first completed-list entry encountered for the group. It is not normalized from MAL details and should be treated as a stable snapshot choice.
- `avg_score` is the arithmetic mean of merged MAL scores rounded to one decimal place. `watched_episodes_sum` is a plain sum across grouped entries.
- `movie_table` is reserved only for isolated one-item movie groups whose related MAL IDs are absent from the completed list. Any linked movie or mixed movie/non-movie group belongs in `series_table`.
- A sync is all-or-nothing at the database level. If anime details still cannot be resolved after retry, the sync fails and the DB snapshot is left unchanged.
- Background sync requests are not serialized by the backend. If multiple `/api/sync` calls overlap, the last successful DB transaction wins.
- The details cache is best-effort infrastructure, not the source of truth. Cache load/save failures only log warnings; successful sync data still comes from MAL responses or usable cached details.
- Successful sync writes are snapshot rewrites, not incremental updates: both DB tables are deleted and repopulated inside one transaction.
- Schema creation and migrations are outside application startup. The backend only verifies that the expected tables and columns are queryable, while deployment is responsible for preparing the DB contract.
- Runtime path resolution depends on the current working directory. Running the backend from `back/` is part of the project contract because env files and default relative paths are resolved from there.

## Logging

The backend now uses structured logging via the standard library `log/slog`.

Examples:

```bash
LOG_LEVEL=debug go run .
LOG_FORMAT=json LOG_LEVEL=info go run .
```

What to expect:
- `info`: startup, sync lifecycle, DB writes, file writes
- `warn`: recoverable problems such as stale cache fallback
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
- `.mal_token.json` does not exist
- `access_token` is expired
- token refresh failed
- `MAL_CLIENT_ID` or `MAL_CLIENT_SECRET` is missing for refresh

### Sync starts but data looks stale

Possible causes:
- MAL details cache is still valid
- MAL returned transient errors and stale cache was used
- no completed anime changed since the last sync

Use `LOG_LEVEL=debug` to see detailed request and cache behavior.

### Data files are created in an unexpected directory

Check:
- `MAL_DATA_DIR`
- `MAL_DB_PATH`
- the working directory you used to launch the app

## Project Files

Main backend files:
- `main.go`: application bootstrap and runtime file helpers
- `api.go`: HTTP routes and handlers
- `auth.go`: MAL token loading, refresh, and OAuth helpers
- `sync.go`: sync orchestration and grouping logic
- `mal_client.go`: MAL API client and retry behavior
- `cache.go`: local anime details cache
- `db.go`: SQLite contract validation, reads, and writes
- `logger.go`: structured logging setup
