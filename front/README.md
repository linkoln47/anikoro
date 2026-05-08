# MAL Frontend

Frontend for the MAL project built with `React + Vite + JavaScript`.

At the current stage the frontend is intentionally small and focused:
- searches public MyAnimeList usernames
- starts public sync for open MAL lists
- signs in through MyAnimeList OAuth for the current user's private/session dashboard
- loads grouped anime and aggregate stats from the Go backend
- starts background sync and listens to sync progress through Server-Sent Events
- uses the backend `HttpOnly` session cookie for signed-in routes

This README is frontend-only.
Backend setup, PostgreSQL schema, and MAL auth flow are described in [../back/README.md](../back/README.md).

## Stack

- React 18
- Vite 8
- plain JavaScript
- browser `fetch`
- browser `EventSource` for sync progress
- local state with React hooks

## Current Structure

```text
front/
├── index.html
├── package.json
├── vite.config.js
└── src/
    ├── App.jsx
    ├── app/
    │   ├── useHashRoute.js
    │   └── useScrollBackground.js
    ├── components/
    │   ├── AnimeDetailsSection.jsx
    │   ├── AnimeListSection.jsx
    │   ├── PublicSearch.jsx
    │   ├── StatsGrid.jsx
    │   ├── StatusBlock.jsx
    │   ├── UserControls.jsx
    │   └── UserPage.jsx
    ├── entities/
    │   ├── anime/
    │   ├── sync/
    │   └── user/
    ├── features/
    │   ├── dashboard/
    │   └── syncJob/
    ├── main.jsx
    ├── shared/
    │   └── api/
    └── styles/
        ├── index.css
        └── theme.css
```

The structure follows a pragmatic feature-sliced hexagonal frontend:
- `app` owns app-level browser adapters such as hash routing and global effects
- `features` own stateful workflows such as dashboard loading and sync progress
- `entities` keep pure MAL/anime/user/sync rules, selectors, formatters, sorting, filtering, and stats
- `shared/api` is the HTTP/API adapter layer
- `components` stay focused on rendering and local interaction state
- `styles` splits global CSS by responsibility while preserving the existing visual behavior

## What The UI Talks To

The frontend uses these backend routes:

- `GET /api/auth/mal/start`
- `GET /api/me`
- `GET /api/anime`
- `GET /api/stats`
- `POST /api/sync`
- `GET /api/sync/jobs/{job_id}`
- `GET /api/sync/jobs/{job_id}/events`
- `POST /api/public/sync`
- `GET /api/public/anime/{username}`
- `GET /api/public/stats/{username}`
- `POST /api/auth/logout`

Important:
- OAuth and tokens are handled by the Go backend
- private frontend requests include credentials so the session cookie is sent
- public username search does not require a session cookie
- sync starts in the background, returns `job_id`, and streams progress over SSE


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
/api/public/sync
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
- centered public MAL username search
- `Search` button for already-synced public snapshots
- `Sync public list` button for open MAL lists
- full-width top auth bar with `Sign in with MAL`
- signed-in actions: `Load my list`, `Sync my list`, and `Sign out`
- sync progress bar fed by Server-Sent Events
- automatic list refresh after sync completion
- scroll-reactive background tint driven by the page scroll position
- loading placeholders for stats and anime list while dashboard data is being fetched
- search input for anime title inside the loaded anime table
- filter button that opens a compact filter panel for score
- `Type` header that cycles a quick filter between all, series, and movies
- clickable table headers for sorting by title, score, merged count, watched count, and air start
- stats cards for series, movies, and total
- anime table rows with covers, score, merged titles, watched episodes, and air start
- franchise details view for a selected grouped anime row
- status and error messages

The browser does not store the MAL token. The backend sets a signed `HttpOnly`
session cookie after the MAL OAuth callback.

## Typical Local Flow

1. Start the backend from `back/`.
2. Make sure PostgreSQL is running and the schema is applied.
3. Make sure `MAL_REDIRECT_URI` points at `http://localhost:8080/api/auth/mal/callback`.
4. Start the frontend from `front/` with `npm run dev`.
5. Open the app in the browser.
6. For public mode, enter an open MAL username and click `Search`.
7. If no public snapshot exists yet, click `Sync public list`; the progress bar updates from the backend and the list refreshes automatically when sync completes.
8. For signed-in mode, click `Sign in with MAL`.
9. After MAL redirects back, use `Load my list` or `Sync my list`; signed-in sync also streams progress and refreshes automatically on completion.

## Current Limitations

- no external router yet; routing is still a small hash-route adapter
- no test setup yet
- no TypeScript yet
- sync job state is in-memory on the backend, so progress disappears after backend restart

## Next Reasonable Steps

- add a proper production deployment note for `nginx`
