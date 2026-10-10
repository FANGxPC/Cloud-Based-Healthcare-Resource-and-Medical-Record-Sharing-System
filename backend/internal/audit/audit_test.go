package audit

import (
	"context"
	"strings"
	"testing"
	"time"

	"healthcloud/internal/auth"
	"healthcloud/internal/models"
)

type fakeStore struct{ events []models.AuditEvent }

func (f *fakeStore) Put(_ context.Context, e models.AuditEvent) error {
	f.events = append(f.events, e)
	return nil
}

func TestRecord(t *testing.T) {
	store := &fakeStore{}
	fixed := time.Date(2026, 10, 10, 18, 30, 0, 0, time.UTC)
	r := Recorder{Store: store, Now: func() time.Time { return fixed }}

	doctor := auth.Principal{UserID: "doc-1", Groups: []auth.Group{auth.Doctor}, HospitalID: "H001"}
	if err := r.Record(context.Background(), doctor, "pat-9", models.ActionViewProfile, "", "req-1"); err != nil {
		t.Fatal(err)
	}

	e := store.events[0]
	if e.PatientID != "pat-9" || e.ActorID != "doc-1" || e.ActorGroup != "Doctor" ||
		e.ActorHospitalID != "H001" || e.Action != models.ActionViewProfile || e.Timestamp != "2026-10-10T18:30:00Z" {
		t.Fatalf("unexpected event %+v", e)
	}
	if !strings.HasPrefix(e.EventKey, "2026-10-10T18:30:00Z#") {
		t.Fatalf("eventKey %q must start with the timestamp so it sorts by time", e.EventKey)
	}
}

func TestEventKeysUniqueInSameSecond(t *testing.T) {
	store := &fakeStore{}
	fixed := time.Now()
	r := Recorder{Store: store, Now: func() time.Time { return fixed }}
	p := auth.Principal{UserID: "d", Groups: []auth.Group{auth.Doctor}}
	for range 2 {
		_ = r.Record(context.Background(), p, "x", models.ActionViewProfile, "", "")
	}
	if store.events[0].EventKey == store.events[1].EventKey {
		t.Fatal("two events in the same second got the same key")
	}
}
