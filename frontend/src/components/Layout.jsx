import { Link, useLocation } from 'react-router-dom';

export default function Layout({ children, pendingCount = 0 }) {
  const location = useLocation();

  return (
    <div className="app">
      <header className="header">
        <div className="header-content">
          <Link to="/" className="logo">
            <svg width="20" height="26" viewBox="0 0 20 26" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
              <path d="M10 1 L14.5 17 L5.5 17 Z" fill="#b5451b"/>
              <rect x="4.5" y="17" width="11" height="2.5" rx="1" fill="rgba(255,252,248,0.38)"/>
              <rect x="5.5" y="19.5" width="9" height="6" rx="3.5" fill="rgba(255,252,248,0.82)"/>
            </svg>
            Mise
          </Link>
          <nav className="nav">
            <Link to="/" className={`nav-pending-link ${location.pathname === '/' ? 'active' : ''}`}>
              Pending
              {pendingCount > 0 && <span className="nav-badge">{pendingCount}</span>}
            </Link>
            <Link to="/generate" className={location.pathname === '/generate' ? 'active' : ''}>Generate</Link>
            <Link to="/import" className={location.pathname === '/import' ? 'active' : ''}>Import</Link>
            <Link to="/recipe/new" className={location.pathname === '/recipe/new' ? 'active' : ''}>New</Link>
            <Link to="/plans" className={location.pathname.startsWith('/plans') ? 'active' : ''}>Plans</Link>
            <Link to="/library" className={location.pathname === '/library' ? 'active' : ''}>Library</Link>
            <Link to="/settings" className={location.pathname === '/settings' ? 'active' : ''}>Settings</Link>
          </nav>
        </div>
      </header>
      <main className="main">{children}</main>
    </div>
  );
}
