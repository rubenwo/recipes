import { Link, useLocation } from 'react-router-dom';

export default function Layout({ children }) {
  const location = useLocation();

  return (
    <div className="app">
      <header className="header">
        <div className="header-content">
          <Link to="/" className="logo">Eten</Link>
          <nav className="nav">
            <Link to="/" className={location.pathname === '/' ? 'active' : ''}>Generate</Link>
            <Link to="/import" className={location.pathname === '/import' ? 'active' : ''}>Import</Link>
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
