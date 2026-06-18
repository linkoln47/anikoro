> [!IMPORTANT]
> **The source code is publicly available for portfolio review purposes only. No permission is granted to reuse, redistribute, modify, or incorporate it into other projects.**

# Anikoro

**Anikoro turns a fragmented anime library into a coherent one.** Most anime catalogs treat every season, movie, OVA, and spin-off as a separate title, so a single story ends up scattered across a dozen disconnected entries. Anikoro reassembles those pieces: it syncs a MyAnimeList profile, walks the relations between titles, and groups each franchise back into one unit you can actually navigate — the whole arc of a story in a single place.

🌐 [**Live**](https://anikoro.me)
📘 [**Backend documentation**](back/README.md)
🎨 [**Frontend documentation:**](front/README.md)

## Overview

Anime franchises are rarely shipped as one thing. A story is split into separate seasons, films, side stories, and alternative versions, each catalogued as its own entry. The result is that a list of "300 anime" is often a far smaller number of actual franchises, fragmented and hard to reason about.

Anikoro is built to solve exactly that fragmentation. It takes a MyAnimeList profile, follows the relation graph that MAL exposes between titles, and collapses each set of related entries into a single franchise. From there the user works at the franchise level — seeing the full timeline of a series at a glance — and can drill into any individual entry inside it when they want detail.

### What a user can do

- **Sign in with MyAnimeList** (OAuth) to load and manage their own list, or **look up any public MAL profile** by username — no account required to browse.
- **Browse the library grouped by franchise**, with each franchise expanded into its full set of seasons, movies, and related entries.
- **Sort, filter, and search** the grouped library by title, score, type (series/movie), merged count, watched episodes, and air date.
- **Edit a list entry in place** — change status, score, or watched episodes, or remove it entirely — with the change written straight back to the MAL account.
- **Watch sync run live**, with progress streamed in real time and the list refreshing automatically when it completes.

---

This project was designed and implemented as an independent portfolio project. Its purpose is to demonstrate my approach to backend development, application architecture, API design, data management, testing, and frontend integration.

---

## Structure

The repository is a small monorepo with a clear front/back split, deployed as three containers (`db`, `backend`, `frontend`).

```text
anikoro/
├── back/    # Go API — sync engine, franchise graph, REST API, PostgreSQL access
├── front/   # React + Vite single-page app
├── docker-compose.yml
└── DEPLOY.md
```

The backend follows a hexagonal (ports-and-adapters) architecture; the frontend follows a pragmatic feature-sliced structure. Both are described in detail in their own documentation.

### Tech stack

| Layer | Technology |
| --- | --- |
| Backend | Go 1.26, `gorilla/mux`, `pgx` |
| Database | PostgreSQL (with row-level security) |
| Frontend | React 18, Vite 8, JavaScript |
| Auth | MyAnimeList OAuth 2.0, signed `HttpOnly` session cookies |
| Realtime | Server-Sent Events |
| External API | MyAnimeList API v2 |
| Logging | Structured logging via `log/slog` |
| Deployment | Docker, Docker Compose, nginx reverse proxy |

## My Contribution

The project was designed and implemented independently.

My responsibilities included:

- defining the product requirements;
- designing the backend architecture;
- implementing the API and business logic;
- designing the database schema;
- implementing authentication and authorization;
- developing the frontend interface;
- integrating the frontend with the API;
- writing tests and technical documentation;
- configuring the development and deployment environments.

Detailed implementation information is available in the technical documentation:

- [Backend architecture](back/README.md)
- [Frontend architecture](front/README.md)

## Project Status

The project is currently under active development.

**Completed:**

- Core backend functionality
- Database integration
- Basic frontend interface
- API integration

**Planned:**

- incremental database migrations to replace the current reset-only schema;
- persistent sync job history and a queue that survives backend restarts;
- background MAL token refresh and scheduled automatic re-sync;
- a TypeScript migration with a component and end-to-end test suite on the frontend;
- richer franchise analytics — watch-order suggestions, completion insights, and recommendations.

## Author

**Aleksei Fedunov**

Software engineering student focused on backend development, Go, Linux, and application architecture.

- GitHub: [@linkoln47](https://github.com/linkoln47)
- LinkedIn: [Fedunov Aleksei](https://www.linkedin.com/in/aleksei-fedunov-46b120318/?locale=ja-JP)
- Email: fedunov1995@gmail.com

## Usage Notice

This repository is publicly accessible for portfolio evaluation and code review purposes.

Unless explicitly stated otherwise, no permission is granted to copy, modify, redistribute, sublicense, publish, or incorporate this source code into another project.

Please do not reuse the project, its source code, documentation, branding, or visual materials without prior written permission.

See [COPYRIGHT.md](COPYRIGHT.md) for details.
