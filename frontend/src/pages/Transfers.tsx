// Transfers for the user's hospital. Incoming: the admin accepts or rejects pending requests.
// Outgoing: requests this hospital made, with their status.
import { useState } from 'react';
import { ArrowLeftRight, ArrowRight, Check, Inbox, RefreshCw, Send, X } from 'lucide-react';
import { api, RESOURCE_LABELS, type Transfer } from '../api';
import { Alert, Badge, Card, Empty, Kpi, PageHeader, Status, errorText, fmtTime, useLoad } from '../components/common';
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
      setMsg({ tone: 'good', text: `Transfer ${decision === 'ACCEPT' ? 'accepted: the patient has been moved' : 'rejected: the held bed was released'}.` });
    } catch (e) {
      setMsg({ tone: 'bad', text: errorText(e) });
    }
    reload();
  }

  const items = data?.items ?? [];
  const pending = items.filter((t) => t.status === 'PENDING_APPROVAL').length;
  const accepted = items.filter((t) => t.status === 'ACCEPTED').length;
  const critical = items.filter((t) => t.priority === 'CRITICAL').length;

  return (
    <>
      <PageHeader
        title="Emergency transfers"
        subtitle={isAdmin ? 'Approve incoming patients. A bed is already held for every pending request.' : 'Track transfer requests from your hospital.'}
        actions={
          <>
            <div className="segmented">
              <button className={direction === 'incoming' ? 'on' : ''} onClick={() => setDirection('incoming')}>
                <Inbox size={14} style={{ verticalAlign: '-2px' }} /> Incoming
              </button>
              <button className={direction === 'outgoing' ? 'on' : ''} onClick={() => setDirection('outgoing')}>
                <Send size={14} style={{ verticalAlign: '-2px' }} /> Outgoing
              </button>
            </div>
            <button className="btn" onClick={reload}>
              <RefreshCw size={16} /> Refresh
            </button>
          </>
        }
      />

      {data && (
        <div className="kpis">
          <Kpi label="Awaiting decision" value={pending} icon={Inbox} tone={pending ? 'warn' : 'good'} foot={pending ? 'Beds held for these patients' : 'Nothing pending'} />
          <Kpi label="Accepted" value={accepted} icon={Check} tone="good" foot={`of ${items.length} ${direction} transfers`} />
          <Kpi label="Critical priority" value={critical} icon={ArrowLeftRight} tone="crit" foot="CRITICAL requests in this list" />
        </div>
      )}

      {msg && <Alert tone={msg.tone}>{msg.text}</Alert>}

      <Card title={direction === 'incoming' ? 'Incoming requests' : 'Outgoing requests'} icon={ArrowLeftRight} flush>
        <Status loading={loading} error={error} />
        {data && items.length === 0 && <Empty icon={Inbox} title={`No ${direction} transfers yet`} hint="Doctors request transfers from a patient's record." />}
        {items.length > 0 && (
          <div className="table-wrap">
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
                {items.map((t) => (
                  <tr key={t.transferId}>
                    <td>
                      <strong>{fmtTime(t.createdAt)}</strong>
                      <div className="mono subtle">{t.transferId.slice(-10)}</div>
                    </td>
                    <td>
                      <span className="route">
                        {t.fromHospitalId} <ArrowRight size={15} className="arrow" /> {t.toHospitalId}
                      </span>
                    </td>
                    <td>{RESOURCE_LABELS[t.resourceType]}</td>
                    <td>
                      <Badge tone={t.priority === 'CRITICAL' ? 'bad' : t.priority === 'URGENT' ? 'warn' : 'neutral'}>{t.priority}</Badge>
                    </td>
                    <td>
                      <TransferStatusBadge status={t.status} />
                      {t.decisionNote && <div className="small muted">{t.decisionNote}</div>}
                    </td>
                    <td style={{ textAlign: 'right' }}>
                      {isAdmin && direction === 'incoming' && t.status === 'PENDING_APPROVAL' && (
                        <div className="row" style={{ justifyContent: 'flex-end' }}>
                          <button className="btn btn-primary btn-sm" onClick={() => decide(t, 'ACCEPT')}>
                            <Check size={15} /> Accept
                          </button>
                          <button className="btn btn-sm btn-danger" onClick={() => decide(t, 'REJECT')}>
                            <X size={15} /> Reject
                          </button>
                        </div>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </>
  );
}
