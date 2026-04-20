# MAL Frontend

Frontend for the MAL project built with `React + Vite + JavaScript`.

At the current stage the frontend is intentionally small and focused:
- accepts internal app `user_id`
- loads grouped anime from the Go backend
- loads aggregate stats
- starts background sync
- stores the last used `user_id` in `localStorage`

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

- `GET /api/anime/:user_id`
- `GET /api/stats/:user_id`
- `POST /api/sync/:user_id`

Important:
- `user_id` is the internal `users.id` from PostgreSQL
- `user_id` is not the MyAnimeList username
- sync starts in the background and returns immediately


## Backend Connection

In development, Vite proxies browser requests from `/api` to the Go backend:

- frontend dev server: `http://localhost:5173`
- backend API: `http://localhost:8080`

Current proxy config lives in [vite.config.js](./vite.config.js).

That means the frontend can call:

```text
/api/anime/1
/api/stats/1
/api/sync/1
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
- input for `user_id`
- `Load Data` button
- `Start Sync` button
- `Refresh` button
- scroll-reactive background tint driven by the page scroll position
- loading placeholders for stats and anime list while dashboard data is being fetched
- search input for anime title or `id`
- filter button that opens a compact filter panel for score
- `Type` header that cycles a quick filter between all, series, and movies
- clickable table headers for sorting by title, score, merged count, watched count, and sync time
- stats cards for series, movies, and total
- anime list cards with score, merged titles, watched episodes, and last sync time
- status and error messages

The last entered user id is saved to `localStorage` under:

```text
mal.front.userId
```

## Typical Local Flow

1. Start the backend from `back/`.
2. Make sure PostgreSQL is running and the schema is applied.
3. Make sure the user already exists in `users` and has a valid token in `mal_tokens`.
4. Start the frontend from `front/` with `npm run dev`.
5. Open the app in the browser.
6. Enter internal `user_id` such as `1`.
7. Click `Load Data`.
8. If there is no data yet, click `Start Sync`, wait a bit, then click `Refresh`.

## Current Limitations

- no router yet
- no test setup yet
- no TypeScript yet
- no polling for sync progress yet
- no auth/session handling in the frontend yet

## Next Reasonable Steps

- add automatic refresh after sync
- add a proper production deployment note for `nginx`
