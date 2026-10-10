package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/oklog/ulid/v2"

	"healthcloud/internal/audit"
	"healthcloud/internal/auth"
	"healthcloud/internal/db"
	"healthcloud/internal/httpx"
	"healthcloud/internal/models"
	"healthcloud/internal/notify"
)

type transferStore interface {
	Create(ctx context.Context, tr models.Transfer, ev models.AuditEvent) error
	Get(ctx context.Context, id string) (models.Transfer, error)
	List(ctx context.Context, hospitalID string, incoming bool, status models.TransferStatus, limit int, token string) ([]models.Transfer, string, error)
	SetStatus(ctx context.Context, id string, from, to models.TransferStatus, note string) error
	Accept(ctx context.Context, tr models.Transfer, decidedBy, note string, ev models.AuditEvent) error
	Reject(ctx context.Context, tr models.Transfer, decidedBy, note string, ev models.AuditEvent) error
}

type patientGetter interface {
	Get(ctx context.Context, patientID string) (models.Patient, error)
}

type hospitalGetter interface {
	Get(ctx context.Context, id string) (models.Hospital, error)
}

type queue interface {
	Send(ctx context.Context, m models.TransferMessage) error
}

type app struct {
	transfers transferStore
	patients  patientGetter
	hospitals hospitalGetter
	queue     queue
	pub       notify.Publisher
	log       *slog.Logger
}

var (
	doctors = []auth.Group{auth.Doctor}
	staff   = []auth.Group{auth.Doctor, auth.HospitalAdmin}
	admins  = []auth.Group{auth.HospitalAdmin}
)

func (a *app) routes() *httpx.Router {
	rt := httpx.NewRouter(a.log)
	rt.Handle("POST /transfers", doctors, a.requestTransfer)
	rt.Handle("GET /transfers", staff, a.listTransfers)
	rt.Handle("GET /transfers/{transferId}", staff, a.getTransfer)
	rt.Handle("POST /transfers/{transferId}/decision", admins, a.decideTransfer)
	return rt
}

type createTransferRequest struct {
	PatientID    string              `json:"patientId"`
	ToHospitalID string              `json:"toHospitalId"`
	ResourceType models.ResourceType `json:"resourceType"`
	Priority     models.Priority     `json:"priority"`
	Reason       string              `json:"reason"`
}

// requestTransfer validates the request, stores it as QUEUED, enqueues it and returns 202.
// The origin hospital is ALWAYS the doctor's own (from the token), never taken from the body,
// so a doctor can't send requests on behalf of another hospital.
func (a *app) requestTransfer(ctx context.Context, r *httpx.Request) httpx.Response {
	from := r.Principal.HospitalID
	if from == "" {
		return httpx.Forbidden(r.RequestID(), "your account has no hospital assigned")
	}
	var body createTransferRequest
	if err := r.DecodeJSON(&body); err != nil {
		return httpx.BadRequest(r.RequestID(), "invalid JSON body")
	}
	switch {
	case body.PatientID == "" || body.ToHospitalID == "":
		return httpx.BadRequest(r.RequestID(), "patientId and toHospitalId are required")
	case !body.ResourceType.Valid():
		return httpx.BadRequest(r.RequestID(), "unknown resourceType")
	case !body.Priority.Valid():
		return httpx.BadRequest(r.RequestID(), "priority must be CRITICAL, URGENT or ROUTINE")
	case body.ToHospitalID == from:
		return httpx.BadRequest(r.RequestID(), "destination must be a different hospital")
	case len(body.Reason) > 500:
		return httpx.BadRequest(r.RequestID(), "reason is limited to 500 characters")
	}

	// Fail fast on bad IDs, so the doctor gets a clear 404 now rather than a FAILED status later.
	if _, err := a.patients.Get(ctx, body.PatientID); errors.Is(err, db.ErrNotFound) {
		return httpx.NotFound(r.RequestID(), "patient not found")
	} else if err != nil {
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}
	if _, err := a.hospitals.Get(ctx, body.ToHospitalID); errors.Is(err, db.ErrNotFound) {
		return httpx.NotFound(r.RequestID(), "destination hospital not found")
	} else if err != nil {
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	tr := models.Transfer{
		TransferID:     ulid.Make().String(),
		PatientID:      body.PatientID,
		FromHospitalID: from,
		ToHospitalID:   body.ToHospitalID,
		ResourceType:   body.ResourceType,
		Priority:       body.Priority,
		Reason:         body.Reason,
		Status:         models.TransferQueued,
		RequestedBy:    r.Principal.UserID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	ev := audit.NewEvent(r.Principal, tr.PatientID, models.ActionRequestTransfer, tr.TransferID, r.RequestID(), time.Now())

	// 1) Store first, so GET /transfers/{id} works immediately and nothing is lost if SQS fails.
	if err := a.transfers.Create(ctx, tr, ev); err != nil {
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}
	// 2) Enqueue. If that fails, mark the transfer FAILED instead of leaving it stuck in QUEUED.
	if err := a.queue.Send(ctx, models.TransferMessage{TransferID: tr.TransferID}); err != nil {
		_ = a.transfers.SetStatus(ctx, tr.TransferID, models.TransferQueued, models.TransferFailed, "could not enqueue")
		return httpx.Internal(ctx, a.log, r.RequestID(), fmt.Errorf("enqueue: %w", err))
	}

	a.log.InfoContext(ctx, "transfer queued", "requestId", r.RequestID(), "transferId", tr.TransferID,
		"from", tr.FromHospitalID, "to", tr.ToHospitalID, "priority", tr.Priority)

	res := httpx.JSON(http.StatusAccepted, tr)
	res.Headers["Location"] = "/transfers/" + tr.TransferID
	return res
}

func (a *app) listTransfers(ctx context.Context, r *httpx.Request) httpx.Response {
	hospitalID := r.Principal.HospitalID
	if hospitalID == "" {
		return httpx.Forbidden(r.RequestID(), "your account has no hospital assigned")
	}
	var incoming bool
	switch r.Query("direction") {
	case "incoming":
		incoming = true
	case "outgoing":
	default:
		return httpx.BadRequest(r.RequestID(), "direction must be incoming or outgoing")
	}
	status := models.TransferStatus(r.Query("status"))
	if status != "" && !status.Valid() {
		return httpx.BadRequest(r.RequestID(), "unknown status")
	}
	limit, err := r.Limit()
	if err != nil {
		return httpx.BadRequest(r.RequestID(), err.Error())
	}

	items, next, err := a.transfers.List(ctx, hospitalID, incoming, status, limit, r.Query("nextToken"))
	if errors.Is(err, db.ErrBadToken) {
		return httpx.BadRequest(r.RequestID(), "invalid nextToken")
	}
	if err != nil {
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}
	return httpx.List(items, next)
}

// getTransfer is visible only to staff of the two hospitals involved.
func (a *app) getTransfer(ctx context.Context, r *httpx.Request) httpx.Response {
	tr, resp := a.loadInvolved(ctx, r)
	if resp != nil {
		return *resp
	}
	return httpx.JSON(http.StatusOK, tr)
}

type decisionRequest struct {
	Decision string `json:"decision"`
	Note     string `json:"note"`
}

// decideTransfer lets the DESTINATION hospital's admin accept or reject.
func (a *app) decideTransfer(ctx context.Context, r *httpx.Request) httpx.Response {
	var body decisionRequest
	if err := r.DecodeJSON(&body); err != nil || (body.Decision != "ACCEPT" && body.Decision != "REJECT") {
		return httpx.BadRequest(r.RequestID(), "decision must be ACCEPT or REJECT")
	}
	if len(body.Note) > 500 {
		return httpx.BadRequest(r.RequestID(), "note is limited to 500 characters")
	}

	tr, err := a.transfers.Get(ctx, r.Path("transferId"))
	if errors.Is(err, db.ErrNotFound) {
		return httpx.NotFound(r.RequestID(), "transfer not found")
	}
	if err != nil {
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}
	if !r.Principal.WorksAt(tr.ToHospitalID) {
		return httpx.Forbidden(r.RequestID(), "only the destination hospital's admin can decide")
	}
	if tr.Status != models.TransferPendingApproval {
		return httpx.Conflict(r.RequestID(), "INVALID_STATE", "transfer is "+string(tr.Status)+", not PENDING_APPROVAL")
	}

	ev := audit.NewEvent(r.Principal, tr.PatientID, models.ActionDecideTransfer, tr.TransferID, r.RequestID(), time.Now())
	if body.Decision == "ACCEPT" {
		err = a.transfers.Accept(ctx, tr, r.Principal.UserID, body.Note, ev)
	} else {
		err = a.transfers.Reject(ctx, tr, r.Principal.UserID, body.Note, ev)
	}
	switch {
	case errors.Is(err, db.ErrInvalidState):
		return httpx.Conflict(r.RequestID(), "INVALID_STATE", "transfer was decided by someone else")
	case errors.Is(err, db.ErrConditionFailed):
		return httpx.Conflict(r.RequestID(), "PATIENT_CHANGED", "the patient's admission changed meanwhile; please retry")
	case errors.Is(err, db.ErrBusy):
		return httpx.Error(http.StatusServiceUnavailable, "BUSY", "high contention; please retry", r.RequestID())
	case err != nil:
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}

	updated, err := a.transfers.Get(ctx, tr.TransferID)
	if err != nil {
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}

	// Tell the requesting hospital. Best-effort: the decision is already saved, so an email
	// failure is only logged.
	verb := map[string]string{"ACCEPT": "ACCEPTED", "REJECT": "REJECTED"}[body.Decision]
	if err := a.pub.Publish(ctx,
		fmt.Sprintf("[HealthCloud] Transfer %s by %s", verb, tr.ToHospitalID),
		fmt.Sprintf("Transfer %s (%s, priority %s) from %s to %s was %s.\n\n(Synthetic demo system. No patient data in this message.)",
			tr.TransferID, tr.ResourceType, tr.Priority, tr.FromHospitalID, tr.ToHospitalID, verb),
		tr.FromHospitalID, "TRANSFER_DECISION"); err != nil {
		a.log.WarnContext(ctx, "decision notification failed", "transferId", tr.TransferID, "error", err.Error())
	}
	return httpx.JSON(http.StatusOK, updated)
}

// loadInvolved fetches a transfer and checks that the caller works at one of its two hospitals.
func (a *app) loadInvolved(ctx context.Context, r *httpx.Request) (models.Transfer, *httpx.Response) {
	tr, err := a.transfers.Get(ctx, r.Path("transferId"))
	if errors.Is(err, db.ErrNotFound) {
		res := httpx.NotFound(r.RequestID(), "transfer not found")
		return tr, &res
	}
	if err != nil {
		res := httpx.Internal(ctx, a.log, r.RequestID(), err)
		return tr, &res
	}
	if !r.Principal.WorksAt(tr.FromHospitalID) && !r.Principal.WorksAt(tr.ToHospitalID) {
		res := httpx.Forbidden(r.RequestID(), "this transfer does not involve your hospital")
		return tr, &res
	}
	return tr, nil
}
