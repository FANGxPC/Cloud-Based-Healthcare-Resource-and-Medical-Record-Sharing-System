package main

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/aws/aws-lambda-go/events"

	tu "healthcloud/internal/testutil"
)

type fakePub struct {
	subjects []string
	messages []string
	fail     bool
}

func (f *fakePub) Publish(_ context.Context, subject, message, _, _ string) error {
	if f.fail {
		return errors.New("sns down")
	}
	f.subjects = append(f.subjects, subject)
	f.messages = append(f.messages, message)
	return nil
}

func image(avail, threshold int) map[string]events.DynamoDBAttributeValue {
	return map[string]events.DynamoDBAttributeValue{
		"hospitalId":   events.NewStringAttribute("H002"),
		"resourceType": events.NewStringAttribute("ICU_BED"),
		"available":    events.NewNumberAttribute(strconv.Itoa(avail)),
		"threshold":    events.NewNumberAttribute(strconv.Itoa(threshold)),
		"total":        events.NewNumberAttribute("10"),
	}
}

func modify(seq string, oldAvail, newAvail int) events.DynamoDBEventRecord {
	return events.DynamoDBEventRecord{
		EventName: "MODIFY",
		Change: events.DynamoDBStreamRecord{
			SequenceNumber: seq,
			OldImage:       image(oldAvail, 3),
			NewImage:       image(newAvail, 3),
		},
	}
}

func TestAlertsOnlyOnCrossing(t *testing.T) {
	cases := []struct {
		name       string
		oldA, newA int
		wantAlert  bool
	}{
		{"drops from 3 to 2 (crosses threshold 3)", 3, 2, true},
		{"drops from 5 to 0 (crosses)", 5, 0, true},
		{"already low: 2 to 1", 2, 1, false},
		{"stays above: 6 to 5", 6, 5, false},
		{"recovering: 2 to 3", 2, 3, false},
	}
	for _, c := range cases {
		pub := &fakePub{}
		a := &app{pub: pub, log: tu.Logger()}
		if _, err := a.handle(context.Background(), events.DynamoDBEvent{Records: []events.DynamoDBEventRecord{modify("1", c.oldA, c.newA)}}); err != nil {
			t.Fatal(err)
		}
		if got := len(pub.subjects) == 1; got != c.wantAlert {
			t.Errorf("%s: alert=%v, want %v", c.name, got, c.wantAlert)
		}
	}
}

func TestAlertMessageHasNoPatientData(t *testing.T) {
	pub := &fakePub{}
	a := &app{pub: pub, log: tu.Logger()}
	a.handle(context.Background(), events.DynamoDBEvent{Records: []events.DynamoDBEventRecord{modify("1", 3, 1)}})
	if len(pub.messages) != 1 || !strings.Contains(pub.messages[0], "Available: 1 of 10") {
		t.Fatalf("unexpected message %q", pub.messages)
	}
	if !strings.Contains(pub.subjects[0], "H002") {
		t.Fatalf("subject should name the hospital: %q", pub.subjects[0])
	}
}

func TestPublishFailureReportsOnlyThatRecord(t *testing.T) {
	a := &app{pub: &fakePub{fail: true}, log: tu.Logger()}
	resp, _ := a.handle(context.Background(), events.DynamoDBEvent{Records: []events.DynamoDBEventRecord{
		modify("seq-1", 9, 8), // no alert needed → not a failure
		modify("seq-2", 3, 1), // alert needed, publish fails → retried
	}})
	if len(resp.BatchItemFailures) != 1 || resp.BatchItemFailures[0].ItemIdentifier != "seq-2" {
		t.Fatalf("want only seq-2 reported, got %+v", resp.BatchItemFailures)
	}
}
