// Everyone: the hospitals in the chain.
import { Building2, MapPin, Phone } from 'lucide-react';
import { api } from '../api';
import { Avatar, PageHeader, Status, useLoad } from '../components/common';

export default function Hospitals() {
  const { data, error, loading } = useLoad(() => api.hospitals(), []);
  const items = [...(data?.items ?? [])].sort((a, b) => a.hospitalId.localeCompare(b.hospitalId));
  return (
    <>
      <PageHeader title="Hospital network" subtitle={`${items.length || ''} hospitals sharing records and capacity in real time`} />
      <Status loading={loading} error={error} />
      <div className="hosp-grid">
        {items.map((h) => (
          <article key={h.hospitalId} className="hosp">
            <div className="cell-person">
              <Avatar name={h.name} seed={h.hospitalId} />
              <div>
                <div className="hosp-name">{h.name}</div>
                <div className="hosp-meta">
                  <Building2 size={13} /> {h.hospitalId}
                </div>
              </div>
            </div>
            <div className="small muted stack" style={{ gap: 6 }}>
              <span>
                <MapPin size={13} style={{ verticalAlign: '-2px' }} /> {h.address}, {h.city}
              </span>
              <span>
                <Phone size={13} style={{ verticalAlign: '-2px' }} /> {h.phone}
              </span>
            </div>
          </article>
        ))}
      </div>
    </>
  );
}
