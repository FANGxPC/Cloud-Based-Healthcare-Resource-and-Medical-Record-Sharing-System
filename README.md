# HealthCloud: Healthcare Resource & Medical Record Sharing (AWS Serverless)

University cloud project. Hospitals in one chain share **synthetic** patient records and track ICU beds,
general beds and ventilators in real time. Fully serverless and pay-per-request (zero idle cost).

- Architecture (report/viva): [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)
- Plan: [docs/PLAN.md](docs/PLAN.md)
- Data model: [docs/DATA_MODEL.md](docs/DATA_MODEL.md)
- API: [api/openapi.yaml](api/openapi.yaml)

## Prerequisites

Go (see `backend/go.mod`), AWS SAM CLI, Docker Desktop, Node 22+.

## Run locally

```bash
# 1. Backend checks
cd backend
go vet ./...
go test ./...

# 2. Build and serve the API locally (needs Docker; serves http://127.0.0.1:3000)
cd ..
sam validate --lint
sam build
sam local start-api            # then: curl http://127.0.0.1:3000/health

# 3. Local DynamoDB on http://localhost:8000. Either Docker:
docker compose up -d
#    or the plain Java jar (no Docker needed), from https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/DynamoDBLocal.DownloadingAndRunning.html
java -Djava.library.path=./DynamoDBLocal_lib -jar DynamoDBLocal.jar -inMemory -sharedDb -port 8000

# 3b. Race-safety proof: 50 concurrent admissions for the last bed, exactly 1 succeeds
cd backend
DYNAMODB_ENDPOINT=http://localhost:8000 go test ./internal/db -run Integration -v
cd ..

# 4. Frontend (http://localhost:5173, /api is proxied to the local API)
cd frontend
cp .env.example .env.local
npm install
npm run dev
```

## Deploy

Deploying is always a deliberate, manual step:

```bash
sam deploy --guided --parameter-overrides Stage=dev     # day-to-day stack
sam deploy --guided --parameter-overrides Stage=demo    # stable stack for presentations
```

Copy the stack outputs (`ApiUrl`, `UserPoolId`, `UserPoolClientId`) into `frontend/.env.local`.
`samconfig.toml` and `.env.local` are git-ignored.

## Seed synthetic data

```bash
cd scripts/seed
go run . -local                                   # DynamoDB Local: creates tables + data
SEED_PASSWORD='<choose one>' go run . -stage dev -user-pool-id <UserPoolId> -region us-east-1   # AWS
```

This creates 5 hospitals, their beds and ventilators, 50 synthetic patients, and these demo logins
(all with `SEED_PASSWORD`):

| Login | Role |
|---|---|
| `doctor.h001@example.com` … `doctor.h005@example.com` | Doctor at that hospital |
| `admin.h001@example.com` … `admin.h005@example.com` | HospitalAdmin of that hospital |
| `patient1@example.com` … `patient3@example.com` | Patient (record at H001) |
