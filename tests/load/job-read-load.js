import http from "k6/http";
import { check, sleep } from "k6";

export const options = {
  scenarios: { expected: { executor: "constant-arrival-rate", rate: 20, timeUnit: "1s", duration: "30s", preAllocatedVUs: 5, maxVUs: 20 } },
  thresholds: { http_req_failed: ["rate<0.01"], http_req_duration: ["p(95)<500"] },
};

const server = __ENV.VERITY_SERVER || "http://127.0.0.1:8000";
const jobID = __ENV.VERITY_JOB_ID;
const auth = __ENV.VERITY_API_TOKEN ? { Authorization: `Bearer ${__ENV.VERITY_API_TOKEN}` } : {};

export default function () {
  const response = http.get(`${server}/api/v1/jobs/${jobID}`, { headers: auth });
  check(response, { "job is readable": (value) => value.status === 200 });
  sleep(0.05);
}
