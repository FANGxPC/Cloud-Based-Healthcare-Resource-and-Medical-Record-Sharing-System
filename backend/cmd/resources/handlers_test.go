package main

import (
	"context"
	"testing"
	"time"

	"healthcloud/internal/cache"
	"healthcloud/internal/db"
	"healthcloud/internal/httpx"
	"healthcloud/internal/models"
	tu "healthcloud/internal/testutil"
)

// ---- in-memory fakes ----

type fakeHospitals struct{ byID map[string]models.Hospital }

func (f fakeHospitals) List(context.Context) ([]models.Hospital, error) {
	var out []models.Hospital
	for _, h := range f.byID {
		out = append(out, h)
	}
	return out, nil
}

func (f fakeHospitals) Get(_ context.Context, id string) (models.Hospital, error) {
	h, ok := f.byID[id]
	if !ok {
		return h, db.ErrNotFound
	}
	return h, nil
}

type fakeResources struct {
	rows      map[string]*models.Resource // key "H001/ICU_BED"
	scanCalls int
}

func key(h string, t models.ResourceType) string { return h + "/" + string(t) }

func (f *fakeResources) ListAll(context.Context) ([]models.Resource, error) {
	f.scanCalls++
	var out []models.Resource
	for _, r := range f.rows {
		out = append(out, *r)
	}
	return out, nil
}

func (f *fakeResources) ListByHospital(_ context.Context, h string) ([]models.Resource, error) {
	var out []models.Resource
	for _, r := range f.rows {
		if r.HospitalID == h {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (f *fakeResources) Get(_ context.Context, h string, t models.ResourceType) (models.Resource, error) {
	r, ok := f.rows[key(h, t)]
	if !ok {
		return models.Resource{}, db.ErrNotFound
	}
	return *r, nil
}

// UpdateCapacity mimics DynamoDB's conditional write: it fails on a version mismatch.
func (f *fakeResources) UpdateCapacity(_ context.Context, h string, t models.ResourceType, total, avail, th, ver int, _ string) (models.Resource, error) {
	r, ok := f.rows[key(h, t)]
	if !ok || r.Version != ver {
		return models.Resource{}, db.ErrConditionFailed
	}
	r.Total, r.Available, r.Threshold, r.Version = total, avail, th, r.Version+1
	return *r, nil
}

func newApp() (*app, *fakeResources) {
	res := &fakeResources{rows: map[string]*models.Resource{
		key("H001", models.ICUBed):     {HospitalID: "H001", ResourceType: models.ICUBed, Total: 10, Available: 1, Threshold: 2, Version: 3},
		key("H002", models.ICUBed):     {HospitalID: "H002", ResourceType: models.ICUBed, Total: 8, Available: 0, Threshold: 2, Version: 1},
		key("H002", models.Ventilator): {HospitalID: "H002", ResourceType: models.Ventilator, Total: 5, Available: 4, Threshold: 1, Version: 1},
	}}
	return &app{
		hospitals: fakeHospitals{byID: map[string]models.Hospital{
			"H001": {HospitalID: "H001", Name: "North"},
			"H002": {HospitalID: "H002", Name: "South"},
		}},
		resources: res,
		cache:     &cache.Value[[]models.Resource]{TTL: 15 * time.Second},
		log:       tu.Logger(),
	}, res
}

func call(t *testing.T, a *app, r tu.Req) (int, string) {
	t.Helper()
	res := serve(t, a, r)
	return res.StatusCode, res.Body
}

func serve(t *testing.T, a *app, r tu.Req) httpx.Response {
	t.Helper()
	res, err := a.routes().Serve(context.Background(), r.Event())
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// ---- tests ----

func TestAdminCanOnlyEditOwnHospital(t *testing.T) {
	a, _ := newApp()
	route := "PUT /hospitals/{hospitalId}/resources/{resourceType}"
	path := map[string]string{"hospitalId": "H001", "resourceType": "ICU_BED"}
	body := `{"total":12,"available":5,"version":3}`

	if s, b := call(t, a, tu.Req{Route: route, Claims: tu.AdminH2, Path: path, Body: body}); s != 403 {
		t.Fatalf("admin of H002 editing H001: status %d %s", s, b)
	}
	if s, _ := call(t, a, tu.Req{Route: route, Claims: tu.DoctorH1, Path: path, Body: body}); s != 403 {
		t.Fatalf("doctor editing resources: status %d, want 403", s)
	}
	s, b := call(t, a, tu.Req{Route: route, Claims: tu.AdminH1, Path: path, Body: body})
	if s != 200 {
		t.Fatalf("own hospital: status %d %s", s, b)
	}
}

func TestUpdateVersionConflict(t *testing.T) {
	a, _ := newApp()
	s, _ := call(t, a, tu.Req{
		Route: "PUT /hospitals/{hospitalId}/resources/{resourceType}", Claims: tu.AdminH1,
		Path: map[string]string{"hospitalId": "H001", "resourceType": "ICU_BED"},
		Body: `{"available":5,"version":2}`, // stale: the current version is 3
	})
	if s != 409 {
		t.Fatalf("stale version: status %d, want 409", s)
	}
}

func TestUpdateValidation(t *testing.T) {
	a, _ := newApp()
	for _, body := range []string{
		`{"available":11,"version":3}`, // more than total (10)
		`{"total":-1,"version":3}`,
		`{"available":1}`,                          // no version
		`{"available":1,"version":3,"extra":true}`, // unknown field
		`not json`,
	} {
		s, _ := call(t, a, tu.Req{
			Route: "PUT /hospitals/{hospitalId}/resources/{resourceType}", Claims: tu.AdminH1,
			Path: map[string]string{"hospitalId": "H001", "resourceType": "ICU_BED"}, Body: body,
		})
		if s != 400 {
			t.Errorf("body %s: status %d, want 400", body, s)
		}
	}
}

func TestListResourcesUsesCacheAndFilters(t *testing.T) {
	a, fake := newApp()
	req := tu.Req{Route: "GET /resources", Claims: tu.DoctorH1,
		Query: map[string]string{"resourceType": "ICU_BED", "onlyAvailable": "true"}}

	res := serve(t, a, req)
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	type resp struct{ Items []models.Resource }
	got := tu.Decode[resp](t, res)
	if len(got.Items) != 1 || got.Items[0].HospitalID != "H001" || !got.Items[0].Low {
		t.Fatalf("want only H001 ICU (1 free, low): %+v", got.Items)
	}

	call(t, a, req)
	if fake.scanCalls != 1 {
		t.Fatalf("second read within TTL should hit the cache; scans = %d", fake.scanCalls)
	}
}

func TestPatientCannotSeeChainResources(t *testing.T) {
	a, _ := newApp()
	if s, _ := call(t, a, tu.Req{Route: "GET /resources", Claims: tu.Patient1}); s != 403 {
		t.Fatalf("status %d, want 403", s)
	}
}

func TestGetHospitalNotFound(t *testing.T) {
	a, _ := newApp()
	s, _ := call(t, a, tu.Req{Route: "GET /hospitals/{hospitalId}", Claims: tu.Patient1,
		Path: map[string]string{"hospitalId": "H999"}})
	if s != 404 {
		t.Fatalf("status %d, want 404", s)
	}
}
