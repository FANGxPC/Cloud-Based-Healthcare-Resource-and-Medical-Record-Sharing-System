# Data Model — DynamoDB

All tables use **on-demand billing (`PAY_PER_REQUEST`)** and the default AWS-owned encryption key (free).
Each table name is suffixed with the stage, e.g. `Patients-dev` and `Patients-demo`.

Design approach: **one table per entity** (the brief asks for six named tables), each keyed for its
main access pattern, with GSIs only where a real screen or endpoint needs a second lookup.
Every access pattern below is served by `GetItem` or `Query`. The two exceptions use `Scan` on tiny tables
(`Hospitals`, and `Resources` for the chain-wide view behind a 15 s cache) and are marked as such.

Conventions:
- IDs are ULIDs (sortable by creation time) except `patientId`, which is the **Cognito `sub`** for
  patients who have a login. Synthetic patients without a login get a random UUID.
- Timestamps are ISO-8601 UTC strings (`2026-10-10T18:30:00Z`). These sort correctly as strings.
- `resourceType` ∈ `ICU_BED`, `GENERAL_BED`, `VENTILATOR`.

---

## 1. Hospitals

| Key | Attribute | Example |
|---|---|---|
| PK | `hospitalId` (S) | `H001` |

Attributes: `name`, `city`, `address`, `phone`, `createdAt`.

| # | Access pattern | Operation |
|---|---|---|
| H1 | Get one hospital | `GetItem(hospitalId)` |
| H2 | List all hospitals in the chain | `Scan`. Acceptable because the chain has only about 5–20 rows |

---

## 2. Resources (bed and ventilator availability)

| Key | Attribute | Example |
|---|---|---|
| PK | `hospitalId` (S) | `H001` |
| SK | `resourceType` (S) | `ICU_BED` |

Attributes: `total` (N), `available` (N), `threshold` (N, alert when `available < threshold`),
`version` (N, optimistic lock for admin edits), `updatedAt`, `updatedBy`.

**GSI `byType`**: PK `resourceType`, SK `hospitalId`, projection ALL.

| # | Access pattern | Operation |
|---|---|---|
| R1 | All resources of one hospital | `Query(PK=hospitalId)` |
| R2 | One resource row | `GetItem(hospitalId, resourceType)` |
| R3 | "Which hospitals have ICU beds?" (one type across the chain) | `Query byType(PK=ICU_BED)`, filtered in code to `available > 0` |
| R4 | Chain-wide availability dashboard | `Scan` (at most 20 hospitals × 3 types = 60 items), **cached 15 s in the Lambda** |
| R5 | **Use one unit (race-safe)** | `UpdateItem SET available = available - :one` **`CONDITION available > :zero`** |
| R6 | Release one unit | `UpdateItem SET available = available + :one` `CONDITION available < total` |
| R7 | Admin edits total or threshold | `UpdateItem ... CONDITION version = :expected`, then `version + 1` |

**Why R5 can't overbook:** DynamoDB evaluates the condition and applies the update atomically on the
item. If two requests race for the last bed, exactly one succeeds. The other gets
`ConditionalCheckFailedException`, which the API maps to `409 NO_CAPACITY`. Reading the count first and
then writing it back would be a race; the conditional write is not.

**Stream:** `NEW_AND_OLD_IMAGES`, consumed by the `resource-alerts` Lambda. An alert fires only when
`old.available >= threshold && new.available < threshold`, that is, on the **crossing**, so a hospital is not
spammed with an email for every bed while it stays low.

---

## 3. Patients

| Key | Attribute | Example |
|---|---|---|
| PK | `patientId` (S) | Cognito sub, e.g. `a1b2c3d4-...` |

Attributes: `firstName`, `lastName`, `dateOfBirth`, `sex`, `bloodGroup`, `allergies` (L),
`conditions` (L), `emergencyContact` (M), `homeHospitalId`, `currentHospitalId`,
`admission` (M or absent: `{hospitalId, resourceType, admittedAt}`), `createdAt`, `updatedAt`.
All values are **synthetic**.

**GSI `byHospital`**: PK `currentHospitalId`, SK `lastName` (S), projection
INCLUDE (`firstName`, `dateOfBirth`, `admission`).

| # | Access pattern | Operation |
|---|---|---|
| P1 | Patient views own profile | `GetItem(patientId = sub)` |
| P2 | Doctor opens any patient (**audited**) | `GetItem(patientId)` plus an AuditLog write |
| P3 | Doctor lists patients currently at a hospital | `Query byHospital(PK=hospitalId)`, sorted by surname. Also supports `begins_with(lastName, :prefix)` search |
| P4 | Admit to a bed | `TransactWriteItems`: R5 on Resources **and** `Update Patients SET admission CONDITION attribute_not_exists(admission)` |
| P5 | Discharge | `TransactWriteItems`: R6 **and** `REMOVE admission CONDITION admission.hospitalId = :h` |

The transaction in P4 means a patient can't hold two beds, and a bed can't be taken without being
recorded against a patient. Either both writes happen or neither does.

---

## 4. Transfers

| Key | Attribute | Example |
|---|---|---|
| PK | `transferId` (S, ULID) | `01JA...` |

Attributes: `patientId`, `fromHospitalId`, `toHospitalId`, `resourceType`, `priority`
(`CRITICAL` / `URGENT` / `ROUTINE`), `reason`, `status`, `requestedBy` (sub),
`decidedBy`, `decisionNote`, `createdAt`, `updatedAt`.

**Status lifecycle**

```
QUEUED ──worker──► PENDING_APPROVAL ──admin accept──► ACCEPTED
   │                  (bed held)     └─admin reject──► REJECTED (bed released)
   └──worker──► NO_CAPACITY / FAILED
```

**GSI `incoming`**: PK `toHospitalId`, SK `createdAt`
**GSI `outgoing`**: PK `fromHospitalId`, SK `createdAt`

| # | Access pattern | Operation |
|---|---|---|
| T1 | Get one transfer (status polling after 202) | `GetItem(transferId)` |
| T2 | Admin's incoming queue, newest first | `Query incoming(PK=hospitalId)`, `ScanIndexForward=false`, optional `status` filter |
| T3 | Doctor's outgoing requests | `Query outgoing(PK=hospitalId)` |
| T4 | Worker: process once (idempotent) | `TransactWriteItems`: R5 on destination **and** `Update Transfers SET status=PENDING_APPROVAL CONDITION status = QUEUED`. If the message is delivered again, the condition fails, so it is a no-op |
| T5 | Accept | `TransactWriteItems`: `Transfers status=ACCEPTED CONDITION status=PENDING_APPROVAL`, Patients `SET currentHospitalId, admission`, plus R6 on the origin if the patient was admitted there |
| T6 | Reject | `TransactWriteItems`: `Transfers status=REJECTED CONDITION status=PENDING_APPROVAL`, plus R6 on the destination |

---

## 5. Documents (metadata only; file bytes live in S3)

| Key | Attribute | Example |
|---|---|---|
| PK | `patientId` (S) | `a1b2...` |
| SK | `documentId` (S, ULID → time-ordered) | `01JA...` |

Attributes: `title`, `category` (`LAB_REPORT`, `IMAGING`, `PRESCRIPTION`, `DISCHARGE_SUMMARY`, `OTHER`),
`fileName`, `contentType`, `sizeBytes`, `s3Key`, `status` (`PENDING_UPLOAD` → `AVAILABLE`),
`uploadedBy`, `uploadedByHospitalId`, `createdAt`.

S3 key format: `patients/{patientId}/{documentId}/{sanitisedFileName}`. There's no GSI: documents are always
viewed in the context of one patient.

| # | Access pattern | Operation |
|---|---|---|
| D1 | List a patient's documents, newest first | `Query(PK=patientId, ScanIndexForward=false)` (audited for Doctors) |
| D2 | Get one document plus presigned download URL | `GetItem(patientId, documentId)`, then presign `GetObject` for 5 min (audited) |
| D3 | Start upload | `PutItem status=PENDING_UPLOAD`, then presign `PutObject` for 5 min, bound to `contentType` |
| D4 | Confirm upload | S3 `HeadObject` to check the object exists, then `UpdateItem status=AVAILABLE` |

---

## 6. AuditLog (append-only)

| Key | Attribute | Example |
|---|---|---|
| PK | `patientId` (S) | whose record was touched |
| SK | `eventKey` (S) = `{timestamp}#{ulid}` | `2026-10-10T18:30:00Z#01JA...` |

Attributes: `actorId` (sub), `actorGroup`, `actorHospitalId`, `action`
(`VIEW_PROFILE`, `LIST_DOCUMENTS`, `VIEW_DOCUMENT`, `UPLOAD_DOCUMENT`, `ADMIT`, `DISCHARGE`,
`REQUEST_TRANSFER`, `DECIDE_TRANSFER`), `targetId` (e.g. documentId), `timestamp`, `requestId`.

**GSI `byActorHospital`**: PK `actorHospitalId`, SK `eventKey`

| # | Access pattern | Operation |
|---|---|---|
| A1 | Write an event | `PutItem`. Items are never updated or deleted by the app |
| A2 | "Who accessed my record?" (Patient) | `Query(PK=sub)`, newest first |
| A3 | Admin reviews accesses made by their hospital's staff | `Query byActorHospital(PK=hospitalId)` |

The audit write is done **synchronously before** the record is returned. If the audit write fails, the
read fails (`500`), so there is never an unaudited read.

---

## Cost notes

- On-demand DynamoDB has no idle cost. The 25 GB always-free storage allowance is far more than synthetic
  data needs.
- Each GSI adds a write per item write. There are only 7 GSIs in total, all on low-write tables.
- Transactions cost twice as many write units as plain writes. That's negligible at demo volumes.
- DynamoDB Streams reads by a Lambda trigger are free.
