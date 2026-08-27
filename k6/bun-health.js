import http from "k6/http";
import { check, sleep } from "k6";
import { BASE_URL, options as sharedOptions } from "./common.js";

export const options = sharedOptions;

export default function () {
  const res = http.get(`${BASE_URL}/api/bun/health`);

  check(res, {
    "bun health 200": (r) => r.status === 200,
    "bun health ok": (r) => r.json("status") === "ok",
  });
}
