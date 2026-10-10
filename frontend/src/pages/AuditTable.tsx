import { type AuditEvent } from '../api';
import { Badge, fmtTime } from '../components/common';

const ACTION_LABELS: Record<string, string> = {
  VIEW_PROFILE: 'Viewed profile',
  LIST_DOCUMENTS: 'Listed documents',
  VIEW_DOCUMENT: 'Opened document',
  UPLOAD_DOCUMENT: 'Uploaded document',
  ADMIT: 'Admitted',
  DISCHARGE: 'Discharged',
  REQUEST_TRANSFER: 'Requested transfer',
  DECIDE_TRANSFER: 'Decided transfer',
};

// Shared table for the patient's "who viewed my record" and the admin's staff-access audit.
export default function AuditTable({ items, showPatient }: { items: AuditEvent[]; showPatient?: boolean }) {
  if (items.length === 0) return <p className="muted">No accesses recorded yet.</p>;
  return (
    <table>
      <thead>
        <tr>
          <th>When</th>
          <th>Who</th>
          <th>Hospital</th>
          <th>Action</th>
          {showPatient && <th>Patient</th>}
        </tr>
      </thead>
      <tbody>
        {items.map((e, i) => (
          <tr key={`${e.timestamp}-${i}`}>
            <td>{fmtTime(e.timestamp)}</td>
            <td>
              <Badge tone="neutral">{e.actorGroup}</Badge> <span className="mono small">{e.actorId.slice(0, 8)}…</span>
            </td>
            <td>{e.actorHospitalId ?? '—'}</td>
            <td>{ACTION_LABELS[e.action] ?? e.action}</td>
            {showPatient && <td className="mono small">{e.patientId.slice(0, 8)}…</td>}
          </tr>
        ))}
      </tbody>
    </table>
  );
}
