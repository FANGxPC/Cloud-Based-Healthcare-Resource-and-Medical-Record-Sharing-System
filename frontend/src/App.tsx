// App shell: login gate plus a role-aware navigation bar.
// The tabs shown depend on the user's Cognito groups. That is only for convenience: the API
// enforces the same rules server-side, so hiding a tab is never the security boundary.
import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { currentUser, logout, type Group, type User } from './auth';
import Login from './components/Login';
import Availability from './pages/Availability';
import Hospitals from './pages/Hospitals';
import ManageResources from './pages/ManageResources';
import MyRecord from './pages/MyRecord';
import Patients from './pages/Patients';
import StaffAudit from './pages/StaffAudit';
import Transfers from './pages/Transfers';

type Tab = { id: string; label: string; groups: Group[]; render: (u: User) => ReactNode };

const TABS: Tab[] = [
  { id: 'my-record', label: 'My record', groups: ['Patient'], render: (u) => <MyRecord patientId={u.userId} /> },
  { id: 'availability', label: 'Availability', groups: ['Doctor', 'HospitalAdmin'], render: () => <Availability /> },
  { id: 'patients', label: 'Patients', groups: ['Doctor'], render: (u) => <Patients ownHospitalId={u.hospitalId} /> },
  {
    id: 'transfers',
    label: 'Transfers',
    groups: ['Doctor', 'HospitalAdmin'],
    render: (u) => <Transfers isAdmin={u.groups.includes('HospitalAdmin')} />,
  },
  {
    id: 'manage',
    label: 'Manage resources',
    groups: ['HospitalAdmin'],
    render: (u) => (u.hospitalId ? <ManageResources hospitalId={u.hospitalId} /> : <NoHospital />),
  },
  {
    id: 'audit',
    label: 'Staff audit',
    groups: ['HospitalAdmin'],
    render: (u) => (u.hospitalId ? <StaffAudit hospitalId={u.hospitalId} /> : <NoHospital />),
  },
  { id: 'hospitals', label: 'Hospitals', groups: ['Patient', 'Doctor', 'HospitalAdmin'], render: () => <Hospitals /> },
];

export default function App() {
  const [user, setUser] = useState<User | null | undefined>(undefined); // undefined = still checking
  const [tab, setTab] = useState<string | null>(null);

  const refreshUser = useCallback(() => {
    currentUser()
      .then(setUser)
      .catch(() => setUser(null));
  }, []);
  useEffect(refreshUser, [refreshUser]);

  if (user === undefined) return <p className="container muted">Loading…</p>;
  if (user === null) return <Login onLoggedIn={refreshUser} />;

  const tabs = TABS.filter((t) => t.groups.some((g) => user.groups.includes(g)));
  const active = tabs.find((t) => t.id === tab) ?? tabs[0];

  return (
    <>
      <header className="topbar">
        <div className="container topbar-inner">
          <strong className="brand">HealthCloud</strong>
          <nav>
            {tabs.map((t) => (
              <button key={t.id} className={t === active ? 'tab active' : 'tab'} onClick={() => setTab(t.id)}>
                {t.label}
              </button>
            ))}
          </nav>
          <div className="who">
            <span>{user.email}</span>
            <span className="muted small">
              {user.groups.join(', ') || 'no group'}
              {user.hospitalId && ` · ${user.hospitalId}`}
            </span>
            <button
              onClick={async () => {
                await logout();
                setUser(null);
                setTab(null);
              }}
            >
              Sign out
            </button>
          </div>
        </div>
      </header>
      <main className="container">
        {active ? active.render(user) : <p className="alert warn">Your account is not in any group yet.</p>}
      </main>
    </>
  );
}

function NoHospital() {
  return <p className="alert warn">Your account has no hospital_id assigned.</p>;
}
