package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"healthcloud/internal/auth"
	"healthcloud/internal/cache"
	"healthcloud/internal/db"
	"healthcloud/internal/httpx"
	"healthcloud/internal/models"
)

// Interfaces describe only what the handlers need. The db package satisfies them in production,
// and the tests pass in-memory fakes.
type hospitalStore interface {
	List(ctx context.Context) ([]models.Hospital, error)
	Get(ctx context.Context, id string) (models.Hospital, error)
}

type resourceStore interface {
	ListAll(ctx context.Context) ([]models.Resource, error)
	ListByHospital(ctx context.Context, hospitalID string) ([]models.Resource, error)
	Get(ctx context.Context, hospitalID string, t models.ResourceType) (models.Resource, error)
	UpdateCapacity(ctx context.Context, hospitalID string, t models.ResourceType,
		total, available, threshold, expectedVersion int, updatedBy string) (models.Resource, error)
}

type app struct {
	hospitals  hospitalStore
	resources  resourceStore
	admissions admissionStore
	cache      *cache.Value[[]models.Resource]
	log        *slog.Logger
}

var (
	everyone = []auth.Group{auth.Patient, auth.Doctor, auth.HospitalAdmin}
	staff    = []auth.Group{auth.Doctor, auth.HospitalAdmin}
	admins   = []auth.Group{auth.HospitalAdmin}
)

// routes maps each API route (as declared in template.yaml) to a handler and its allowed groups.
// The same table appears in api/openapi.yaml as x-allowed-groups.
func (a *app) routes() *httpx.Router {
	rt := httpx.NewRouter(a.log)
	rt.Handle("GET /hospitals", everyone, a.listHospitals)
	rt.Handle("GET /hospitals/{hospitalId}", everyone, a.getHospital)
	rt.Handle("GET /resources", staff, a.listResources)
	rt.Handle("GET /hospitals/{hospitalId}/resources", staff, a.listHospitalResources)
	rt.Handle("PUT /hospitals/{hospitalId}/resources/{resourceType}", admins, a.updateResource)
	rt.Handle("POST /hospitals/{hospitalId}/admissions", staff, a.admit)
	rt.Handle("DELETE /hospitals/{hospitalId}/admissions/{patientId}", staff, a.discharge)
	return rt
}

func (a *app) listHospitals(ctx context.Context, r *httpx.Request) httpx.Response {
	hs, err := a.hospitals.List(ctx)
	if err != nil {
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}
	return httpx.List(hs, "")
}

func (a *app) getHospital(ctx context.Context, r *httpx.Request) httpx.Response {
	h, err := a.hospitals.Get(ctx, r.Path("hospitalId"))
	if errors.Is(err, db.ErrNotFound) {
		return httpx.NotFound(r.RequestID(), "hospital not found")
	}
	if err != nil {
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}
	rs, err := a.resources.ListByHospital(ctx, h.HospitalID)
	if err != nil {
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}
	return httpx.JSON(http.StatusOK, struct {
		models.Hospital
		Resources []models.Resource `json:"resources"`
	}{h, withLow(rs)})
}

// listResources is the chain-wide availability view. It is served from the 15 s cache.
func (a *app) listResources(ctx context.Context, r *httpx.Request) httpx.Response {
	typeFilter := models.ResourceType(r.Query("resourceType"))
	if typeFilter != "" && !typeFilter.Valid() {
		return httpx.BadRequest(r.RequestID(), "unknown resourceType")
	}
	onlyAvailable := r.Query("onlyAvailable") == "true"

	all, loadedAt, hit, err := a.cache.Get(func() ([]models.Resource, error) {
		return a.resources.ListAll(ctx)
	})
	if err != nil {
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}
	a.log.InfoContext(ctx, "resources read", "requestId", r.RequestID(), "cacheHit", hit)

	// Filter into a new slice. Never modify the cached slice.
	items := make([]models.Resource, 0, len(all))
	for _, res := range all {
		if typeFilter != "" && res.ResourceType != typeFilter {
			continue
		}
		if onlyAvailable && res.Available == 0 {
			continue
		}
		items = append(items, res.WithLow())
	}
	return httpx.JSON(http.StatusOK, map[string]any{
		"cachedAt": loadedAt.UTC().Format(time.RFC3339),
		"items":    items,
	})
}

func (a *app) listHospitalResources(ctx context.Context, r *httpx.Request) httpx.Response {
	id := r.Path("hospitalId")
	rs, err := a.resources.ListByHospital(ctx, id)
	if err != nil {
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}
	if len(rs) == 0 { // tell "no such hospital" apart from "hospital with no resources"
		if _, err := a.hospitals.Get(ctx, id); errors.Is(err, db.ErrNotFound) {
			return httpx.NotFound(r.RequestID(), "hospital not found")
		}
	}
	return httpx.List(withLow(rs), "")
}

// updateResourceRequest uses pointers so we can tell "field omitted" from "set to 0".
type updateResourceRequest struct {
	Total     *int `json:"total"`
	Available *int `json:"available"`
	Threshold *int `json:"threshold"`
	Version   *int `json:"version"`
}

// updateResource lets a HospitalAdmin change capacity, availability or the alert threshold,
// but ONLY for their own hospital (enforced here from the JWT claim, not by the frontend).
func (a *app) updateResource(ctx context.Context, r *httpx.Request) httpx.Response {
	hospitalID := r.Path("hospitalId")
	rt := models.ResourceType(r.Path("resourceType"))

	if !r.Principal.WorksAt(hospitalID) {
		return httpx.Forbidden(r.RequestID(), "you can only manage resources of your own hospital")
	}
	if !rt.Valid() {
		return httpx.BadRequest(r.RequestID(), "unknown resourceType")
	}

	var body updateResourceRequest
	if err := r.DecodeJSON(&body); err != nil {
		return httpx.BadRequest(r.RequestID(), "invalid JSON body")
	}
	if body.Version == nil {
		return httpx.BadRequest(r.RequestID(), "version is required (optimistic locking)")
	}

	cur, err := a.resources.Get(ctx, hospitalID, rt)
	if errors.Is(err, db.ErrNotFound) {
		return httpx.NotFound(r.RequestID(), "resource not found")
	}
	if err != nil {
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}

	// Start from the current values and apply only the fields that were sent.
	total, available, threshold := cur.Total, cur.Available, cur.Threshold
	if body.Total != nil {
		total = *body.Total
	}
	if body.Available != nil {
		available = *body.Available
	}
	if body.Threshold != nil {
		threshold = *body.Threshold
	}
	if total < 0 || available < 0 || threshold < 0 {
		return httpx.BadRequest(r.RequestID(), "values must not be negative")
	}
	if available > total {
		return httpx.BadRequest(r.RequestID(), "available cannot exceed total")
	}

	updated, err := a.resources.UpdateCapacity(ctx, hospitalID, rt, total, available, threshold, *body.Version, r.Principal.UserID)
	if errors.Is(err, db.ErrConditionFailed) {
		return httpx.Conflict(r.RequestID(), "VERSION_CONFLICT",
			"this resource changed since you loaded it; reload and try again")
	}
	if err != nil {
		return httpx.Internal(ctx, a.log, r.RequestID(), err)
	}

	a.cache.Invalidate() // this container's dashboard reflects the change immediately
	return httpx.JSON(http.StatusOK, updated.WithLow())
}

func withLow(rs []models.Resource) []models.Resource {
	out := make([]models.Resource, len(rs))
	for i, r := range rs {
		out[i] = r.WithLow()
	}
	return out
}
