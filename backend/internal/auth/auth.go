// Package auth turns the JWT claims that API Gateway has already verified into a Principal
// (who is calling) and provides the authorization checks used by every handler.
//
// Security model:
//  1. The HTTP API JWT authorizer verifies the Cognito ID token's signature, expiry, issuer and
//     audience. An invalid token never reaches Lambda (API Gateway returns 401).
//  2. API Gateway copies the verified claims into requestContext.authorizer.jwt.claims.
//  3. This package reads those claims. Handlers then enforce the access rules (group, own hospital,
//     own record), so the rules hold even if someone calls the API directly, bypassing the frontend.
package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"

	"github.com/aws/aws-lambda-go/events"
)

// Group is a Cognito user pool group.
type Group string

const (
	Patient       Group = "Patient"
	Doctor        Group = "Doctor"
	HospitalAdmin Group = "HospitalAdmin"
)

// Principal is the authenticated caller.
type Principal struct {
	UserID     string  // Cognito "sub". For patients this is also their patientId.
	Email      string  // only used for display, never for authorization
	Groups     []Group // from "cognito:groups"
	HospitalID string  // from "custom:hospital_id" (empty for patients)
}

// ErrNoClaims means the request carried no usable identity.
var ErrNoClaims = errors.New("no JWT claims on request")

// FromRequest extracts the caller from an HTTP API (payload v2) request.
func FromRequest(req events.APIGatewayV2HTTPRequest) (Principal, error) {
	var claims map[string]string
	if a := req.RequestContext.Authorizer; a != nil && a.JWT != nil {
		claims = a.JWT.Claims
	}

	// `sam local start-api` does not run the JWT authorizer, so claims are missing locally.
	// Only inside sam local (AWS_SAM_LOCAL=true is set by SAM CLI and never by real Lambda) do we
	// read the claims from the bearer token WITHOUT verifying it, so the frontend works against
	// the local API. In AWS this branch can never run.
	if len(claims) == 0 && os.Getenv("AWS_SAM_LOCAL") == "true" {
		claims = unverifiedClaims(req.Headers["authorization"])
	}

	p := Principal{
		UserID:     claims["sub"],
		Email:      claims["email"],
		Groups:     parseGroups(claims["cognito:groups"]),
		HospitalID: claims["custom:hospital_id"],
	}
	if p.UserID == "" {
		return Principal{}, ErrNoClaims
	}
	return p, nil
}

// parseGroups handles how API Gateway flattens the JSON array claim "cognito:groups":
// it arrives as a string such as "[Doctor]" or "[Doctor HospitalAdmin]".
// The comma and quote forms are accepted too, to be safe.
func parseGroups(raw string) []Group {
	raw = strings.Trim(strings.TrimSpace(raw), "[]")
	fields := strings.FieldsFunc(raw, func(r rune) bool { return r == ' ' || r == ',' })
	groups := make([]Group, 0, len(fields))
	for _, f := range fields {
		if f = strings.Trim(f, `"`); f != "" {
			groups = append(groups, Group(f))
		}
	}
	return groups
}

// unverifiedClaims decodes a JWT payload without checking the signature. Local development only.
func unverifiedClaims(authHeader string) map[string]string {
	parts := strings.Split(strings.TrimPrefix(authHeader, "Bearer "), ".")
	if len(parts) != 3 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var raw map[string]any
	if json.Unmarshal(payload, &raw) != nil {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		switch v := v.(type) {
		case string:
			out[k] = v
		case []any: // cognito:groups → same "[A B]" form API Gateway produces
			s := make([]string, 0, len(v))
			for _, x := range v {
				if str, ok := x.(string); ok {
					s = append(s, str)
				}
			}
			out[k] = "[" + strings.Join(s, " ") + "]"
		}
	}
	return out
}

// Is reports whether the caller is in group g.
func (p Principal) Is(g Group) bool { return slices.Contains(p.Groups, g) }

// IsAny reports whether the caller is in at least one of the groups.
func (p Principal) IsAny(gs ...Group) bool {
	return slices.ContainsFunc(gs, p.Is)
}

// WorksAt reports whether the caller belongs to hospitalID. An empty hospital_id never matches,
// so a user without the attribute can't pass an own-hospital check by accident.
func (p Principal) WorksAt(hospitalID string) bool {
	return p.HospitalID != "" && p.HospitalID == hospitalID
}

// PrimaryGroup is the most privileged group the caller holds. It is recorded in the audit log.
func (p Principal) PrimaryGroup() Group {
	for _, g := range []Group{HospitalAdmin, Doctor, Patient} {
		if p.Is(g) {
			return g
		}
	}
	return ""
}
