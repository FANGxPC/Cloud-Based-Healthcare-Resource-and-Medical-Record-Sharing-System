// Package audit records who accessed which patient record, when, and from which hospital.
//
// Handlers call Record *before* returning patient data. If the audit write fails, the handler
// returns 500 instead of the data. This "fail closed" design means there is never an
// unaudited read of a medical record.
package audit

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"

	"healthcloud/internal/auth"
	"healthcloud/internal/models"
)

// Store is where events go. db.AuditLog implements it; tests use a fake.
type Store interface {
	Put(ctx context.Context, e models.AuditEvent) error
}

// Recorder builds and stores audit events.
type Recorder struct {
	Store Store
	Now   func() time.Time // injectable clock for tests; nil means time.Now
}

// Record appends one event for patientID. targetID is optional (e.g. a documentId).
func (r Recorder) Record(ctx context.Context, actor auth.Principal, patientID string,
	action models.AuditAction, targetID, requestID string) error {

	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	return r.Store.Put(ctx, NewEvent(actor, patientID, action, targetID, requestID, now()))
}

// NewEvent builds an event without storing it. Write operations (admit, discharge, transfers)
// put it in the SAME DynamoDB transaction as the change, so the change and its audit record
// succeed or fail together.
func NewEvent(actor auth.Principal, patientID string, action models.AuditAction,
	targetID, requestID string, at time.Time) models.AuditEvent {

	ts := at.UTC().Format(time.RFC3339)
	return models.AuditEvent{
		PatientID: patientID,
		// Timestamp first so the sort key orders events by time. The ULID makes it unique
		// even when two events happen in the same second.
		EventKey:        ts + "#" + ulid.Make().String(),
		ActorID:         actor.UserID,
		ActorGroup:      string(actor.PrimaryGroup()),
		ActorHospitalID: actor.HospitalID,
		Action:          action,
		TargetID:        targetID,
		Timestamp:       ts,
		RequestID:       requestID,
	}
}
