package db

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"healthcloud/internal/models"
)

// Resources is the repository for the Resources table (bed and ventilator counts).
type Resources struct {
	C     *dynamodb.Client
	Table string
}

// ListAll returns every resource row in the chain (access pattern R4). It is a Scan over a small
// table (hospitals × 3 types). The resources Lambda caches the result for ~15 s.
func (r Resources) ListAll(ctx context.Context) ([]models.Resource, error) {
	var out []models.Resource
	p := dynamodb.NewScanPaginator(r.C, &dynamodb.ScanInput{TableName: aws.String(r.Table)})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		var items []models.Resource
		if err := attributevalue.UnmarshalListOfMaps(page.Items, &items); err != nil {
			return nil, err
		}
		out = append(out, items...)
	}
	return out, nil
}

// ListByHospital returns one hospital's resources (access pattern R1): a Query on the partition key.
func (r Resources) ListByHospital(ctx context.Context, hospitalID string) ([]models.Resource, error) {
	res, err := r.C.Query(ctx, &dynamodb.QueryInput{
		TableName:                 aws.String(r.Table),
		KeyConditionExpression:    aws.String("hospitalId = :h"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":h": str(hospitalID)},
		ConsistentRead:            aws.Bool(true), // admins must see their own edits immediately
	})
	if err != nil {
		return nil, err
	}
	var out []models.Resource
	err = attributevalue.UnmarshalListOfMaps(res.Items, &out)
	return out, err
}

// Get returns one resource row (access pattern R2).
func (r Resources) Get(ctx context.Context, hospitalID string, t models.ResourceType) (models.Resource, error) {
	res, err := r.C.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      aws.String(r.Table),
		Key:            resourceKey(hospitalID, t),
		ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return models.Resource{}, err
	}
	if res.Item == nil {
		return models.Resource{}, ErrNotFound
	}
	var out models.Resource
	err = attributevalue.UnmarshalMap(res.Item, &out)
	return out, err
}

// UpdateCapacity is the admin edit (access pattern R7), protected by optimistic locking:
// the write only succeeds if the row's version still equals expectedVersion. Every write to a
// resource row increments version, including bed bookings, so an admin can never
// overwrite a booking that happened after they loaded the form. They get ErrConditionFailed
// (409 VERSION_CONFLICT), reload and retry.
func (r Resources) UpdateCapacity(ctx context.Context, hospitalID string, t models.ResourceType,
	total, available, threshold, expectedVersion int, updatedBy string) (models.Resource, error) {

	res, err := r.C.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:           aws.String(r.Table),
		Key:                 resourceKey(hospitalID, t),
		UpdateExpression:    aws.String("SET #total = :t, available = :a, threshold = :th, version = version + :one, updatedAt = :now, updatedBy = :by"),
		ConditionExpression: aws.String("attribute_exists(hospitalId) AND version = :v"),
		// "total" is not a reserved word, but naming it through #total shows the pattern used for reserved words.
		ExpressionAttributeNames: map[string]string{"#total": "total"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":t": num(total), ":a": num(available), ":th": num(threshold),
			":v": num(expectedVersion), ":one": num(1), ":now": str(Now()), ":by": str(updatedBy),
		},
		ReturnValues: types.ReturnValueAllNew,
	})
	if isConditionFailed(err) {
		return models.Resource{}, ErrConditionFailed
	}
	if err != nil {
		return models.Resource{}, err
	}
	var out models.Resource
	err = attributevalue.UnmarshalMap(res.Attributes, &out)
	return out, err
}

// Put writes a resource row (seed script).
func (r Resources) Put(ctx context.Context, x models.Resource) error {
	item, err := attributevalue.MarshalMap(x)
	if err != nil {
		return err
	}
	_, err = r.C.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(r.Table), Item: item})
	return err
}

func resourceKey(hospitalID string, t models.ResourceType) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{"hospitalId": str(hospitalID), "resourceType": str(string(t))}
}
