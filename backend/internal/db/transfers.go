package db

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"healthcloud/internal/models"
)

// ErrInvalidState means the transfer is not in the status the operation requires, e.g.
// accepting a transfer that was already decided, or the worker seeing a duplicate SQS message.
var ErrInvalidState = errors.New("transfer is not in the required state")

// Transfers covers the Transfers table plus the multi-table transactions that move beds and
// patients when a transfer progresses.
type Transfers struct {
	C      *dynamodb.Client
	Tables Tables
}

// Create stores a new QUEUED transfer together with its audit event, atomically.
func (t Transfers) Create(ctx context.Context, tr models.Transfer, ev models.AuditEvent) error {
	item, err := attributevalue.MarshalMap(tr)
	if err != nil {
		return err
	}
	auditItem, err := attributevalue.MarshalMap(ev)
	if err != nil {
		return err
	}
	_, err = t.C.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{
			{Put: &types.Put{
				TableName:           aws.String(t.Tables.Transfers),
				Item:                item,
				ConditionExpression: aws.String("attribute_not_exists(transferId)"),
			}},
			{Put: &types.Put{TableName: aws.String(t.Tables.AuditLog), Item: auditItem}},
		},
	})
	return err
}

// Get returns one transfer (access pattern T1).
func (t Transfers) Get(ctx context.Context, id string) (models.Transfer, error) {
	res, err := t.C.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      aws.String(t.Tables.Transfers),
		Key:            map[string]types.AttributeValue{"transferId": str(id)},
		ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return models.Transfer{}, err
	}
	if res.Item == nil {
		return models.Transfer{}, ErrNotFound
	}
	var out models.Transfer
	err = attributevalue.UnmarshalMap(res.Item, &out)
	return out, err
}

// List returns a hospital's incoming (T2) or outgoing (T3) transfers, newest first, optionally
// filtered by status.
func (t Transfers) List(ctx context.Context, hospitalID string, incoming bool, status models.TransferStatus, limit int, token string) ([]models.Transfer, string, error) {
	start, err := decodeToken(token)
	if err != nil {
		return nil, "", err
	}
	index, keyAttr := IndexTransfersOutgoing, "fromHospitalId"
	if incoming {
		index, keyAttr = IndexTransfersIncoming, "toHospitalId"
	}
	in := &dynamodb.QueryInput{
		TableName:                 aws.String(t.Tables.Transfers),
		IndexName:                 aws.String(index),
		KeyConditionExpression:    aws.String(keyAttr + " = :h"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":h": str(hospitalID)},
		ScanIndexForward:          aws.Bool(false),
		Limit:                     aws.Int32(int32(limit)),
		ExclusiveStartKey:         start,
	}
	if status != "" {
		in.FilterExpression = aws.String("#s = :s")
		in.ExpressionAttributeNames = map[string]string{"#s": "status"} // "status" is a reserved word
		in.ExpressionAttributeValues[":s"] = str(string(status))
	}
	res, err := t.C.Query(ctx, in)
	if err != nil {
		return nil, "", err
	}
	var out []models.Transfer
	if err := attributevalue.UnmarshalListOfMaps(res.Items, &out); err != nil {
		return nil, "", err
	}
	return out, encodeToken(res.LastEvaluatedKey), nil
}

// SetStatus moves a transfer from one status to another, e.g. QUEUED → NO_CAPACITY. The
// condition makes it a no-op (ErrInvalidState) if someone else changed it first.
func (t Transfers) SetStatus(ctx context.Context, id string, from, to models.TransferStatus, note string) error {
	_, err := t.C.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                aws.String(t.Tables.Transfers),
		Key:                      map[string]types.AttributeValue{"transferId": str(id)},
		UpdateExpression:         aws.String("SET #s = :to, decisionNote = :note, updatedAt = :now"),
		ConditionExpression:      aws.String("#s = :from"),
		ExpressionAttributeNames: map[string]string{"#s": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":to": str(string(to)), ":from": str(string(from)), ":note": str(note), ":now": str(Now()),
		},
	})
	if isConditionFailed(err) {
		return ErrInvalidState
	}
	return err
}

// HoldBed is the worker step (access pattern T4). In one transaction it:
//
//	[0] takes one unit at the destination: available - 1, ONLY IF available > 0 (race-safe)
//	[1] moves the transfer QUEUED → PENDING_APPROVAL, ONLY IF it is still QUEUED
//
// Condition [1] makes the worker idempotent. SQS may deliver the same message twice; the second
// delivery fails the condition, so no second bed is taken. It returns ErrInvalidState on
// duplicates and ErrNoCapacity when the destination is full.
func (t Transfers) HoldBed(ctx context.Context, tr models.Transfer) error {
	now := Now()
	err := transactWithRetry(ctx, t.C, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{
			{Update: &types.Update{
				TableName:           aws.String(t.Tables.Resources),
				Key:                 resourceKey(tr.ToHospitalID, tr.ResourceType),
				UpdateExpression:    aws.String("SET available = available - :one, version = version + :one, updatedAt = :now"),
				ConditionExpression: aws.String("available > :zero"),
				ExpressionAttributeValues: map[string]types.AttributeValue{
					":one": num(1), ":zero": num(0), ":now": str(now),
				},
			}},
			{Update: &types.Update{
				TableName:                aws.String(t.Tables.Transfers),
				Key:                      map[string]types.AttributeValue{"transferId": str(tr.TransferID)},
				UpdateExpression:         aws.String("SET #s = :pending, updatedAt = :now"),
				ConditionExpression:      aws.String("#s = :queued"),
				ExpressionAttributeNames: map[string]string{"#s": "status"},
				ExpressionAttributeValues: map[string]types.AttributeValue{
					":pending": str(string(models.TransferPendingApproval)), ":queued": str(string(models.TransferQueued)), ":now": str(now),
				},
			}},
		},
	})
	var tce *types.TransactionCanceledException
	if errors.As(err, &tce) && len(tce.CancellationReasons) == 2 {
		if failed(tce.CancellationReasons[1]) {
			return ErrInvalidState // already processed: a duplicate delivery
		}
		if failed(tce.CancellationReasons[0]) {
			return ErrNoCapacity
		}
	}
	return err
}

// Accept completes a transfer (access pattern T5). In one transaction it:
//
//	[0] transfer PENDING_APPROVAL → ACCEPTED (stops double-accept and accept-after-reject)
//	[1] patient moves: currentHospitalId = destination, admission = the held unit; ONLY IF the
//	    patient's admission is still what we read (or still absent)
//	[2] if the patient occupied a unit somewhere else, release it there: available + 1
//	[3] audit event
func (t Transfers) Accept(ctx context.Context, tr models.Transfer, decidedBy, note string, ev models.AuditEvent) error {
	res, err := t.C.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      aws.String(t.Tables.Patients),
		Key:            map[string]types.AttributeValue{"patientId": str(tr.PatientID)},
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

	now := Now()
	newAdm, err := attributevalue.MarshalMap(models.Admission{HospitalID: tr.ToHospitalID, ResourceType: tr.ResourceType, AdmittedAt: now})
	if err != nil {
		return err
	}
	auditItem, err := attributevalue.MarshalMap(ev)
	if err != nil {
		return err
	}

	patientUpdate := &types.Update{
		TableName:        aws.String(t.Tables.Patients),
		Key:              map[string]types.AttributeValue{"patientId": str(tr.PatientID)},
		UpdateExpression: aws.String("SET currentHospitalId = :to, admission = :adm, updatedAt = :now"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":to": str(tr.ToHospitalID), ":adm": &types.AttributeValueMemberM{Value: newAdm}, ":now": str(now),
		},
	}
	if p.Admission == nil {
		patientUpdate.ConditionExpression = aws.String("attribute_not_exists(admission)")
	} else {
		patientUpdate.ConditionExpression = aws.String("admission.hospitalId = :oldH AND admission.resourceType = :oldT")
		patientUpdate.ExpressionAttributeValues[":oldH"] = str(p.Admission.HospitalID)
		patientUpdate.ExpressionAttributeValues[":oldT"] = str(string(p.Admission.ResourceType))
	}

	items := []types.TransactWriteItem{
		{Update: &types.Update{
			TableName:                aws.String(t.Tables.Transfers),
			Key:                      map[string]types.AttributeValue{"transferId": str(tr.TransferID)},
			UpdateExpression:         aws.String("SET #s = :accepted, decidedBy = :by, decisionNote = :note, updatedAt = :now"),
			ConditionExpression:      aws.String("#s = :pending"),
			ExpressionAttributeNames: map[string]string{"#s": "status"},
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":accepted": str(string(models.TransferAccepted)), ":pending": str(string(models.TransferPendingApproval)),
				":by": str(decidedBy), ":note": str(note), ":now": str(now),
			},
		}},
		{Update: patientUpdate},
		{Put: &types.Put{TableName: aws.String(t.Tables.AuditLog), Item: auditItem}},
	}
	if p.Admission != nil {
		items = append(items, types.TransactWriteItem{Update: releaseUnit(t.Tables.Resources, p.Admission.HospitalID, p.Admission.ResourceType, now)})
	}

	err = transactWithRetry(ctx, t.C, &dynamodb.TransactWriteItemsInput{TransactItems: items})
	var tce *types.TransactionCanceledException
	if errors.As(err, &tce) && len(tce.CancellationReasons) >= 2 {
		if failed(tce.CancellationReasons[0]) {
			return ErrInvalidState
		}
		if failed(tce.CancellationReasons[1]) {
			return ErrConditionFailed // the patient's admission changed meanwhile; retry
		}
	}
	return err
}

// Reject declines a transfer and gives the held unit back (access pattern T6).
func (t Transfers) Reject(ctx context.Context, tr models.Transfer, decidedBy, note string, ev models.AuditEvent) error {
	auditItem, err := attributevalue.MarshalMap(ev)
	if err != nil {
		return err
	}
	now := Now()
	err = transactWithRetry(ctx, t.C, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{
			{Update: &types.Update{
				TableName:                aws.String(t.Tables.Transfers),
				Key:                      map[string]types.AttributeValue{"transferId": str(tr.TransferID)},
				UpdateExpression:         aws.String("SET #s = :rejected, decidedBy = :by, decisionNote = :note, updatedAt = :now"),
				ConditionExpression:      aws.String("#s = :pending"),
				ExpressionAttributeNames: map[string]string{"#s": "status"},
				ExpressionAttributeValues: map[string]types.AttributeValue{
					":rejected": str(string(models.TransferRejected)), ":pending": str(string(models.TransferPendingApproval)),
					":by": str(decidedBy), ":note": str(note), ":now": str(now),
				},
			}},
			{Update: releaseUnit(t.Tables.Resources, tr.ToHospitalID, tr.ResourceType, now)},
			{Put: &types.Put{TableName: aws.String(t.Tables.AuditLog), Item: auditItem}},
		},
	})
	var tce *types.TransactionCanceledException
	if errors.As(err, &tce) && len(tce.CancellationReasons) >= 2 {
		if failed(tce.CancellationReasons[0]) {
			return ErrInvalidState
		}
		if failed(tce.CancellationReasons[1]) {
			return ErrCapacityOverflow
		}
	}
	return err
}

// releaseUnit is the "give one unit back" update shared by discharge, accept and reject.
func releaseUnit(table, hospitalID string, rt models.ResourceType, now string) *types.Update {
	return &types.Update{
		TableName:                aws.String(table),
		Key:                      resourceKey(hospitalID, rt),
		UpdateExpression:         aws.String("SET available = available + :one, version = version + :one, updatedAt = :now"),
		ConditionExpression:      aws.String("available < #total"),
		ExpressionAttributeNames: map[string]string{"#total": "total"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":one": num(1), ":now": str(now),
		},
	}
}
