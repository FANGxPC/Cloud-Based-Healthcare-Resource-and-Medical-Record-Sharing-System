// Command patients is the Lambda behind identity, patient-record and audit endpoints:
//
//	GET /me
//	GET /patients, GET /patients/{patientId}
//	GET /patients/{patientId}/access-log, GET /audit
//	GET|POST /patients/{patientId}/documents, GET …/documents/{documentId}, POST …/complete
//
// Handlers live in handlers.go. This file only wires up dependencies.
package main

import (
	"context"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"healthcloud/internal/audit"
	"healthcloud/internal/db"
	"healthcloud/internal/logging"
	"healthcloud/internal/storage"
)

func main() {
	log := logging.New("patients", os.Getenv("STAGE"))

	client, err := db.NewClient(context.Background())
	if err != nil {
		log.Error("startup failed", "error", err.Error())
		os.Exit(1)
	}
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		log.Error("startup failed", "error", err.Error())
		os.Exit(1)
	}
	tables := db.TablesFromEnv()
	auditLog := db.AuditLog{C: client, Table: tables.AuditLog}

	a := &app{
		patients:  db.Patients{C: client, Table: tables.Patients},
		auditLog:  auditLog,
		recorder:  audit.Recorder{Store: auditLog},
		documents: db.Documents{C: client, Tables: tables},
		files:     storage.S3{Client: s3.NewFromConfig(cfg), Bucket: os.Getenv("DOCUMENTS_BUCKET")},
		log:       log,
	}
	lambda.Start(a.routes().Serve)
}
