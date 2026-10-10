package db

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"healthcloud/internal/models"
)

// TestIntegrationReadPaths exercises every query, index and pagination path against DynamoDB
// Local, so key or index typos are caught before deploying.
func TestIntegrationReadPaths(t *testing.T) {
	adm, res, pats := localTables(t)
	ctx := context.Background()
	tables := adm.Tables
	c := adm.C

	// Hospitals
	hs := Hospitals{C: c, Table: tables.Hospitals}
	for _, id := range []string{"H001", "H002"} {
		if err := hs.Put(ctx, models.Hospital{HospitalID: id, Name: "Hospital " + id}); err != nil {
			t.Fatal(err)
		}
	}
	if list, err := hs.List(ctx); err != nil || len(list) != 2 {
		t.Fatalf("hospitals list: %v %d", err, len(list))
	}
	if _, err := hs.Get(ctx, "H999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing hospital: %v", err)
	}

	// Resources: list, by hospital, optimistic locking
	for _, h := range []string{"H001", "H002"} {
		for _, rt := range models.ResourceTypes {
			if err := res.Put(ctx, models.Resource{HospitalID: h, ResourceType: rt, Total: 10, Available: 5, Threshold: 2, Version: 1}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if all, _ := res.ListAll(ctx); len(all) != 6 {
		t.Fatalf("resources ListAll = %d, want 6", len(all))
	}
	if mine, _ := res.ListByHospital(ctx, "H001"); len(mine) != 3 {
		t.Fatalf("ListByHospital = %d, want 3", len(mine))
	}
	updated, err := res.UpdateCapacity(ctx, "H001", models.ICUBed, 12, 7, 3, 1, "admin")
	if err != nil || updated.Version != 2 || updated.Total != 12 || updated.Available != 7 {
		t.Fatalf("update: %v %+v", err, updated)
	}
	if _, err := res.UpdateCapacity(ctx, "H001", models.ICUBed, 12, 7, 3, 1, "admin"); !errors.Is(err, ErrConditionFailed) {
		t.Fatalf("stale version must conflict: %v", err)
	}

	// Patients: GSI by hospital, surname prefix, pagination
	for i, last := range []string{"Patel", "Perez", "Singh", "Smith", "Young"} {
		if err := pats.Put(ctx, models.Patient{PatientID: fmt.Sprintf("p%d", i), FirstName: "A", LastName: last, CurrentHospitalID: "H001"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := pats.Put(ctx, models.Patient{PatientID: "other", LastName: "Patel", CurrentHospitalID: "H002"}); err != nil {
		t.Fatal(err)
	}
	pList, _, err := pats.ListByHospital(ctx, "H001", "P", 25, "")
	if err != nil || len(pList) != 2 || pList[0].LastName != "Patel" || pList[1].LastName != "Perez" {
		t.Fatalf("prefix search: %v %+v", err, pList)
	}
	page1, token, err := pats.ListByHospital(ctx, "H001", "", 3, "")
	if err != nil || len(page1) != 3 || token == "" {
		t.Fatalf("page 1: %v len=%d token=%q", err, len(page1), token)
	}
	page2, _, err := pats.ListByHospital(ctx, "H001", "", 3, token)
	if err != nil || len(page2) != 2 || page2[0].LastName != "Smith" {
		t.Fatalf("page 2: %v %+v", err, page2)
	}
	if _, _, err := pats.ListByHospital(ctx, "H001", "", 3, "not-a-token!"); !errors.Is(err, ErrBadToken) {
		t.Fatalf("bad token: %v", err)
	}

	// Audit log: newest first, admin filter, GSI by actor hospital
	al := AuditLog{C: c, Table: tables.AuditLog}
	base := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	for i, h := range []string{"H001", "H002", "H001"} {
		ts := base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		if err := al.Put(ctx, models.AuditEvent{PatientID: "p0", EventKey: ts + "#" + fmt.Sprint(i), ActorID: "d", ActorGroup: "Doctor",
			ActorHospitalID: h, Action: models.ActionViewProfile, Timestamp: ts}); err != nil {
			t.Fatal(err)
		}
	}
	evs, _, err := al.ListForPatient(ctx, "p0", "", 25, "")
	if err != nil || len(evs) != 3 || evs[0].Timestamp < evs[2].Timestamp {
		t.Fatalf("patient log (newest first): %v %+v", err, evs)
	}
	if evs, _, _ := al.ListForPatient(ctx, "p0", "H002", 25, ""); len(evs) != 1 {
		t.Fatalf("admin-filtered log = %d, want 1", len(evs))
	}
	if evs, _, _ := al.ListByActorHospital(ctx, "H001", 25, ""); len(evs) != 2 {
		t.Fatalf("by actor hospital = %d, want 2", len(evs))
	}

	// Documents
	ds := Documents{C: c, Tables: tables}
	doc := models.Document{PatientID: "p0", DocumentID: "D1", Title: "Lab", Category: "LAB_REPORT", FileName: "a.pdf",
		ContentType: "application/pdf", SizeBytes: 10, S3Key: "patients/p0/D1/a.pdf", Status: models.DocPendingUpload, CreatedAt: Now()}
	if err := ds.Create(ctx, doc, auditEvent("p0")); err != nil {
		t.Fatal(err)
	}
	if got, err := ds.MarkAvailable(ctx, "p0", "D1", 99); err != nil || got.Status != models.DocAvailable || got.SizeBytes != 99 {
		t.Fatalf("mark available: %v %+v", err, got)
	}
	if _, err := ds.MarkAvailable(ctx, "p0", "D1", 99); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("second mark: %v", err)
	}
	if list, _ := ds.List(ctx, "p0"); len(list) != 1 {
		t.Fatalf("documents list = %d", len(list))
	}

	// Transfers: incoming/outgoing GSIs and the status filter ("status" is a reserved word)
	trs := Transfers{C: c, Tables: tables}
	for i, st := range []models.TransferStatus{models.TransferQueued, models.TransferAccepted} {
		x := models.Transfer{TransferID: fmt.Sprintf("T%d", i), PatientID: "p0", FromHospitalID: "H001", ToHospitalID: "H002",
			ResourceType: models.ICUBed, Priority: models.PriorityUrgent, Status: st, CreatedAt: base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)}
		if err := trs.Create(ctx, x, auditEvent("p0")); err != nil {
			t.Fatal(err)
		}
	}
	if in, _, _ := trs.List(ctx, "H002", true, "", 25, ""); len(in) != 2 || in[0].TransferID != "T1" {
		t.Fatalf("incoming newest first: %+v", in)
	}
	if out, _, _ := trs.List(ctx, "H001", false, models.TransferQueued, 25, ""); len(out) != 1 || out[0].TransferID != "T0" {
		t.Fatalf("outgoing filtered by status: %+v", out)
	}
	if err := trs.SetStatus(ctx, "T0", models.TransferQueued, models.TransferFailed, "x"); err != nil {
		t.Fatal(err)
	}
	if err := trs.SetStatus(ctx, "T0", models.TransferQueued, models.TransferFailed, "x"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("SetStatus from wrong state: %v", err)
	}
}
