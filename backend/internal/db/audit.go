package db

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"healthcloud/internal/models"
)

// AuditLog is the repository for the append-only AuditLog table.
type AuditLog struct {
	C     *dynamodb.Client
	Table string
}

// Put appends one event (access pattern A1). The condition stops an event from ever being
// overwritten, even if two events got the same key (which the ULID makes practically impossible).
func (a AuditLog) Put(ctx context.Context, e models.AuditEvent) error {
	item, err := attributevalue.MarshalMap(e)
	if err != nil {
		return err
	}
	_, err = a.C.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(a.Table),
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(eventKey)"),
	})
	return err
}

// ListForPatient returns who accessed a patient's record, newest first (access pattern A2).
// If onlyActorHospital is set, only accesses by that hospital's staff are returned. That is the
// HospitalAdmin view; the filter is applied after the key lookup.
func (a AuditLog) ListForPatient(ctx context.Context, patientID, onlyActorHospital string, limit int, token string) ([]models.AuditEvent, string, error) {
	in := &dynamodb.QueryInput{
		TableName:                 aws.String(a.Table),
		KeyConditionExpression:    aws.String("patientId = :p"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":p": str(patientID)},
		ScanIndexForward:          aws.Bool(false), // newest first
	}
	if onlyActorHospital != "" {
		in.FilterExpression = aws.String("actorHospitalId = :h")
		in.ExpressionAttributeValues[":h"] = str(onlyActorHospital)
	}
	return a.query(ctx, in, limit, token)
}

// ListByActorHospital returns accesses made by staff of one hospital, newest first (access pattern A3).
func (a AuditLog) ListByActorHospital(ctx context.Context, hospitalID string, limit int, token string) ([]models.AuditEvent, string, error) {
	return a.query(ctx, &dynamodb.QueryInput{
		TableName:                 aws.String(a.Table),
		IndexName:                 aws.String(IndexAuditByActorHospital),
		KeyConditionExpression:    aws.String("actorHospitalId = :h"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":h": str(hospitalID)},
		ScanIndexForward:          aws.Bool(false),
	}, limit, token)
}

func (a AuditLog) query(ctx context.Context, in *dynamodb.QueryInput, limit int, token string) ([]models.AuditEvent, string, error) {
	start, err := decodeToken(token)
	if err != nil {
		return nil, "", err
	}
	in.Limit = aws.Int32(int32(limit))
	in.ExclusiveStartKey = start
	res, err := a.C.Query(ctx, in)
	if err != nil {
		return nil, "", err
	}
	var out []models.AuditEvent
	if err := attributevalue.UnmarshalListOfMaps(res.Items, &out); err != nil {
		return nil, "", err
	}
	return out, encodeToken(res.LastEvaluatedKey), nil
}
