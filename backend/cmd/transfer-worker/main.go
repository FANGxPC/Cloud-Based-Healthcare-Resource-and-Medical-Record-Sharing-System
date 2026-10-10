// Command transfer-worker consumes the transfer SQS queue. For each queued transfer it:
//
//  1. re-reads the transfer from DynamoDB (the message carries only the ID)
//  2. holds a bed at the destination (race-safe, idempotent; see db.Transfers.HoldBed)
//  3. emails the destination hospital (pending approval) or the origin (no capacity) via SNS
//
// Retries and failure handling:
//   - transient errors (e.g. DynamoDB throttling) → the message is reported as failed and SQS
//     redelivers it later. After 3 failed receives, SQS moves it to the dead-letter queue, where a
//     CloudWatch alarm notices it
//   - duplicate deliveries → harmless: the transfer is no longer QUEUED, so it is skipped
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sns"

	"healthcloud/internal/db"
	"healthcloud/internal/logging"
	"healthcloud/internal/models"
	"healthcloud/internal/notify"
)

type transferStore interface {
	Get(ctx context.Context, id string) (models.Transfer, error)
	HoldBed(ctx context.Context, tr models.Transfer) error
	SetStatus(ctx context.Context, id string, from, to models.TransferStatus, note string) error
}

type app struct {
	transfers transferStore
	pub       notify.Publisher
	log       *slog.Logger
}

// handle processes a batch and returns only the failed messages (partial batch response), so
// successful ones are not reprocessed.
func (a *app) handle(ctx context.Context, ev events.SQSEvent) (events.SQSEventResponse, error) {
	var resp events.SQSEventResponse
	for _, msg := range ev.Records {
		if err := a.process(ctx, msg.Body); err != nil {
			a.log.ErrorContext(ctx, "transfer processing failed; will retry",
				"messageId", msg.MessageId, "receiveCount", msg.Attributes["ApproximateReceiveCount"], "error", err.Error())
			resp.BatchItemFailures = append(resp.BatchItemFailures, events.SQSBatchItemFailure{ItemIdentifier: msg.MessageId})
		}
	}
	return resp, nil
}

// process returns an error ONLY for failures worth retrying.
func (a *app) process(ctx context.Context, body string) error {
	var m models.TransferMessage
	if err := json.Unmarshal([]byte(body), &m); err != nil || m.TransferID == "" {
		a.log.ErrorContext(ctx, "dropping malformed message", "body_len", len(body))
		return nil // retrying can't fix a malformed message
	}

	tr, err := a.transfers.Get(ctx, m.TransferID)
	if errors.Is(err, db.ErrNotFound) {
		a.log.WarnContext(ctx, "transfer not found; dropping", "transferId", m.TransferID)
		return nil
	}
	if err != nil {
		return err
	}
	if tr.Status != models.TransferQueued {
		a.log.InfoContext(ctx, "already processed; skipping duplicate", "transferId", tr.TransferID, "status", tr.Status)
		return nil
	}

	switch err := a.transfers.HoldBed(ctx, tr); {
	case err == nil:
		a.log.InfoContext(ctx, "bed held; awaiting approval", "transferId", tr.TransferID, "to", tr.ToHospitalID)
		a.notify(ctx, tr.ToHospitalID, "TRANSFER_REQUEST",
			fmt.Sprintf("[HealthCloud] %s transfer request from %s", tr.Priority, tr.FromHospitalID),
			fmt.Sprintf("Incoming transfer request %s\n\nFrom:     %s\nTo:       %s\nResource: %s (one unit is now HELD for this patient)\nPriority: %s\n\nPlease accept or reject it in HealthCloud (Incoming transfers).\n(Synthetic demo system. No patient data in this message.)",
				tr.TransferID, tr.FromHospitalID, tr.ToHospitalID, tr.ResourceType, tr.Priority))
		return nil

	case errors.Is(err, db.ErrInvalidState):
		return nil // a concurrent duplicate got there first

	case errors.Is(err, db.ErrNoCapacity):
		if err := a.transfers.SetStatus(ctx, tr.TransferID, models.TransferQueued, models.TransferNoCapacity,
			"no "+string(tr.ResourceType)+" available at "+tr.ToHospitalID); err != nil && !errors.Is(err, db.ErrInvalidState) {
			return err
		}
		a.log.InfoContext(ctx, "no capacity at destination", "transferId", tr.TransferID, "to", tr.ToHospitalID)
		a.notify(ctx, tr.FromHospitalID, "TRANSFER_NO_CAPACITY",
			fmt.Sprintf("[HealthCloud] Transfer to %s failed: no %s", tr.ToHospitalID, tr.ResourceType),
			fmt.Sprintf("Transfer %s could not be placed: %s has no %s available.\nPlease choose another hospital.\n(Synthetic demo system. No patient data in this message.)",
				tr.TransferID, tr.ToHospitalID, tr.ResourceType))
		return nil

	default:
		return err // transient: let SQS retry
	}
}

// notify is best-effort. The status change is already saved, and retrying the whole message
// would be skipped as a duplicate anyway, so a failed email is logged, not retried.
func (a *app) notify(ctx context.Context, hospitalID, kind, subject, msg string) {
	if err := a.pub.Publish(ctx, subject, msg, hospitalID, kind); err != nil {
		a.log.WarnContext(ctx, "notification failed", "kind", kind, "hospitalId", hospitalID, "error", err.Error())
	}
}

func main() {
	log := logging.New("transfer-worker", os.Getenv("STAGE"))
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
	a := &app{
		transfers: db.Transfers{C: client, Tables: db.TablesFromEnv()},
		pub:       notify.SNS{Client: sns.NewFromConfig(cfg), TopicArn: os.Getenv("ALERTS_TOPIC_ARN")},
		log:       log,
	}
	lambda.Start(a.handle)
}
