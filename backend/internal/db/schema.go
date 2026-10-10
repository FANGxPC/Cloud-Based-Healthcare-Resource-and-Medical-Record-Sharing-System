package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// CreateTables creates every table in DynamoDB Local, for the seed script's -local mode and
// integration tests. In AWS the tables are created by template.yaml, not by this function.
// Keep the two definitions in sync.
func CreateTables(ctx context.Context, c *dynamodb.Client, t Tables) error {
	s := func(name string) types.AttributeDefinition {
		return types.AttributeDefinition{AttributeName: aws.String(name), AttributeType: types.ScalarAttributeTypeS}
	}
	key := func(pk, sk string) []types.KeySchemaElement {
		k := []types.KeySchemaElement{{AttributeName: aws.String(pk), KeyType: types.KeyTypeHash}}
		if sk != "" {
			k = append(k, types.KeySchemaElement{AttributeName: aws.String(sk), KeyType: types.KeyTypeRange})
		}
		return k
	}
	gsi := func(name, pk, sk string) types.GlobalSecondaryIndex {
		return types.GlobalSecondaryIndex{
			IndexName:  aws.String(name),
			KeySchema:  key(pk, sk),
			Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll},
		}
	}

	defs := []dynamodb.CreateTableInput{
		{
			TableName:            aws.String(t.Hospitals),
			AttributeDefinitions: []types.AttributeDefinition{s("hospitalId")},
			KeySchema:            key("hospitalId", ""),
		},
		{
			TableName:              aws.String(t.Resources),
			AttributeDefinitions:   []types.AttributeDefinition{s("hospitalId"), s("resourceType")},
			KeySchema:              key("hospitalId", "resourceType"),
			GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{gsi(IndexResourcesByType, "resourceType", "hospitalId")},
		},
		{
			TableName:              aws.String(t.Patients),
			AttributeDefinitions:   []types.AttributeDefinition{s("patientId"), s("currentHospitalId"), s("lastName")},
			KeySchema:              key("patientId", ""),
			GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{gsi(IndexPatientsByHospital, "currentHospitalId", "lastName")},
		},
		{
			TableName:              aws.String(t.AuditLog),
			AttributeDefinitions:   []types.AttributeDefinition{s("patientId"), s("eventKey"), s("actorHospitalId")},
			KeySchema:              key("patientId", "eventKey"),
			GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{gsi(IndexAuditByActorHospital, "actorHospitalId", "eventKey")},
		},
		{
			TableName:            aws.String(t.Documents),
			AttributeDefinitions: []types.AttributeDefinition{s("patientId"), s("documentId")},
			KeySchema:            key("patientId", "documentId"),
		},
		{
			TableName:            aws.String(t.Transfers),
			AttributeDefinitions: []types.AttributeDefinition{s("transferId"), s("toHospitalId"), s("fromHospitalId"), s("createdAt")},
			KeySchema:            key("transferId", ""),
			GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{
				gsi(IndexTransfersIncoming, "toHospitalId", "createdAt"),
				gsi(IndexTransfersOutgoing, "fromHospitalId", "createdAt"),
			},
		},
	}

	for _, d := range defs {
		d.BillingMode = types.BillingModePayPerRequest
		_, err := c.CreateTable(ctx, &d)
		var exists *types.ResourceInUseException
		if err != nil && !errors.As(err, &exists) {
			return fmt.Errorf("create %s: %w", aws.ToString(d.TableName), err)
		}
	}
	return nil
}

// DeleteTables removes the tables (integration-test cleanup, DynamoDB Local only).
func DeleteTables(ctx context.Context, c *dynamodb.Client, t Tables) {
	for _, name := range []string{t.Hospitals, t.Resources, t.Patients, t.AuditLog, t.Documents, t.Transfers} {
		_, _ = c.DeleteTable(ctx, &dynamodb.DeleteTableInput{TableName: aws.String(name)})
	}
}
