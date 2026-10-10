// Package db contains the DynamoDB repositories, one file per table.
// Table, key and index names match docs/DATA_MODEL.md and template.yaml.
package db

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Index names (GSIs). They must match template.yaml.
const (
	IndexResourcesByType      = "byType"
	IndexPatientsByHospital   = "byHospital"
	IndexAuditByActorHospital = "byActorHospital"
	IndexTransfersIncoming    = "incoming"
	IndexTransfersOutgoing    = "outgoing"
)

// Tables holds the physical table names, which carry the stage suffix (e.g. "hc-patients-dev").
type Tables struct {
	Hospitals string
	Resources string
	Patients  string
	AuditLog  string
	Documents string
	Transfers string
}

// TablesFromEnv reads the table names that template.yaml injects as environment variables.
func TablesFromEnv() Tables {
	return Tables{
		Hospitals: os.Getenv("HOSPITALS_TABLE"),
		Resources: os.Getenv("RESOURCES_TABLE"),
		Patients:  os.Getenv("PATIENTS_TABLE"),
		AuditLog:  os.Getenv("AUDIT_TABLE"),
		Documents: os.Getenv("DOCUMENTS_TABLE"),
		Transfers: os.Getenv("TRANSFERS_TABLE"),
	}
}

// TablesForStage builds the names from the naming convention. The seed script and tests use it.
func TablesForStage(stage string) Tables {
	n := func(t string) string { return fmt.Sprintf("hc-%s-%s", t, stage) }
	return Tables{
		Hospitals: n("hospitals"),
		Resources: n("resources"),
		Patients:  n("patients"),
		AuditLog:  n("auditlog"),
		Documents: n("documents"),
		Transfers: n("transfers"),
	}
}

// NewClient creates a DynamoDB client. If DYNAMODB_ENDPOINT is set (e.g. http://localhost:8000
// for DynamoDB Local), requests go there instead of AWS.
func NewClient(ctx context.Context) (*dynamodb.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	return dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		if ep := os.Getenv("DYNAMODB_ENDPOINT"); ep != "" {
			o.BaseEndpoint = aws.String(ep)
		}
	}), nil
}

// ErrNotFound is returned when an item does not exist.
var ErrNotFound = errors.New("not found")

// ErrBadToken means a nextToken from the client could not be decoded (maps to 400).
var ErrBadToken = errors.New("bad nextToken")

// ErrConditionFailed is returned when a conditional write is rejected, for example a version
// mismatch or no capacity left. Callers map it to a 409 with a specific code.
var ErrConditionFailed = errors.New("condition failed")

// isConditionFailed recognises a failed condition from a plain write or from a transaction.
func isConditionFailed(err error) bool {
	var ccf *types.ConditionalCheckFailedException
	if errors.As(err, &ccf) {
		return true
	}
	var tce *types.TransactionCanceledException
	if errors.As(err, &tce) {
		for _, r := range tce.CancellationReasons {
			if aws.ToString(r.Code) == "ConditionalCheckFailed" {
				return true
			}
		}
	}
	return false
}

// Now returns the current UTC time as an ISO-8601 string (sorts correctly as text).
func Now() string { return time.Now().UTC().Format(time.RFC3339) }

// encodeToken turns DynamoDB's LastEvaluatedKey into an opaque nextToken for the API.
// All our keys are strings, so only S values are kept.
func encodeToken(key map[string]types.AttributeValue) string {
	if len(key) == 0 {
		return ""
	}
	m := map[string]string{}
	for k, v := range key {
		if s, ok := v.(*types.AttributeValueMemberS); ok {
			m[k] = s.Value
		}
	}
	b, _ := json.Marshal(m)
	return base64.RawURLEncoding.EncodeToString(b)
}

// decodeToken reverses encodeToken. A malformed token is an error (the caller returns 400).
func decodeToken(token string) (map[string]types.AttributeValue, error) {
	if token == "" {
		return nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, ErrBadToken
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, ErrBadToken
	}
	key := make(map[string]types.AttributeValue, len(m))
	for k, v := range m {
		key[k] = &types.AttributeValueMemberS{Value: v}
	}
	return key, nil
}

func str(v string) types.AttributeValue { return &types.AttributeValueMemberS{Value: v} }
func num(v int) types.AttributeValue {
	return &types.AttributeValueMemberN{Value: fmt.Sprint(v)}
}
