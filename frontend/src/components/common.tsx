// Shared UI building blocks plus a small data-loading hook.
import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { AlertTriangle, CheckCircle2, Info, Inbox, XCircle, type LucideIcon } from 'lucide-react';
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

export const fmtTime = (iso?: string) =>
  iso ? new Date(iso).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' }) : '—';

export const fmtDate = (iso?: string) => (iso ? new Date(iso).toLocaleDateString(undefined, { dateStyle: 'medium' }) : '—');

export function ageFrom(dob?: string): number | null {
  if (!dob) return null;
  const d = new Date(dob);
  const now = new Date();
  let age = now.getFullYear() - d.getFullYear();
  if (now < new Date(now.getFullYear(), d.getMonth(), d.getDate())) age--;
  return age;
}

// ---------------------------------------------------------------- layout pieces

export function PageHeader({ title, subtitle, actions }: { title: string; subtitle?: string; actions?: ReactNode }) {
  return (
    <div className="page-head fade-in">
      <div>
        <h1>{title}</h1>
        {subtitle && <p>{subtitle}</p>}
      </div>
      {actions && <div className="row">{actions}</div>}
    </div>
  );
}

export function Card({
  title,
  icon: Icon,
  actions,
  children,
  flush,
}: {
  title?: string;
  icon?: LucideIcon;
  actions?: ReactNode;
  children: ReactNode;
  flush?: boolean;
}) {
  return (
    <section className="card fade-in">
      {(title || actions) && (
        <div className="card-head">
          <div className="title">
            {Icon && (
              <span className="card-icon">
                <Icon size={17} />
              </span>
            )}
            {title && <h2>{title}</h2>}
          </div>
          {actions && <div className="row">{actions}</div>}
        </div>
      )}
      <div className={flush ? 'card-body flush' : 'card-body'}>{children}</div>
    </section>
  );
}

export function Kpi({
  label,
  value,
  suffix,
  foot,
  icon: Icon,
  tone = 'brand',
}: {
  label: string;
  value: ReactNode;
  suffix?: string;
  foot?: ReactNode;
  icon: LucideIcon;
  tone?: 'brand' | 'accent' | 'good' | 'warn' | 'bad' | 'crit';
}) {
  return (
    <div className="kpi fade-in">
      <div className="kpi-top">
        <span className="kpi-label">{label}</span>
        <span className={`kpi-icon tone-${tone}`}>
          <Icon size={18} />
        </span>
      </div>
      <div className="kpi-value">
        {value}
        {suffix && <small>{suffix}</small>}
      </div>
      {foot && <div className="kpi-foot">{foot}</div>}
    </div>
  );
}

// ---------------------------------------------------------------- feedback

export function Status({ loading, error }: { loading: boolean; error: string | null }) {
  if (error) return <Alert tone="bad">{error}</Alert>;
  if (loading)
    return (
      <div className="loading">
        <span className="spinner" /> Loading…
      </div>
    );
  return null;
}

const ALERT_ICONS = { good: CheckCircle2, bad: XCircle, warn: AlertTriangle, info: Info };

export function Alert({ tone, children }: { tone: 'good' | 'bad' | 'warn' | 'info'; children: ReactNode }) {
  const Icon = ALERT_ICONS[tone];
  return (
    <div className={`alert ${tone} fade-in`} role={tone === 'bad' ? 'alert' : 'status'}>
      <Icon size={17} />
      <div>{children}</div>
    </div>
  );
}

export function Empty({ icon: Icon = Inbox, title, hint }: { icon?: LucideIcon; title: string; hint?: string }) {
  return (
    <div className="empty">
      <span className="empty-icon">
        <Icon size={22} />
      </span>
      <strong>{title}</strong>
      {hint && <span className="small">{hint}</span>}
    </div>
  );
}

export function Badge({ tone, plain, children }: { tone: 'good' | 'bad' | 'warn' | 'neutral' | 'info'; plain?: boolean; children: ReactNode }) {
  return <span className={`badge ${tone}${plain ? ' plain' : ''}`}>{children}</span>;
}

// Avatar shows initials on a colour derived from the text, so the same person always gets the same colour.
const AVATAR_GRADIENTS = [
  ['#14b8a6', '#0d9488'],
  ['#3b82f6', '#2563eb'],
  ['#8b5cf6', '#7c3aed'],
  ['#f59e0b', '#d97706'],
  ['#ec4899', '#db2777'],
  ['#10b981', '#059669'],
  ['#06b6d4', '#0891b2'],
  ['#f43f5e', '#e11d48'],
];

export function Avatar({ name, seed, size }: { name: string; seed?: string; size?: 'sm' | 'lg' }) {
  const initials =
    name
      .split(/[\s.@_-]+/)
      .filter(Boolean)
      .slice(0, 2)
      .map((p) => p[0]!.toUpperCase())
      .join('') || '?';
  const key = seed ?? name;
  let h = 0;
  for (let i = 0; i < key.length; i++) h = (h * 31 + key.charCodeAt(i)) >>> 0;
  const [a, b] = AVATAR_GRADIENTS[h % AVATAR_GRADIENTS.length]!;
  return (
    <span className={`avatar${size ? ' ' + size : ''}`} style={{ background: `linear-gradient(135deg, ${a}, ${b})` }}>
      {initials}
    </span>
  );
}
