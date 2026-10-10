package db

import (
	"context"
	"errors"
	"sync"
	"testing"

	"healthcloud/internal/models"
)

// transferFixture: patient p00 is admitted to a GENERAL_BED at H001 and is being transferred to
// an ICU bed at H002, which has `icuFree` free units.
func transferFixture(t *testing.T, icuFree int) (Transfers, Resources, Patients, models.Transfer) {
	t.Helper()
	adm, res, pats := localTables(t)
	ctx := context.Background()
	tr := Transfers{C: adm.C, Tables: adm.Tables}

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(res.Put(ctx, models.Resource{HospitalID: "H001", ResourceType: models.GeneralBed, Total: 10, Available: 5, Threshold: 2, Version: 1}))
	must(res.Put(ctx, models.Resource{HospitalID: "H002", ResourceType: models.ICUBed, Total: 4, Available: icuFree, Threshold: 1, Version: 1}))
	must(pats.Put(ctx, models.Patient{PatientID: "p00", LastName: "Test", CurrentHospitalID: "H001"}))
	_, err := adm.Admit(ctx, "H001", "p00", models.GeneralBed, auditEvent("p00")) // H001 general: 5 → 4
	must(err)

	x := models.Transfer{
		TransferID: "T1", PatientID: "p00", FromHospitalID: "H001", ToHospitalID: "H002",
		ResourceType: models.ICUBed, Priority: models.PriorityCritical, Status: models.TransferQueued,
		RequestedBy: "doc", CreatedAt: Now(), UpdatedAt: Now(),
	}
	must(tr.Create(ctx, x, auditEvent("p00")))
	return tr, res, pats, x
}

func available(t *testing.T, res Resources, h string, rt models.ResourceType) int {
	t.Helper()
	r, err := res.Get(context.Background(), h, rt)
	if err != nil {
		t.Fatal(err)
	}
	return r.Available
}

// SQS delivers at least once. Ten concurrent deliveries of the same message must hold exactly one bed.
func TestIntegrationWorkerIsIdempotent(t *testing.T) {
	tr, res, _, x := transferFixture(t, 3)

	var wg sync.WaitGroup
	var mu sync.Mutex
	outcomes := map[string]int{}
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := tr.HoldBed(context.Background(), x)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				outcomes["held"]++
			case errors.Is(err, ErrInvalidState):
				outcomes["duplicate"]++
			default:
				outcomes[err.Error()]++
			}
		}()
	}
	wg.Wait()
	t.Logf("outcomes: %v", outcomes)

	if outcomes["held"] != 1 || outcomes["duplicate"] != 9 {
		t.Fatalf("want 1 held + 9 duplicates, got %v", outcomes)
	}
	if a := available(t, res, "H002", models.ICUBed); a != 2 {
		t.Fatalf("H002 ICU available = %d, want 2 (exactly one bed held)", a)
	}
	got, _ := tr.Get(context.Background(), "T1")
	if got.Status != models.TransferPendingApproval {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestIntegrationWorkerNoCapacity(t *testing.T) {
	tr, res, _, x := transferFixture(t, 0)
	if err := tr.HoldBed(context.Background(), x); !errors.Is(err, ErrNoCapacity) {
		t.Fatalf("err = %v, want ErrNoCapacity", err)
	}
	if a := available(t, res, "H002", models.ICUBed); a != 0 {
		t.Fatalf("available = %d, must stay 0", a)
	}
}

func TestIntegrationAcceptMovesPatientAndReleasesOrigin(t *testing.T) {
	tr, res, pats, x := transferFixture(t, 3)
	ctx := context.Background()
	if err := tr.HoldBed(ctx, x); err != nil {
		t.Fatal(err)
	}
	if err := tr.Accept(ctx, x, "admin2", "ok", auditEvent("p00")); err != nil {
		t.Fatal(err)
	}

	p, _ := pats.Get(ctx, "p00")
	if p.CurrentHospitalID != "H002" || p.Admission == nil || p.Admission.HospitalID != "H002" || p.Admission.ResourceType != models.ICUBed {
		t.Fatalf("patient not moved correctly: %+v / %+v", p, p.Admission)
	}
	if a := available(t, res, "H001", models.GeneralBed); a != 5 {
		t.Fatalf("origin general beds = %d, want 5 (released)", a)
	}
	if a := available(t, res, "H002", models.ICUBed); a != 2 {
		t.Fatalf("destination ICU = %d, want 2 (held bed now occupied)", a)
	}
	// A second accept, or a reject after accept, must be refused.
	if err := tr.Accept(ctx, x, "admin2", "", auditEvent("p00")); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("double accept: %v, want ErrInvalidState", err)
	}
	if err := tr.Reject(ctx, x, "admin2", "", auditEvent("p00")); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("reject after accept: %v, want ErrInvalidState", err)
	}
}

func TestIntegrationRejectReleasesHeldBed(t *testing.T) {
	tr, res, pats, x := transferFixture(t, 3)
	ctx := context.Background()
	if err := tr.HoldBed(ctx, x); err != nil {
		t.Fatal(err)
	}
	if err := tr.Reject(ctx, x, "admin2", "no specialist", auditEvent("p00")); err != nil {
		t.Fatal(err)
	}
	if a := available(t, res, "H002", models.ICUBed); a != 3 {
		t.Fatalf("destination ICU = %d, want 3 (held bed released)", a)
	}
	p, _ := pats.Get(ctx, "p00")
	if p.CurrentHospitalID != "H001" {
		t.Fatal("rejected transfer must not move the patient")
	}
	got, _ := tr.Get(ctx, "T1")
	if got.Status != models.TransferRejected || got.DecisionNote != "no specialist" {
		t.Fatalf("unexpected transfer %+v", got)
	}
}
