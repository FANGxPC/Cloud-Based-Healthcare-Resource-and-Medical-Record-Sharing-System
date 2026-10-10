// HospitalAdmin: every patient-record access made by staff of their own hospital.
import { ClipboardList, RefreshCw } from 'lucide-react';
import { api } from '../api';
import { Card, PageHeader, Status, useLoad } from '../components/common';
import AuditTable from './AuditTable';

export default function StaffAudit({ hospitalId }: { hospitalId: string }) {
  const { data, error, loading, reload } = useLoad(() => api.hospitalAudit(), []);
  return (
    <>
      <PageHeader title="Staff audit" subtitle={`Every patient-record access by ${hospitalId} staff. The log is append-only and cannot be edited.`} />
      <Card
        title="Access log"
        icon={ClipboardList}
        flush
        actions={
          <button className="btn btn-sm" onClick={reload}>
            <RefreshCw size={14} /> Refresh
          </button>
        }
      >
        <Status loading={loading} error={error} />
        {data && <AuditTable items={data.items} showPatient />}
      </Card>
    </>
  );
}
