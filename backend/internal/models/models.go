// Package models defines the domain structs shared by the Lambdas, the seed script and tests.
//
// Each struct has two sets of tags:
//   - `dynamodbav` is the attribute name stored in DynamoDB (see docs/DATA_MODEL.md)
//   - `json` is the field name in API responses (see api/openapi.yaml)
package models

import "slices"

// ResourceType is a kind of tracked capacity.
type ResourceType string

const (
	ICUBed     ResourceType = "ICU_BED"
	GeneralBed ResourceType = "GENERAL_BED"
	Ventilator ResourceType = "VENTILATOR"
)

// ResourceTypes lists every valid ResourceType, in display order.
var ResourceTypes = []ResourceType{ICUBed, GeneralBed, Ventilator}

// Valid reports whether t is a known resource type (used to validate path and query input).
func (t ResourceType) Valid() bool { return slices.Contains(ResourceTypes, t) }

// Hospital is one hospital in the chain. Table: Hospitals (PK hospitalId).
type Hospital struct {
	HospitalID string `dynamodbav:"hospitalId" json:"hospitalId"`
	Name       string `dynamodbav:"name" json:"name"`
	City       string `dynamodbav:"city" json:"city,omitempty"`
	Address    string `dynamodbav:"address" json:"address,omitempty"`
	Phone      string `dynamodbav:"phone" json:"phone,omitempty"`
	CreatedAt  string `dynamodbav:"createdAt" json:"-"`
}

// Resource is the live capacity of one resource type at one hospital.
// Table: Resources (PK hospitalId, SK resourceType).
type Resource struct {
	HospitalID   string       `dynamodbav:"hospitalId" json:"hospitalId"`
	ResourceType ResourceType `dynamodbav:"resourceType" json:"resourceType"`
	HospitalName string       `dynamodbav:"hospitalName" json:"hospitalName,omitempty"`
	Total        int          `dynamodbav:"total" json:"total"`
	Available    int          `dynamodbav:"available" json:"available"`
	Threshold    int          `dynamodbav:"threshold" json:"threshold"`
	Version      int          `dynamodbav:"version" json:"version"`
	UpdatedAt    string       `dynamodbav:"updatedAt" json:"updatedAt,omitempty"`
	UpdatedBy    string       `dynamodbav:"updatedBy,omitempty" json:"-"`
	Low          bool         `dynamodbav:"-" json:"low"` // computed, not stored
}

// WithLow fills the computed Low flag (available below the alert threshold).
func (r Resource) WithLow() Resource {
	r.Low = r.Available < r.Threshold
	return r
}

// Admission records which bed or ventilator a patient currently occupies.
type Admission struct {
	PatientID    string       `dynamodbav:"-" json:"patientId,omitempty"`
	HospitalID   string       `dynamodbav:"hospitalId" json:"hospitalId"`
	ResourceType ResourceType `dynamodbav:"resourceType" json:"resourceType"`
	AdmittedAt   string       `dynamodbav:"admittedAt" json:"admittedAt"`
}

// EmergencyContact is a synthetic next-of-kin.
type EmergencyContact struct {
	Name  string `dynamodbav:"name" json:"name"`
	Phone string `dynamodbav:"phone" json:"phone"`
}

// Patient is a full (synthetic) patient record. Table: Patients (PK patientId).
type Patient struct {
	PatientID         string            `dynamodbav:"patientId" json:"patientId"`
	FirstName         string            `dynamodbav:"firstName" json:"firstName"`
	LastName          string            `dynamodbav:"lastName" json:"lastName"`
	DateOfBirth       string            `dynamodbav:"dateOfBirth" json:"dateOfBirth"`
	Sex               string            `dynamodbav:"sex" json:"sex"`
	BloodGroup        string            `dynamodbav:"bloodGroup" json:"bloodGroup"`
	Allergies         []string          `dynamodbav:"allergies" json:"allergies"`
	Conditions        []string          `dynamodbav:"conditions" json:"conditions"`
	EmergencyContact  *EmergencyContact `dynamodbav:"emergencyContact,omitempty" json:"emergencyContact,omitempty"`
	HomeHospitalID    string            `dynamodbav:"homeHospitalId" json:"homeHospitalId"`
	CurrentHospitalID string            `dynamodbav:"currentHospitalId" json:"currentHospitalId"`
	Admission         *Admission        `dynamodbav:"admission,omitempty" json:"admission"`
	CreatedAt         string            `dynamodbav:"createdAt" json:"createdAt"`
	UpdatedAt         string            `dynamodbav:"updatedAt" json:"updatedAt"`
}

// PatientSummary is the subset returned by list endpoints (and projected into the GSI).
type PatientSummary struct {
	PatientID         string     `dynamodbav:"patientId" json:"patientId"`
	FirstName         string     `dynamodbav:"firstName" json:"firstName"`
	LastName          string     `dynamodbav:"lastName" json:"lastName"`
	DateOfBirth       string     `dynamodbav:"dateOfBirth" json:"dateOfBirth"`
	CurrentHospitalID string     `dynamodbav:"currentHospitalId" json:"currentHospitalId"`
	Admission         *Admission `dynamodbav:"admission,omitempty" json:"admission"`
}

// AuditAction is what was done to a patient record.
type AuditAction string

const (
	ActionViewProfile     AuditAction = "VIEW_PROFILE"
	ActionListDocuments   AuditAction = "LIST_DOCUMENTS"
	ActionViewDocument    AuditAction = "VIEW_DOCUMENT"
	ActionUploadDocument  AuditAction = "UPLOAD_DOCUMENT"
	ActionAdmit           AuditAction = "ADMIT"
	ActionDischarge       AuditAction = "DISCHARGE"
	ActionRequestTransfer AuditAction = "REQUEST_TRANSFER"
	ActionDecideTransfer  AuditAction = "DECIDE_TRANSFER"
)

// AuditEvent is one append-only access record. Table: AuditLog (PK patientId, SK eventKey).
type AuditEvent struct {
	PatientID       string      `dynamodbav:"patientId" json:"patientId"`
	EventKey        string      `dynamodbav:"eventKey" json:"-"` // "{timestamp}#{ulid}": sorts by time, stays unique
	ActorID         string      `dynamodbav:"actorId" json:"actorId"`
	ActorGroup      string      `dynamodbav:"actorGroup" json:"actorGroup"`
	ActorHospitalID string      `dynamodbav:"actorHospitalId,omitempty" json:"actorHospitalId,omitempty"`
	Action          AuditAction `dynamodbav:"action" json:"action"`
	TargetID        string      `dynamodbav:"targetId,omitempty" json:"targetId,omitempty"`
	Timestamp       string      `dynamodbav:"timestamp" json:"timestamp"`
	RequestID       string      `dynamodbav:"requestId,omitempty" json:"-"`
}

// TransferStatus is the lifecycle of an emergency transfer:
//
//	QUEUED ──worker──► PENDING_APPROVAL (bed held) ──admin──► ACCEPTED | REJECTED (bed released)
//	   └────worker──► NO_CAPACITY | FAILED
type TransferStatus string

const (
	TransferQueued          TransferStatus = "QUEUED"
	TransferPendingApproval TransferStatus = "PENDING_APPROVAL"
	TransferAccepted        TransferStatus = "ACCEPTED"
	TransferRejected        TransferStatus = "REJECTED"
	TransferNoCapacity      TransferStatus = "NO_CAPACITY"
	TransferFailed          TransferStatus = "FAILED"
)

// Valid reports whether s is a known status (used to validate the ?status= filter).
func (s TransferStatus) Valid() bool {
	return slices.Contains([]TransferStatus{TransferQueued, TransferPendingApproval, TransferAccepted,
		TransferRejected, TransferNoCapacity, TransferFailed}, s)
}

// Priority of a transfer request.
type Priority string

const (
	PriorityCritical Priority = "CRITICAL"
	PriorityUrgent   Priority = "URGENT"
	PriorityRoutine  Priority = "ROUTINE"
)

func (p Priority) Valid() bool {
	return p == PriorityCritical || p == PriorityUrgent || p == PriorityRoutine
}

// Transfer is a request to move a patient to another hospital's bed or ventilator.
// Table: Transfers (PK transferId). GSIs: incoming (toHospitalId, createdAt) and outgoing (fromHospitalId, createdAt).
type Transfer struct {
	TransferID     string         `dynamodbav:"transferId" json:"transferId"`
	PatientID      string         `dynamodbav:"patientId" json:"patientId"`
	FromHospitalID string         `dynamodbav:"fromHospitalId" json:"fromHospitalId"`
	ToHospitalID   string         `dynamodbav:"toHospitalId" json:"toHospitalId"`
	ResourceType   ResourceType   `dynamodbav:"resourceType" json:"resourceType"`
	Priority       Priority       `dynamodbav:"priority" json:"priority"`
	Reason         string         `dynamodbav:"reason,omitempty" json:"reason,omitempty"`
	Status         TransferStatus `dynamodbav:"status" json:"status"`
	RequestedBy    string         `dynamodbav:"requestedBy" json:"requestedBy"`
	DecidedBy      string         `dynamodbav:"decidedBy,omitempty" json:"decidedBy,omitempty"`
	DecisionNote   string         `dynamodbav:"decisionNote,omitempty" json:"decisionNote,omitempty"`
	CreatedAt      string         `dynamodbav:"createdAt" json:"createdAt"`
	UpdatedAt      string         `dynamodbav:"updatedAt" json:"updatedAt"`
}

// TransferMessage is the SQS message body. It carries only the ID: the worker re-reads the
// current state from DynamoDB, so a stale or duplicate message can't apply old data.
type TransferMessage struct {
	TransferID string `json:"transferId"`
}
