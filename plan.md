# VPS Stress Lab — Experiment Notes

**Tujuan:** Benchmark head-to-head antara arsitektur *simple* (Golang, single instance, direct DB connection) vs *over-engineered* (Bun, horizontal scaling + PgBouncer + rate limiting), untuk melihat di titik load berapa masing-masing mulai degradasi, dan apakah kompleksitas tambahan di sisi Bun benar-benar worth it.

**Spek VPS:** 2GB RAM / 2 vCPU

---

## 1. Resource Budget (2GB RAM)

| Komponen | Estimasi RAM |
|---|---|
| OS + overhead | 200–300 MB |
| PostgreSQL (shared_buffers ~256MB) | 300–400 MB |
| PgBouncer | 20–50 MB |
| Backend Golang (1 instance) | 50–100 MB |
| Backend Bun (2 instance) | 200–300 MB |
| Traefik | 30–50 MB |
| Redis (opsional, rate limit counter) | 50–100 MB |
| Sisa (OS cache, buffer saat load test) | ~300–500 MB |

Jalankan k6 dari mesin **terpisah** (laptop/VPS lain), jangan numpang di VPS yang sama — supaya hasil benchmark tidak bias karena k6 ikut rebutan CPU/RAM.

---

## 2. Arsitektur Final (A/B Comparison)

```
                         [Traefik: reverse proxy + LB + SSL + rate limit L1]
                                          │
                    ┌─────────────────────┴─────────────────────┐
                    │                                            │
            /api/go/*  (path A - SIMPLE)              /api/bun/*  (path B - SCALED)
                    │                                            │
           [Golang - 1 instance]                    [Bun instance #1] [Bun instance #2]
                    │                                            │         │
                    │                                    (Traefik round-robin LB)
                    │                                            │
                    │                                     [PgBouncer :6432]
                    │                                            │
                    └──────────────────► [PostgreSQL :5432] ◄────┘
```

**Path A (Golang — "simple"):**
- 1 instance
- Koneksi langsung ke Postgres (`pgx` pool kecil, default)
- Tanpa PgBouncer, tanpa rate limit khusus di app-level
- Tujuan: baseline "apa adanya", minim moving parts

**Path B (Bun — "over-engineered"):**
- 2 instance (cluster, port beda: 3001 & 3002), di-load-balance Traefik
- Semua koneksi DB lewat PgBouncer (`transaction` pooling mode)
- Rate limiting app-level (counter, per endpoint)
- Tujuan: representasi arsitektur "production-grade" umum

Keduanya hit tabel yang **sama persis** di Postgres, supaya perbandingan adil.

---

## 3. Orchestration: Docker Swarm + Traefik

**Kenapa Traefik (bukan Nginx) di konteks Swarm:**
- Native service discovery — auto detect saat `docker service scale bun=3`, tanpa reload manual
- Built-in Let's Encrypt (ACME) — SSL otomatis via label, tanpa setup certbot manual
- Trade-off: overhead RAM lebih besar dari Nginx (~30-50MB vs ~5-10MB), tapi worth-it untuk tujuan belajar Swarm

**Struktur folder:**
```
vps-stress-lab/
├── docker-compose.yml          # Swarm stack file
├── go-service/
│   ├── Dockerfile
│   ├── main.go
│   └── go.mod
├── bun-service/
│   ├── Dockerfile
│   ├── index.ts
│   └── package.json
├── postgres/
│   └── init.sql
├── pgbouncer/
│   └── pgbouncer.ini
├── traefik/
│   └── traefik.yml
└── k6/
    ├── scenario-health.js
    ├── scenario-read.js
    ├── scenario-insert.js
    └── scenario-race-condition.js
```

**docker-compose.yml (Swarm stack) — kerangka:**
```yaml
version: "3.9"

services:
  traefik:
    image: traefik:v3.0
    command:
      - "--providers.swarm=true"
      - "--providers.swarm.exposedbydefault=false"
      - "--entrypoints.web.address=:80"
      - "--entrypoints.websecure.address=:443"
      - "--certificatesresolvers.le.acme.email=your@email.com"
      - "--certificatesresolvers.le.acme.storage=/letsencrypt/acme.json"
      - "--certificatesresolvers.le.acme.httpchallenge.entrypoint=web"
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - "letsencrypt:/letsencrypt"
      - "/var/run/docker.sock:/var/run/docker.sock:ro"
    deploy:
      placement:
        constraints: [node.role == manager]

  go-service:
    build: ./go-service
    environment:
      DATABASE_URL: "postgres://user:pass@postgres:5432/stresslab?sslmode=disable"
    deploy:
      replicas: 1
      labels:
        - "traefik.enable=true"
        - "traefik.http.routers.go.rule=PathPrefix(`/api/go`)"
        - "traefik.http.services.go.loadbalancer.server.port=8080"
    networks: [backend]

  bun-service:
    build: ./bun-service
    environment:
      DATABASE_URL: "postgres://user:pass@pgbouncer:6432/stresslab"
    deploy:
      replicas: 2
      labels:
        - "traefik.enable=true"
        - "traefik.http.routers.bun.rule=PathPrefix(`/api/bun`)"
        - "traefik.http.services.bun.loadbalancer.server.port=3000"
    networks: [backend]

  pgbouncer:
    image: edoburu/pgbouncer:latest
    environment:
      DATABASE_URL: "postgres://user:pass@postgres:5432/stresslab"
      POOL_MODE: "transaction"
      MAX_CLIENT_CONN: "200"
      DEFAULT_POOL_SIZE: "20"
    networks: [backend]

  postgres:
    image: postgres:16
    environment:
      POSTGRES_USER: user
      POSTGRES_PASSWORD: pass
      POSTGRES_DB: stresslab
    command: >
      -c shared_buffers=256MB
      -c max_connections=100
    volumes:
      - pgdata:/var/lib/postgresql/data
      - ./postgres/init.sql:/docker-entrypoint-initdb.d/init.sql
    networks: [backend]

volumes:
  pgdata:
  letsencrypt:

networks:
  backend:
    driver: overlay
```

> Catatan: `deploy.labels` untuk Traefik discovery butuh `--providers.swarm.exposedbydefault=false` + label eksplisit seperti di atas. Cek versi Traefik terbaru untuk sintaks label yang valid.

---

## 4. Schema Database

```sql
CREATE TABLE products (
    id          BIGSERIAL PRIMARY KEY,
    name        VARCHAR(100) NOT NULL,
    stock       INT NOT NULL DEFAULT 0,
    price       NUMERIC(12,2) NOT NULL,
    version     INT NOT NULL DEFAULT 0,  -- optimistic locking
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_products_id ON products(id);
```

**Seed data:** generate beberapa ribu row dummy untuk baseline read test, tapi sisakan **5 row khusus** (misal id 1–5) dengan stock besar (misal 100000) sebagai target endpoint race-condition — supaya semua VU nembak row yang sama (flash-sale scenario), bukan random ID.

---

## 5. PgBouncer Config (pgbouncer.ini)

```ini
[databases]
stresslab = host=postgres port=5432 dbname=stresslab

[pgbouncer]
listen_addr = 0.0.0.0
listen_port = 6432
auth_type = md5
pool_mode = transaction
max_client_conn = 200
default_pool_size = 20
reserve_pool_size = 5
```

> Catatan penting: `SELECT ... FOR UPDATE` butuh transaksi yang konsisten pegang 1 koneksi. Di `pool_mode = transaction`, ini tetap aman **selama** `BEGIN...COMMIT` dieksekusi utuh dalam satu round-trip tanpa app logic yang "menahan" transaksi lama (misal nunggu response API lain di tengah transaksi). Ini justru salah satu titik yang mau diuji — expose keterbatasan pooling mode ini di high concurrency.

---

## 6. Endpoint yang Dibutuhkan (Go & Bun, paralel)

| Endpoint | Method | Fungsi |
|---|---|---|
| `/health` | GET | Return `{"status":"ok"}` — baseline infra |
| `/products/:id` | GET | Simple read by index |
| `/products/bulk` | POST | Bulk insert (pakai `COPY`/`unnest()`, bukan loop insert) |
| `/products/:id/purchase` | POST | Decrement stock — race condition test, **pessimistic** (`SELECT FOR UPDATE`) |
| `/products/:id/purchase-optimistic` | POST | Decrement stock — race condition test, **optimistic** (`WHERE version = $x`) |

**Go — bulk insert pakai COPY (`pgx.CopyFrom`):**
```go
rows := [][]interface{}{}
for i := 0; i < n; i++ {
    rows = append(rows, []interface{}{name, stock, price})
}
copyCount, err := conn.CopyFrom(
    ctx,
    pgx.Identifier{"products"},
    []string{"name", "stock", "price"},
    pgx.CopyFromRows(rows),
)
```

**Go — pessimistic lock:**
```go
tx, _ := conn.Begin(ctx)
defer tx.Rollback(ctx)

var stock int
tx.QueryRow(ctx, "SELECT stock FROM products WHERE id=$1 FOR UPDATE", id).Scan(&stock)
if stock <= 0 {
    return errors.New("out of stock")
}
tx.Exec(ctx, "UPDATE products SET stock = stock - 1, updated_at = now() WHERE id=$1", id)
tx.Commit(ctx)
```

**Bun — bulk insert pakai `unnest()`:**
```ts
await sql`
  INSERT INTO products (name, stock, price)
  SELECT * FROM unnest(
    ${names}::text[],
    ${stocks}::int[],
    ${prices}::numeric[]
  )
`;
```

**Bun — optimistic lock (retry loop):**
```ts
async function purchaseOptimistic(id: number, maxRetries = 3) {
  for (let i = 0; i < maxRetries; i++) {
    const current = await sql`SELECT stock, version FROM products WHERE id = ${id}`;
    const { stock, version } = current[0];
    if (stock <= 0) throw new Error("out of stock");

    const result = await sql`
      UPDATE products
      SET stock = stock - 1, version = version + 1, updated_at = now()
      WHERE id = ${id} AND version = ${version}
    `;
    if (result.count > 0) return; // success
  }
  throw new Error("conflict, max retries exceeded");
}
```

---

## 7. Rate Limiting

**Layer 1 — Traefik (edge, murah):**
```yaml
labels:
  - "traefik.http.middlewares.ratelimit.ratelimit.average=100"
  - "traefik.http.middlewares.ratelimit.ratelimit.burst=50"
  - "traefik.http.routers.bun.middlewares=ratelimit"
```

**Layer 2 — App-level counter (Bun only, buat bandingin dengan Go yang no rate limit):**
- Simple in-memory sliding window (Map<ip, timestamps[]>) kalau mau hemat RAM, atau
- Redis `INCR` + `EXPIRE` per key `ratelimit:{ip}:{endpoint}` kalau mau shared counter antar 2 instance Bun (in-memory gak sinkron antar proses!)

**Skenario Cloudflare (opsional, kalau ada domain):**
1. Tanpa Cloudflare — full rate limit di server
2. DNS-only — Cloudflare cuma resolve DNS, rate limit tetap full di server
3. Proxied — Cloudflare rate limiting rules di edge + app-level tetap jalan (defense-in-depth)

Bandingkan efeknya ke latency & false-positive rate.

---

## 8. Anti-DDoS (Level Server)

```bash
# nftables/iptables — SYN flood protection
sudo nft add rule inet filter input tcp flags syn limit rate 50/second accept
sudo nft add rule inet filter input tcp flags syn drop

# Connection limit per IP
sudo nft add rule inet filter input ip saddr . tcp dport ct count over 20 drop
```

- Install **fail2ban**, buat jail custom baca log Traefik, auto-ban IP yang trigger rate limit terlalu sering
- `systemd`/Docker resource limits (`mem_limit`, `cpus` di compose) per service — biar 1 service ngamuk (misal Postgres kena flood insert) gak nge-OOM-kill semua container lain

---

## 9. k6 Benchmark Scenarios

**Target:** 1000 VU (virtual users), ukur RPS yang dihasilkan tiap stage, bukan RPS sebagai target fix.

**Struktur umum tiap scenario file:**
```js
import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  scenarios: {
    ramp_up: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 100 },
        { duration: '1m', target: 500 },
        { duration: '2m', target: 1000 },
        { duration: '1m', target: 1000 }, // sustain
        { duration: '30s', target: 0 },   // ramp down
      ],
    },
  },
  thresholds: {
    http_req_duration: ['p(95)<500', 'p(99)<1000'],
    http_req_failed: ['rate<0.05'],
  },
};
```

**Stage 1 — `/health` (baseline infra):**
```js
export default function () {
  const resGo = http.get('https://yourvps.com/api/go/health');
  const resBun = http.get('https://yourvps.com/api/bun/health');
  check(resGo, { 'go status 200': (r) => r.status === 200 });
  check(resBun, { 'bun status 200': (r) => r.status === 200 });
}
```

**Stage 2 — `GET /products/:id` (simple read):**
```js
export default function () {
  const id = Math.floor(Math.random() * 10000) + 1; // random dari seed data
  http.get(`https://yourvps.com/api/go/products/${id}`);
  http.get(`https://yourvps.com/api/bun/products/${id}`);
  sleep(0.1);
}
```

**Stage 3 — bulk insert (write throughput):**
```js
export default function () {
  const payload = JSON.stringify({
    products: Array.from({ length: 50 }, (_, i) => ({
      name: `product-${__VU}-${__ITER}-${i}`,
      stock: 100,
      price: 9.99,
    })),
  });
  http.post('https://yourvps.com/api/go/products/bulk', payload, {
    headers: { 'Content-Type': 'application/json' },
  });
  http.post('https://yourvps.com/api/bun/products/bulk', payload, {
    headers: { 'Content-Type': 'application/json' },
  });
}
```

**Stage 4 — race condition (paling berat, pin ke ID 1–5):**
```js
export default function () {
  const targetId = (__VU % 5) + 1; // pin ke id 1-5, bukan random — supaya contention kena
  http.post(`https://yourvps.com/api/go/products/${targetId}/purchase`);
  http.post(`https://yourvps.com/api/bun/products/${targetId}/purchase`);
}
```

**Jalankan tiap stage terpisah** (bukan digabung satu file), supaya hasil metrik per-jenis-load bisa dibandingkan bersih:
```bash
k6 run scenario-health.js --out json=results-health.json
k6 run scenario-read.js --out json=results-read.json
k6 run scenario-insert.js --out json=results-insert.json
k6 run scenario-race-condition.js --out json=results-race.json
```

---

## 10. Metrik yang Dicatat per Stage

- **p50/p95/p99 latency** — Go vs Bun, per endpoint
- **RPS actual** yang dihasilkan dari 1000 VU (bukan asumsi 1:1 VU:RPS)
- **Error rate** (`http_req_failed`) — termasuk 429 (rate limit) dan 500/timeout
- **Titik degradasi** — di stage/VU berapa p95 mulai melonjak tajam atau error rate > 5%
- **Postgres side:** `pg_stat_activity` (jumlah koneksi aktif), lock waits (`pg_locks`), CPU/RAM Postgres via `vmstat`/`htop` selama test
- **PgBouncer side:** `SHOW POOLS;` — lihat `cl_waiting` (client nunggu koneksi) saat load tinggi, ini indikator kunci apakah pooling jadi bottleneck atau justru penyelamat

**Catatan observability benchmark (opsional, bukan scope implementasi awal):**
Observability di sini maksudnya hanya catatan/command pendamping saat load test untuk menjelaskan *kenapa* latency/error naik, bukan sistem monitoring lengkap. Minimal cukup pakai `docker stats`, log Traefik/app, `pg_stat_activity`, `pg_locks`, dan kalau admin PgBouncer sudah disiapkan nanti bisa tambah `SHOW POOLS;`. Kalau fokus awal masih build endpoint dan infra dasar, bagian ini boleh diskip sampai fase benchmark final.

---

## 11. Hipotesis yang Mau Dibuktikan

1. **`/health` & simple read:** Go (direct, minim proses) kemungkinan menang latency di load rendah-menengah karena less overhead.
2. **Bulk insert & sustained load:** Go bakal lebih cepat exhaust `max_connections` Postgres karena tiap request buka koneksi baru/dari pool kecil langsung ke Postgres — sementara Bun+PgBouncer lebih tahan karena koneksi di-multiplex.
3. **Race condition (`FOR UPDATE`):** ini titik paling menarik — PgBouncer transaction-mode pooling punya keterbatasan menahan lock antar request; kemungkinan muncul `cl_waiting` tinggi di PgBouncer saat lock contention tinggi, sementara Go (direct connection) gak kena masalah pooling tapi kena masalah lain: exhaust `max_connections` duluan.
4. **Kesimpulan akhir yang diharapkan:** over-engineering (Bun) worth-it di titik load tertentu ke atas, tapi overhead-nya (kompleksitas + resource) gak sepadan kalau traffic rendah — dan justru pooling config yang salah (transaction mode + long-held lock) bisa jadi liability, bukan solusi otomatis.

---

## 12. Urutan Eksekusi

1. Setup Docker Swarm init (`docker swarm init`) di VPS
2. Build & deploy stack (`docker stack deploy -c docker-compose.yml stresslab`)
3. Setup domain + DNS pointing ke VPS (buat SSL Traefik + skenario Cloudflare)
4. Seed database (bulk insert dummy products + 5 row target race condition)
5. Smoke test manual (curl tiap endpoint, pastikan jalan)
6. Jalankan k6 Stage 1 → 4 berurutan, dari mesin terpisah
7. Ulangi Stage 3 & 4 dengan variasi: tanpa Cloudflare / DNS-only / proxied
8. Kumpulkan semua `results-*.json`, bandingkan p95/p99/error-rate Go vs Bun per stage
9. Cross-check dengan `pg_stat_activity`, `SHOW POOLS`, dan resource usage VPS (`htop`/`vmstat`) selama tiap run
10. Tulis kesimpulan: di titik VU/RPS berapa masing-masing arsitektur mulai degradasi, dan apakah kompleksitas Bun+PgBouncer terbukti worth-it


vps awal: idle awal 218mb
setalh instal docker: 227mb
instalasi and run: 500mb
