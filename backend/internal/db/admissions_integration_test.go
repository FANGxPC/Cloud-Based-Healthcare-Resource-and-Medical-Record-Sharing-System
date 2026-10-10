package db

// Integration tests against DynamoDB Local. They PROVE the race-safety claim by firing many
// truly concurrent requests at a real DynamoDB engine. Mocks can't show this, because the
// guarantee comes from DynamoDB's conditional writes, not from our code.
//
// Run DynamoDB Local (docker compose up -d, or the jar on port 8000), then:
//
//	DYNAMODB_ENDPOINT=http://localhost:8000 go test ./internal/db -run Integration -v
//
// Without DYNAMODB_ENDPOINT these tests are skipped, so `go test ./...` still works anywhere.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"healthcloud/internal/models"
)

func localTables(t *testing.T) (Admissions, Resources, Patients) {
	t.Helper()
	if os.Getenv("DYNAMODB_ENDPOINT") == "" {
		t.Skip("DYNAMODB_ENDPOINT not set; skipping DynamoDB Local integration test")
	}
	// DynamoDB Local accepts any credentials and region.
	for k, v := range map[string]string{"AWS_ACCESS_KEY_ID": "local", "AWS_SECRET_ACCESS_KEY": "local", "AWS_REGION": "us-east-1"} {
		if os.Getenv(k) == "" {
			t.Setenv(k, v)
		}
	}
	ctx := context.Background()
	c, err := NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Unique table names per test, so tests never interfere with each other or with seeded data.
	tables := TablesForStage(fmt.Sprintf("t%d", time.Now().UnixNano()))
	if err := CreateTables(ctx, c, tables); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { DeleteTables(context.Background(), c, tables) })

	return Admissions{C: c, Tables: tables},
		Resources{C: c, Table: tables.Resources},
		Patients{C: c, Table: tables.Patients}
}

func seed(t *testing.T, res Resources, pats Patients, available, patients int) {
	t.Helper()
	ctx := context.Background()
	if err := res.Put(ctx, models.Resource{HospitalID: "H001", ResourceType: models.ICUBed, Total: 10, Available: available, Threshold: 2, Version: 1}); err != nil {
		t.Fatal(err)
	}
	for i := range patients {
		if err := pats.Put(ctx, models.Patient{PatientID: fmt.Sprintf("p%02d", i), LastName: "Test", CurrentHospitalID: "H002"}); err != nil {
			t.Fatal(err)
		}
	}
}

func auditEvent(patientID string) models.AuditEvent {
	return models.AuditEvent{
		PatientID: patientID, EventKey: fmt.Sprintf("%s#%d", Now(), time.Now().UnixNano()),
		ActorID: "test", ActorGroup: "Doctor", Action: models.ActionAdmit, Timestamp: Now(),
	}
}

// race fires n Admit calls at exactly the same moment and tallies the outcomes.
func race(t *testing.T, adm Admissions, n int, patientFor func(i int) string) map[string]int {
	t.Helper()
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		start   = make(chan struct{})
		results = map[string]int{}
	)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // every goroutine waits here, then all are released together
			pid := patientFor(i)
			_, err := adm.Admit(context.Background(), "H001", pid, models.ICUBed, auditEvent(pid))

			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				results["ok"]++
			case errors.Is(err, ErrNoCapacity):
				results["no_capacity"]++
			case errors.Is(err, ErrAlreadyAdmitted):
				results["already_admitted"]++
			case errors.Is(err, ErrBusy):
				results["busy"]++
			default:
				results["unexpected: "+err.Error()]++
			}
		}()
	}
	close(start)
	wg.Wait()
	return results
}

// TestIntegrationLastBedNeverOverbooked is the headline test: 50 doctors try to admit 50
// different patients into the LAST free ICU bed at the same instant. Exactly one must win.
func TestIntegrationLastBedNeverOverbooked(t *testing.T) {
	adm, res, pats := localTables(t)
	seed(t, res, pats, 1, 50)

	results := race(t, adm, 50, func(i int) string { return fmt.Sprintf("p%02d", i) })
	t.Logf("outcomes: %v", results)

	if results["ok"] != 1 {
		t.Fatalf("exactly 1 admission must succeed, got %d (%v)", results["ok"], results)
	}
	if results["ok"]+results["no_capacity"]+results["busy"] != 50 {
		t.Fatalf("every loser must be told NO_CAPACITY (or BUSY), got %v", results)
	}

	r, _ := res.Get(context.Background(), "H001", models.ICUBed)
	if r.Available != 0 {
		t.Fatalf("available = %d, want 0 (never negative)", r.Available)
	}
	assertAdmittedCount(t, pats, 50, 1)
}

// TestIntegrationCapacityRespected: 5 free beds and 40 concurrent requests → exactly 5 admitted.
func TestIntegrationCapacityRespected(t *testing.T) {
	adm, res, pats := localTables(t)
	seed(t, res, pats, 5, 40)

	results := race(t, adm, 40, func(i int) string { return fmt.Sprintf("p%02d", i) })
	t.Logf("outcomes: %v", results)

	if results["ok"] != 5 {
		t.Fatalf("exactly 5 admissions must succeed, got %v", results)
	}
	r, _ := res.Get(context.Background(), "H001", models.ICUBed)
	if r.Available != 0 || r.Version != 1+5 {
		t.Fatalf("available=%d version=%d, want 0 and 6", r.Available, r.Version)
	}
	assertAdmittedCount(t, pats, 40, 5)
}

// TestIntegrationOnePatientOneBed: the same patient double-clicked 10 times → one bed only.
func TestIntegrationOnePatientOneBed(t *testing.T) {
	adm, res, pats := localTables(t)
	seed(t, res, pats, 5, 1)

	results := race(t, adm, 10, func(int) string { return "p00" })
	t.Logf("outcomes: %v", results)

	if results["ok"] != 1 {
		t.Fatalf("the same patient must be admitted once, got %v", results)
	}
	r, _ := res.Get(context.Background(), "H001", models.ICUBed)
	if r.Available != 4 {
		t.Fatalf("available = %d, want 4 (only one bed taken)", r.Available)
	}
}

func TestIntegrationDischargeReleasesBed(t *testing.T) {
	adm, res, pats := localTables(t)
	seed(t, res, pats, 1, 1)
	ctx := context.Background()

	if _, err := adm.Admit(ctx, "H001", "p00", models.ICUBed, auditEvent("p00")); err != nil {
		t.Fatal(err)
	}
	if err := adm.Discharge(ctx, "H002", "p00", auditEvent("p00")); !errors.Is(err, ErrNotAdmittedHere) {
		t.Fatalf("discharge at the wrong hospital: %v, want ErrNotAdmittedHere", err)
	}
	if err := adm.Discharge(ctx, "H001", "p00", auditEvent("p00")); err != nil {
		t.Fatal(err)
	}
	r, _ := res.Get(ctx, "H001", models.ICUBed)
	if r.Available != 1 {
		t.Fatalf("available = %d after discharge, want 1", r.Available)
	}
	p, _ := pats.Get(ctx, "p00")
	if p.Admission != nil {
		t.Fatal("admission should be cleared")
	}
	if _, err := adm.Admit(ctx, "H001", "nobody", models.ICUBed, auditEvent("nobody")); !errors.Is(err, ErrPatientNotFound) {
		t.Fatalf("unknown patient: %v, want ErrPatientNotFound", err)
	}
}

func assertAdmittedCount(t *testing.T, pats Patients, total, want int) {
	t.Helper()
	got := 0
	for i := range total {
		p, err := pats.Get(context.Background(), fmt.Sprintf("p%02d", i))
		if err != nil {
			t.Fatal(err)
		}
		if p.Admission != nil {
			got++
		}
	}
	if got != want {
		t.Fatalf("%d patients hold a bed, want %d", got, want)
	}
}
