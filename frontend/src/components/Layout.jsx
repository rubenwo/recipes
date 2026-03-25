import { useState } from 'react';
import { Link, useLocation } from 'react-router-dom';

export default function Layout({ children, pendingCount = 0 }) {
  const location = useLocation();
  const [menuOpen, setMenuOpen] = useState(false);

  const closeMenu = () => setMenuOpen(false);

  return (
    <div className="app">
      <header className="header">
        <div className="header-content">
          <Link to="/" className="logo" onClick={closeMenu}>
            <svg width="28" height="22" viewBox="0 0 28 22" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
              <circle cx="10" cy="11" r="9" fill="#b5451b"/>
              <circle cx="10" cy="11" r="9" stroke="rgba(255,252,248,0.5)" strokeWidth="1.5"/>
              <circle cx="7.5" cy="8.5" r="2.5" fill="rgba(255,252,248,0.08)"/>
              <path d="M19 9.5 L26.5 7.5 Q27.5 11 26.5 14.5 L19 12.5 Z" fill="rgba(255,252,248,0.85)" stroke="rgba(255,252,248,0.25)" strokeWidth="0.5"/>
              <circle cx="21" cy="11" r="0.75" fill="#1c1510" opacity="0.4"/>
              <circle cx="23.5" cy="11" r="0.75" fill="#1c1510" opacity="0.4"/>
            </svg>
            Mise
          </Link>
          <button
            className={`nav-hamburger${menuOpen ? ' nav-hamburger-open' : ''}`}
            onClick={() => setMenuOpen(o => !o)}
            aria-label="Toggle menu"
            aria-expanded={menuOpen}
          >
            <span /><span /><span />
          </button>
          <nav className={`nav${menuOpen ? ' nav-open' : ''}`}>
            <Link to="/" className={`nav-pending-link ${location.pathname === '/' ? 'active' : ''}`} onClick={closeMenu}>
              Pending
              {pendingCount > 0 && <span className="nav-badge">{pendingCount}</span>}
            </Link>
            <Link to="/recipe/new" className={location.pathname === '/recipe/new' ? 'active' : ''} onClick={closeMenu}>Add Recipe</Link>
            <Link to="/inventory" className={location.pathname === '/inventory' ? 'active' : ''} onClick={closeMenu}>Inventory</Link>
            <Link to="/plans" className={location.pathname.startsWith('/plans') ? 'active' : ''} onClick={closeMenu}>Plans</Link>
            <Link to="/library" className={location.pathname === '/library' ? 'active' : ''} onClick={closeMenu}>Library</Link>
            <Link to="/settings" className={location.pathname === '/settings' ? 'active' : ''} onClick={closeMenu}>Settings</Link>
          </nav>
        </div>
      </header>
      <main className="main">{children}</main>
    </div>
  );
}
