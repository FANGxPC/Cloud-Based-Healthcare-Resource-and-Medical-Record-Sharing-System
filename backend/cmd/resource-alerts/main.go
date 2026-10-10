// Command resource-alerts consumes the Resources table's DynamoDB Stream and emails an alert
// (via SNS) when a hospital's available count DROPS BELOW its threshold.
//
// Why a stream instead of publishing from the API code:
//   - every path that changes availability (admission, transfer, admin edit) is covered
//     automatically, with no risk of forgetting one
//   - the API response is never slowed down or failed by email delivery
//   - Lambda reads from DynamoDB Streams for free
//
// It alerts only on the CROSSING (old >= threshold, new < threshold), not on every change while
// the count stays low, so admins get one email per incident rather than one per bed.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sns"

	"healthcloud/internal/logging"
	"healthcloud/internal/notify"
)

type app struct {
	pub notify.Publisher
	log *slog.Logger
}

// snapshot is the part of a Resources row we need from a stream image.
type snapshot struct {
	hospitalID, hospitalName, resourceType string
	available, threshold, total            int
}

func parseImage(img map[string]events.DynamoDBAttributeValue) (snapshot, bool) {
	n := func(k string) (int, bool) {
		v, ok := img[k]
		if !ok || v.DataType() != events.DataTypeNumber {
			return 0, false
		}
		i, err := strconv.Atoi(v.Number())
		return i, err == nil
	}
	s := func(k string) string {
		if v, ok := img[k]; ok && v.DataType() == events.DataTypeString {
			return v.String()
		}
		return ""
	}
	avail, ok1 := n("available")
	th, ok2 := n("threshold")
	total, _ := n("total")
	return snapshot{
		hospitalID: s("hospitalId"), hospitalName: s("hospitalName"), resourceType: s("resourceType"),
		available: avail, threshold: th, total: total,
	}, ok1 && ok2
}

// crossedBelow is the alert rule. It is a pure function, so it is easy to unit-test.
func crossedBelow(oldS, newS snapshot) bool {
	return oldS.available >= oldS.threshold && newS.available < newS.threshold
}

// handle processes a batch. It reports individual failed records (partial batch response), so
// one bad record doesn't make Lambda re-send the whole batch and email admins twice.
func (a *app) handle(ctx context.Context, ev events.DynamoDBEvent) (events.DynamoDBEventResponse, error) {
	var resp events.DynamoDBEventResponse
	for _, rec := range ev.Records {
		if rec.EventName != "MODIFY" {
			continue
		}
		oldS, ok1 := parseImage(rec.Change.OldImage)
		newS, ok2 := parseImage(rec.Change.NewImage)
		if !ok1 || !ok2 || !crossedBelow(oldS, newS) {
			continue
		}

		name := newS.hospitalName
		if name == "" {
			name = newS.hospitalID
		}
		subject := fmt.Sprintf("[HealthCloud] LOW %s at %s", newS.resourceType, newS.hospitalID)
		msg := fmt.Sprintf("Low resource alert\n\nHospital:  %s (%s)\nResource:  %s\nAvailable: %d of %d (alert threshold %d)\n\nConsider diverting new admissions or requesting transfers.\n(Synthetic demo system. This message contains no patient data.)",
			name, newS.hospitalID, newS.resourceType, newS.available, newS.total, newS.threshold)

		if err := a.pub.Publish(ctx, subject, msg, newS.hospitalID, "LOW_RESOURCE"); err != nil {
			a.log.ErrorContext(ctx, "publish failed", "hospitalId", newS.hospitalID, "error", err.Error())
			resp.BatchItemFailures = append(resp.BatchItemFailures, events.DynamoDBBatchItemFailure{ItemIdentifier: rec.Change.SequenceNumber})
			continue
		}
		a.log.InfoContext(ctx, "low resource alert sent",
			"hospitalId", newS.hospitalID, "resourceType", newS.resourceType,
			"available", newS.available, "threshold", newS.threshold)
	}
	return resp, nil
}

func main() {
	log := logging.New("resource-alerts", os.Getenv("STAGE"))
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		log.Error("startup failed", "error", err.Error())
		os.Exit(1)
	}
	a := &app{
		pub: notify.SNS{Client: sns.NewFromConfig(cfg), TopicArn: os.Getenv("ALERTS_TOPIC_ARN")},
		log: log,
	}
	lambda.Start(a.handle)
}
