# HealthCloud: Healthcare Resource & Medical Record Sharing (AWS Serverless)

University cloud project. Hospitals in one chain share **synthetic** patient records and track ICU beds,
general beds and ventilators in real time. Fully serverless and pay-per-request (zero idle cost).

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

# 3. Local DynamoDB (from Phase 1.2 on)
docker compose up -d           # http://localhost:8000

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
