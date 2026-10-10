package db

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"healthcloud/internal/models"
)

// Errors returned by Admit and Discharge. Handlers map each one to a 409 with its own code.
var (
	ErrNoCapacity       = errors.New("no unit available")
	ErrAlreadyAdmitted  = errors.New("patient already admitted")
	ErrPatientNotFound  = errors.New("patient not found")
	ErrNotAdmittedHere  = errors.New("patient is not admitted at this hospital")
	ErrCapacityOverflow = errors.New("release would exceed total")
	ErrBusy             = errors.New("too much contention, try again")
)

// Admissions performs the race-safe bed and ventilator operations. It spans three tables, so
// it holds the client and all table names.
type Admissions struct {
	C      *dynamodb.Client
	Tables Tables
}

// Admit occupies one unit of resourceType at hospitalID for patientID.
//
// THIS IS THE RACE-SAFE OPERATION. Three writes run in one DynamoDB transaction
// (all succeed or none do):
//
//  1. Resources: available = available - 1, ONLY IF available > 0.
//     DynamoDB checks the condition and applies the update atomically on the item, so when two
//     requests race for the last bed, exactly one passes the check. The other is rejected
//     instead of driving available to -1. A read-then-write in application code could not
//     give this guarantee.
//  2. Patients: record the admission, ONLY IF the patient exists and has no admission yet
//     (one patient can't hold two beds).
//  3. AuditLog: append the ADMIT event, so the action and its audit record can't diverge.
func (a Admissions) Admit(ctx context.Context, hospitalID, patientID string, t models.ResourceType, ev models.AuditEvent) (models.Admission, error) {
	adm := models.Admission{HospitalID: hospitalID, ResourceType: t, AdmittedAt: Now()}
	admAV, err := attributevalue.MarshalMap(adm)
	if err != nil {
		return models.Admission{}, err
	}
	auditItem, err := attributevalue.MarshalMap(ev)
	if err != nil {
		return models.Admission{}, err
	}

	err = transactWithRetry(ctx, a.C, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{
			{Update: &types.Update{ // [0] take one unit
				TableName:           aws.String(a.Tables.Resources),
				Key:                 resourceKey(hospitalID, t),
				UpdateExpression:    aws.String("SET available = available - :one, version = version + :one, updatedAt = :now"),
				ConditionExpression: aws.String("available > :zero"),
				ExpressionAttributeValues: map[string]types.AttributeValue{
					":one": num(1), ":zero": num(0), ":now": str(adm.AdmittedAt),
				},
			}},
			{Update: &types.Update{ // [1] record it on the patient
				TableName:           aws.String(a.Tables.Patients),
				Key:                 map[string]types.AttributeValue{"patientId": str(patientID)},
				UpdateExpression:    aws.String("SET admission = :adm, currentHospitalId = :h, updatedAt = :now"),
				ConditionExpression: aws.String("attribute_exists(patientId) AND attribute_not_exists(admission)"),
				ExpressionAttributeValues: map[string]types.AttributeValue{
					":adm": &types.AttributeValueMemberM{Value: admAV}, ":h": str(hospitalID), ":now": str(adm.AdmittedAt),
				},
				// Return the patient's current state if this check fails, to tell "missing" from "already admitted".
				ReturnValuesOnConditionCheckFailure: types.ReturnValuesOnConditionCheckFailureAllOld,
			}},
			{Put: &types.Put{ // [2] audit
				TableName: aws.String(a.Tables.AuditLog),
				Item:      auditItem,
			}},
		},
	})
	if err == nil {
		adm.PatientID = patientID
		return adm, nil
	}

	// Work out WHICH condition failed from the per-item cancellation reasons (same order as above).
	var tce *types.TransactionCanceledException
	if errors.As(err, &tce) && len(tce.CancellationReasons) >= 2 {
		if failed(tce.CancellationReasons[0]) {
			return models.Admission{}, ErrNoCapacity
		}
		if r := tce.CancellationReasons[1]; failed(r) {
			if len(r.Item) == 0 {
				return models.Admission{}, ErrPatientNotFound
			}
			return models.Admission{}, ErrAlreadyAdmitted
		}
	}
	return models.Admission{}, err
}

// Discharge frees the unit a patient holds at hospitalID. One transaction:
//
//  1. Patients: remove the admission, ONLY IF it is at this hospital and of this type.
//  2. Resources: available = available + 1, ONLY IF available < total (never over capacity).
//  3. AuditLog: append the DISCHARGE event.
func (a Admissions) Discharge(ctx context.Context, hospitalID, patientID string, ev models.AuditEvent) error {
	// Read the admission first, to learn which resource type to release. The transaction's
	// condition re-checks it, so a concurrent change between this read and the write is still safe.
	res, err := a.C.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      aws.String(a.Tables.Patients),
		Key:            map[string]types.AttributeValue{"patientId": str(patientID)},
		ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return err
	}
	if res.Item == nil {
		return ErrPatientNotFound
	}
	var p models.Patient
	if err := attributevalue.UnmarshalMap(res.Item, &p); err != nil {
		return err
	}
	if p.Admission == nil || p.Admission.HospitalID != hospitalID {
		return ErrNotAdmittedHere
	}
	t := p.Admission.ResourceType

	auditItem, err := attributevalue.MarshalMap(ev)
	if err != nil {
		return err
	}
	now := Now()
	err = transactWithRetry(ctx, a.C, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{
			{Update: &types.Update{ // [0] clear the admission
				TableName:           aws.String(a.Tables.Patients),
				Key:                 map[string]types.AttributeValue{"patientId": str(patientID)},
				UpdateExpression:    aws.String("REMOVE admission SET updatedAt = :now"),
				ConditionExpression: aws.String("admission.hospitalId = :h AND admission.resourceType = :t"),
				ExpressionAttributeValues: map[string]types.AttributeValue{
					":h": str(hospitalID), ":t": str(string(t)), ":now": str(now),
				},
			}},
			{Update: &types.Update{ // [1] give the unit back
				TableName:                aws.String(a.Tables.Resources),
				Key:                      resourceKey(hospitalID, t),
				UpdateExpression:         aws.String("SET available = available + :one, version = version + :one, updatedAt = :now"),
				ConditionExpression:      aws.String("available < #total"),
				ExpressionAttributeNames: map[string]string{"#total": "total"},
				ExpressionAttributeValues: map[string]types.AttributeValue{
					":one": num(1), ":now": str(now),
				},
			}},
			{Put: &types.Put{TableName: aws.String(a.Tables.AuditLog), Item: auditItem}}, // [2]
		},
	})
	var tce *types.TransactionCanceledException
	if errors.As(err, &tce) && len(tce.CancellationReasons) >= 2 {
		if failed(tce.CancellationReasons[0]) {
			return ErrNotAdmittedHere // discharged concurrently by someone else
		}
		if failed(tce.CancellationReasons[1]) {
			return ErrCapacityOverflow // an admin lowered total meanwhile; needs manual fix
		}
	}
	return err
}

func failed(r types.CancellationReason) bool {
	return aws.ToString(r.Code) == "ConditionalCheckFailed"
}

// transactWithRetry runs a transaction, retrying when DynamoDB cancels it with
// "TransactionConflict". That happens when another transaction is touching the same item at
// the same moment (e.g. a burst of admissions for the last bed). A conflict means NOTHING was
// written, so retrying is safe. A retried request then either succeeds or hits a real
// condition failure (e.g. no capacity). If contention persists, it returns ErrBusy.
func transactWithRetry(ctx context.Context, c *dynamodb.Client, in *dynamodb.TransactWriteItemsInput) error {
	const attempts = 8
	backoff := 10 * time.Millisecond
	for i := 0; ; i++ {
		_, err := c.TransactWriteItems(ctx, in)
		if err == nil || !isTransactionConflict(err) {
			return err
		}
		if i == attempts-1 {
			return ErrBusy
		}
		// Exponential backoff with jitter, so retrying requests don't collide again.
		sleep := backoff + time.Duration(rand.Int64N(int64(backoff)))
		select {
		case <-time.After(sleep):
		case <-ctx.Done():
			return ctx.Err()
		}
		backoff *= 2
	}
}

func isTransactionConflict(err error) bool {
	var tce *types.TransactionCanceledException
	if !errors.As(err, &tce) {
		return false
	}
	for _, r := range tce.CancellationReasons {
		if aws.ToString(r.Code) == "TransactionConflict" {
			return true
		}
	}
	return false
}
