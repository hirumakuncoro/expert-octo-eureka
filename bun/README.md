# Bun service

Minimal Bun API untuk stress lab.

## Endpoints

- `GET /health`
- `GET /products/:id`

## Environment

- `PORT` default `3000`
- `DATABASE_URL` wajib untuk query Postgres, contoh:

```bash
DATABASE_URL=postgres://user:pass@localhost:5432/stresslab?sslmode=disable
```

## Run

```bash
bun install
bun run dev
```

Production/local:

```bash
bun run start
```
