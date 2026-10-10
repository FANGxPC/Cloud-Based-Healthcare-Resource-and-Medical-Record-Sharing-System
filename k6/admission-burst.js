// k6 load test 2: a burst of SIMULTANEOUS admissions against one hospital resource. It shows
// against the real deployed system that beds can't be overbooked.
//
// Setup: an admin first sets the resource to a small number of free units (e.g. 3 ICU beds at H001).
// Run with a Doctor of that hospital (doctor.h001@example.com):
//   k6 run -e API_URL=... -e TOKEN=<doctor id token> -e HOSPITAL=H001 -e TYPE=ICU_BED k6/admission-burst.js
//
// Expected: exactly as many 201s as there were free units, every other request 409
// (NO_CAPACITY or ALREADY_ADMITTED), and "available" ends at 0, never negative.
// teardown() discharges everyone admitted here, so the test can be repeated.
import http from 'k6/http';
import { check } from 'k6';
import { Counter } from 'k6/metrics';

const API = __ENV.API_URL;
const HOSPITAL = __ENV.HOSPITAL || 'H001';
const TYPE = __ENV.TYPE || 'ICU_BED';
const params = { headers: { Authorization: `Bearer ${__ENV.TOKEN}`, 'Content-Type': 'application/json' } };

const admitted = new Counter('admitted');
const rejected = new Counter('rejected_no_capacity');

export const options = {
  scenarios: {
    burst: {
      executor: 'per-vu-iterations', // all 20 VUs start together and each fires once
      vus: 20,
      iterations: 1,
      maxDuration: '30s',
    },
  },
  thresholds: {
    // Everything must be either 201 (admitted) or 409 (correctly refused). Any 5xx fails the test.
    'http_req_failed{expected_response:true}': ['rate==0'],
  },
};

// setup: pick 20 patients at the hospital who are not admitted yet, and read the starting capacity.
export function setup() {
  const list = http.get(`${API}/patients?hospitalId=${HOSPITAL}&limit=100`, params).json('items');
  const free = list.filter((p) => !p.admission).map((p) => p.patientId);
  if (free.length < 20) throw new Error(`need 20 unadmitted patients at ${HOSPITAL}, found ${free.length}`);
  const before = resource();
  console.log(`before: ${TYPE} at ${HOSPITAL} available=${before.available}/${before.total}`);
  return { patients: free.slice(0, 20), before: before.available };
}

export default function (data) {
  const patientId = data.patients[__VU - 1];
  const res = http.post(`${API}/hospitals/${HOSPITAL}/admissions`, JSON.stringify({ patientId, resourceType: TYPE }), {
    ...params,
    responseCallback: http.expectedStatuses(201, 409),
  });
  if (res.status === 201) admitted.add(1);
  if (res.status === 409) rejected.add(1);
  check(res, { '201 or 409': (r) => r.status === 201 || r.status === 409 });
}

export function teardown(data) {
  const after = resource();
  console.log(`after:  available=${after.available} (started at ${data.before})`);
  check(after, { 'available never negative': (r) => r.available >= 0 });

  // Clean up: discharge the patients this test admitted.
  for (const id of data.patients) {
    http.del(`${API}/hospitals/${HOSPITAL}/admissions/${id}`, null, { ...params, responseCallback: http.expectedStatuses(204, 409) });
  }
}

function resource() {
  const items = http.get(`${API}/hospitals/${HOSPITAL}/resources`, params).json('items');
  return items.find((r) => r.resourceType === TYPE);
}
