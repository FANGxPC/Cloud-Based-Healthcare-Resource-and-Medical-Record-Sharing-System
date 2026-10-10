import { useState, type FormEvent } from 'react';
import { Activity, ArrowLeftRight, BedDouble, Building2, Lock, LogIn, Mail, ShieldCheck, Stethoscope, User } from 'lucide-react';
import { login } from '../auth';
import { Alert, errorText } from './common';

const DEMO = [
  { label: 'Doctor', email: 'doctor.h001@example.com', icon: Stethoscope },
  { label: 'Admin', email: 'admin.h002@example.com', icon: Building2 },
  { label: 'Patient', email: 'patient1@example.com', icon: User },
];

export default function Login({ onLoggedIn }: { onLoggedIn: () => void }) {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await login(email.trim(), password);
      onLoggedIn();
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="login-page">
      <section className="login-hero">
        <div className="brand" style={{ padding: 0 }}>
          <span className="brand-mark">
            <Activity size={20} color="#fff" />
          </span>
          <div>
            <div className="brand-name">HealthCloud</div>
            <div className="brand-sub">Care Network</div>
          </div>
        </div>

        <div>
          <h1>
            One network.
            <br />
            <em>Every bed, every record.</em>
          </h1>
          <p className="lead">
            Real-time ICU, bed and ventilator availability across the hospital chain, secure record sharing, and emergency
            transfers in seconds.
          </p>
          <div className="features">
            <div className="feature">
              <span className="feature-icon">
                <BedDouble size={19} />
              </span>
              <div>
                <strong>Live capacity, never overbooked</strong>
                <span>Atomic, race-safe admissions across every hospital.</span>
              </div>
            </div>
            <div className="feature">
              <span className="feature-icon">
                <ArrowLeftRight size={19} />
              </span>
              <div>
                <strong>Emergency transfers</strong>
                <span>Request, hold a bed, approve. Destination staff are alerted instantly.</span>
              </div>
            </div>
            <div className="feature">
              <span className="feature-icon">
                <ShieldCheck size={19} />
              </span>
              <div>
                <strong>Audited record sharing</strong>
                <span>Every access is logged, and patients can see who viewed their record.</span>
              </div>
            </div>
          </div>
        </div>

        <div className="hero-foot">
          <span>
            <Lock size={14} /> Encrypted at rest &amp; in transit
          </span>
          <span>
            <span className="status-dot" /> Serverless on AWS
          </span>
          <span>Synthetic data only</span>
        </div>
      </section>

      <section className="login-panel">
        <form className="login-card" onSubmit={submit}>
          <div>
            <h2>Welcome back</h2>
            <p className="muted">Sign in to your hospital workspace.</p>
          </div>

          <label className="field">
            Email
            <span className="input-icon">
              <Mail size={17} />
              <input type="email" autoComplete="username" placeholder="you@example.com" value={email} onChange={(e) => setEmail(e.target.value)} required />
            </span>
          </label>
          <label className="field">
            Password
            <span className="input-icon">
              <Lock size={17} />
              <input type="password" autoComplete="current-password" placeholder="••••••••" value={password} onChange={(e) => setPassword(e.target.value)} required />
            </span>
          </label>

          {error && <Alert tone="bad">{error}</Alert>}

          <button className="btn btn-primary btn-block" disabled={busy}>
            {busy ? <span className="spinner" style={{ borderTopColor: '#fff' }} /> : <LogIn size={18} />}
            {busy ? 'Signing in…' : 'Sign in'}
          </button>

          <div className="demo-box">
            <div className="demo-title">Demo accounts (click to fill)</div>
            <div className="demo-chips">
              {DEMO.map(({ label, email: demoEmail, icon: Icon }) => (
                <button type="button" key={label} className="demo-chip" onClick={() => setEmail(demoEmail)} title={demoEmail}>
                  <Icon size={18} />
                  {label}
                </button>
              ))}
            </div>
          </div>
        </form>
      </section>
    </div>
  );
}
