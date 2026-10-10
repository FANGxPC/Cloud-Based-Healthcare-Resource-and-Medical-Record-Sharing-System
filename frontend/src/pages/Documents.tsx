// A patient's medical documents. Everyone allowed can list and open; Doctors can also upload.
// Opening fetches a 5-minute presigned S3 URL. Doctor opens are audited server-side.
import { useState, type FormEvent } from 'react';
import { api, type DocumentCategory, type MedicalDocument } from '../api';
import { Badge, Card, Status, errorText, fmtTime, useLoad } from '../components/common';

const CATEGORIES: DocumentCategory[] = ['LAB_REPORT', 'IMAGING', 'PRESCRIPTION', 'DISCHARGE_SUMMARY', 'OTHER'];
const ACCEPT = 'application/pdf,image/png,image/jpeg,text/plain';

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
    <Card title="Documents" actions={<button onClick={reload}>Refresh</button>}>
      <Status loading={loading} error={error} />
      {msg && <p className={`alert ${msg.tone}`}>{msg.text}</p>}
      {data && data.items.length === 0 && <p className="muted">No documents yet.</p>}
      {data && data.items.length > 0 && (
        <table>
          <thead>
            <tr>
              <th>Title</th>
              <th>Category</th>
              <th>Uploaded</th>
              <th>Size</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {data.items.map((d) => (
              <tr key={d.documentId}>
                <td>
                  {d.title}
                  <div className="muted small">{d.fileName}</div>
                </td>
                <td>
                  <Badge tone="neutral">{d.category.replace('_', ' ')}</Badge>
                </td>
                <td>
                  {fmtTime(d.createdAt)}
                  <div className="muted small">by {d.uploadedByHospitalId || '—'}</div>
                </td>
                <td>{(d.sizeBytes / 1024).toFixed(0)} KB</td>
                <td>
                  {d.status === 'AVAILABLE' ? <button onClick={() => open(d)}>Open</button> : <Badge tone="warn">Upload pending</Badge>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
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
      onDone('Document uploaded.', 'good');
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
    <form className="actions-box" onSubmit={submit}>
      <h3>Upload a document (synthetic files only, max 10 MB)</h3>
      <div className="row">
        <input type="file" accept={ACCEPT} onChange={(e) => setFile(e.target.files?.[0] ?? null)} required />
        <input placeholder="Title" maxLength={120} value={title} onChange={(e) => setTitle(e.target.value)} />
        <select value={category} onChange={(e) => setCategory(e.target.value as DocumentCategory)} aria-label="category">
          {CATEGORIES.map((c) => (
            <option key={c} value={c}>
              {c.replace('_', ' ')}
            </option>
          ))}
        </select>
        <button className="primary" disabled={busy || !file}>
          {busy ? 'Uploading…' : 'Upload'}
        </button>
      </div>
    </form>
  );
}
