package main

import (
	"context"
	"testing"

	"healthcloud/internal/db"
	"healthcloud/internal/models"
	tu "healthcloud/internal/testutil"
)

// fakeAdmissions returns a preset error, so we can check how each one maps to HTTP.
// The real race-safety is proven in internal/db/admissions_integration_test.go.
type fakeAdmissions struct {
	err    error
	lastEv models.AuditEvent
	called bool
}

func (f *fakeAdmissions) Admit(_ context.Context, h, p string, t models.ResourceType, ev models.AuditEvent) (models.Admission, error) {
	f.called, f.lastEv = true, ev
	return models.Admission{PatientID: p, HospitalID: h, ResourceType: t}, f.err
}

func (f *fakeAdmissions) Discharge(_ context.Context, _, _ string, ev models.AuditEvent) error {
	f.called, f.lastEv = true, ev
	return f.err
}

func admitReq(claims map[string]string, hospital string) tu.Req {
	return tu.Req{
		Route: "POST /hospitals/{hospitalId}/admissions", Claims: claims,
		Path: map[string]string{"hospitalId": hospital},
		Body: `{"patientId":"pat-1","resourceType":"ICU_BED"}`,
	}
}

func TestAdmitErrorMapping(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{nil, 201, ""},
		{db.ErrNoCapacity, 409, "NO_CAPACITY"},
		{db.ErrAlreadyAdmitted, 409, "ALREADY_ADMITTED"},
		{db.ErrPatientNotFound, 404, "NOT_FOUND"},
		{db.ErrBusy, 503, "BUSY"},
	}
	for _, c := range cases {
		a, _ := newApp()
		fake := &fakeAdmissions{err: c.err}
		a.admissions = fake

		res := serve(t, a, admitReq(tu.DoctorH1, "H001"))
		if res.StatusCode != c.status {
			t.Errorf("err %v: status %d, want %d", c.err, res.StatusCode, c.status)
		}
		if c.code != "" {
			if got := tu.Decode[struct{ Code string }](t, res).Code; got != c.code {
				t.Errorf("err %v: code %q, want %q", c.err, got, c.code)
			}
		}
	}
}

func TestAdmitOnlyAtOwnHospital(t *testing.T) {
	a, _ := newApp()
	fake := &fakeAdmissions{}
	a.admissions = fake

	if res := serve(t, a, admitReq(tu.DoctorH1, "H002")); res.StatusCode != 403 {
		t.Fatalf("doctor of H001 admitting at H002: %d, want 403", res.StatusCode)
	}
	if res := serve(t, a, admitReq(tu.Patient1, "H001")); res.StatusCode != 403 {
		t.Fatalf("patient admitting: %d, want 403", res.StatusCode)
	}
	if fake.called {
		t.Fatal("the database must not be touched when authorization fails")
	}
}

func TestAdmitIsAudited(t *testing.T) {
	a, _ := newApp()
	fake := &fakeAdmissions{}
	a.admissions = fake

	serve(t, a, admitReq(tu.AdminH1, "H001"))
	if fake.lastEv.Action != models.ActionAdmit || fake.lastEv.ActorID != "adm-1" || fake.lastEv.PatientID != "pat-1" {
		t.Fatalf("audit event not built correctly: %+v", fake.lastEv)
	}
}

func TestDischargeMapping(t *testing.T) {
	a, _ := newApp()
	a.admissions = &fakeAdmissions{err: db.ErrNotAdmittedHere}
	res := serve(t, a, tu.Req{
		Route: "DELETE /hospitals/{hospitalId}/admissions/{patientId}", Claims: tu.DoctorH1,
		Path: map[string]string{"hospitalId": "H001", "patientId": "pat-1"},
	})
	if res.StatusCode != 409 {
		t.Fatalf("status %d, want 409", res.StatusCode)
	}

	a.admissions = &fakeAdmissions{}
	res = serve(t, a, tu.Req{
		Route: "DELETE /hospitals/{hospitalId}/admissions/{patientId}", Claims: tu.DoctorH1,
		Path: map[string]string{"hospitalId": "H001", "patientId": "pat-1"},
	})
	if res.StatusCode != 204 {
		t.Fatalf("status %d, want 204", res.StatusCode)
	}
}
