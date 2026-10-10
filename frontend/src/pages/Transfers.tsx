// Transfers for the user's hospital. Incoming: the admin accepts or rejects pending requests.
// Outgoing: requests this hospital made, with their status.
import { useState } from 'react';
import { api, RESOURCE_LABELS, type Transfer } from '../api';
import { Badge, Card, Status, errorText, fmtTime, useLoad } from '../components/common';
import TransferStatusBadge from '../components/TransferStatusBadge';

export default function Transfers({ isAdmin }: { isAdmin: boolean }) {
  const [direction, setDirection] = useState<'incoming' | 'outgoing'>(isAdmin ? 'incoming' : 'outgoing');
  const { data, error, loading, reload } = useLoad(() => api.transfers(direction), [direction]);
  const [msg, setMsg] = useState<{ tone: 'good' | 'bad'; text: string } | null>(null);

  async function decide(t: Transfer, decision: 'ACCEPT' | 'REJECT') {
    const note = decision === 'REJECT' ? (prompt('Reason for rejecting (optional):') ?? '') : '';
    setMsg(null);
    try {
      await api.decideTransfer(t.transferId, decision, note || undefined);
      setMsg({ tone: 'good', text: `Transfer ${decision === 'ACCEPT' ? 'accepted' : 'rejected'}.` });
    } catch (e) {
      setMsg({ tone: 'bad', text: errorText(e) });
    }
    reload();
  }

  return (
    <Card
      title="Transfers"
      actions={
        <div className="row">
          <button className={direction === 'incoming' ? 'tab active' : 'tab'} onClick={() => setDirection('incoming')}>
            Incoming
          </button>
          <button className={direction === 'outgoing' ? 'tab active' : 'tab'} onClick={() => setDirection('outgoing')}>
            Outgoing
          </button>
          <button onClick={reload}>Refresh</button>
        </div>
      }
    >
      <Status loading={loading} error={error} />
      {msg && <p className={`alert ${msg.tone}`}>{msg.text}</p>}
      {data && data.items.length === 0 && <p className="muted">No {direction} transfers.</p>}
      {data && data.items.length > 0 && (
        <table>
          <thead>
            <tr>
              <th>Requested</th>
              <th>Route</th>
              <th>Resource</th>
              <th>Priority</th>
              <th>Status</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {data.items.map((t) => (
              <tr key={t.transferId}>
                <td>
                  {fmtTime(t.createdAt)}
                  <div className="mono small muted">{t.transferId}</div>
                </td>
                <td>
                  {t.fromHospitalId} → {t.toHospitalId}
                </td>
                <td>{RESOURCE_LABELS[t.resourceType]}</td>
                <td>
                  <Badge tone={t.priority === 'CRITICAL' ? 'bad' : t.priority === 'URGENT' ? 'warn' : 'neutral'}>{t.priority}</Badge>
                </td>
                <td>
                  <TransferStatusBadge status={t.status} />
                  {t.decisionNote && <div className="muted small">{t.decisionNote}</div>}
                </td>
                <td>
                  {isAdmin && direction === 'incoming' && t.status === 'PENDING_APPROVAL' && (
                    <div className="row">
                      <button className="primary" onClick={() => decide(t, 'ACCEPT')}>
                        Accept
                      </button>
                      <button onClick={() => decide(t, 'REJECT')}>Reject</button>
                    </div>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Card>
  );
}
