// Chain-wide bed and ventilator availability (Doctor, HospitalAdmin).
// Served from the resources Lambda's 15 s cache, so "data as of" can be up to ~15 s old.
import { useState } from 'react';
import { api, RESOURCE_LABELS, type Resource, type ResourceType } from '../api';
import { Badge, Card, Status, fmtTime, useLoad } from '../components/common';

const TYPES: ResourceType[] = ['ICU_BED', 'GENERAL_BED', 'VENTILATOR'];

export default function Availability() {
  const [onlyAvailable, setOnlyAvailable] = useState(false);
  const { data, error, loading, reload } = useLoad(() => api.resources(undefined, onlyAvailable), [onlyAvailable]);

  // Pivot the flat list into one row per hospital, one column per resource type.
  const rows = new Map<string, { name: string; byType: Partial<Record<ResourceType, Resource>> }>();
  for (const r of data?.items ?? []) {
    const row = rows.get(r.hospitalId) ?? { name: r.hospitalName ?? r.hospitalId, byType: {} };
    row.byType[r.resourceType] = r;
    rows.set(r.hospitalId, row);
  }
  const sorted = [...rows.entries()].sort(([a], [b]) => a.localeCompare(b));

  return (
    <Card
      title="Resource availability across the chain"
      actions={
        <div className="row">
          <label className="inline">
            <input type="checkbox" checked={onlyAvailable} onChange={(e) => setOnlyAvailable(e.target.checked)} /> Only with free units
          </label>
          <button onClick={reload}>Refresh</button>
        </div>
      }
    >
      <Status loading={loading} error={error} />
      {data && (
        <>
          <table>
            <thead>
              <tr>
                <th>Hospital</th>
                {TYPES.map((t) => (
                  <th key={t}>{RESOURCE_LABELS[t]}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {sorted.map(([id, row]) => (
                <tr key={id}>
                  <td>
                    <strong>{row.name}</strong>
                    <div className="muted small">{id}</div>
                  </td>
                  {TYPES.map((t) => {
                    const r = row.byType[t];
                    if (!r) return <td key={t} className="muted">—</td>;
                    return (
                      <td key={t}>
                        <span className="count">{r.available}</span>
                        <span className="muted"> / {r.total}</span>{' '}
                        {r.available === 0 ? <Badge tone="bad">Full</Badge> : r.low ? <Badge tone="warn">Low</Badge> : null}
                      </td>
                    );
                  })}
                </tr>
              ))}
            </tbody>
          </table>
          <p className="muted small">Data as of {fmtTime(data.cachedAt)} (cached up to 15 s).</p>
        </>
      )}
    </Card>
  );
}
