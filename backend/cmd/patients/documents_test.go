package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"healthcloud/internal/db"
	"healthcloud/internal/models"
	"healthcloud/internal/storage"
	tu "healthcloud/internal/testutil"
)

type fakeDocs struct{ byKey map[string]models.Document }

func (f *fakeDocs) Create(_ context.Context, d models.Document, _ models.AuditEvent) error {
	f.byKey[d.PatientID+"/"+d.DocumentID] = d
	return nil
}
func (f *fakeDocs) List(_ context.Context, p string) ([]models.Document, error) {
	var out []models.Document
	for _, d := range f.byKey {
		if d.PatientID == p {
			out = append(out, d)
		}
	}
	return out, nil
}
func (f *fakeDocs) Get(_ context.Context, p, id string) (models.Document, error) {
	d, ok := f.byKey[p+"/"+id]
	if !ok {
		return d, db.ErrNotFound
	}
	return d, nil
}
func (f *fakeDocs) MarkAvailable(_ context.Context, p, id string, size int) (models.Document, error) {
	d := f.byKey[p+"/"+id]
	d.Status, d.SizeBytes = models.DocAvailable, size
	f.byKey[p+"/"+id] = d
	return d, nil
}

type fakeFiles struct {
	sizes   map[string]int64
	deleted []string
}

func (f *fakeFiles) PresignUpload(_ context.Context, key, _ string) (string, time.Time, error) {
	return "https://s3.example/put/" + key, time.Now(), nil
}
func (f *fakeFiles) PresignDownload(_ context.Context, key, _ string) (string, time.Time, error) {
	return "https://s3.example/get/" + key, time.Now(), nil
}
func (f *fakeFiles) Size(_ context.Context, key string) (int64, error) {
	s, ok := f.sizes[key]
	if !ok {
		return 0, storage.ErrObjectMissing
	}
	return s, nil
}
func (f *fakeFiles) Delete(_ context.Context, key string) error {
	f.deleted = append(f.deleted, key)
	return nil
}

func newDocApp() (*app, *fakeAudit, *fakeDocs, *fakeFiles) {
	a, audit := newApp()
	docs := &fakeDocs{byKey: map[string]models.Document{
		"pat-1/D1": {PatientID: "pat-1", DocumentID: "D1", S3Key: "patients/pat-1/D1/lab.pdf", FileName: "lab.pdf", Status: models.DocAvailable},
		"pat-1/D2": {PatientID: "pat-1", DocumentID: "D2", S3Key: "patients/pat-1/D2/x.pdf", Status: models.DocPendingUpload},
	}}
	files := &fakeFiles{sizes: map[string]int64{}}
	a.documents, a.files = docs, files
	return a, audit, docs, files
}

func getDoc(claims map[string]string, patient, doc string) tu.Req {
	return tu.Req{Route: "GET /patients/{patientId}/documents/{documentId}", Claims: claims,
		Path: map[string]string{"patientId": patient, "documentId": doc}}
}

func TestDocumentAccessRules(t *testing.T) {
	a, audit, _, _ := newDocApp()

	if res := serve(t, a, getDoc(tu.Patient1, "pat-1", "D1")); res.StatusCode != 200 {
		t.Fatalf("own document: %d %s", res.StatusCode, res.Body)
	}
	if res := serve(t, a, getDoc(tu.Patient2, "pat-1", "D1")); res.StatusCode != 403 {
		t.Fatalf("another patient's document: %d, want 403", res.StatusCode)
	}
	if res := serve(t, a, getDoc(tu.AdminH1, "pat-1", "D1")); res.StatusCode != 403 {
		t.Fatalf("admin: %d, want 403", res.StatusCode)
	}
	res := serve(t, a, getDoc(tu.DoctorH1, "pat-1", "D1"))
	if res.StatusCode != 200 || len(audit.recorded) != 1 || audit.recorded[0] != models.ActionViewDocument {
		t.Fatalf("doctor read must succeed and be audited: %d %v", res.StatusCode, audit.recorded)
	}
	if strings.Contains(res.Body, "s3Key") {
		t.Fatal("the internal S3 key must not be exposed")
	}
	if res := serve(t, a, getDoc(tu.DoctorH1, "pat-1", "D2")); res.StatusCode != 409 {
		t.Fatalf("pending upload: %d, want 409", res.StatusCode)
	}
}

func TestUploadFlow(t *testing.T) {
	a, _, docs, files := newDocApp()
	res := serve(t, a, tu.Req{
		Route: "POST /patients/{patientId}/documents", Claims: tu.DoctorH1,
		Path: map[string]string{"patientId": "pat-1"},
		Body: `{"title":"Blood test","category":"LAB_REPORT","fileName":"../../etc/passwd report.pdf","contentType":"application/pdf","sizeBytes":1234}`,
	})
	if res.StatusCode != 201 {
		t.Fatalf("create: %d %s", res.StatusCode, res.Body)
	}
	got := tu.Decode[struct {
		Document  models.Document
		UploadURL string
	}](t, res)
	stored := docs.byKey["pat-1/"+got.Document.DocumentID]
	if stored.Status != models.DocPendingUpload || stored.FileName != "passwd_report.pdf" ||
		!strings.HasPrefix(stored.S3Key, "patients/pat-1/") {
		t.Fatalf("unexpected stored doc %+v", stored)
	}

	complete := tu.Req{Route: "POST /patients/{patientId}/documents/{documentId}/complete", Claims: tu.DoctorH1,
		Path: map[string]string{"patientId": "pat-1", "documentId": got.Document.DocumentID}}

	if res := serve(t, a, complete); res.StatusCode != 409 {
		t.Fatalf("complete before upload: %d, want 409 OBJECT_MISSING", res.StatusCode)
	}
	files.sizes[stored.S3Key] = 1234
	if res := serve(t, a, complete); res.StatusCode != 200 || docs.byKey["pat-1/"+got.Document.DocumentID].Status != models.DocAvailable {
		t.Fatalf("complete: %d %s", res.StatusCode, res.Body)
	}
}

func TestOversizedUploadIsDiscarded(t *testing.T) {
	a, _, _, files := newDocApp()
	files.sizes["patients/pat-1/D2/x.pdf"] = models.MaxDocumentBytes + 1
	res := serve(t, a, tu.Req{Route: "POST /patients/{patientId}/documents/{documentId}/complete", Claims: tu.DoctorH1,
		Path: map[string]string{"patientId": "pat-1", "documentId": "D2"}})
	if res.StatusCode != 400 || len(files.deleted) != 1 {
		t.Fatalf("status %d deleted %v", res.StatusCode, files.deleted)
	}
}

func TestUploadValidation(t *testing.T) {
	a, _, _, _ := newDocApp()
	for _, body := range []string{
		`{"title":"x","fileName":"a.exe","contentType":"application/x-msdownload","sizeBytes":10}`,
		`{"title":"x","fileName":"a.pdf","contentType":"application/pdf","sizeBytes":20000000}`,
		`{"title":"","fileName":"a.pdf","contentType":"application/pdf","sizeBytes":10}`,
	} {
		res := serve(t, a, tu.Req{Route: "POST /patients/{patientId}/documents", Claims: tu.DoctorH1,
			Path: map[string]string{"patientId": "pat-1"}, Body: body})
		if res.StatusCode != 400 {
			t.Errorf("%s: %d, want 400", body, res.StatusCode)
		}
	}
	// Patients can't upload.
	res := serve(t, a, tu.Req{Route: "POST /patients/{patientId}/documents", Claims: tu.Patient1,
		Path: map[string]string{"patientId": "pat-1"}, Body: `{"title":"x","fileName":"a.pdf","contentType":"application/pdf","sizeBytes":10}`})
	if res.StatusCode != 403 {
		t.Errorf("patient upload: %d, want 403", res.StatusCode)
	}
}

func TestSanitizeFileName(t *testing.T) {
	cases := map[string]string{
		"report.pdf":            "report.pdf",
		`C:\Users\x\scan 1.png`: "scan_1.png",
		"../../secret":          "secret",
		"...":                   "file",
		"résumé (final).pdf":    "r_sum_final_.pdf",
	}
	for in, want := range cases {
		if got := sanitizeFileName(in); got != want {
			t.Errorf("sanitizeFileName(%q) = %q, want %q", in, got, want)
		}
	}
}
