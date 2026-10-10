// Patient: their own record, plus everyone who has accessed it (transparency).
import { Eye, RefreshCw } from 'lucide-react';
import { api } from '../api';
import { Card, PageHeader, Status, useLoad } from '../components/common';
import AuditTable from './AuditTable';
import Documents from './Documents';
import PatientProfile from './PatientProfile';

export default function MyRecord({ patientId }: { patientId: string }) {
  const record = useLoad(() => api.patient(patientId), [patientId]);
  const log = useLoad(() => api.accessLog(patientId), [patientId]);

  return (
    <>
      <PageHeader
        title={record.data ? `Hello, ${record.data.firstName}` : 'My health record'}
        subtitle="Your record is shared securely across the hospital network, and you can see every access."
      />
      <Status loading={record.loading} error={record.error} />
      {record.data && (
        <section className="card fade-in">
          <PatientProfile p={record.data} />
        </section>
      )}
      <Documents patientId={patientId} canUpload={false} />
      <Card
        title="Who accessed my record"
        icon={Eye}
        flush
        actions={
          <button className="btn btn-sm" onClick={log.reload}>
            <RefreshCw size={14} /> Refresh
          </button>
        }
      >
        <Status loading={log.loading} error={log.error} />
        {log.data && <AuditTable items={log.data.items} />}
      </Card>
    </>
  );
}
