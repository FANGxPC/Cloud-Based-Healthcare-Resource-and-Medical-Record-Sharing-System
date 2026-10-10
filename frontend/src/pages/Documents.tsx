// A patient's medical documents. Everyone allowed can list and open; Doctors can also upload.
// Opening fetches a 5-minute presigned S3 URL. Doctor opens are audited server-side.
import { useState, type FormEvent } from 'react';
import { Download, FileImage, FileText, FolderHeart, Lock, RefreshCw, Upload } from 'lucide-react';
import { api, type DocumentCategory, type MedicalDocument } from '../api';
import { Alert, Badge, Card, Empty, Status, errorText, fmtTime, useLoad } from '../components/common';

const CATEGORIES: DocumentCategory[] = ['LAB_REPORT', 'IMAGING', 'PRESCRIPTION', 'DISCHARGE_SUMMARY', 'OTHER'];
const ACCEPT = 'application/pdf,image/png,image/jpeg,text/plain';
const label = (c: string) => c.replace(/_/g, ' ').toLowerCase().replace(/^\w/, (x) => x.toUpperCase());

export default function Documents({ patientId, canUpload }: { patientId: string; canUpload: boolean }) {
  const { data, error, loading, reload } = useLoad(() => api.documents(patientId), [patientId]);
  const [msg, setMsg] = useState<{ tone: 'good' | 'bad'; text: string } | null>(null);

  async function open(d: MedicalDocument) {
    setMsg(null);
    try {
      const { downloadUrl } = await api.document(patientId, d.documentId);
      window.open(downloadUrl, '_blank', 'noopener');
    } catch (e) {
      setMsg({ tone: 'bad', text: errorText(e) });
    }
  }

  return (
    <Card
      title="Medical documents"
      icon={FolderHeart}
      flush
      actions={
        <button className="btn btn-sm" onClick={reload}>
          <RefreshCw size={14} /> Refresh
        </button>
      }
    >
      <Status loading={loading} error={error} />
      {msg && (
        <div style={{ padding: '12px 20px 0' }}>
          <Alert tone={msg.tone}>{msg.text}</Alert>
        </div>
      )}
      {data && data.items.length === 0 && <Empty icon={FolderHeart} title="No documents yet" hint={canUpload ? 'Upload a synthetic lab report below.' : undefined} />}
      {data && data.items.length > 0 && (
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Document</th>
                <th>Category</th>
                <th>Uploaded</th>
                <th>Size</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {data.items.map((d) => {
                const Icon = d.contentType.startsWith('image/') ? FileImage : FileText;
                return (
                  <tr key={d.documentId}>
                    <td>
                      <div className="cell-person">
                        <span className="file-icon">
                          <Icon size={18} />
                        </span>
                        <div>
                          <strong>{d.title}</strong>
                          <div className="small muted">{d.fileName}</div>
                        </div>
                      </div>
                    </td>
                    <td>
                      <Badge tone="info" plain>
                        {label(d.category)}
                      </Badge>
                    </td>
                    <td>
                      {fmtTime(d.createdAt)}
                      <div className="small muted">by {d.uploadedByHospitalId || '—'}</div>
                    </td>
                    <td>{d.sizeBytes < 1024 ? `${d.sizeBytes} B` : `${(d.sizeBytes / 1024).toFixed(0)} KB`}</td>
                    <td style={{ textAlign: 'right' }}>
                      {d.status === 'AVAILABLE' ? (
                        <button className="btn btn-sm" onClick={() => open(d)}>
                          <Download size={14} /> Open
                        </button>
                      ) : (
                        <Badge tone="warn">Upload pending</Badge>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      {canUpload && <UploadForm patientId={patientId} onDone={(text, tone) => (setMsg({ text, tone }), reload())} />}
    </Card>
  );
}

function UploadForm({ patientId, onDone }: { patientId: string; onDone: (text: string, tone: 'good' | 'bad') => void }) {
  const [file, setFile] = useState<File | null>(null);
  const [title, setTitle] = useState('');
  const [category, setCategory] = useState<DocumentCategory>('LAB_REPORT');
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (!file) return;
    const form = e.currentTarget;
    setBusy(true);
    try {
      await api.uploadDocument(patientId, file, title || file.name, category);
      onDone('Document uploaded securely to S3.', 'good');
      setFile(null);
      setTitle('');
      form.reset();
    } catch (err) {
      onDone(errorText(err), 'bad');
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="section" onSubmit={submit}>
      <h3>
        <Upload size={16} /> Upload a document
      </h3>
      <div className="inline-form">
        <input type="file" accept={ACCEPT} onChange={(e) => setFile(e.target.files?.[0] ?? null)} required />
        <input placeholder="Title" maxLength={120} value={title} onChange={(e) => setTitle(e.target.value)} />
        <select value={category} onChange={(e) => setCategory(e.target.value as DocumentCategory)} aria-label="category">
          {CATEGORIES.map((c) => (
            <option key={c} value={c}>
              {label(c)}
            </option>
          ))}
        </select>
        <button className="btn btn-primary" disabled={busy || !file}>
          {busy ? <span className="spinner" style={{ borderTopColor: '#fff' }} /> : <Upload size={16} />}
          {busy ? 'Uploading…' : 'Upload'}
        </button>
      </div>
      <span className="section-hint">
        <Lock size={12} style={{ verticalAlign: '-1px' }} /> Sent directly to an encrypted private S3 bucket via a 5-minute signed link. PDF, PNG, JPEG or text, max
        10 MB. Synthetic files only.
      </span>
    </form>
  );
}
