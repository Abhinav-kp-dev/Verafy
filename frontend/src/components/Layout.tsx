import { useEffect, useState } from 'react'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { api } from '../api'
import { useLive, useLiveRefresh } from '../live'
import { ChatWidget } from './ChatWidget'
import { Icon } from './ui'

export function Layout() {
  const { connected } = useLive()
  const [reviewCount, setReviewCount] = useState(0)
  const [q, setQ] = useState('')
  const nav = useNavigate()
  useLiveRefresh(() => { api.stats().then((s) => setReviewCount(s.needsReview)).catch(() => {}) }, [])
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if ((e.metaKey || e.ctrlKey) && e.key === 'k') { e.preventDefault(); (document.getElementById('global-search') as HTMLInputElement)?.focus() } }
    window.addEventListener('keydown', h); return () => window.removeEventListener('keydown', h)
  }, [])

  const items = [
    ['/', 'home', 'Dashboard'], ['/verify', 'shield', 'Verify Insurance'], ['/batch', 'upload', 'Batch Upload'],
    ['/patients', 'users', 'Patients'], ['/history', 'history', 'Verification History'], ['/review', 'review', 'Manual Review'],
    ['/previsit', 'mail', 'Pre-Visit Notices'],
    ['/reports', 'chart', 'Reports'], ['/settings', 'settings', 'Settings'],
  ] as const

  return (
    <div className="app">
      <aside className="sidebar">
        <div className="brand">
          <div className="brand-mark"><Icon name="tooth" size={20} /></div>
          <div><div className="brand-name">Coverage<span>Check</span></div><div className="brand-tag">Verify. Understand. Care Ahead.</div></div>
        </div>
        <nav className="nav">
          {items.map(([to, icon, label]) => (
            <NavLink key={to} to={to} end={to === '/'}>
              <Icon name={icon} />{label}
              {to === '/review' && reviewCount > 0 && <span className="badge-count num">{reviewCount}</span>}
            </NavLink>
          ))}
        </nav>
        <div className="sidebar-foot">
          <h3>Faster<br />Verifications.<br /><em>Happier Patients.</em></h3>
          <p>Reducing admin work so you can focus on care.</p>
        </div>
      </aside>
      <div className="main">
        <header className="topbar">
          <form className="search" onSubmit={(e) => { e.preventDefault(); nav(`/history?search=${encodeURIComponent(q)}`) }}>
            <Icon name="search" size={16} />
            <input id="global-search" placeholder="Search patients, member ID, or payer…" value={q} onChange={(e) => setQ(e.target.value)} />
            <kbd>⌘K</kbd>
          </form>
          <div className="topbar-right">
            <div className="live-pill"><span className={`live-dot ${connected ? 'on' : ''}`} />{connected ? 'Live' : 'Reconnecting…'}</div>
            <Icon name="bell" />
            <div className="user"><div className="avatar">SL</div><div><b>Dr. Sarah Lee</b><small>Riverside Dental Care</small></div></div>
          </div>
        </header>
        <Outlet />
      </div>
      <ChatWidget />
    </div>
  )
}
