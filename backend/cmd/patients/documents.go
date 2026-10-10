package main

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"healthcloud/internal/audit"
	"healthcloud/internal/auth"
	"healthcloud/internal/db"
	"healthcloud/internal/httpx"
	"healthcloud/internal/models"
	"healthcloud/internal/storage"
)

type documentStore interface {
	Create(ctx context.Context, doc models.Document, ev models.AuditEvent) error
	List(ctx context.Context, patientID string) ([]models.Document, error)
	Get(ctx context.Context, patientID, documentID string) (models.Document, error)
	MarkAvailable(ctx context.Context, patientID, documentID string, size int) (models.Document, error)
}

type fileStore interface {
	PresignUpload(ctx context.Context, key, contentType string) (string, time.Time, error)
	PresignDownload(ctx context.Context, key, fileName string) (string, time.Time, error)
	Size(ctx context.Context, key string) (int64, error)
	Delete(ctx context.Context, key string) error
}

// listDocuments returns metadata only. Patients see their own; Doctor reads are audited first.
func (a *app) listDocuments(ctx context.Context, r *httpx.Request) httpx.Response {
	patientID := r.Path("patientId")
	if !canReadRecord(r.Principal, patientID) {
		return httpx.Forbidden(r.RequestID(), "you can only view your own documents")
	}
	if resp := a.auditDoctorRead(ctx, r, patientID, models.ActionListDocuments, ""); resp != nil {
		return *resp
	}
	docs, err := a.documents.List(ctx, patientID)
	if err != nil {
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}
	return httpx.List(docs, "")
}

type createDocumentRequest struct {
	Title       string                  `json:"title"`
	Category    models.DocumentCategory `json:"category"`
	FileName    string                  `json:"fileName"`
	ContentType string                  `json:"contentType"`
	SizeBytes   int                     `json:"sizeBytes"`
}

// createDocumentUpload (Doctor) stores PENDING_UPLOAD metadata and returns a presigned PUT URL.
func (a *app) createDocumentUpload(ctx context.Context, r *httpx.Request) httpx.Response {
	patientID := r.Path("patientId")
	var body createDocumentRequest
	if err := r.DecodeJSON(&body); err != nil {
		return httpx.BadRequest(r.RequestID(), "invalid JSON body")
	}
	if body.Category == "" {
		body.Category = "OTHER"
	}
	switch {
	case strings.TrimSpace(body.Title) == "" || len(body.Title) > 120:
		return httpx.BadRequest(r.RequestID(), "title is required (max 120 characters)")
	case !body.Category.Valid():
		return httpx.BadRequest(r.RequestID(), "unknown category")
	case !slices.Contains(models.AllowedContentTypes, body.ContentType):
		return httpx.BadRequest(r.RequestID(), "contentType must be PDF, PNG, JPEG or plain text")
	case body.SizeBytes < 1 || body.SizeBytes > models.MaxDocumentBytes:
		return httpx.BadRequest(r.RequestID(), "file must be between 1 byte and 10 MB")
	case body.FileName == "" || len(body.FileName) > 200:
		return httpx.BadRequest(r.RequestID(), "fileName is required (max 200 characters)")
	}

	if _, err := a.patients.Get(ctx, patientID); errors.Is(err, db.ErrNotFound) {
		return httpx.NotFound(r.RequestID(), "patient not found")
	} else if err != nil {
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}

	docID := ulid.Make().String()
	safeName := sanitizeFileName(body.FileName)
	doc := models.Document{
		PatientID:            patientID,
		DocumentID:           docID,
		Title:                strings.TrimSpace(body.Title),
		Category:             body.Category,
		FileName:             safeName,
		ContentType:          body.ContentType,
		SizeBytes:            body.SizeBytes,
		S3Key:                "patients/" + patientID + "/" + docID + "/" + safeName, // server-chosen key, never client-chosen
		Status:               models.DocPendingUpload,
		UploadedBy:           r.Principal.UserID,
		UploadedByHospitalID: r.Principal.HospitalID,
		CreatedAt:            time.Now().UTC().Format(time.RFC3339),
	}

	uploadURL, expires, err := a.files.PresignUpload(ctx, doc.S3Key, doc.ContentType)
	if err != nil {
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}
	ev := audit.NewEvent(r.Principal, patientID, models.ActionUploadDocument, docID, r.RequestID(), time.Now())
	if err := a.documents.Create(ctx, doc, ev); err != nil {
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}
	return httpx.JSON(http.StatusCreated, map[string]any{
		"document":  doc,
		"uploadUrl": uploadURL,
		"expiresAt": expires.Format(time.RFC3339),
	})
}

// completeDocumentUpload (Doctor) checks the object really is in S3, enforces the size limit,
// and marks the document AVAILABLE.
func (a *app) completeDocumentUpload(ctx context.Context, r *httpx.Request) httpx.Response {
	patientID, docID := r.Path("patientId"), r.Path("documentId")
	doc, err := a.documents.Get(ctx, patientID, docID)
	if errors.Is(err, db.ErrNotFound) {
		return httpx.NotFound(r.RequestID(), "document not found")
	}
	if err != nil {
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}
	if doc.Status == models.DocAvailable {
		return httpx.JSON(http.StatusOK, doc) // already complete: idempotent
	}

	size, err := a.files.Size(ctx, doc.S3Key)
	if errors.Is(err, storage.ErrObjectMissing) {
		return httpx.Conflict(r.RequestID(), "OBJECT_MISSING", "no file has been uploaded for this document yet")
	}
	if err != nil {
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}
	// A presigned PUT can't cap the size, so it is checked here. Oversized files are deleted.
	if size > models.MaxDocumentBytes {
		_ = a.files.Delete(ctx, doc.S3Key)
		return httpx.BadRequest(r.RequestID(), "uploaded file exceeds 10 MB and was discarded")
	}

	updated, err := a.documents.MarkAvailable(ctx, patientID, docID, int(size))
	if errors.Is(err, db.ErrInvalidState) {
		updated, err = a.documents.Get(ctx, patientID, docID) // completed concurrently
	}
	if err != nil {
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}
	return httpx.JSON(http.StatusOK, updated)
}

// getDocument returns metadata plus a 5-minute presigned download URL. Doctor reads are audited first.
func (a *app) getDocument(ctx context.Context, r *httpx.Request) httpx.Response {
	patientID, docID := r.Path("patientId"), r.Path("documentId")
	if !canReadRecord(r.Principal, patientID) {
		return httpx.Forbidden(r.RequestID(), "you can only view your own documents")
	}
	doc, err := a.documents.Get(ctx, patientID, docID)
	if errors.Is(err, db.ErrNotFound) {
		return httpx.NotFound(r.RequestID(), "document not found")
	}
	if err != nil {
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}
	if doc.Status != models.DocAvailable {
		return httpx.Conflict(r.RequestID(), "NOT_UPLOADED", "the upload for this document was never completed")
	}
	if resp := a.auditDoctorRead(ctx, r, patientID, models.ActionViewDocument, docID); resp != nil {
		return *resp
	}

	url, expires, err := a.files.PresignDownload(ctx, doc.S3Key, doc.FileName)
	if err != nil {
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}
	return httpx.JSON(http.StatusOK, map[string]any{
		"document":    doc,
		"downloadUrl": url,
		"expiresAt":   expires.Format(time.RFC3339),
	})
}

// auditDoctorRead records a Doctor's read before data is returned. It returns a response only on failure.
func (a *app) auditDoctorRead(ctx context.Context, r *httpx.Request, patientID string, action models.AuditAction, targetID string) *httpx.Response {
	if !r.Principal.Is(auth.Doctor) {
		return nil
	}
	if err := a.recorder.Record(ctx, r.Principal, patientID, action, targetID, r.RequestID()); err != nil {
		res := httpx.Internal(ctx, a.log, r.RequestID(), err)
		return &res
	}
	return nil
}

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// sanitizeFileName keeps file names safe for use in an S3 key and a download header:
// no path separators and no control characters.
func sanitizeFileName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Trim(unsafeChars.ReplaceAllString(name, "_"), "._")
	if name == "" {
		name = "file"
	}
	if len(name) > 100 {
		name = name[len(name)-100:]
	}
	return name
}
