// Typed client for the HealthCloud API (see api/openapi.yaml).
import { getIdToken } from './auth';

// In dev, calls go through the Vite proxy (/api → backend). In a production build they go
// straight to the API Gateway URL.
export const API_BASE = import.meta.env.DEV ? '/api' : import.meta.env.VITE_API_URL;

// ---- types (mirror components/schemas in openapi.yaml) ----

export type ResourceType = 'ICU_BED' | 'GENERAL_BED' | 'VENTILATOR';
export const RESOURCE_LABELS: Record<ResourceType, string> = {
  ICU_BED: 'ICU beds',
  GENERAL_BED: 'General beds',
  VENTILATOR: 'Ventilators',
};

export type Hospital = { hospitalId: string; name: string; city?: string; address?: string; phone?: string };

export type Resource = {
  hospitalId: string;
  hospitalName?: string;
  resourceType: ResourceType;
  total: number;
  available: number;
  threshold: number;
  low: boolean;
  version: number;
  updatedAt?: string;
};

export type Admission = { hospitalId: string; resourceType: ResourceType; admittedAt: string };

export type PatientSummary = {
  patientId: string;
  firstName: string;
  lastName: string;
  dateOfBirth: string;
  currentHospitalId: string;
  admission: Admission | null;
};

export type Patient = PatientSummary & {
  sex: string;
  bloodGroup: string;
  allergies: string[];
  conditions: string[];
  homeHospitalId: string;
  emergencyContact?: { name: string; phone: string };
};

export type AuditEvent = {
  patientId: string;
  actorId: string;
  actorGroup: string;
  actorHospitalId?: string;
  action: string;
  targetId?: string;
  timestamp: string;
};

export type Page<T> = { items: T[]; nextToken?: string };

export type DocumentCategory = 'LAB_REPORT' | 'IMAGING' | 'PRESCRIPTION' | 'DISCHARGE_SUMMARY' | 'OTHER';

export type MedicalDocument = {
  patientId: string;
  documentId: string;
  title: string;
  category: DocumentCategory;
  fileName: string;
  contentType: string;
  sizeBytes: number;
  status: 'PENDING_UPLOAD' | 'AVAILABLE';
  uploadedBy: string;
  uploadedByHospitalId: string;
  createdAt: string;
};

export type Priority ='CRITICAL' | 'URGENT' | 'ROUTINE';
export type TransferStatus = 'QUEUED' | 'PENDING_APPROVAL' | 'ACCEPTED' | 'REJECTED' | 'NO_CAPACITY' | 'FAILED';

export type Transfer = {
  transferId: string;
  patientId: string;
  fromHospitalId: string;
  toHospitalId: string;
  resourceType: ResourceType;
  priority: Priority;
  reason?: string;
  status: TransferStatus;
  requestedBy: string;
  decidedBy?: string;
  decisionNote?: string;
  createdAt: string;
  updatedAt: string;
};

// ---- errors ----

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message);
  }
}

// ---- core fetch ----

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const token = await getIdToken();
  const res = await fetch(`${API_BASE}${path}`, {
    method,
    headers: {
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (res.status === 204) return undefined as T;
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new ApiError(res.status, data.code ?? 'HTTP_' + res.status, data.message ?? res.statusText);
  }
  return data as T;
}

const qs = (params: Record<string, string | undefined>) => {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v) p.set(k, v);
  const s = p.toString();
  return s ? `?${s}` : '';
};

// ---- endpoints ----

export const api = {
  health: () => request<{ status: string; stage: string }>('GET', '/health'),

  hospitals: () => request<Page<Hospital>>('GET', '/hospitals'),

  resources: (resourceType?: ResourceType, onlyAvailable?: boolean) =>
    request<{ cachedAt: string; items: Resource[] }>(
      'GET',
      `/resources${qs({ resourceType, onlyAvailable: onlyAvailable ? 'true' : undefined })}`,
    ),

  hospitalResources: (hospitalId: string) => request<Page<Resource>>('GET', `/hospitals/${hospitalId}/resources`),

  updateResource: (
    hospitalId: string,
    type: ResourceType,
    body: { total?: number; available?: number; threshold?: number; version: number },
  ) => request<Resource>('PUT', `/hospitals/${hospitalId}/resources/${type}`, body),

  patients: (hospitalId?: string, lastNamePrefix?: string, nextToken?: string) =>
    request<Page<PatientSummary>>('GET', `/patients${qs({ hospitalId, lastNamePrefix, nextToken })}`),

  patient: (patientId: string) => request<Patient>('GET', `/patients/${patientId}`),

  accessLog: (patientId: string, nextToken?: string) =>
    request<Page<AuditEvent>>('GET', `/patients/${patientId}/access-log${qs({ nextToken })}`),

  hospitalAudit: (nextToken?: string) => request<Page<AuditEvent>>('GET', `/audit${qs({ nextToken })}`),

  admit: (hospitalId: string, patientId: string, resourceType: ResourceType) =>
    request<Admission>('POST', `/hospitals/${hospitalId}/admissions`, { patientId, resourceType }),

  discharge: (hospitalId: string, patientId: string) =>
    request<void>('DELETE', `/hospitals/${hospitalId}/admissions/${patientId}`),

  requestTransfer: (body: { patientId: string; toHospitalId: string; resourceType: ResourceType; priority: Priority; reason?: string }) =>
    request<Transfer>('POST', '/transfers', body),

  transfers: (direction: 'incoming' | 'outgoing', status?: TransferStatus) =>
    request<Page<Transfer>>('GET', `/transfers${qs({ direction, status })}`),

  transfer: (transferId: string) => request<Transfer>('GET', `/transfers/${transferId}`),

  decideTransfer: (transferId: string, decision: 'ACCEPT' | 'REJECT', note?: string) =>
    request<Transfer>('POST', `/transfers/${transferId}/decision`, { decision, note }),

  documents: (patientId: string) => request<Page<MedicalDocument>>('GET', `/patients/${patientId}/documents`),

  document: (patientId: string, documentId: string) =>
    request<{ document: MedicalDocument; downloadUrl: string; expiresAt: string }>('GET', `/patients/${patientId}/documents/${documentId}`),

  // Upload in three steps: (1) ask the API for a presigned URL, (2) PUT the file DIRECTLY to S3,
  // (3) tell the API it's done. The file never passes through our Lambda.
  uploadDocument: async (patientId: string, file: File, title: string, category: DocumentCategory) => {
    const { document, uploadUrl } = await request<{ document: MedicalDocument; uploadUrl: string }>(
      'POST',
      `/patients/${patientId}/documents`,
      { title, category, fileName: file.name, contentType: file.type, sizeBytes: file.size },
    );
    const put = await fetch(uploadUrl, { method: 'PUT', headers: { 'Content-Type': file.type }, body: file });
    if (!put.ok) throw new ApiError(put.status, 'S3_UPLOAD_FAILED', `Upload to S3 failed (${put.status})`);
    return request<MedicalDocument>('POST', `/patients/${patientId}/documents/${document.documentId}/complete`);
  },
};
