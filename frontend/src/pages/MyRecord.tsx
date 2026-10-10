// Patient: their own record, plus everyone who has accessed it (transparency).
import { api } from '../api';
import { Card, Status, useLoad } from '../components/common';
import AuditTable from './AuditTable';
import Documents from './Documents';
import PatientProfile from './PatientProfile';

export default function MyRecord({ patientId }: { patientId: string }) {
  const record = useLoad(() => api.patient(patientId), [patientId]);
  const log = useLoad(() => api.accessLog(patientId), [patientId]);

  return (
    <>
      <Card title="My record">
        <Status loading={record.loading} error={record.error} />
        {record.data && <PatientProfile p={record.data} />}
      </Card>
      <Documents patientId={patientId} canUpload={false} />
      <Card title="Who accessed my record" actions={<button onClick={log.reload}>Refresh</button>}>
        <Status loading={log.loading} error={log.error} />
        {log.data && <AuditTable items={log.data.items} />}
      </Card>
    </>
  );
}
