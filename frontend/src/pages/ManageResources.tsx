// HospitalAdmin: edit capacity, availability and alert thresholds for their OWN hospital.
// Each save sends the version the admin loaded. If the row changed meanwhile (e.g. a bed was
// booked), the API answers 409 VERSION_CONFLICT and we reload instead of overwriting.
import { useState } from 'react';
import { api, ApiError, RESOURCE_LABELS, type Resource } from '../api';
import { Badge, Card, Status, errorText, fmtTime, useLoad } from '../components/common';

export default function ManageResources({ hospitalId }: { hospitalId: string }) {
  const { data, error, loading, reload } = useLoad(() => api.hospitalResources(hospitalId), [hospitalId]);
  const [message, setMessage] = useState<{ tone: 'good' | 'bad'; text: string } | null>(null);

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
      setMessage({ tone: 'bad', text: conflict ? 'Someone changed this row meanwhile. Reloaded the latest values; please re-apply.' : errorText(e) });
    }
    reload();
  }

  return (
    <Card title={`Manage resources · ${hospitalId}`}>
      <Status loading={loading} error={error} />
      {message && <p className={`alert ${message.tone}`}>{message.text}</p>}
      {data && (
        <table>
          <thead>
            <tr>
              <th>Resource</th>
              <th>Total</th>
              <th>Available</th>
              <th>Alert below</th>
              <th>Last update</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {data.items.map((r) => (
              // key includes version so the inputs reset to fresh values after each reload
              <tr key={`${r.resourceType}-${r.version}`}>
                <td>
                  {RESOURCE_LABELS[r.resourceType]} {r.low && <Badge tone="warn">Low</Badge>}
                </td>
                <td colSpan={3}>
                  <form
                    id={`f-${r.resourceType}`}
                    className="row"
                    onSubmit={(e) => {
                      e.preventDefault();
                      save(r, e.currentTarget);
                    }}
                  >
                    <input name="total" type="number" min={0} defaultValue={r.total} aria-label="total" />
                    <input name="available" type="number" min={0} defaultValue={r.available} aria-label="available" />
                    <input name="threshold" type="number" min={0} defaultValue={r.threshold} aria-label="threshold" />
                  </form>
                </td>
                <td className="muted small">{fmtTime(r.updatedAt)}</td>
                <td>
                  <button className="primary" form={`f-${r.resourceType}`}>
                    Save
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Card>
  );
}
