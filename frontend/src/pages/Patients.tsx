// Doctor: browse patients at any hospital in the chain and open a record.
// Opening a record writes an AuditLog entry on the server.
import { useState } from 'react';
import { ArrowLeft, BedDouble, ChevronRight, LogIn, LogOut, Search, Users } from 'lucide-react';
import { api, RESOURCE_LABELS, type Patient, type PatientSummary, type ResourceType } from '../api';
import { Alert, Avatar, Badge, Card, Empty, PageHeader, Status, ageFrom, errorText, useLoad } from '../components/common';
import Documents from './Documents';
import PatientProfile from './PatientProfile';
import RequestTransfer from './RequestTransfer';

export default function Patients({ ownHospitalId }: { ownHospitalId: string | null }) {
  const [hospitalId, setHospitalId] = useState(ownHospitalId ?? 'H001');
  const [prefix, setPrefix] = useState('');
  const [selected, setSelected] = useState<string | null>(null);

  const hospitals = useLoad(() => api.hospitals(), []);
  const list = useLoad(() => api.patients(hospitalId, prefix || undefined), [hospitalId, prefix]);

  if (selected) return <PatientRecord patientId={selected} ownHospitalId={ownHospitalId} onBack={() => setSelected(null)} />;

  const hospitalOptions = [...(hospitals.data?.items ?? [{ hospitalId, name: hospitalId }])].sort((a, b) => a.hospitalId.localeCompare(b.hospitalId));

  return (
    <>
      <PageHeader title="Patients" subtitle="Shared records across the hospital network. Every record you open is audited." />
      <Card
        title={`Patients at ${hospitalId}`}
        icon={Users}
        flush
        actions={
          <>
            <select value={hospitalId} onChange={(e) => setHospitalId(e.target.value)} aria-label="hospital">
              {hospitalOptions.map((h) => (
                <option key={h.hospitalId} value={h.hospitalId}>
                  {h.hospitalId} · {h.name}
                </option>
              ))}
            </select>
            <span className="search">
              <Search size={16} />
              <input placeholder="Surname starts with…" value={prefix} onChange={(e) => setPrefix(e.target.value)} />
            </span>
          </>
        }
      >
        <Status loading={list.loading} error={list.error} />
        {list.data && <PatientTable items={list.data.items} onOpen={setSelected} />}
      </Card>
    </>
  );
}

function PatientTable({ items, onOpen }: { items: PatientSummary[]; onOpen: (id: string) => void }) {
  if (items.length === 0) return <Empty icon={Users} title="No patients found" hint="Try another hospital or surname." />;
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Patient</th>
            <th>Age</th>
            <th>Status</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {items.map((p) => (
            <tr key={p.patientId} style={{ cursor: 'pointer' }} onClick={() => onOpen(p.patientId)}>
              <td>
                <div className="cell-person">
                  <Avatar name={`${p.firstName} ${p.lastName}`} seed={p.patientId} />
                  <div>
                    <strong>
                      {p.firstName} {p.lastName}
                    </strong>
                    <div className="small muted">Born {p.dateOfBirth}</div>
                  </div>
                </div>
              </td>
              <td>{ageFrom(p.dateOfBirth) ?? '—'}</td>
              <td>
                {p.admission ? (
                  <Badge tone="warn">
                    {RESOURCE_LABELS[p.admission.resourceType]} · {p.admission.hospitalId}
                  </Badge>
                ) : (
                  <Badge tone="neutral">Outpatient</Badge>
                )}
              </td>
              <td style={{ textAlign: 'right' }}>
                <button className="btn btn-sm">
                  Open <ChevronRight size={15} />
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function PatientRecord({ patientId, ownHospitalId, onBack }: { patientId: string; ownHospitalId: string | null; onBack: () => void }) {
  const { data, error, loading, reload } = useLoad(() => api.patient(patientId), [patientId]);
  return (
    <>
      <div className="row">
        <button className="btn btn-ghost" onClick={onBack}>
          <ArrowLeft size={17} /> Back to patients
        </button>
      </div>
      <Alert tone="info">
        This access has been recorded in the audit log, and the patient can see it.
      </Alert>
      <Status loading={loading} error={error} />
      {data && (
        <section className="card fade-in">
          <PatientProfile p={data} />
          {ownHospitalId && <AdmissionControls p={data} hospitalId={ownHospitalId} onChange={reload} />}
          {ownHospitalId && <RequestTransfer patientId={data.patientId} fromHospitalId={ownHospitalId} />}
        </section>
      )}
      {data && <Documents patientId={data.patientId} canUpload />}
    </>
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
    <div className="section">
      <h3>
        <BedDouble size={17} /> Bed management · {hospitalId}
      </h3>
      {msg && <Alert tone={msg.tone}>{msg.text}</Alert>}
      {p.admission ? (
        p.admission.hospitalId === hospitalId ? (
          <div className="inline-form">
            <span className="section-hint">
              Occupying a {RESOURCE_LABELS[p.admission.resourceType].replace(/s$/, '').toLowerCase()} here.
            </span>
            <button className="btn btn-danger" disabled={busy} onClick={() => run(() => api.discharge(hospitalId, p.patientId), 'Patient discharged; unit released.')}>
              <LogOut size={16} /> Discharge
            </button>
          </div>
        ) : (
          <span className="section-hint">Admitted at {p.admission.hospitalId}. Only that hospital can discharge.</span>
        )
      ) : (
        <div className="inline-form">
          <select value={type} onChange={(e) => setType(e.target.value as ResourceType)} aria-label="resource type">
            {(Object.keys(RESOURCE_LABELS) as ResourceType[]).map((t) => (
              <option key={t} value={t}>
                {RESOURCE_LABELS[t]}
              </option>
            ))}
          </select>
          <button className="btn btn-primary" disabled={busy} onClick={() => run(() => api.admit(hospitalId, p.patientId, type), 'Patient admitted.')}>
            <LogIn size={16} /> Admit here
          </button>
          <span className="section-hint">Race-safe: the last free unit can never be given out twice.</span>
        </div>
      )}
    </div>
  );
}
