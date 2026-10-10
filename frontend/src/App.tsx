import { useEffect, useState } from 'react';
import { getHealth, type Health } from './api';

// Phase 0 shell: proves the frontend can reach the backend. Login and real screens arrive in Phase 1.
export default function App() {
  const [health, setHealth] = useState<Health | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    getHealth().then(setHealth).catch((e: Error) => setError(e.message));
  }, []);

  return (
    <main className="container">
      <h1>HealthCloud</h1>
      <p className="muted">Healthcare resource &amp; medical record sharing. Synthetic data only.</p>

      <section className="card">
        <h2>Backend status</h2>
        {error && <p className="bad">Unreachable: {error}</p>}
        {!error && !health && <p className="muted">Checking…</p>}
        {health && (
          <p className="good">
            {health.status.toUpperCase()}, stage <code>{health.stage}</code>
          </p>
        )}
      </section>
    </main>
  );
}
