// Small shared UI pieces and a data-loading hook.
import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { ApiError } from '../api';

// useLoad runs an async loader and tracks loading and error state. Call reload() to refetch.
export function useLoad<T>(loader: () => Promise<T>, deps: unknown[]) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  // deps decides when to reload (like useEffect deps).
  const run = useCallback(loader, deps);

  const reload = useCallback(() => {
    setLoading(true);
    setError(null);
    run()
      .then(setData)
      .catch((e) => setError(errorText(e)))
      .finally(() => setLoading(false));
  }, [run]);

  useEffect(reload, [reload]);
  return { data, error, loading, reload };
}

export function errorText(e: unknown): string {
  if (e instanceof ApiError) return `${e.message} (${e.code})`;
  if (e instanceof Error) return e.message;
  return String(e);
}

export function Status({ loading, error }: { loading: boolean; error: string | null }) {
  if (error) return <p className="alert bad">{error}</p>;
  if (loading) return <p className="muted">Loading…</p>;
  return null;
}

export function Card({ title, actions, children }: { title?: string; actions?: ReactNode; children: ReactNode }) {
  return (
    <section className="card">
      {(title || actions) && (
        <div className="card-head">
          {title && <h2>{title}</h2>}
          {actions}
        </div>
      )}
      {children}
    </section>
  );
}

export function Badge({ tone, children }: { tone: 'good' | 'bad' | 'warn' | 'neutral'; children: ReactNode }) {
  return <span className={`badge ${tone}`}>{children}</span>;
}

export const fmtTime = (iso?: string) => (iso ? new Date(iso).toLocaleString() : '—');
