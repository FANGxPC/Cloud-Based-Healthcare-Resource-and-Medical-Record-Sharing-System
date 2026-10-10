package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	"healthcloud/internal/audit"
	"healthcloud/internal/db"
	"healthcloud/internal/httpx"
	"healthcloud/internal/models"
)

// admissionStore is implemented by db.Admissions (race-safe DynamoDB transactions).
type admissionStore interface {
	Admit(ctx context.Context, hospitalID, patientID string, t models.ResourceType, ev models.AuditEvent) (models.Admission, error)
	Discharge(ctx context.Context, hospitalID, patientID string, ev models.AuditEvent) error
}

type admitRequest struct {
	PatientID    string              `json:"patientId"`
	ResourceType models.ResourceType `json:"resourceType"`
}

// admit occupies one bed or ventilator. Staff may only admit into their OWN hospital.
// Overbooking is impossible: see db.Admissions.Admit and its integration tests.
func (a *app) admit(ctx context.Context, r *httpx.Request) httpx.Response {
	hospitalID := r.Path("hospitalId")
	if !r.Principal.WorksAt(hospitalID) {
		return httpx.Forbidden(r.RequestID(), "you can only admit patients to your own hospital")
	}
	var body admitRequest
	if err := r.DecodeJSON(&body); err != nil || body.PatientID == "" {
		return httpx.BadRequest(r.RequestID(), "patientId and resourceType are required")
	}
	if !body.ResourceType.Valid() {
		return httpx.BadRequest(r.RequestID(), "unknown resourceType")
	}

	ev := audit.NewEvent(r.Principal, body.PatientID, models.ActionAdmit, hospitalID, r.RequestID(), time.Now())
	adm, err := a.admissions.Admit(ctx, hospitalID, body.PatientID, body.ResourceType, ev)
	switch {
	case errors.Is(err, db.ErrNoCapacity):
		return httpx.Conflict(r.RequestID(), "NO_CAPACITY", "no "+string(body.ResourceType)+" available at "+hospitalID)
	case errors.Is(err, db.ErrAlreadyAdmitted):
		return httpx.Conflict(r.RequestID(), "ALREADY_ADMITTED", "patient already occupies a unit")
	case errors.Is(err, db.ErrPatientNotFound):
		return httpx.NotFound(r.RequestID(), "patient not found")
	case errors.Is(err, db.ErrBusy):
		return busy(r)
	case err != nil:
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}

	a.cache.Invalidate()
	return httpx.JSON(http.StatusCreated, adm)
}

// discharge frees the unit a patient holds at the caller's own hospital.
func (a *app) discharge(ctx context.Context, r *httpx.Request) httpx.Response {
	hospitalID, patientID := r.Path("hospitalId"), r.Path("patientId")
	if !r.Principal.WorksAt(hospitalID) {
		return httpx.Forbidden(r.RequestID(), "you can only discharge patients from your own hospital")
	}

	ev := audit.NewEvent(r.Principal, patientID, models.ActionDischarge, hospitalID, r.RequestID(), time.Now())
	err := a.admissions.Discharge(ctx, hospitalID, patientID, ev)
	switch {
	case errors.Is(err, db.ErrNotAdmittedHere):
		return httpx.Conflict(r.RequestID(), "NOT_ADMITTED_HERE", "patient is not admitted at "+hospitalID)
	case errors.Is(err, db.ErrPatientNotFound):
		return httpx.NotFound(r.RequestID(), "patient not found")
	case errors.Is(err, db.ErrCapacityOverflow):
		return httpx.Conflict(r.RequestID(), "CAPACITY_OVERFLOW", "available would exceed total; an admin must fix the total first")
	case errors.Is(err, db.ErrBusy):
		return busy(r)
	case err != nil:
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}

	a.cache.Invalidate()
	return httpx.Response{StatusCode: http.StatusNoContent}
}

func busy(r *httpx.Request) httpx.Response {
	return httpx.Error(http.StatusServiceUnavailable, "BUSY", "high contention on this resource; please retry", r.RequestID())
}
