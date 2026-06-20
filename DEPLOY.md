# Docker Deploy

This project runs as three containers:

- `db`: PostgreSQL
- `backend`: Go API
- `frontend`: nginx serving the built React app and proxying `/api` to the backend

Only the frontend service is exposed to the host by default.

## 1. Create `.env`

```bash
cp .env.example .env
```

Edit `.env` and set real values:

```bash
POSTGRES_PASSWORD=use_a_real_database_password
DATABASE_URL=postgres://postgres:use_a_real_database_password@db:5432/mal?sslmode=disable

MAL_CLIENT_ID=your_mal_client_id
MAL_CLIENT_SECRET=your_mal_client_secret
MAL_REDIRECT_URI=http://your-domain-or-server-ip/api/auth/mal/callback
MAL_FRONTEND_URL=http://your-domain-or-server-ip/
MAL_SESSION_SECRET=use_a_long_random_secret
```

`MAL_REDIRECT_URI` must exactly match the callback URL configured in your MyAnimeList application.

Generate a session secret with:

```bash
openssl rand -hex 32
```

If your database password contains URL-reserved characters such as `@`, `/`, `:`, or `?`, URL-encode it in `DATABASE_URL`.

## 2. Start

```bash
docker compose up -d --build
```

A one-shot `migrate` service runs `migrate up` and must exit 0 before `backend` starts. PostgreSQL is expected to run outside the compose stack (the backend reaches it via `host.docker.internal`).

Always take a backup before applying migrations in production:

```bash
pg_dump -Fc "$DATABASE_URL" -f /var/backups/anikoro-$(date +%F-%H%M).dump
```

## 3. Check

```bash
docker compose ps
docker compose logs -f backend
```

Open:

```text
http://your-domain-or-server-ip/
```

## Applying Migrations Manually

If you need to apply migrations outside the compose flow (e.g. with the backend already running):

```bash
docker compose run --rm migrate up
```

Or directly from the host with a Go toolchain:

```bash
cd back && go run ./cmd/migrate up
```

## Update

After pulling new code:

```bash
docker compose up -d --build
```

