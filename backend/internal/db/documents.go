package db

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"healthcloud/internal/models"
)

// Documents is the repository for document METADATA. The files themselves live in S3.
type Documents struct {
	C      *dynamodb.Client
	Tables Tables
}

// Create stores new PENDING_UPLOAD metadata and the UPLOAD_DOCUMENT audit event atomically
// (access pattern D3).
func (d Documents) Create(ctx context.Context, doc models.Document, ev models.AuditEvent) error {
	item, err := attributevalue.MarshalMap(doc)
	if err != nil {
		return err
	}
	auditItem, err := attributevalue.MarshalMap(ev)
	if err != nil {
		return err
	}
	_, err = d.C.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{
			{Put: &types.Put{
				TableName:           aws.String(d.Tables.Documents),
				Item:                item,
				ConditionExpression: aws.String("attribute_not_exists(documentId)"),
			}},
			{Put: &types.Put{TableName: aws.String(d.Tables.AuditLog), Item: auditItem}},
		},
	})
	return err
}

// List returns a patient's documents, newest first (access pattern D1). Document IDs are ULIDs,
// so sorting by the sort key is sorting by time.
func (d Documents) List(ctx context.Context, patientID string) ([]models.Document, error) {
	var out []models.Document
	p := dynamodb.NewQueryPaginator(d.C, &dynamodb.QueryInput{
		TableName:                 aws.String(d.Tables.Documents),
		KeyConditionExpression:    aws.String("patientId = :p"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":p": str(patientID)},
		ScanIndexForward:          aws.Bool(false),
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		var items []models.Document
		if err := attributevalue.UnmarshalListOfMaps(page.Items, &items); err != nil {
			return nil, err
		}
		out = append(out, items...)
	}
	return out, nil
}

// Get returns one document's metadata (access pattern D2).
func (d Documents) Get(ctx context.Context, patientID, documentID string) (models.Document, error) {
	res, err := d.C.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(d.Tables.Documents),
		Key:       map[string]types.AttributeValue{"patientId": str(patientID), "documentId": str(documentID)},
	})
	if err != nil {
		return models.Document{}, err
	}
	if res.Item == nil {
		return models.Document{}, ErrNotFound
	}
	var out models.Document
	err = attributevalue.UnmarshalMap(res.Item, &out)
	return out, err
}

// MarkAvailable flips PENDING_UPLOAD → AVAILABLE and records the real size (access pattern D4).
func (d Documents) MarkAvailable(ctx context.Context, patientID, documentID string, size int) (models.Document, error) {
	res, err := d.C.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                aws.String(d.Tables.Documents),
		Key:                      map[string]types.AttributeValue{"patientId": str(patientID), "documentId": str(documentID)},
		UpdateExpression:         aws.String("SET #s = :avail, sizeBytes = :size"),
		ConditionExpression:      aws.String("#s = :pending"),
		ExpressionAttributeNames: map[string]string{"#s": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":avail": str(models.DocAvailable), ":pending": str(models.DocPendingUpload), ":size": num(size),
		},
		ReturnValues: types.ReturnValueAllNew,
	})
	if isConditionFailed(err) {
		return models.Document{}, ErrInvalidState
	}
	if err != nil {
		return models.Document{}, err
	}
	var out models.Document
	err = attributevalue.UnmarshalMap(res.Attributes, &out)
	return out, err
}
