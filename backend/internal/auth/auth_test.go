package auth

import (
	"encoding/base64"
	"slices"
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

func reqWithClaims(claims map[string]string) events.APIGatewayV2HTTPRequest {
	var req events.APIGatewayV2HTTPRequest
	req.RequestContext.Authorizer = &events.APIGatewayV2HTTPRequestContextAuthorizerDescription{
		JWT: &events.APIGatewayV2HTTPRequestContextAuthorizerJWTDescription{Claims: claims},
	}
	return req
}

func TestFromRequest(t *testing.T) {
	p, err := FromRequest(reqWithClaims(map[string]string{
		"sub":                "user-1",
		"email":              "doc@example.com",
		"cognito:groups":     "[Doctor HospitalAdmin]",
		"custom:hospital_id": "H001",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if p.UserID != "user-1" || p.HospitalID != "H001" {
		t.Fatalf("unexpected principal %+v", p)
	}
	if !p.Is(Doctor) || !p.Is(HospitalAdmin) || p.Is(Patient) {
		t.Fatalf("groups parsed wrong: %v", p.Groups)
	}
	if p.PrimaryGroup() != HospitalAdmin {
		t.Fatalf("primary = %s", p.PrimaryGroup())
	}
}

func TestFromRequestWithoutClaims(t *testing.T) {
	if _, err := FromRequest(events.APIGatewayV2HTTPRequest{}); err != ErrNoClaims {
		t.Fatalf("err = %v, want ErrNoClaims", err)
	}
}

func TestParseGroups(t *testing.T) {
	cases := map[string][]Group{
		"[Doctor]":                    {Doctor},
		"[Doctor HospitalAdmin]":      {Doctor, HospitalAdmin},
		`["Patient","Doctor"]`:        {Patient, Doctor},
		"":                            {},
		"[]":                          {},
		"  [HospitalAdmin, Patient] ": {HospitalAdmin, Patient},
	}
	for in, want := range cases {
		if got := parseGroups(in); !slices.Equal(got, want) {
			t.Errorf("parseGroups(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestWorksAtRejectsEmptyHospital(t *testing.T) {
	p := Principal{UserID: "u", HospitalID: ""}
	if p.WorksAt("") {
		t.Fatal("empty hospital_id must never match")
	}
	p.HospitalID = "H001"
	if !p.WorksAt("H001") || p.WorksAt("H002") {
		t.Fatal("WorksAt wrong")
	}
}

func TestLocalUnverifiedClaimsOnlyUnderSamLocal(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString(
		[]byte(`{"sub":"p-1","cognito:groups":["Patient"],"custom:hospital_id":"H002"}`))
	req := events.APIGatewayV2HTTPRequest{
		Headers: map[string]string{"authorization": "Bearer x." + payload + ".sig"},
	}

	// Outside sam local, an unverified token must be ignored.
	t.Setenv("AWS_SAM_LOCAL", "")
	if _, err := FromRequest(req); err != ErrNoClaims {
		t.Fatalf("unverified token accepted outside sam local: %v", err)
	}

	t.Setenv("AWS_SAM_LOCAL", "true")
	p, err := FromRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if p.UserID != "p-1" || !p.Is(Patient) || p.HospitalID != "H002" {
		t.Fatalf("unexpected principal %+v", p)
	}
}
