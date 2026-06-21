# anikoro Frontend

Frontend for the anikoro project built with `React + Vite + JavaScript`.

At the current stage the frontend is intentionally small and focused:
- searches public MyAnimeList usernames
- starts public sync for open MAL lists
- registers and signs in native accounts with email + password (the primary login)
- lets a signed-in user connect their MyAnimeList account (OAuth) to enable sync
- loads grouped anime and aggregate stats from the Go backend
- starts background sync and listens to sync progress through Server-Sent Events
- lets a signed-in user edit or remove a single list entry, writing the change back to MAL
- uses the backend `HttpOnly` session cookie for signed-in routes
- is ready for production behind `nginx`, where the frontend is served from `dist`
  and `/api/...` is proxied to the Go backend

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
    ├── main.jsx
    ├── app/
    │   ├── useHashRoute.js
    │   └── useScrollBackground.js
    ├── components/
    │   ├── AnimeDetailsSection.jsx
    │   ├── AnimeListSection.jsx
    │   ├── AuthPanel.jsx
    │   ├── Footer.jsx
    │   ├── FranchiseEntryEditor.jsx
    │   ├── FranchiseEntryStats.jsx
    │   ├── PublicSearch.jsx
    │   ├── StatsGrid.jsx
    │   ├── StatusBlock.jsx
    │   ├── UserControls.jsx
    │   └── UserPage.jsx
    ├── entities/
    │   ├── anime/      # constants, filters, formatters, metrics, selectors, sort
    │   ├── sync/       # sync progress rules
    │   └── user/       # user stats
    ├── features/
    │   ├── dashboard/  # useDashboardController
    │   ├── listEdit/   # useListEdit
    │   └── syncJob/    # useSyncJob
    ├── shared/
    │   ├── api/        # api.js + client.js (HTTP adapter)
    │   └── security/   # inputValidation.js (+ test)
    └── styles/
        ├── animations.css
        ├── anime-details.css
        ├── anime-list.css
        ├── base.css
        ├── controls.css
        ├── index.css
        ├── layout.css
        ├── responsive.css
        ├── skeleton.css
        ├── stats.css
        ├── theme.css
        └── user-page.css
```

The structure follows a pragmatic feature-sliced hexagonal frontend:
- `app` owns app-level browser adapters such as hash routing and global effects
- `features` own stateful workflows such as dashboard loading, list editing, and sync progress
- `entities` keep pure MAL/anime/user/sync rules, selectors, formatters, sorting, filtering, and stats
- `shared/api` is the HTTP/API adapter layer; `shared/security` holds input validation helpers
- `components` stay focused on rendering and local interaction state
- `styles` splits global CSS by responsibility while preserving the existing visual behavior

The current browser-facing routes are handled by the lightweight hash-route adapter in
`src/app/useHashRoute.js`:

- `#/user`
- `#/anime/{anime_id}`

## What The UI Talks To

The frontend uses these backend routes:

- `POST /api/auth/register`
- `POST /api/auth/login`
- `GET /api/auth/mal/start`
- `POST /api/auth/mal/disconnect`
- `GET /api/me`
- `GET /api/anime`
- `PATCH /api/anime/{anime_id}/list-status`
- `DELETE /api/anime/{anime_id}/list-status`
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
- full-width top auth bar with `Sign in` / `Register` (native email + password)
- `AuthPanel` modal with login and registration forms
- signed-in actions: `My page`, reload/sync (when MAL is linked), and `Sign out`
- the MAL link control lives on `My page`, right of the `User Page` eyebrow:
  `Connect MAL` when unlinked, or `MAL linked` (hover/focus to reveal
  `Disconnect`) when linked; disconnecting keeps the synced data
- sync progress bar fed by Server-Sent Events
- automatic list refresh after sync completion
- hash routes for the user page and anime detail view
- scroll-reactive background tint driven by the page scroll position
- loading placeholders for stats and anime list while dashboard data is being fetched
- search input for anime title inside the loaded anime table
- filter button that opens a compact filter panel for score
- `Type` header that cycles a quick filter between all, series, and movies
- clickable table headers for sorting by title, score, merged count, watched count, and air start
- stats cards for series, movies, and total
- anime table rows with covers, score, merged titles, watched episodes, and air start
- franchise details view for a selected grouped anime row
- inline editor on a franchise entry to change status, score, and watched episodes, or remove the entry (signed-in only), with the change written back to MAL
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
8. For a signed-in account, click `Register` to create an email + password account (or `Sign in`).
9. Open `My page` and click `Connect MAL` to link your MyAnimeList account; after MAL redirects back, use the reload/sync action. Signed-in sync streams progress and refreshes automatically on completion. On `My page`, `MAL linked` reveals `Disconnect` on hover, which unlinks MAL while keeping the synced data.

## Current Limitations

- routing is intentionally a small hash-route adapter instead of a router dependency
- test coverage is limited to the security input-validation suite (`npm run test:security`); there is no component/UI test setup yet
- no TypeScript yet
- sync job state is in-memory on the backend, so progress disappears after backend restart

## Next Reasonable Steps

- add a `systemd` unit or deployment script for the backend binary
