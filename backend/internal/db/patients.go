package db

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"healthcloud/internal/models"
)

// Patients is the repository for the Patients table.
type Patients struct {
	C     *dynamodb.Client
	Table string
}

// Get returns one full patient record (access patterns P1/P2) or ErrNotFound.
func (p Patients) Get(ctx context.Context, patientID string) (models.Patient, error) {
	res, err := p.C.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(p.Table),
		Key:       map[string]types.AttributeValue{"patientId": str(patientID)},
	})
	if err != nil {
		return models.Patient{}, err
	}
	if res.Item == nil {
		return models.Patient{}, ErrNotFound
	}
	var out models.Patient
	err = attributevalue.UnmarshalMap(res.Item, &out)
	return out, err
}

// ListByHospital returns patients currently at a hospital, sorted by surname (access pattern P3).
// It queries the byHospital GSI. With a prefix, it narrows to surnames starting with it.
func (p Patients) ListByHospital(ctx context.Context, hospitalID, lastNamePrefix string, limit int, token string) ([]models.PatientSummary, string, error) {
	start, err := decodeToken(token)
	if err != nil {
		return nil, "", err
	}
	keyCond := "currentHospitalId = :h"
	values := map[string]types.AttributeValue{":h": str(hospitalID)}
	if lastNamePrefix != "" {
		keyCond += " AND begins_with(lastName, :prefix)"
		values[":prefix"] = str(lastNamePrefix)
	}
	res, err := p.C.Query(ctx, &dynamodb.QueryInput{
		TableName:                 aws.String(p.Table),
		IndexName:                 aws.String(IndexPatientsByHospital),
		KeyConditionExpression:    aws.String(keyCond),
		ExpressionAttributeValues: values,
		Limit:                     aws.Int32(int32(limit)),
		ExclusiveStartKey:         start,
	})
	if err != nil {
		return nil, "", err
	}
	var out []models.PatientSummary
	if err := attributevalue.UnmarshalListOfMaps(res.Items, &out); err != nil {
		return nil, "", err
	}
	return out, encodeToken(res.LastEvaluatedKey), nil
}

// Put writes a patient (seed script).
func (p Patients) Put(ctx context.Context, x models.Patient) error {
	item, err := attributevalue.MarshalMap(x)
	if err != nil {
		return err
	}
	_, err = p.C.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(p.Table), Item: item})
	return err
}
