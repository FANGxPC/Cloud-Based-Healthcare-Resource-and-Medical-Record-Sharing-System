package main

import (
	"context"
	"errors"
	"testing"

	"healthcloud/internal/db"
	"healthcloud/internal/httpx"
	"healthcloud/internal/models"
	tu "healthcloud/internal/testutil"
)

type fakeTransfers struct {
	byID     map[string]models.Transfer
	accepted bool
	rejected bool
}

func (f *fakeTransfers) Create(_ context.Context, tr models.Transfer, _ models.AuditEvent) error {
	f.byID[tr.TransferID] = tr
	return nil
}
func (f *fakeTransfers) Get(_ context.Context, id string) (models.Transfer, error) {
	tr, ok := f.byID[id]
	if !ok {
		return tr, db.ErrNotFound
	}
	return tr, nil
}
func (f *fakeTransfers) List(context.Context, string, bool, models.TransferStatus, int, string) ([]models.Transfer, string, error) {
	return nil, "", nil
}
func (f *fakeTransfers) SetStatus(_ context.Context, id string, _, to models.TransferStatus, _ string) error {
	tr := f.byID[id]
	tr.Status = to
	f.byID[id] = tr
	return nil
}
func (f *fakeTransfers) Accept(_ context.Context, tr models.Transfer, _, _ string, _ models.AuditEvent) error {
	f.accepted = true
	tr.Status = models.TransferAccepted
	f.byID[tr.TransferID] = tr
	return nil
}
func (f *fakeTransfers) Reject(_ context.Context, tr models.Transfer, _, _ string, _ models.AuditEvent) error {
	f.rejected = true
	tr.Status = models.TransferRejected
	f.byID[tr.TransferID] = tr
	return nil
}

type fakeGetter[T any] struct{ ok map[string]bool }

func (f fakeGetter[T]) Get(_ context.Context, id string) (T, error) {
	var zero T
	if !f.ok[id] {
		return zero, db.ErrNotFound
	}
	return zero, nil
}

type fakeQueue struct {
	sent []string
	fail bool
}

func (q *fakeQueue) Send(_ context.Context, m models.TransferMessage) error {
	if q.fail {
		return errors.New("sqs down")
	}
	q.sent = append(q.sent, m.TransferID)
	return nil
}

type fakePub struct{ n int }

func (p *fakePub) Publish(context.Context, string, string, string, string) error { p.n++; return nil }

func newApp() (*app, *fakeTransfers, *fakeQueue) {
	tr := &fakeTransfers{byID: map[string]models.Transfer{
		"T-pending": {TransferID: "T-pending", PatientID: "pat-1", FromHospitalID: "H001", ToHospitalID: "H002",
			ResourceType: models.ICUBed, Priority: models.PriorityUrgent, Status: models.TransferPendingApproval},
		"T-done": {TransferID: "T-done", FromHospitalID: "H001", ToHospitalID: "H002", Status: models.TransferAccepted},
	}}
	q := &fakeQueue{}
	return &app{
		transfers: tr,
		patients:  fakeGetter[models.Patient]{ok: map[string]bool{"pat-1": true}},
		hospitals: fakeGetter[models.Hospital]{ok: map[string]bool{"H001": true, "H002": true}},
		queue:     q,
		pub:       &fakePub{},
		log:       tu.Logger(),
	}, tr, q
}

func serve(t *testing.T, a *app, r tu.Req) httpx.Response {
	t.Helper()
	res, err := a.routes().Serve(context.Background(), r.Event())
	if err != nil {
		t.Fatal(err)
	}
	return res
}

const validBody = `{"patientId":"pat-1","toHospitalId":"H002","resourceType":"ICU_BED","priority":"CRITICAL"}`

func TestRequestTransferQueuesAndReturns202(t *testing.T) {
	a, store, q := newApp()
	res := serve(t, a, tu.Req{Route: "POST /transfers", Claims: tu.DoctorH1, Body: validBody})
	if res.StatusCode != 202 {
		t.Fatalf("status %d %s", res.StatusCode, res.Body)
	}
	tr := tu.Decode[models.Transfer](t, res)
	if tr.Status != models.TransferQueued || tr.FromHospitalID != "H001" {
		t.Fatalf("unexpected transfer %+v", tr)
	}
	if res.Headers["Location"] != "/transfers/"+tr.TransferID {
		t.Fatalf("Location = %q", res.Headers["Location"])
	}
	if len(q.sent) != 1 || q.sent[0] != tr.TransferID {
		t.Fatalf("message not enqueued: %v", q.sent)
	}
	if _, ok := store.byID[tr.TransferID]; !ok {
		t.Fatal("transfer not stored before enqueueing")
	}
}

func TestRequestTransferValidation(t *testing.T) {
	a, _, _ := newApp()
	cases := map[string]struct {
		body   string
		status int
	}{
		"same hospital":    {`{"patientId":"pat-1","toHospitalId":"H001","resourceType":"ICU_BED","priority":"CRITICAL"}`, 400},
		"bad priority":     {`{"patientId":"pat-1","toHospitalId":"H002","resourceType":"ICU_BED","priority":"ASAP"}`, 400},
		"bad type":         {`{"patientId":"pat-1","toHospitalId":"H002","resourceType":"MRI","priority":"CRITICAL"}`, 400},
		"unknown patient":  {`{"patientId":"nobody","toHospitalId":"H002","resourceType":"ICU_BED","priority":"CRITICAL"}`, 404},
		"unknown hospital": {`{"patientId":"pat-1","toHospitalId":"H999","resourceType":"ICU_BED","priority":"CRITICAL"}`, 404},
	}
	for name, c := range cases {
		if res := serve(t, a, tu.Req{Route: "POST /transfers", Claims: tu.DoctorH1, Body: c.body}); res.StatusCode != c.status {
			t.Errorf("%s: status %d, want %d", name, res.StatusCode, c.status)
		}
	}
	// Admins can't request transfers (doctors only).
	if res := serve(t, a, tu.Req{Route: "POST /transfers", Claims: tu.AdminH1, Body: validBody}); res.StatusCode != 403 {
		t.Errorf("admin request: %d, want 403", res.StatusCode)
	}
}

func TestEnqueueFailureMarksFailed(t *testing.T) {
	a, store, q := newApp()
	q.fail = true
	res := serve(t, a, tu.Req{Route: "POST /transfers", Claims: tu.DoctorH1, Body: validBody})
	if res.StatusCode != 500 {
		t.Fatalf("status %d, want 500", res.StatusCode)
	}
	for _, tr := range store.byID {
		if tr.PatientID == "pat-1" && tr.Status == models.TransferQueued && tr.TransferID != "T-pending" {
			t.Fatal("transfer left stuck in QUEUED after enqueue failure")
		}
	}
}

func decide(claims map[string]string, id, decision string) tu.Req {
	return tu.Req{Route: "POST /transfers/{transferId}/decision", Claims: claims,
		Path: map[string]string{"transferId": id}, Body: `{"decision":"` + decision + `"}`}
}

func TestOnlyDestinationAdminDecides(t *testing.T) {
	a, store, _ := newApp()
	if res := serve(t, a, decide(tu.AdminH1, "T-pending", "ACCEPT")); res.StatusCode != 403 {
		t.Fatalf("origin admin: %d, want 403", res.StatusCode)
	}
	if res := serve(t, a, decide(tu.DoctorH1, "T-pending", "ACCEPT")); res.StatusCode != 403 {
		t.Fatalf("doctor: %d, want 403", res.StatusCode)
	}
	if store.accepted {
		t.Fatal("must not accept on 403")
	}
	res := serve(t, a, decide(tu.AdminH2, "T-pending", "ACCEPT"))
	if res.StatusCode != 200 || !store.accepted {
		t.Fatalf("destination admin: %d %s", res.StatusCode, res.Body)
	}
}

func TestDecideRequiresPending(t *testing.T) {
	a, _, _ := newApp()
	if res := serve(t, a, decide(tu.AdminH2, "T-done", "REJECT")); res.StatusCode != 409 {
		t.Fatalf("status %d, want 409", res.StatusCode)
	}
}

func TestGetTransferOnlyForInvolvedHospitals(t *testing.T) {
	a, _, _ := newApp()
	h3 := map[string]string{"sub": "d3", "cognito:groups": "[Doctor]", "custom:hospital_id": "H003"}
	req := func(c map[string]string) tu.Req {
		return tu.Req{Route: "GET /transfers/{transferId}", Claims: c, Path: map[string]string{"transferId": "T-pending"}}
	}
	if res := serve(t, a, req(h3)); res.StatusCode != 403 {
		t.Fatalf("uninvolved hospital: %d, want 403", res.StatusCode)
	}
	if res := serve(t, a, req(tu.DoctorH1)); res.StatusCode != 200 {
		t.Fatalf("origin doctor: %d, want 200", res.StatusCode)
	}
}
