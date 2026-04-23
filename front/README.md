# MAL Frontend

Frontend for the MAL project built with `React + Vite + JavaScript`.

At the current stage the frontend is intentionally small and focused:
- signs in through MyAnimeList OAuth
- loads grouped anime from the Go backend
- loads aggregate stats
- starts background sync
- uses the backend `HttpOnly` session cookie

This README is frontend-only.
Backend setup, PostgreSQL schema, and MAL auth flow are described in [../back/README.md](../back/README.md).

## Stack

- React 18
- Vite 8
- plain JavaScript
- browser `fetch`
- local state with React hooks

## Current Structure

```text
front/
├── index.html
├── package.json
├── vite.config.js
└── src/
    ├── api.js
    ├── App.jsx
    ├── components/
    │   ├── AnimeListSection.jsx
    │   ├── StatsGrid.jsx
    │   ├── StatusBlock.jsx
    │   └── UserControls.jsx
    ├── main.jsx
    ├── useScrollBackground.js
    └── styles.css
```

## What The UI Talks To

The frontend uses these backend routes:

- `GET /api/auth/mal/start`
- `GET /api/me`
- `GET /api/anime`
- `GET /api/stats`
- `POST /api/sync`
- `POST /api/auth/logout`

Important:
- OAuth and tokens are handled by the Go backend
- frontend requests include credentials so the session cookie is sent
- sync starts in the background and returns immediately


## Backend Connection

In development, Vite proxies browser requests from `/api` to the Go backend:

- frontend dev server: `http://localhost:5173`
- backend API: `http://localhost:8080`

Current proxy config lives in [vite.config.js](./vite.config.js).

That means the frontend can call:

```text
/api/me
/api/anime
/api/stats
/api/sync
```

without hardcoding the full backend URL in development.

## Environment Variables

The frontend supports:

- `VITE_API_BASE_URL`

Behavior:
- when empty, requests use relative paths like `/api/...`
- this works well with the Vite dev proxy
- for production deploys behind a reverse proxy, keeping it empty is also usually fine
- if frontend and backend are hosted on different origins, set `VITE_API_BASE_URL` explicitly

Example:

```bash
VITE_API_BASE_URL="http://localhost:8080" npm run dev
```

## Current UX

The current screen includes:
- `Sign in with MAL` button
- `Load Data` button
- `Start Sync` button
- `Sign out` button
- scroll-reactive background tint driven by the page scroll position
- loading placeholders for stats and anime list while dashboard data is being fetched
- search input for anime title or `id`
- filter button that opens a compact filter panel for score
- `Type` header that cycles a quick filter between all, series, and movies
- clickable table headers for sorting by title, score, merged count, watched count, and sync time
- stats cards for series, movies, and total
- anime list cards with score, merged titles, watched episodes, and last sync time
- status and error messages

The browser does not store the MAL token. The backend sets a signed `HttpOnly`
session cookie after the MAL OAuth callback.

## Typical Local Flow

1. Start the backend from `back/`.
2. Make sure PostgreSQL is running and the schema is applied.
3. Make sure `MAL_REDIRECT_URI` points at `http://localhost:8080/api/auth/mal/callback`.
4. Start the frontend from `front/` with `npm run dev`.
5. Open the app in the browser.
6. Click `Sign in with MAL`.
7. After MAL redirects back, click `Load Data`.
8. If there is no data yet, click `Start Sync`, wait a bit, then click `Load Data` again.

## Current Limitations

- no router yet
- no test setup yet
- no TypeScript yet
- no polling for sync progress yet

## Next Reasonable Steps

- add automatic refresh after sync
- add a proper production deployment note for `nginx`
