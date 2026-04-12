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
| `MAL_DATA_DIR` | No | directory of the Go source file, then working directory fallback | Base directory for runtime files such as DB, token, and cache. |
| `MAL_DB_PATH` | No | `<MAL_DATA_DIR>/mal.db` | Explicit path to the SQLite database file. Overrides default DB location only. |
| `LOG_LEVEL` | No | `info` | Log level: `debug`, `info`, `warn`, `error`. |
| `LOG_FORMAT` | No | `text` | Log format: `text` or `json`. |

## Runtime Files

By default, the app stores its runtime files inside `MAL_DATA_DIR`. If `MAL_DATA_DIR` is not set, it falls back to the backend source directory.

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
export MAL_CLIENT_ID="your_client_id"
export MAL_CLIENT_SECRET="your_client_secret"
export PORT="8080"
export MAL_DATA_DIR="$(pwd)"
export LOG_LEVEL="info"
export LOG_FORMAT="text"

go run .
```

The server starts on `http://localhost:8080`.

Create or refresh the token file:

```bash
go run . auth
```

The command starts a temporary callback listener, prints the MAL authorization URL, waits for the browser callback, and then writes `.mal_token.json` into `MAL_DATA_DIR` or the default runtime directory.

Before calling `/api/sync`, ensure `.mal_token.json` exists in `MAL_DATA_DIR` or the default runtime directory.

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

## Database Schema

The backend maintains two SQLite tables:

- `series_table`
- `movie_table`

Each row contains:
- canonical MAL ID
- deterministic `group_key`
- display title
- merged title count
- average score
- total watched episodes
- sync timestamp

On startup, the service automatically:
- creates missing tables
- backfills older schema fields when needed
- creates a unique index on `group_key`

## Sync Details

The sync process currently operates like this:

1. Reads all completed anime from MAL using paginated requests.
2. Fetches details per anime with retry logic for transient MAL errors.
3. Caches detail responses locally for 7 days.
4. Merges titles that are linked through `related_anime`.
5. Uses average score and total watched episodes across the merged set.
6. Treats isolated standalone movies as `movie`; other grouped items land in `series`.

If MAL is temporarily unstable and a stale cache entry exists, the backend may use cached details instead of failing immediately.

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
- `debug`: verbose MAL request and cache activity

Each log entry includes a `component` field so it is easier to filter logs by subsystem.

## CORS

The server currently allows cross-origin requests from:

- `http://localhost:3000`
- `http://localhost:3001`

Allowed methods:
- `GET`
- `POST`
- `PUT`
- `DELETE`
- `OPTIONS`

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
- `db.go`: SQLite schema, migrations, reads, and writes
- `logger.go`: structured logging setup
