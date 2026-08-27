import http from "k6/http";
import { check, sleep } from "k6";
import { BASE_URL, options as sharedOptions } from "./common.js";

export const options = sharedOptions;

export default function () {
  const res = http.post(`${BASE_URL}/api/bun/products/random-purchase`);

  check(res, {
    "bun purchase 200": (r) => r.status === 200,
    "bun purchase has product": (r) => r.json("product.id") > 0,
    "bun purchase has decrement": (r) => r.json("decrement") > 0,
  });
}
