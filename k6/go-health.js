import http from "k6/http";
import { check, sleep } from "k6";
import { BASE_URL, options as sharedOptions } from "./common.js";

export const options = sharedOptions;

export default function () {
  const res = http.get(`${BASE_URL}/api/go/health`);

  check(res, {
    "go health 200": (r) => r.status === 200,
    "go health ok": (r) => r.json("status") === "ok",
  });
}
