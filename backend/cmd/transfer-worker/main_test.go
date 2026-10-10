package main

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-lambda-go/events"

	"healthcloud/internal/db"
	"healthcloud/internal/models"
	tu "healthcloud/internal/testutil"
)

type fakeStore struct {
	tr      models.Transfer
	holdErr error
	status  models.TransferStatus
}

func (f *fakeStore) Get(_ context.Context, id string) (models.Transfer, error) {
	if id != f.tr.TransferID {
		return models.Transfer{}, db.ErrNotFound
	}
	return f.tr, nil
}
func (f *fakeStore) HoldBed(context.Context, models.Transfer) error { return f.holdErr }
func (f *fakeStore) SetStatus(_ context.Context, _ string, _, to models.TransferStatus, _ string) error {
	f.status = to
	return nil
}

type fakePub struct{ kinds []string }

func (p *fakePub) Publish(_ context.Context, _, _, _, kind string) error {
	p.kinds = append(p.kinds, kind)
	return nil
}

func run(t *testing.T, store *fakeStore, body string) (events.SQSEventResponse, *fakePub) {
	t.Helper()
	pub := &fakePub{}
	a := &app{transfers: store, pub: pub, log: tu.Logger()}
	resp, err := a.handle(context.Background(), events.SQSEvent{Records: []events.SQSMessage{{MessageId: "m1", Body: body}}})
	if err != nil {
		t.Fatal(err)
	}
	return resp, pub
}

func queued() models.Transfer {
	return models.Transfer{TransferID: "T1", FromHospitalID: "H001", ToHospitalID: "H002", Status: models.TransferQueued}
}

func TestBedHeldNotifiesDestination(t *testing.T) {
	resp, pub := run(t, &fakeStore{tr: queued()}, `{"transferId":"T1"}`)
	if len(resp.BatchItemFailures) != 0 || len(pub.kinds) != 1 || pub.kinds[0] != "TRANSFER_REQUEST" {
		t.Fatalf("failures=%v notifications=%v", resp.BatchItemFailures, pub.kinds)
	}
}

func TestNoCapacityMarksAndNotifiesOrigin(t *testing.T) {
	store := &fakeStore{tr: queued(), holdErr: db.ErrNoCapacity}
	resp, pub := run(t, store, `{"transferId":"T1"}`)
	if len(resp.BatchItemFailures) != 0 || store.status != models.TransferNoCapacity || pub.kinds[0] != "TRANSFER_NO_CAPACITY" {
		t.Fatalf("failures=%v status=%s notifications=%v", resp.BatchItemFailures, store.status, pub.kinds)
	}
}

func TestDuplicateIsSkipped(t *testing.T) {
	tr := queued()
	tr.Status = models.TransferPendingApproval // already processed by an earlier delivery
	resp, pub := run(t, &fakeStore{tr: tr}, `{"transferId":"T1"}`)
	if len(resp.BatchItemFailures) != 0 || len(pub.kinds) != 0 {
		t.Fatal("a duplicate delivery must do nothing")
	}
}

func TestTransientErrorIsRetried(t *testing.T) {
	resp, _ := run(t, &fakeStore{tr: queued(), holdErr: errors.New("throttled")}, `{"transferId":"T1"}`)
	if len(resp.BatchItemFailures) != 1 || resp.BatchItemFailures[0].ItemIdentifier != "m1" {
		t.Fatalf("transient error must be reported for retry: %+v", resp.BatchItemFailures)
	}
}

func TestMalformedMessageIsDropped(t *testing.T) {
	resp, _ := run(t, &fakeStore{tr: queued()}, `not json`)
	if len(resp.BatchItemFailures) != 0 {
		t.Fatal("malformed messages can't succeed on retry and must not be retried")
	}
}
