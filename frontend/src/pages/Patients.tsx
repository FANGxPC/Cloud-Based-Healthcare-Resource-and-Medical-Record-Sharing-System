// Doctor: browse patients at any hospital in the chain and open a record.
// Opening a record writes an AuditLog entry on the server.
import { useState } from 'react';
import { api, RESOURCE_LABELS, type Patient, type PatientSummary, type ResourceType } from '../api';
import { Card, Status, errorText, useLoad } from '../components/common';
import PatientProfile from './PatientProfile';

export default function Patients({ ownHospitalId }: { ownHospitalId: string | null }) {
  const [hospitalId, setHospitalId] = useState(ownHospitalId ?? 'H001');
  const [prefix, setPrefix] = useState('');
  const [selected, setSelected] = useState<string | null>(null);

  const hospitals = useLoad(() => api.hospitals(), []);
  const list = useLoad(() => api.patients(hospitalId, prefix || undefined), [hospitalId, prefix]);

  if (selected) return <PatientRecord patientId={selected} ownHospitalId={ownHospitalId} onBack={() => setSelected(null)} />;

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

function PatientRecord({ patientId, ownHospitalId, onBack }: { patientId: string; ownHospitalId: string | null; onBack: () => void }) {
  const { data, error, loading, reload } = useLoad(() => api.patient(patientId), [patientId]);
  return (
    <Card title="Patient record" actions={<button onClick={onBack}>← Back</button>}>
      <p className="alert warn small">This access has been recorded in the audit log.</p>
      <Status loading={loading} error={error} />
      {data && <PatientProfile p={data} />}
      {data && ownHospitalId && <AdmissionControls p={data} hospitalId={ownHospitalId} onChange={reload} />}
    </Card>
  );
}

// Admit into (or discharge from) the doctor's OWN hospital. The server enforces this rule and
// the race-safe bed count. A 409 NO_CAPACITY here means someone else just took the last unit.
function AdmissionControls({ p, hospitalId, onChange }: { p: Patient; hospitalId: string; onChange: () => void }) {
  const [type, setType] = useState<ResourceType>('GENERAL_BED');
  const [msg, setMsg] = useState<{ tone: 'good' | 'bad'; text: string } | null>(null);
  const [busy, setBusy] = useState(false);

  async function run(action: () => Promise<unknown>, ok: string) {
    setBusy(true);
    setMsg(null);
    try {
      await action();
      setMsg({ tone: 'good', text: ok });
      onChange();
    } catch (e) {
      setMsg({ tone: 'bad', text: errorText(e) });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="actions-box">
      <h3>Bed management · {hospitalId}</h3>
      {msg && <p className={`alert ${msg.tone}`}>{msg.text}</p>}
      {p.admission ? (
        p.admission.hospitalId === hospitalId ? (
          <button disabled={busy} onClick={() => run(() => api.discharge(hospitalId, p.patientId), 'Patient discharged; unit released.')}>
            Discharge
          </button>
        ) : (
          <p className="muted">Admitted at {p.admission.hospitalId}; only that hospital can discharge.</p>
        )
      ) : (
        <div className="row">
          <select value={type} onChange={(e) => setType(e.target.value as ResourceType)} aria-label="resource type">
            {(Object.keys(RESOURCE_LABELS) as ResourceType[]).map((t) => (
              <option key={t} value={t}>
                {RESOURCE_LABELS[t]}
              </option>
            ))}
          </select>
          <button className="primary" disabled={busy} onClick={() => run(() => api.admit(hospitalId, p.patientId, type), 'Patient admitted.')}>
            Admit here
          </button>
        </div>
      )}
    </div>
  );
}
