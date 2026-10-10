// Read-only view of one patient record. Used by Doctors (audited) and by Patients (own record).
// Meant to sit inside a flush <Card>.
import { Building2, CalendarDays, Droplet, HeartPulse, Home, Phone, Pill, Stethoscope } from 'lucide-react';
import { RESOURCE_LABELS, type Patient } from '../api';
import { Avatar, Badge, ageFrom, fmtDate, fmtTime } from '../components/common';

const SEX: Record<string, string> = { F: 'Female', M: 'Male', X: 'Unspecified' };

export default function PatientProfile({ p }: { p: Patient }) {
  const age = ageFrom(p.dateOfBirth);
  return (
    <>
      <div className="profile-hero">
        <Avatar name={`${p.firstName} ${p.lastName}`} seed={p.patientId} size="lg" />
        <div>
          <h2>
            {p.firstName} {p.lastName}
          </h2>
          <div className="profile-tags">
            {age !== null && <Badge tone="neutral" plain>{age} yrs</Badge>}
            <Badge tone="neutral" plain>
              {SEX[p.sex] ?? p.sex}
            </Badge>
            <Badge tone="info" plain>
              Blood {p.bloodGroup}
            </Badge>
            {p.admission ? (
              <Badge tone="warn">
                Admitted · {RESOURCE_LABELS[p.admission.resourceType]} at {p.admission.hospitalId}
              </Badge>
            ) : (
              <Badge tone="good">Not admitted</Badge>
            )}
          </div>
          <div className="small subtle mono" style={{ marginTop: 8 }}>
            ID {p.patientId}
          </div>
        </div>
      </div>

      <dl className="facts">
        <Fact icon={CalendarDays} label="Date of birth" value={fmtDate(p.dateOfBirth)} />
        <Fact icon={Droplet} label="Blood group" value={p.bloodGroup} />
        <Fact icon={Home} label="Home hospital" value={p.homeHospitalId} />
        <Fact icon={Building2} label="Current hospital" value={p.currentHospitalId} />
        <Fact
          icon={Pill}
          label="Allergies"
          wide
          value={p.allergies?.length ? p.allergies.map((a) => <Badge key={a} tone="bad">{a}</Badge>) : <span className="muted">None recorded</span>}
        />
        <Fact
          icon={Stethoscope}
          label="Conditions"
          wide
          value={p.conditions?.length ? p.conditions.map((c) => <Badge key={c} tone="neutral" plain>{c}</Badge>) : <span className="muted">None recorded</span>}
        />
        {p.admission && (
          <Fact icon={HeartPulse} label="Admitted since" value={`${fmtTime(p.admission.admittedAt)} · ${RESOURCE_LABELS[p.admission.resourceType]}`} />
        )}
        {p.emergencyContact && <Fact icon={Phone} label="Emergency contact" value={`${p.emergencyContact.name} · ${p.emergencyContact.phone}`} />}
      </dl>
    </>
  );
}

function Fact({ icon: Icon, label, value, wide }: { icon: typeof Phone; label: string; value: React.ReactNode; wide?: boolean }) {
  return (
    <div className={wide ? 'fact wide' : 'fact'}>
      <dt>
        <Icon size={13} /> {label}
      </dt>
      <dd>{value}</dd>
    </div>
  );
}
