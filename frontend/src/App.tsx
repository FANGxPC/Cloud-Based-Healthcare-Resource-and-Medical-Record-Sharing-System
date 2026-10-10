// App shell: login gate, sidebar navigation and top bar.
// The tabs shown depend on the user's Cognito groups. That is only for convenience: the API
// enforces the same rules server-side, so hiding a tab is never the security boundary.
import { useCallback, useEffect, useState, type ReactNode } from 'react';
import {
  Activity,
  ArrowLeftRight,
  Building2,
  ClipboardList,
  FileHeart,
  Gauge,
  LogOut,
  ShieldCheck,
  SlidersHorizontal,
  Users,
  type LucideIcon,
} from 'lucide-react';
import { currentUser, logout, type Group, type User } from './auth';
import Login from './components/Login';
import { Alert, Avatar } from './components/common';
import Availability from './pages/Availability';
import Hospitals from './pages/Hospitals';
import ManageResources from './pages/ManageResources';
import MyRecord from './pages/MyRecord';
import Patients from './pages/Patients';
import StaffAudit from './pages/StaffAudit';
import Transfers from './pages/Transfers';

type Tab = { id: string; label: string; icon: LucideIcon; groups: Group[]; section: 'main' | 'admin'; render: (u: User) => ReactNode };

const TABS: Tab[] = [
  { id: 'my-record', label: 'My health record', icon: FileHeart, groups: ['Patient'], section: 'main', render: (u) => <MyRecord patientId={u.userId} /> },
  { id: 'availability', label: 'Live capacity', icon: Gauge, groups: ['Doctor', 'HospitalAdmin'], section: 'main', render: () => <Availability /> },
  { id: 'patients', label: 'Patients', icon: Users, groups: ['Doctor'], section: 'main', render: (u) => <Patients ownHospitalId={u.hospitalId} /> },
  {
    id: 'transfers',
    label: 'Transfers',
    icon: ArrowLeftRight,
    groups: ['Doctor', 'HospitalAdmin'],
    section: 'main',
    render: (u) => <Transfers isAdmin={u.groups.includes('HospitalAdmin')} />,
  },
  { id: 'hospitals', label: 'Hospital network', icon: Building2, groups: ['Patient', 'Doctor', 'HospitalAdmin'], section: 'main', render: () => <Hospitals /> },
  {
    id: 'manage',
    label: 'Manage resources',
    icon: SlidersHorizontal,
    groups: ['HospitalAdmin'],
    section: 'admin',
    render: (u) => (u.hospitalId ? <ManageResources hospitalId={u.hospitalId} /> : <NoHospital />),
  },
  {
    id: 'audit',
    label: 'Staff audit',
    icon: ClipboardList,
    groups: ['HospitalAdmin'],
    section: 'admin',
    render: (u) => (u.hospitalId ? <StaffAudit hospitalId={u.hospitalId} /> : <NoHospital />),
  },
];

const ROLE_LABEL: Record<Group, string> = { Patient: 'Patient', Doctor: 'Doctor', HospitalAdmin: 'Hospital admin' };

export default function App() {
  const [user, setUser] = useState<User | null | undefined>(undefined); // undefined = still checking
  const [tab, setTab] = useState<string | null>(null);

  const refreshUser = useCallback(() => {
    currentUser()
      .then(setUser)
      .catch(() => setUser(null));
  }, []);
  useEffect(refreshUser, [refreshUser]);

  if (user === undefined)
    return (
      <div className="loading" style={{ minHeight: '100vh', justifyContent: 'center' }}>
        <span className="spinner" /> Loading HealthCloud…
      </div>
    );
  if (user === null) return <Login onLoggedIn={refreshUser} />;

  const tabs = TABS.filter((t) => t.groups.some((g) => user.groups.includes(g)));
  const active = tabs.find((t) => t.id === tab) ?? tabs[0];
  const role = user.groups.map((g) => ROLE_LABEL[g] ?? g).join(', ') || 'No role';
  const navGroup = (section: Tab['section']) =>
    tabs
      .filter((t) => t.section === section)
      .map((t) => (
        <button key={t.id} className={t === active ? 'nav-item active' : 'nav-item'} onClick={() => setTab(t.id)}>
          <t.icon size={18} />
          {t.label}
        </button>
      ));

  return (
    <div className="shell">
      <aside className="sidebar">
        <div className="brand">
          <span className="brand-mark">
            <Activity size={19} color="#fff" />
          </span>
          <div>
            <div className="brand-name">HealthCloud</div>
            <div className="brand-sub">Care Network</div>
          </div>
        </div>
        <nav className="nav">
          <div className="nav-label">Workspace</div>
          {navGroup('main')}
          {tabs.some((t) => t.section === 'admin') && <div className="nav-label">Administration</div>}
          {navGroup('admin')}
        </nav>
        <div className="sidebar-foot">
          <span className="status-dot" />
          All systems operational
          <div style={{ marginTop: 6 }}>Synthetic demo data</div>
        </div>
      </aside>

      <div className="main">
        <header className="topbar">
          <div className="topbar-title">
            <span className="eyebrow">{user.hospitalId ? `Hospital ${user.hospitalId}` : 'Patient portal'}</span>
            <strong>{active?.label ?? 'HealthCloud'}</strong>
          </div>
          <div className="spacer" />
          <div className="user-chip">
            <Avatar name={user.email.split('@')[0]!} seed={user.userId} size="sm" />
            <div className="user-meta">
              <strong>{user.email}</strong>
              <span>
                <ShieldCheck size={11} style={{ verticalAlign: '-1px' }} /> {role}
              </span>
            </div>
          </div>
          <button
            className="btn btn-ghost"
            title="Sign out"
            onClick={async () => {
              await logout();
              setUser(null);
              setTab(null);
            }}
          >
            <LogOut size={17} />
            <span className="hide-sm">Sign out</span>
          </button>
        </header>

        <main className="content" key={active?.id}>
          {active ? active.render(user) : <Alert tone="warn">Your account is not in any group yet.</Alert>}
        </main>
      </div>
    </div>
  );
}

function NoHospital() {
  return <Alert tone="warn">Your account has no hospital_id assigned.</Alert>;
}
