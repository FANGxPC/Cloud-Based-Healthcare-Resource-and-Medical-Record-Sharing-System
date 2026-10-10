// Doctor: request an emergency transfer for a patient. The API answers 202 immediately
// (queued). We then poll GET /transfers/{id} until the worker has processed it.
import { useEffect, useState, type FormEvent } from 'react';
import { api, RESOURCE_LABELS, type Hospital, type Priority, type ResourceType, type Transfer, type TransferStatus } from '../api';
import { Check, Send, Siren, X } from 'lucide-react';
import { Alert, errorText } from '../components/common';
import TransferStatusBadge from '../components/TransferStatusBadge';

export default function RequestTransfer({ patientId, fromHospitalId }: { patientId: string; fromHospitalId: string }) {
  const [hospitals, setHospitals] = useState<Hospital[]>([]);
  const [to, setTo] = useState('');
  const [type, setType] = useState<ResourceType>('ICU_BED');
  const [priority, setPriority] = useState<Priority>('URGENT');
  const [reason, setReason] = useState('');
  const [transfer, setTransfer] = useState<Transfer | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api.hospitals().then((h) => {
      const others = h.items.filter((x) => x.hospitalId !== fromHospitalId).sort((a, b) => a.hospitalId.localeCompare(b.hospitalId));
      setHospitals(others);
      setTo((cur) => cur || others[0]?.hospitalId || '');
    });
  }, [fromHospitalId]);

  // Poll every 2 s while the transfer is still QUEUED (the worker usually finishes in about a second).
  useEffect(() => {
    if (!transfer || transfer.status !== 'QUEUED') return;
    const id = setTimeout(() => {
      api.transfer(transfer.transferId).then(setTransfer).catch((e) => setError(errorText(e)));
    }, 2000);
    return () => clearTimeout(id);
  }, [transfer]);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      setTransfer(await api.requestTransfer({ patientId, toHospitalId: to, resourceType: type, priority, reason: reason || undefined }));
    } catch (err) {
      setError(errorText(err));
    }
  }

  return (
    <div className="section">
      <h3>
        <Siren size={17} /> Emergency transfer from {fromHospitalId}
      </h3>
      <form className="inline-form" onSubmit={submit}>
        <select value={to} onChange={(e) => setTo(e.target.value)} aria-label="destination">
          {hospitals.map((h) => (
            <option key={h.hospitalId} value={h.hospitalId}>
              To {h.hospitalId} · {h.name}
            </option>
          ))}
        </select>
        <select value={type} onChange={(e) => setType(e.target.value as ResourceType)} aria-label="resource">
          {(Object.keys(RESOURCE_LABELS) as ResourceType[]).map((t) => (
            <option key={t} value={t}>
              {RESOURCE_LABELS[t]}
            </option>
          ))}
        </select>
        <select value={priority} onChange={(e) => setPriority(e.target.value as Priority)} aria-label="priority">
          <option>CRITICAL</option>
          <option>URGENT</option>
          <option>ROUTINE</option>
        </select>
        <input placeholder="Reason (synthetic)" maxLength={500} value={reason} onChange={(e) => setReason(e.target.value)} />
        <button className="btn btn-primary" disabled={!to}>
          <Send size={16} /> Request transfer
        </button>
      </form>
      {error && <Alert tone="bad">{error}</Alert>}
      {transfer && <TransferProgress t={transfer} />}
    </div>
  );
}

// Three-step tracker: requested → bed held → decision. Updates live while polling.
type StepState = 'done' | 'now' | 'fail' | 'todo';
const STEPS: Record<TransferStatus, [StepState, StepState, StepState]> = {
  QUEUED: ['now', 'todo', 'todo'],
  PENDING_APPROVAL: ['done', 'now', 'todo'],
  ACCEPTED: ['done', 'done', 'done'],
  REJECTED: ['done', 'done', 'fail'],
  NO_CAPACITY: ['done', 'fail', 'todo'],
  FAILED: ['fail', 'todo', 'todo'],
};

function TransferProgress({ t }: { t: Transfer }) {
  const states = STEPS[t.status];
  const labels = [
    'Requested',
    t.status === 'NO_CAPACITY' ? `No free unit at ${t.toHospitalId}` : `Bed held at ${t.toHospitalId}`,
    t.status === 'REJECTED' ? 'Rejected' : 'Accepted',
  ];
  return (
    <div className="stepper fade-in">
      {labels.map((label, i) => (
        <div key={i} className={'step ' + states[i]}>
          <span className="step-dot">{states[i] === 'done' ? <Check size={13} /> : states[i] === 'fail' ? <X size={13} /> : i + 1}</span>
          <span className="step-label">{label}</span>
        </div>
      ))}
      <div className="step-meta">
        <TransferStatusBadge status={t.status} />
        <span className="mono subtle">{t.transferId}</span>
        {t.status === 'QUEUED' && <span className="spinner" />}
        {t.decisionNote && <span className="small muted">{t.decisionNote}</span>}
      </div>
    </div>
  );
}
