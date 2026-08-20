import { SQL } from "bun";

const port = Number(Bun.env.PORT ?? 3000);
const dbMaxConns = Number(Bun.env.DB_MAX_CONNS ?? 10);
const productIdMin = Number(Bun.env.PRODUCT_ID_MIN ?? 1);
const productIdMax = Number(Bun.env.PRODUCT_ID_MAX ?? 5);
const decrementMin = Number(Bun.env.DECREMENT_MIN ?? 1);
const decrementMax = Number(Bun.env.DECREMENT_MAX ?? 10);
const sql = new SQL({
  url: Bun.env.DATABASE_URL,
  max: dbMaxConns,
  prepare: false,
  idleTimeout: 30,
  connectionTimeout: 30,
});

type ProductRow = {
  id: number;
  name: string;
  stock: number;
  price: string;
  version: number;
  created_at: Date;
  updated_at: Date;
};

function json(data: unknown, status = 200) {
  return Response.json(data, {
    status,
    headers: {
      "Cache-Control": "no-store",
    },
  });
}

function notFound() {
  return json({ error: "not found" }, 404);
}

function randomInt(min: number, max: number) {
  return Math.floor(Math.random() * (max - min + 1)) + min;
}

async function getProduct(id: number) {
  const [row] = await sql<ProductRow[]>`
    SELECT id, name, stock, price, version, created_at, updated_at
    FROM products
    WHERE id = ${id}
    LIMIT 1
  `;

  return row ?? null;
}

async function purchaseRandomProduct() {
  const productId = randomInt(productIdMin, productIdMax);
  const decrement = randomInt(decrementMin, decrementMax);

  return sql.begin(async (tx) => {
    const [row] = await tx<ProductRow[]>`
      SELECT id, name, stock, price, version, created_at, updated_at
      FROM products
      WHERE id = ${productId}
      LIMIT 1
      FOR UPDATE
    `;

    if (!row) return null;

    if (row.stock < decrement) {
      return {
        product: row,
        decrement,
        updated: false,
      };
    }

    const [updatedProduct] = await tx<ProductRow[]>`
      UPDATE products
      SET stock = stock - ${decrement},
          updated_at = now()
      WHERE id = ${row.id}
      RETURNING id, name, stock, price, version, created_at, updated_at
    `;

    return {
      product: updatedProduct,
      decrement,
      updated: true,
    };
  });
}

const server = Bun.serve({
  port,
  async fetch(req) {
    const url = new URL(req.url);

    if (req.method === "GET" && url.pathname === "/health") {
      return json({ status: "ok" });
    }

    const productMatch = url.pathname.match(/^\/products\/([^/]+)$/);
    if (req.method === "GET" && productMatch) {
      const id = Number(productMatch[1]);
      if (!Number.isSafeInteger(id) || id <= 0) {
        return json({ error: "invalid product id" }, 400);
      }

      const product = await getProduct(id);
      if (!product) return notFound();

      return json({ product });
    }

    if (req.method === "POST" && url.pathname === "/products/random-purchase") {
      const result = await purchaseRandomProduct();
      if (!result) return notFound();

      return json(result);
    }

    return notFound();
  },
});

console.log(`bun-service listening on :${server.port}`);
