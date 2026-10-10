// HospitalAdmin: edit capacity, availability and alert thresholds for their OWN hospital.
// Each save sends the version the admin loaded. If the row changed meanwhile (e.g. a bed was
// booked), the API answers 409 VERSION_CONFLICT and we reload instead of overwriting.
import { useState } from 'react';
import { BedDouble, HeartPulse, Save, Wind } from 'lucide-react';
import { api, ApiError, RESOURCE_LABELS, type Resource } from '../api';
import { Alert, Badge, PageHeader, Status, errorText, fmtTime, useLoad } from '../components/common';

const ICONS = { ICU_BED: HeartPulse, GENERAL_BED: BedDouble, VENTILATOR: Wind };

export default function ManageResources({ hospitalId }: { hospitalId: string }) {
  const { data, error, loading, reload } = useLoad(() => api.hospitalResources(hospitalId), [hospitalId]);
  const [message, setMessage] = useState<{ tone: 'good' | 'bad' | 'warn'; text: string } | null>(null);

  async function save(r: Resource, form: HTMLFormElement) {
    const f = new FormData(form);
    try {
      await api.updateResource(hospitalId, r.resourceType, {
        total: Number(f.get('total')),
        available: Number(f.get('available')),
        threshold: Number(f.get('threshold')),
        version: r.version,
      });
      setMessage({ tone: 'good', text: `${RESOURCE_LABELS[r.resourceType]} updated.` });
    } catch (e) {
      const conflict = e instanceof ApiError && e.code === 'VERSION_CONFLICT';
      setMessage({
        tone: conflict ? 'warn' : 'bad',
        text: conflict ? 'This resource changed while you were editing (e.g. a bed was just booked). The latest values have been loaded; please re-apply.' : errorText(e),
      });
    }
    reload();
  }

  return (
    <>
      <PageHeader title="Manage resources" subtitle={`Capacity and alert thresholds for ${hospitalId}. Alerts are emailed when availability drops below the threshold.`} />
      <Status loading={loading && !data} error={error} />
      {message && <Alert tone={message.tone}>{message.text}</Alert>}
      {data && (
        <div className="hosp-grid">
          {data.items.map((r) => {
            const Icon = ICONS[r.resourceType];
            const tone = r.available === 0 ? 'bad' : r.low ? 'warn' : 'good';
            return (
              // key includes version so the inputs reset to fresh values after each reload
              <form
                key={`${r.resourceType}-${r.version}`}
                className="hosp"
                onSubmit={(e) => {
                  e.preventDefault();
                  save(r, e.currentTarget);
                }}
              >
                <div className="hosp-head">
                  <div className="title row">
                    <span className="card-icon">
                      <Icon size={17} />
                    </span>
                    <div>
                      <div className="hosp-name">{RESOURCE_LABELS[r.resourceType]}</div>
                      <div className="hosp-meta">Updated {fmtTime(r.updatedAt)}</div>
                    </div>
                  </div>
                  {r.available === 0 ? <Badge tone="bad">Full</Badge> : r.low ? <Badge tone="warn">Low</Badge> : <Badge tone="good">OK</Badge>}
                </div>
                <div className="meter">
                  <div className="meter-top">
                    <span className="meter-label">Currently free</span>
                    <span className="meter-value">
                      <strong>{r.available}</strong> <span>/ {r.total}</span>
                    </span>
                  </div>
                  <div className="bar">
                    <i className={`fill-${tone}`} style={{ width: `${r.total ? Math.round((r.available / r.total) * 100) : 0}%` }} />
                  </div>
                </div>
                <div className="row" style={{ alignItems: 'flex-end' }}>
                  <label className="field">
                    Total
                    <input name="total" type="number" min={0} defaultValue={r.total} />
                  </label>
                  <label className="field">
                    Free
                    <input name="available" type="number" min={0} defaultValue={r.available} />
                  </label>
                  <label className="field">
                    Alert below
                    <input name="threshold" type="number" min={0} defaultValue={r.threshold} />
                  </label>
                </div>
                <button className="btn btn-primary">
                  <Save size={16} /> Save changes
                </button>
              </form>
            );
          })}
        </div>
      )}
    </>
  );
}
