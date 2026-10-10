// Command transfers-api is the Lambda behind the emergency-transfer endpoints:
//
//	POST /transfers                       → store QUEUED, send to SQS, return 202
//	GET  /transfers?direction=…           → incoming/outgoing for the caller's hospital
//	GET  /transfers/{transferId}          → poll status
//	POST /transfers/{transferId}/decision → destination admin accepts or rejects
//
// The slow part (holding a bed and emailing the destination) is done asynchronously by
// transfer-worker, so a burst of requests can't overload the API and is never lost: messages
// wait in SQS, and failures land in the dead-letter queue.
package main

import (
	"context"
	"encoding/json"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"healthcloud/internal/db"
	"healthcloud/internal/logging"
	"healthcloud/internal/models"
	"healthcloud/internal/notify"
)

// sqsQueue sends transfer messages to the queue.
type sqsQueue struct {
	c   *sqs.Client
	url string
}

func (q sqsQueue) Send(ctx context.Context, m models.TransferMessage) error {
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = q.c.SendMessage(ctx, &sqs.SendMessageInput{QueueUrl: aws.String(q.url), MessageBody: aws.String(string(body))})
	return err
}

func main() {
	log := logging.New("transfers-api", os.Getenv("STAGE"))
	ctx := context.Background()

	client, err := db.NewClient(ctx)
	if err != nil {
		log.Error("startup failed", "error", err.Error())
		os.Exit(1)
	}
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Error("startup failed", "error", err.Error())
		os.Exit(1)
	}
	tables := db.TablesFromEnv()

	a := &app{
		transfers: db.Transfers{C: client, Tables: tables},
		patients:  db.Patients{C: client, Table: tables.Patients},
		hospitals: db.Hospitals{C: client, Table: tables.Hospitals},
		queue:     sqsQueue{c: sqs.NewFromConfig(cfg), url: os.Getenv("TRANSFER_QUEUE_URL")},
		pub:       notify.SNS{Client: sns.NewFromConfig(cfg), TopicArn: os.Getenv("ALERTS_TOPIC_ARN")},
		log:       log,
	}
	lambda.Start(a.routes().Serve)
}
