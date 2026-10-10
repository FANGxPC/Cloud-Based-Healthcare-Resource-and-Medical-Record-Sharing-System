// Read-only view of one patient record. Used by Doctors (audited) and by Patients (own record).
import { RESOURCE_LABELS, type Patient } from '../api';
import { Badge, fmtTime } from '../components/common';

export default function PatientProfile({ p }: { p: Patient }) {
  return (
    <div className="profile">
      <h3>
        {p.firstName} {p.lastName}
      </h3>
      <dl>
        <dt>Patient ID</dt>
        <dd className="mono small">{p.patientId}</dd>
        <dt>Date of birth</dt>
        <dd>{p.dateOfBirth}</dd>
        <dt>Sex</dt>
        <dd>{p.sex}</dd>
        <dt>Blood group</dt>
        <dd>{p.bloodGroup}</dd>
        <dt>Allergies</dt>
        <dd>{p.allergies?.length ? p.allergies.map((a) => <Badge key={a} tone="bad">{a}</Badge>) : 'None recorded'}</dd>
        <dt>Conditions</dt>
        <dd>{p.conditions?.length ? p.conditions.join(', ') : 'None recorded'}</dd>
        <dt>Home hospital</dt>
        <dd>{p.homeHospitalId}</dd>
        <dt>Current hospital</dt>
        <dd>{p.currentHospitalId}</dd>
        <dt>Admission</dt>
        <dd>
          {p.admission
            ? `${RESOURCE_LABELS[p.admission.resourceType]} at ${p.admission.hospitalId} since ${fmtTime(p.admission.admittedAt)}`
            : 'Not admitted'}
        </dd>
        {p.emergencyContact && (
          <>
            <dt>Emergency contact</dt>
            <dd>
              {p.emergencyContact.name} · {p.emergencyContact.phone}
            </dd>
          </>
        )}
      </dl>
    </div>
  );
}
