package db

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"healthcloud/internal/models"
)

// Hospitals is the repository for the Hospitals table.
type Hospitals struct {
	C     *dynamodb.Client
	Table string
}

// List returns every hospital (access pattern H2). A Scan is fine here because the chain has
// only a handful of hospitals. The loop follows pagination anyway, for correctness.
func (h Hospitals) List(ctx context.Context) ([]models.Hospital, error) {
	var out []models.Hospital
	p := dynamodb.NewScanPaginator(h.C, &dynamodb.ScanInput{TableName: aws.String(h.Table)})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		var items []models.Hospital
		if err := attributevalue.UnmarshalListOfMaps(page.Items, &items); err != nil {
			return nil, err
		}
		out = append(out, items...)
	}
	return out, nil
}

// Get returns one hospital (access pattern H1) or ErrNotFound.
func (h Hospitals) Get(ctx context.Context, id string) (models.Hospital, error) {
	res, err := h.C.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(h.Table),
		Key:       map[string]types.AttributeValue{"hospitalId": str(id)},
	})
	if err != nil {
		return models.Hospital{}, err
	}
	if res.Item == nil {
		return models.Hospital{}, ErrNotFound
	}
	var out models.Hospital
	err = attributevalue.UnmarshalMap(res.Item, &out)
	return out, err
}

// Put writes a hospital (seed script).
func (h Hospitals) Put(ctx context.Context, x models.Hospital) error {
	item, err := attributevalue.MarshalMap(x)
	if err != nil {
		return err
	}
	_, err = h.C.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(h.Table), Item: item})
	return err
}
