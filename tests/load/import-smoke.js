import http from "k6/http";
import { check, sleep } from "k6";

export const options = {
  scenarios: { smoke: { executor: "constant-vus", vus: 1, duration: "15s" } },
  thresholds: { http_req_failed: ["rate<0.01"] },
};

const server = __ENV.VERITY_SERVER || "http://127.0.0.1:8000";
const auth = __ENV.VERITY_API_TOKEN ? { Authorization: `Bearer ${__ENV.VERITY_API_TOKEN}` } : {};

export default function () {
  const response = http.get(`${server}/api/v1/workspace`, { headers: auth });
  check(response, { "workspace is readable": (value) => value.status === 200 });
  sleep(0.2);
}
