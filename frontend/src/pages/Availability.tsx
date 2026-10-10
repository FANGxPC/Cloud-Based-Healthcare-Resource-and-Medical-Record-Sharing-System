// Live capacity across the chain (Doctor, HospitalAdmin).
// Served from the resources Lambda's 15 s cache, so "updated" can be up to ~15 s old.
import { useEffect, useState } from 'react';
import { AlertTriangle, BedDouble, Building2, HeartPulse, MapPin, RefreshCw, Search, Wind } from 'lucide-react';
import { api, RESOURCE_LABELS, type Resource, type ResourceType } from '../api';
import { Badge, Empty, Kpi, PageHeader, Status, useLoad } from '../components/common';

const TYPES: ResourceType[] = ['ICU_BED', 'GENERAL_BED', 'VENTILATOR'];
const ICONS = { ICU_BED: HeartPulse, GENERAL_BED: BedDouble, VENTILATOR: Wind };

type HospitalRow = { id: string; name: string; byType: Partial<Record<ResourceType, Resource>> };

export default function Availability() {
  const { data, error, loading, reload } = useLoad(() => api.resources(), []);
  const [query, setQuery] = useState('');
  const [sort, setSort] = useState<'id' | 'icu'>('id');

  // Auto-refresh every 30 s while the page is open: this is a live dashboard.
  useEffect(() => {
    const id = setInterval(reload, 30_000);
    return () => clearInterval(id);
  }, [reload]);

  const items = data?.items ?? [];
  const totals = (t: ResourceType) =>
    items.filter((r) => r.resourceType === t).reduce((acc, r) => ({ free: acc.free + r.available, total: acc.total + r.total }), { free: 0, total: 0 });
  const lowCount = new Set(items.filter((r) => r.low).map((r) => r.hospitalId)).size;

  // Pivot the flat list into one entry per hospital.
  const map = new Map<string, HospitalRow>();
  for (const r of items) {
    const row = map.get(r.hospitalId) ?? { id: r.hospitalId, name: r.hospitalName ?? r.hospitalId, byType: {} };
    row.byType[r.resourceType] = r;
    map.set(r.hospitalId, row);
  }
  let hospitals = [...map.values()].filter((h) => `${h.id} ${h.name}`.toLowerCase().includes(query.toLowerCase()));
  hospitals = hospitals.sort((a, b) =>
    sort === 'icu' ? (b.byType.ICU_BED?.available ?? 0) - (a.byType.ICU_BED?.available ?? 0) : a.id.localeCompare(b.id),
  );

  const icu = totals('ICU_BED');
  const beds = totals('GENERAL_BED');
  const vents = totals('VENTILATOR');

  return (
    <>
      <PageHeader
        title="Live capacity"
        subtitle={data ? `Network-wide availability · updated ${new Date(data.cachedAt).toLocaleTimeString()}` : 'Network-wide availability'}
        actions={
          <button className="btn" onClick={reload} disabled={loading}>
            <RefreshCw size={16} className={loading ? 'spin' : undefined} /> Refresh
          </button>
        }
      />
      <Status loading={loading && !data} error={error} />

      {data && (
        <>
          <div className="kpis">
            <Kpi label="ICU beds free" value={icu.free} suffix={`/ ${icu.total}`} icon={HeartPulse} tone="crit" foot={`${pct(icu)}% of ICU capacity available`} />
            <Kpi label="General beds free" value={beds.free} suffix={`/ ${beds.total}`} icon={BedDouble} tone="accent" foot={`${pct(beds)}% available`} />
            <Kpi label="Ventilators free" value={vents.free} suffix={`/ ${vents.total}`} icon={Wind} tone="brand" foot={`${pct(vents)}% available`} />
            <Kpi
              label="Hospitals on alert"
              value={lowCount}
              suffix={`/ ${map.size}`}
              icon={AlertTriangle}
              tone={lowCount ? 'warn' : 'good'}
              foot={lowCount ? 'Below alert threshold on at least one resource' : 'All above thresholds'}
            />
          </div>

          <div className="page-head">
            <h2>Hospitals</h2>
            <div className="row">
              <span className="search">
                <Search size={16} />
                <input placeholder="Search hospitals…" value={query} onChange={(e) => setQuery(e.target.value)} />
              </span>
              <div className="segmented">
                <button className={sort === 'id' ? 'on' : ''} onClick={() => setSort('id')}>
                  A–Z
                </button>
                <button className={sort === 'icu' ? 'on' : ''} onClick={() => setSort('icu')}>
                  Most ICU free
                </button>
              </div>
            </div>
          </div>

          {hospitals.length === 0 ? (
            <Empty icon={Building2} title="No hospitals match your search" />
          ) : (
            <div className="hosp-grid">
              {hospitals.map((h) => (
                <HospitalCard key={h.id} h={h} />
              ))}
            </div>
          )}
        </>
      )}
    </>
  );
}

function HospitalCard({ h }: { h: HospitalRow }) {
  const rows = TYPES.map((t) => h.byType[t]).filter(Boolean) as Resource[];
  const full = rows.some((r) => r.available === 0);
  const low = rows.some((r) => r.low);
  return (
    <article className="hosp">
      <div className="hosp-head">
        <div>
          <div className="hosp-name">{h.name}</div>
          <div className="hosp-meta">
            <MapPin size={13} /> {h.id}
          </div>
        </div>
        {full ? <Badge tone="bad">At capacity</Badge> : low ? <Badge tone="warn">Low</Badge> : <Badge tone="good">Available</Badge>}
      </div>
      {rows.map((r) => (
        <Meter key={r.resourceType} r={r} />
      ))}
    </article>
  );
}

function Meter({ r }: { r: Resource }) {
  const Icon = ICONS[r.resourceType];
  const width = r.total ? Math.round((r.available / r.total) * 100) : 0;
  const tone = r.available === 0 ? 'bad' : r.low ? 'warn' : 'good';
  return (
    <div className="meter">
      <div className="meter-top">
        <span className="meter-label">
          <Icon size={15} /> {RESOURCE_LABELS[r.resourceType]}
        </span>
        <span className="meter-value">
          <strong>{r.available}</strong> <span>/ {r.total} free</span>
        </span>
      </div>
      <div className="bar" title={`Alert threshold: ${r.threshold}`}>
        <i className={`fill-${tone}`} style={{ width: `${Math.max(width, r.available ? 4 : 0)}%` }} />
      </div>
    </div>
  );
}

const pct = (x: { free: number; total: number }) => (x.total ? Math.round((x.free / x.total) * 100) : 0);
