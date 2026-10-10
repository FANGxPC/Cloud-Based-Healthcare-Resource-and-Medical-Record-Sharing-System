// Doctor: request an emergency transfer for a patient. The API answers 202 immediately
// (queued). We then poll GET /transfers/{id} until the worker has processed it.
import { useEffect, useState, type FormEvent } from 'react';
import { api, RESOURCE_LABELS, type Hospital, type Priority, type ResourceType, type Transfer } from '../api';
import { errorText } from '../components/common';
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
    <div className="actions-box">
      <h3>Emergency transfer from {fromHospitalId}</h3>
      <form className="row" onSubmit={submit}>
        <select value={to} onChange={(e) => setTo(e.target.value)} aria-label="destination">
          {hospitals.map((h) => (
            <option key={h.hospitalId} value={h.hospitalId}>
              → {h.hospitalId} · {h.name}
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
        <button className="primary" disabled={!to}>
          Request transfer
        </button>
      </form>
      {error && <p className="alert bad">{error}</p>}
      {transfer && (
        <p>
          Transfer <span className="mono small">{transfer.transferId}</span>: <TransferStatusBadge status={transfer.status} />
          {transfer.status === 'QUEUED' && <span className="muted small"> processing…</span>}
          {transfer.status === 'PENDING_APPROVAL' && <span className="muted small"> bed held; waiting for {transfer.toHospitalId} to approve</span>}
          {transfer.decisionNote && <span className="muted small"> ({transfer.decisionNote})</span>}
        </p>
      )}
    </div>
  );
}
