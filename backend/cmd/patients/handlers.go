package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"healthcloud/internal/auth"
	"healthcloud/internal/db"
	"healthcloud/internal/httpx"
	"healthcloud/internal/models"
)

type patientStore interface {
	Get(ctx context.Context, patientID string) (models.Patient, error)
	ListByHospital(ctx context.Context, hospitalID, lastNamePrefix string, limit int, token string) ([]models.PatientSummary, string, error)
}

type auditReader interface {
	ListForPatient(ctx context.Context, patientID, onlyActorHospital string, limit int, token string) ([]models.AuditEvent, string, error)
	ListByActorHospital(ctx context.Context, hospitalID string, limit int, token string) ([]models.AuditEvent, string, error)
}

type auditRecorder interface {
	Record(ctx context.Context, actor auth.Principal, patientID string, action models.AuditAction, targetID, requestID string) error
}

type app struct {
	patients patientStore
	auditLog auditReader
	recorder auditRecorder
	log      *slog.Logger
}

var (
	everyone          = []auth.Group{auth.Patient, auth.Doctor, auth.HospitalAdmin}
	doctors           = []auth.Group{auth.Doctor}
	patientsOrDoctors = []auth.Group{auth.Patient, auth.Doctor}
	patientsOrAdmins  = []auth.Group{auth.Patient, auth.HospitalAdmin}
	admins            = []auth.Group{auth.HospitalAdmin}
)

func (a *app) routes() *httpx.Router {
	rt := httpx.NewRouter(a.log)
	rt.Handle("GET /me", everyone, a.getMe)
	rt.Handle("GET /patients", doctors, a.listPatients)
	rt.Handle("GET /patients/{patientId}", patientsOrDoctors, a.getPatient)
	rt.Handle("GET /patients/{patientId}/access-log", patientsOrAdmins, a.getAccessLog)
	rt.Handle("GET /audit", admins, a.listHospitalAudit)
	return rt
}

// canReadRecord is THE rule for patient data, shared by every record endpoint:
//   - a Patient may read only their own record (patientId == their Cognito sub)
//   - a Doctor may read any patient in the chain (sharing is the point of the system), but
//     every such read is audited by the caller
func canReadRecord(p auth.Principal, patientID string) bool {
	if p.Is(auth.Doctor) {
		return true
	}
	return p.Is(auth.Patient) && p.UserID == patientID
}

func (a *app) getMe(_ context.Context, r *httpx.Request) httpx.Response {
	p := r.Principal
	var hospitalID *string
	if p.HospitalID != "" {
		hospitalID = &p.HospitalID
	}
	return httpx.JSON(http.StatusOK, map[string]any{
		"userId":     p.UserID,
		"email":      p.Email,
		"groups":     p.Groups,
		"hospitalId": hospitalID,
	})
}

func (a *app) listPatients(ctx context.Context, r *httpx.Request) httpx.Response {
	hospitalID := r.Query("hospitalId")
	if hospitalID == "" {
		hospitalID = r.Principal.HospitalID // default: the doctor's own hospital
	}
	if hospitalID == "" {
		return httpx.BadRequest(r.RequestID(), "hospitalId is required")
	}
	limit, err := r.Limit()
	if err != nil {
		return httpx.BadRequest(r.RequestID(), err.Error())
	}

	items, next, err := a.patients.ListByHospital(ctx, hospitalID, r.Query("lastNamePrefix"), limit, r.Query("nextToken"))
	if err != nil {
		if errors.Is(err, db.ErrBadToken) {
			return httpx.BadRequest(r.RequestID(), "invalid nextToken")
		}
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}
	return httpx.List(items, next)
}

func (a *app) getPatient(ctx context.Context, r *httpx.Request) httpx.Response {
	patientID := r.Path("patientId")
	if !canReadRecord(r.Principal, patientID) {
		return httpx.Forbidden(r.RequestID(), "you can only view your own record")
	}

	p, err := a.patients.Get(ctx, patientID)
	if errors.Is(err, db.ErrNotFound) {
		return httpx.NotFound(r.RequestID(), "patient not found")
	}
	if err != nil {
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}

	// Doctor reads are audited BEFORE the data is returned. If the audit write fails, the read
	// fails too, so there is never an unaudited view of a medical record.
	if r.Principal.Is(auth.Doctor) {
		if err := a.recorder.Record(ctx, r.Principal, patientID, models.ActionViewProfile, "", r.RequestID()); err != nil {
			return httpx.Internal(ctx, a.log, r.RequestID(), err)
		}
	}
	return httpx.JSON(http.StatusOK, p)
}

// getAccessLog shows who viewed a patient's record:
//   - the Patient sees every access to their own record
//   - a HospitalAdmin sees only accesses made by staff of their own hospital (oversight of their staff)
func (a *app) getAccessLog(ctx context.Context, r *httpx.Request) httpx.Response {
	patientID := r.Path("patientId")
	p := r.Principal

	var onlyHospital string
	switch {
	case p.Is(auth.Patient) && p.UserID == patientID:
		// own record, unfiltered
	case p.Is(auth.HospitalAdmin) && p.HospitalID != "":
		onlyHospital = p.HospitalID
	default:
		return httpx.Forbidden(r.RequestID(), "you can only view your own access log")
	}

	limit, err := r.Limit()
	if err != nil {
		return httpx.BadRequest(r.RequestID(), err.Error())
	}
	items, next, err := a.auditLog.ListForPatient(ctx, patientID, onlyHospital, limit, r.Query("nextToken"))
	if err != nil {
		if errors.Is(err, db.ErrBadToken) {
			return httpx.BadRequest(r.RequestID(), "invalid nextToken")
		}
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}
	return httpx.List(items, next)
}

// listHospitalAudit shows a HospitalAdmin every record access made by their hospital's staff.
// It is always scoped to the hospital in the admin's own token.
func (a *app) listHospitalAudit(ctx context.Context, r *httpx.Request) httpx.Response {
	if r.Principal.HospitalID == "" {
		return httpx.Forbidden(r.RequestID(), "your account has no hospital assigned")
	}
	limit, err := r.Limit()
	if err != nil {
		return httpx.BadRequest(r.RequestID(), err.Error())
	}
	items, next, err := a.auditLog.ListByActorHospital(ctx, r.Principal.HospitalID, limit, r.Query("nextToken"))
	if err != nil {
		if errors.Is(err, db.ErrBadToken) {
			return httpx.BadRequest(r.RequestID(), "invalid nextToken")
		}
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}
	return httpx.List(items, next)
}
