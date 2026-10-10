# Build Plan

Each step is a **vertical slice**: OpenAPI → Go Lambda → `template.yaml` → frontend screen. Every step
ends in something you can demo. After each step, run the checks in `CLAUDE.md` (vet, test,
`sam validate --lint`, `sam build`, frontend build), then stop for review and commit.

Estimates assume AI-assisted pairing (I write, you review and run) and are rough.
**Bold** items are on the critical path.

---

## ⏰ Deadline: tonight — cut line

The full four-phase brief is several days of work. For tonight, aim for:

| Must have tonight (≈ 6–7 h) | Nice if time remains | Describe in report as "future work" |
|---|---|---|
| Phase 0 setup | 2.2 Low-bed SNS alerts (≈ 45 min) | Phase 3 k6 load tests, dashboard polish |
| 1.1 Login + roles | 3.1 15 s cache (≈ 30 min) | Phase 4 Bedrock |
| 1.2 Bed dashboard + admin edit | 3.2 Alarms + 1 dashboard (≈ 45 min) | CI workflow |
| 1.3 Patient records + audit | 1.4 Documents via presigned URLs (≈ 1.5 h) | |
| **2.1 Race-safe admission + concurrency test** | 2.3 Async transfers SQS→worker (≈ 2 h) | |

This puts the project's most defensible feature (**provably race-safe booking**) inside the must-have list.
If you run short of time, the order above is the order to drop items from the right-hand columns.

**Before the first deploy:** I will ask before every `sam deploy`. Deploying needs your AWS CLI profile and
an email address for the SNS subscription (you confirm it from your inbox).

---

## Phase 0 — Setup (≈ 45 min, mostly installs)

| # | Task | Est. | Demo |
|---|---|---|---|
| **0.1** | **Install the missing tools:** Go (latest stable), AWS SAM CLI, Docker Desktop (for DynamoDB Local and `sam local`). Node 22 and git are already present | 20–30 min | `go version`, `sam --version`, `docker ps` |
| **0.2** | Scaffold `backend/go.mod`, `internal/` packages, the `/health` Lambda, `template.yaml` with the `Stage` param, an HTTP API and an empty Cognito pool | 15 min | `sam local start-api` → `GET /health` |
| 0.3 | Scaffold `frontend/` (Vite + React + TS) | 5 min | `npm run dev` shows the shell |

## Phase 1 — Core MVP

| # | Feature slice | Est. | Demo |
|---|---|---|---|
| **1.1** | **Auth & roles.** Cognito pool, 3 groups, `custom:hospital_id`, self sign-up off. JWT authorizer. `internal/auth` claim parsing plus unit tests. `GET /me`. Seed script creates the `Hospitals` table and 1 demo user per group per hospital. Frontend: Amplify login and role-aware nav | 1.5 h | Log in as each role; `/me` shows the groups |
| **1.2** | **Hospitals & resources.** `Hospitals` and `Resources` tables. `GET /hospitals`, `GET /hospitals/{id}`, `GET /resources`, `GET/PUT /hospitals/{id}/resources/...` with optimistic lock and an own-hospital check. Seed the synthetic counts. Frontend: availability dashboard (table with a red "low" badge) and an admin edit form | 1.5 h | An admin edits ICU beds at H001; a Doctor sees the change; an admin at H002 gets **403** |
| **1.3** | **Patient records + audit.** `Patients` and `AuditLog` tables. `GET /patients`, `GET /patients/{id}` (Patient = own only; Doctor = any, audited), `GET /patients/{id}/access-log`, `GET /audit`. Seed about 50 synthetic patients (a generated JSON generator; Synthea is optional later). Frontend: Doctor patient search and detail view, plus a Patient "My record" page and "Who viewed my record" | 1.5 h | Doctor opens a patient → the patient sees that access in their log; Patient A → Patient B gives **403** |
| 1.4 | **Documents.** S3 bucket (SSE-S3, BPA, CORS for the frontend origin), `Documents` table, presigned PUT → `complete` → presigned GET. Frontend: upload widget, list, download | 1.5 h | Doctor uploads a synthetic PDF; the Patient downloads it |

## Phase 2 — Race-safe booking, SQS/SNS

| # | Feature slice | Est. | Demo |
|---|---|---|---|
| **2.1** | **Admissions (race-safe).** `POST/DELETE /hospitals/{id}/admissions` with `TransactWriteItems` and `available > :zero`. **Concurrency test:** against DynamoDB Local, set `available=1`, fire 50 goroutines, and assert exactly 1 success, 49 `NO_CAPACITY`, and final `available == 0`. Frontend: "Admit" button on the patient page | 1.5 h | Run the test live in the viva; click Admit on the last bed twice → the second gets 409 |
| 2.2 | **Low-resource alerts.** Stream on `Resources` → `resource-alerts` Lambda → SNS email (only on the threshold *crossing*; `hospitalId` message attribute for filter policies). Unit test for the crossing logic | 45 min | Admit until below threshold → the email arrives |
| 2.3 | **Async transfers.** `Transfers` table and GSIs, SQS plus DLQ (maxReceiveCount 3, SSE-SQS), `POST /transfers` → 202, `transfer-worker` (idempotent hold-bed transaction) → SNS, `GET /transfers`, decision endpoint (accept/reject transactions). Frontend: Doctor "Request transfer" form with status polling, and an Admin "Incoming transfers" inbox | 2–2.5 h | Doctor at H001 requests an ICU transfer to H002 → the H002 admin gets an email → accepts → the patient moves and the counts update |

## Phase 3 — Caching, observability, load

| # | Task | Est. | Demo |
|---|---|---|---|
| 3.1 | `internal/cache` TTL cache (mutex + expiry) with unit tests. Wire it into `GET /resources`. Log `cache_hit` | 30 min | Logs show hit/miss; `cachedAt` in the response |
| 3.2 | Structured JSON logging with request ID, user ID, route and latency. CloudWatch dashboard (invocations, errors, p95 duration, SQS depth, DLQ depth). Alarms: Lambda errors > 0 and DLQ messages > 0 → SNS email. Log retention 7 days | 45 min | Force an error → the alarm emails you |
| 3.3 | k6 scripts: (a) read load on `/resources` showing the cache effect, (b) a burst of concurrent admissions showing zero overbooking. Keep VUs and duration small so usage stays within the free tier | 1 h | k6 summary chart in the report |
| 3.4 | `.github/workflows/ci.yml`: go vet/test (with a DynamoDB Local service container), `sam validate --lint`, `sam build`, frontend build. **No deploy step** | 30 min | Green CI badge |

## Phase 4 — Optional: natural-language queries (Bedrock)

| # | Task | Est. | Demo |
|---|---|---|---|
| 4.1 | `EnableNLQuery` parameter (default `false`). The `nlquery` Lambda calls Nova Micro to turn the question into a JSON filter, then answers from resource data. **No patient data is sent to the model.** IAM is scoped to that one model | 1.5 h | "Which hospitals have a free ICU bed and a ventilator?" |

> ⚠️ Bedrock is **not** always-free. Nova Micro bills per token and would use Free-plan credits. We need to
> confirm Bedrock model access works on your Free-plan account before building this. Keep the flag off
> for the `demo` stack unless it has been confirmed.

---

## Free-tier guardrails (checked at every step)

- DynamoDB on-demand only. Lambda without provisioned concurrency. HTTP API (cheaper than REST API).
- No VPC, so no NAT gateway. AWS-managed or AWS-owned encryption only (SSE-S3, SSE-SQS, DynamoDB default).
- SNS email only (1,000 free emails/month). CloudWatch: 1 dashboard (3 free) and 2–3 alarms (10 free),
  7-day log retention.
- Two stacks (`dev`, `demo`) double the resource count but not the cost, since everything is pay-per-request.
- Set an **AWS Budget of $1** with an email alert as a safety net. Budgets themselves are free (ask before creating).

## Status

| Step | Status |
|---|---|
| CLAUDE.md, PLAN.md, DATA_MODEL.md, openapi.yaml | ✅ drafted |
| 0.1 Tools | ✅ Go 1.27.2, SAM 1.168.0. DynamoDB Local runs as a Java jar (Docker optional) |
| 0.2 Backend scaffold + template.yaml (Cognito, HTTP API, /health) | ✅ vet, test, validate --lint, build pass |
| 0.3 Frontend shell | ✅ builds |
| 1.1 Auth & roles (Cognito, JWT authorizer, `internal/auth`, `/me`, seed users, login UI) | ✅ code + unit tests. ⏳ not yet deployed |
| 1.2 Hospitals & resources (+ 3.1 TTL cache, done early) | ✅ code + unit tests. ⏳ not yet deployed |
| 1.3 Patient records + audit | ✅ code + unit tests. ⏳ not yet deployed |
| 2.1 Race-safe admissions + concurrency proof | ✅ 50-way race → exactly 1 admitted (DynamoDB Local, repeated 5×) |
| Everything else | ⏳ not started |
