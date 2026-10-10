import { ShieldCheck } from 'lucide-react';
import { type AuditEvent } from '../api';
import { Avatar, Badge, Empty, fmtTime } from '../components/common';

const ACTION: Record<string, { label: string; tone: 'info' | 'warn' | 'good' | 'neutral' | 'bad' }> = {
  VIEW_PROFILE: { label: 'Viewed profile', tone: 'info' },
  LIST_DOCUMENTS: { label: 'Listed documents', tone: 'neutral' },
  VIEW_DOCUMENT: { label: 'Opened document', tone: 'info' },
  UPLOAD_DOCUMENT: { label: 'Uploaded document', tone: 'good' },
  ADMIT: { label: 'Admitted', tone: 'warn' },
  DISCHARGE: { label: 'Discharged', tone: 'good' },
  REQUEST_TRANSFER: { label: 'Requested transfer', tone: 'warn' },
  DECIDE_TRANSFER: { label: 'Decided transfer', tone: 'neutral' },
};

// Shared table for the patient's "who viewed my record" and the admin's staff-access audit.
export default function AuditTable({ items, showPatient }: { items: AuditEvent[]; showPatient?: boolean }) {
  if (items.length === 0) return <Empty icon={ShieldCheck} title="No accesses recorded yet" hint="Every view by a doctor will appear here." />;
  return (
    <div className="table-wrap">
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
          {items.map((e, i) => {
            const a = ACTION[e.action] ?? { label: e.action, tone: 'neutral' as const };
            return (
              <tr key={`${e.timestamp}-${i}`}>
                <td>{fmtTime(e.timestamp)}</td>
                <td>
                  <div className="cell-person">
                    <Avatar name={e.actorGroup} seed={e.actorId} size="sm" />
                    <div>
                      <strong>{e.actorGroup === 'HospitalAdmin' ? 'Hospital admin' : e.actorGroup}</strong>
                      <div className="mono subtle">{e.actorId.slice(0, 8)}</div>
                    </div>
                  </div>
                </td>
                <td>{e.actorHospitalId ?? '—'}</td>
                <td>
                  <Badge tone={a.tone}>{a.label}</Badge>
                </td>
                {showPatient && <td className="mono subtle">{e.patientId.slice(0, 8)}</td>}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
