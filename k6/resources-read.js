// k6 load test 1: availability reads, showing the effect of the 15 s in-memory cache.
//
// Run (after deploying; uses a Doctor's ID token):
//   k6 run -e API_URL=https://xxxx.execute-api.us-east-1.amazonaws.com -e TOKEN=<id token> k6/resources-read.js
//
// Kept deliberately SMALL (≤ 10 virtual users, 1 minute ≈ a few thousand requests) so it stays well
// inside the free tier (1M Lambda and 1M HTTP API requests per month) and under the API's
// throttling limit (25 req/s, burst 50).
// What to show in the report: p95 latency, and in CloudWatch Logs Insights the "cacheHit" ratio.
import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '15s', target: 5 },
    { duration: '30s', target: 10 },
    { duration: '15s', target: 0 },
  ],
  thresholds: {
    http_req_failed: ['rate<0.01'], // < 1% errors
    http_req_duration: ['p(95)<800'], // p95 under 800 ms (includes cold starts)
  },
};

const API = __ENV.API_URL;
const params = { headers: { Authorization: `Bearer ${__ENV.TOKEN}` } };

export default function () {
  const res = http.get(`${API}/resources`, params);
  check(res, {
    'status 200': (r) => r.status === 200,
    'has items': (r) => Array.isArray(r.json('items')),
  });
  sleep(1); // ~1 request per second per user: a dashboard refreshing quickly
}
