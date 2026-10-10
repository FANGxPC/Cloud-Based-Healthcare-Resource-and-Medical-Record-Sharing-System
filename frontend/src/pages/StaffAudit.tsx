// HospitalAdmin: every patient-record access made by staff of their own hospital.
import { api } from '../api';
import { Card, Status, useLoad } from '../components/common';
import AuditTable from './AuditTable';

export default function StaffAudit({ hospitalId }: { hospitalId: string }) {
  const { data, error, loading, reload } = useLoad(() => api.hospitalAudit(), []);
  return (
    <Card title={`Record accesses by ${hospitalId} staff`} actions={<button onClick={reload}>Refresh</button>}>
      <Status loading={loading} error={error} />
      {data && <AuditTable items={data.items} showPatient />}
    </Card>
  );
}
