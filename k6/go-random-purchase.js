import http from "k6/http";
import { check, sleep } from "k6";
import { BASE_URL, options as sharedOptions } from "./common.js";

export const options = sharedOptions;

export default function () {
  const res = http.post(`${BASE_URL}/api/go/products/random-purchase`);

  check(res, {
    "go purchase 200": (r) => r.status === 200,
    "go purchase has product": (r) => r.json("product.id") > 0,
    "go purchase has decrement": (r) => r.json("decrement") > 0,
  });
}
