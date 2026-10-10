// Doctor: browse patients at any hospital in the chain and open a record.
// Opening a record writes an AuditLog entry on the server.
import { useState } from 'react';
import { api, type PatientSummary } from '../api';
import { Card, Status, useLoad } from '../components/common';
import PatientProfile from './PatientProfile';

export default function Patients({ ownHospitalId }: { ownHospitalId: string | null }) {
  const [hospitalId, setHospitalId] = useState(ownHospitalId ?? 'H001');
  const [prefix, setPrefix] = useState('');
  const [selected, setSelected] = useState<string | null>(null);

  const hospitals = useLoad(() => api.hospitals(), []);
  const list = useLoad(() => api.patients(hospitalId, prefix || undefined), [hospitalId, prefix]);

  if (selected) return <PatientRecord patientId={selected} onBack={() => setSelected(null)} />;

  return (
    <Card
      title="Patients"
      actions={
        <div className="row">
          <select value={hospitalId} onChange={(e) => setHospitalId(e.target.value)} aria-label="hospital">
            {(hospitals.data?.items ?? [{ hospitalId, name: hospitalId }]).map((h) => (
              <option key={h.hospitalId} value={h.hospitalId}>
                {h.hospitalId} · {h.name}
              </option>
            ))}
          </select>
          <input placeholder="Surname starts with…" value={prefix} onChange={(e) => setPrefix(e.target.value)} />
        </div>
      }
    >
      <Status loading={list.loading} error={list.error} />
      {list.data && (
        <PatientTable items={list.data.items} onOpen={setSelected} />
      )}
    </Card>
  );
}

function PatientTable({ items, onOpen }: { items: PatientSummary[]; onOpen: (id: string) => void }) {
  if (items.length === 0) return <p className="muted">No patients found.</p>;
  return (
    <table>
      <thead>
        <tr>
          <th>Name</th>
          <th>Date of birth</th>
          <th>Status</th>
          <th />
        </tr>
      </thead>
      <tbody>
        {items.map((p) => (
          <tr key={p.patientId}>
            <td>
              {p.lastName}, {p.firstName}
            </td>
            <td>{p.dateOfBirth}</td>
            <td>{p.admission ? `Admitted (${p.admission.resourceType})` : '—'}</td>
            <td>
              <button onClick={() => onOpen(p.patientId)}>Open</button>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function PatientRecord({ patientId, onBack }: { patientId: string; onBack: () => void }) {
  const { data, error, loading } = useLoad(() => api.patient(patientId), [patientId]);
  return (
    <Card title="Patient record" actions={<button onClick={onBack}>← Back</button>}>
      <p className="alert warn small">This access has been recorded in the audit log.</p>
      <Status loading={loading} error={error} />
      {data && <PatientProfile p={data} />}
    </Card>
  );
}
