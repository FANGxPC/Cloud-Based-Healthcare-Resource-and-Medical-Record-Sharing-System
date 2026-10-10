# CLAUDE.md — Cloud-Based Healthcare Resource & Medical Record Sharing System

University cloud project, built by a **solo student developer**. A serverless AWS platform where
hospitals in one healthcare chain share patient medical records and track resources (ICU beds,
general beds, ventilators) in real time, with emergency transfer requests and alerts.

Key docs (keep them in sync with the code — they are used in the project report and viva):
- `docs/PLAN.md` — phased build plan and current status
- `docs/DATA_MODEL.md` — DynamoDB tables, keys, GSIs → access patterns
- `api/openapi.yaml` — every endpoint, schema and the Cognito group(s) allowed to call it

---

## Hard rules (non-negotiable)

1. **Zero cost.** The account is on the AWS **Free plan**. Use only pay-per-request or always-free services.
2. **Never use:** RDS, Aurora, ElastiCache, VPCs, NAT Gateways, EC2, customer-managed KMS keys,
   SNS SMS, or provisioned capacity of any kind (DynamoDB stays `PAY_PER_REQUEST`, and Lambda has no
   provisioned concurrency).
3. **Never deploy or touch AWS without asking first.** That covers `sam deploy`, `sam delete`, `sam sync`, and
   any `aws ...` command, including read-only ones against the real account. Local commands
   (`sam validate`, `sam build`, `sam local ...`, DynamoDB Local) are fine.
4. **Synthetic data only.** Use no real patient data anywhere: not in seeds, tests, screenshots or logs.
5. **Never commit secrets.** `.gitignore` already covers `.env*`, `samconfig.toml`, `env.json` and
   local frontend config. Never hard-code account IDs, pool IDs or emails in committed code. Pass them in as
   parameters or environment variables.
6. **Don't log PHI.** Logs carry IDs, never names, DOBs, diagnoses or file contents.

## Stack

| Layer | Choice |
|---|---|
| Backend | Go (latest stable, pinned in `backend/go.mod`), `aws-lambda-go`, `aws-sdk-go-v2` |
| Lambda | runtime `provided.al2023`, arch `arm64`, SAM `Metadata: BuildMethod: go1.x`, handler `bootstrap` |
| IaC | AWS SAM `template.yaml`. The `Stage` parameter (default `dev`) is suffixed on **every** resource name, e.g. `Patients-dev`, so the `demo` stack stays stable for presentations |
| API | API Gateway **HTTP API** + **JWT authorizer** (Cognito issuer, audience = app client ID) |
| Auth | Cognito User Pool. Groups `Patient`, `Doctor`, `HospitalAdmin`. Custom attribute `custom:hospital_id`. **Self sign-up disabled.** Users are created by the seed script |
| Data | DynamoDB on-demand: `Hospitals`, `Patients`, `Resources`, `Transfers`, `Documents`, `AuditLog` (see `docs/DATA_MODEL.md`) |
| Files | S3, SSE-S3, Block Public Access on. **Uploads and downloads only via presigned URLs.** Never stream file bytes through Lambda |
| Async | SQS transfer queue + DLQ (SSE-SQS). SNS topic with **EMAIL subscriptions only** |
| Alerts | DynamoDB Stream on `Resources` → `resource-alerts` Lambda → SNS when `available` crosses below `threshold` |
| Monitoring | Structured JSON logs (`log/slog` JSON handler), 1 CloudWatch dashboard, alarms on Lambda errors + DLQ depth. Log retention 7 days |
| Cache | In-memory TTL cache (~15 s) inside the `resources` Lambda for availability reads. No Redis |
| Frontend | React + Vite + TypeScript. `aws-amplify` v6 **only** for Cognito auth. Simple, clean UI |
| Optional | Amazon Bedrock (Amazon Nova Micro) for natural-language queries, behind the `EnableNLQuery` flag (default `false`) |

Default region: `us-east-1` (Nova Micro is available there). Change it only if the user says so.

## Auth & authorization conventions

- The frontend sends the Cognito **ID token** as `Authorization: Bearer <token>`. The ID token contains
  `cognito:groups` and `custom:hospital_id`. The access token does not carry custom attributes without a paid-tier trigger.
- The HTTP API JWT authorizer validates the signature, issuer and audience. **Authorization is enforced in Go**
  (`backend/internal/auth`) from the claims, never only in the frontend.
- `sub` is the user's identity. **For Patients, `patientId == Cognito sub`.**
- Rules:
  - **Patient**: read only their own profile, documents and access log (`patientId == sub`).
  - **Doctor**: read or upload records for **any** patient in the chain. **Every read writes an AuditLog
    entry** (who, what, when, hospital). Can admit or discharge at their own hospital and request transfers from it.
  - **HospitalAdmin**: manage resources for their **own `hospital_id` only**. Approve or reject transfers
    **into** their hospital. Admit or discharge at their own hospital.
- Unauthorized returns `403` with the standard error body. A missing or invalid token returns `401` (from API Gateway).

## Core behaviour conventions

- **Race-safe bed use:** every decrement is a DynamoDB conditional write
  `SET available = available - :one` with `ConditionExpression: available > :zero`. Use a
  `TransactWriteItems` call when the patient or transfer row must change atomically with it. A test must prove
  concurrent requests never overbook.
- **Transfers:** `POST /transfers` writes a `QUEUED` row, sends to SQS, and returns **202**. The
  `transfer-worker` Lambda holds a destination bed (conditional decrement) and sets `PENDING_APPROVAL`, or sets
  `NO_CAPACITY`, then publishes to SNS. The destination HospitalAdmin accepts (moves the patient) or
  rejects (releases the bed). Workers must be **idempotent**, because SQS delivers at least once.
- **SNS messages contain IDs and counts only (no PHI).** Each message has a `hospitalId` message attribute so
  email subscriptions can use filter policies.

## Repo layout

```
api/openapi.yaml
backend/
  go.mod
  cmd/<function>/main.go     # one dir per Lambda: resources, patients, transfers-api,
                             # transfer-worker, resource-alerts, (nlquery)
  internal/auth/             # claim parsing + authorization helpers
  internal/db/               # DynamoDB repositories
  internal/models/           # shared structs
  internal/cache/            # TTL cache
  internal/audit/            # AuditLog writer
  internal/httpx/            # (helpers) JSON responses, errors, routing
frontend/                    # React + Vite + TS
template.yaml                # SAM
scripts/seed/                # synthetic data generator + DynamoDB/Cognito seeding
docs/                        # PLAN.md, DATA_MODEL.md, report material
.github/workflows/ci.yml
```

## Way of working

- **Build vertically:** finish each feature end-to-end (OpenAPI → Lambda → template.yaml → frontend
  screen) before starting the next, so there is always a working demo. Follow the order in `docs/PLAN.md`.
- **Keep `api/openapi.yaml` in sync** with every handler change.
- **Small, focused changes.** Write clear comments explaining *why*. The user must explain every part in a viva.
- **Prefer local testing:** `sam local start-api`, DynamoDB Local in Docker.
- **After each phase or feature, run and fix everything:**
  ```
  cd backend && go vet ./... && go test ./...
  sam validate --lint
  sam build
  cd frontend && npm run build
  ```
  Then **stop and summarise what changed** so the user can review and commit. Do not commit unless asked.
- If a step needs tools that aren't installed (Go, SAM CLI, Docker), say so and don't skip silently.
