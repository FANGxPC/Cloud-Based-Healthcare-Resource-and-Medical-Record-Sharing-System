package main

import (
	"context"
	"errors"
	"testing"

	"healthcloud/internal/auth"
	"healthcloud/internal/db"
	"healthcloud/internal/httpx"
	"healthcloud/internal/models"
	tu "healthcloud/internal/testutil"
)

type fakePatients struct{ byID map[string]models.Patient }

func (f fakePatients) Get(_ context.Context, id string) (models.Patient, error) {
	p, ok := f.byID[id]
	if !ok {
		return p, db.ErrNotFound
	}
	return p, nil
}

func (f fakePatients) ListByHospital(_ context.Context, h, _ string, _ int, _ string) ([]models.PatientSummary, string, error) {
	var out []models.PatientSummary
	for _, p := range f.byID {
		if p.CurrentHospitalID == h {
			out = append(out, models.PatientSummary{PatientID: p.PatientID, LastName: p.LastName, CurrentHospitalID: h})
		}
	}
	return out, "", nil
}

type fakeAudit struct {
	recorded  []models.AuditAction
	failWrite bool
	lastScope string // which hospital filter ListForPatient was called with
}

func (f *fakeAudit) Record(_ context.Context, _ auth.Principal, _ string, action models.AuditAction, _, _ string) error {
	if f.failWrite {
		return errors.New("dynamodb unavailable")
	}
	f.recorded = append(f.recorded, action)
	return nil
}

func (f *fakeAudit) ListForPatient(_ context.Context, _, onlyHospital string, _ int, _ string) ([]models.AuditEvent, string, error) {
	f.lastScope = onlyHospital
	return nil, "", nil
}

func (f *fakeAudit) ListByActorHospital(context.Context, string, int, string) ([]models.AuditEvent, string, error) {
	return nil, "", nil
}

func newApp() (*app, *fakeAudit) {
	fa := &fakeAudit{}
	return &app{
		patients: fakePatients{byID: map[string]models.Patient{
			"pat-1": {PatientID: "pat-1", LastName: "Synthetic", CurrentHospitalID: "H001"},
			"pat-2": {PatientID: "pat-2", LastName: "Example", CurrentHospitalID: "H002"},
		}},
		auditLog: fa,
		recorder: fa,
		log:      tu.Logger(),
	}, fa
}

func serve(t *testing.T, a *app, r tu.Req) httpx.Response {
	t.Helper()
	res, err := a.routes().Serve(context.Background(), r.Event())
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func getPatient(id string, claims map[string]string) tu.Req {
	return tu.Req{Route: "GET /patients/{patientId}", Claims: claims, Path: map[string]string{"patientId": id}}
}

func TestPatientReadsOnlyOwnRecord(t *testing.T) {
	a, audit := newApp()

	if res := serve(t, a, getPatient("pat-1", tu.Patient1)); res.StatusCode != 200 {
		t.Fatalf("own record: %d %s", res.StatusCode, res.Body)
	}
	if res := serve(t, a, getPatient("pat-2", tu.Patient1)); res.StatusCode != 403 {
		t.Fatalf("other patient's record: %d, want 403", res.StatusCode)
	}
	if len(audit.recorded) != 0 {
		t.Fatalf("a patient reading their own record is not audited, got %v", audit.recorded)
	}
}

func TestDoctorReadsAnyRecordAndIsAudited(t *testing.T) {
	a, audit := newApp()

	// The doctor works at H001 but can read a patient at H002: records are shared across the chain.
	res := serve(t, a, getPatient("pat-2", tu.DoctorH1))
	if res.StatusCode != 200 {
		t.Fatalf("status %d %s", res.StatusCode, res.Body)
	}
	if len(audit.recorded) != 1 || audit.recorded[0] != models.ActionViewProfile {
		t.Fatalf("doctor read must be audited, got %v", audit.recorded)
	}
}

func TestAuditFailureBlocksTheRead(t *testing.T) {
	a, audit := newApp()
	audit.failWrite = true

	res := serve(t, a, getPatient("pat-1", tu.DoctorH1))
	if res.StatusCode != 500 {
		t.Fatalf("status %d, want 500 (fail closed)", res.StatusCode)
	}
	if body := tu.Decode[httpx.ErrorBody](t, res); body.Code != "INTERNAL" {
		t.Fatalf("patient data must not be returned when the audit fails: %s", res.Body)
	}
}

func TestAdminCannotReadRecords(t *testing.T) {
	a, _ := newApp()
	if res := serve(t, a, getPatient("pat-1", tu.AdminH1)); res.StatusCode != 403 {
		t.Fatalf("status %d, want 403", res.StatusCode)
	}
}

func TestAccessLogScoping(t *testing.T) {
	a, audit := newApp()
	route := "GET /patients/{patientId}/access-log"

	if res := serve(t, a, tu.Req{Route: route, Claims: tu.Patient1, Path: map[string]string{"patientId": "pat-1"}}); res.StatusCode != 200 || audit.lastScope != "" {
		t.Fatalf("patient own log: %d scope=%q", res.StatusCode, audit.lastScope)
	}
	if res := serve(t, a, tu.Req{Route: route, Claims: tu.Patient1, Path: map[string]string{"patientId": "pat-2"}}); res.StatusCode != 403 {
		t.Fatalf("patient other log: %d, want 403", res.StatusCode)
	}
	if res := serve(t, a, tu.Req{Route: route, Claims: tu.AdminH2, Path: map[string]string{"patientId": "pat-1"}}); res.StatusCode != 200 || audit.lastScope != "H002" {
		t.Fatalf("admin must be scoped to own hospital: %d scope=%q", res.StatusCode, audit.lastScope)
	}
}

func TestListPatientsDefaultsToOwnHospital(t *testing.T) {
	a, _ := newApp()
	res := serve(t, a, tu.Req{Route: "GET /patients", Claims: tu.DoctorH1})
	got := tu.Decode[struct{ Items []models.PatientSummary }](t, res)
	if len(got.Items) != 1 || got.Items[0].PatientID != "pat-1" {
		t.Fatalf("want H001's patient only, got %+v", got.Items)
	}
	if res := serve(t, a, tu.Req{Route: "GET /patients", Claims: tu.DoctorH1, Query: map[string]string{"limit": "500"}}); res.StatusCode != 400 {
		t.Fatalf("limit 500: %d, want 400", res.StatusCode)
	}
}

func TestMe(t *testing.T) {
	a, _ := newApp()
	got := tu.Decode[struct {
		UserID     string
		Groups     []string
		HospitalID *string
	}](t, serve(t, a, tu.Req{Route: "GET /me", Claims: tu.AdminH1}))
	if got.UserID != "adm-1" || got.HospitalID == nil || *got.HospitalID != "H001" || got.Groups[0] != "HospitalAdmin" {
		t.Fatalf("unexpected /me %+v", got)
	}
}
