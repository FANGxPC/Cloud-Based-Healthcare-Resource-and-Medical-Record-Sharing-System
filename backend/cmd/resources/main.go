// Command resources is the Lambda behind the hospital and resource-availability endpoints:
//
//	GET /hospitals, GET /hospitals/{hospitalId}
//	GET /resources (cached ~15 s), GET /hospitals/{hospitalId}/resources
//	PUT /hospitals/{hospitalId}/resources/{resourceType}
//	POST /hospitals/{hospitalId}/admissions, DELETE /hospitals/{hospitalId}/admissions/{patientId}
//
// Handlers live in handlers.go. This file only wires up dependencies.
package main

import (
	"context"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda"

	"healthcloud/internal/cache"
	"healthcloud/internal/db"
	"healthcloud/internal/logging"
	"healthcloud/internal/models"
)

func main() {
	log := logging.New("resources", os.Getenv("STAGE"))

	// The client and app are created once per container (cold start) and reused by later invocations.
	client, err := db.NewClient(context.Background())
	if err != nil {
		log.Error("startup failed", "error", err.Error())
		os.Exit(1)
	}
	tables := db.TablesFromEnv()

	a := &app{
		hospitals:  db.Hospitals{C: client, Table: tables.Hospitals},
		resources:  db.Resources{C: client, Table: tables.Resources},
		admissions: db.Admissions{C: client, Tables: tables},
		cache:      &cache.Value[[]models.Resource]{TTL: 15 * time.Second},
		log:        log,
	}
	lambda.Start(a.routes().Serve)
}
