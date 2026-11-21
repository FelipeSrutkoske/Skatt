import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { Link, NavLink, Outlet, useLocation } from 'react-router-dom'
import { api } from '../lib/api'
import type { BackupReminderStatus } from '../types/api'
import { SkattLogo } from './SkattLogo'
import { WatercolorBackground } from './WatercolorBackground'

export interface LayoutProps {
  children?: ReactNode
}

const SIDEBAR_STORAGE_KEY = 'skatt.sidebar.collapsed'

const PRIMARY_NAV = [
  { to: '/', label: 'Visão geral', short: 'VG', end: true },
  { to: '/import', label: 'Importar', short: 'IM', end: false },
  { to: '/people', label: 'Pessoas', short: 'PE', end: false },
  { to: '/receipts', label: 'Recibos', short: 'RE', end: false },
]

function backupTooltipText(reminder: BackupReminderStatus | null): string {
  if (!reminder || !reminder.needs_backup) {
    return 'Backup em dia. Clique para exportar uma cópia (.zip).'
  }
  if (!reminder.last_backup_at) {
    return 'Backup pendente: há alterações sem cópia de segurança. Clique para exportar (.zip).'
  }
  if (reminder.days_since_backup !== null && reminder.days_since_backup > 0) {
    const unit = reminder.days_since_backup === 1 ? 'dia' : 'dias'
    return `Backup pendente: alterações há ${reminder.days_since_backup} ${unit}. Clique para exportar (.zip).`
  }
  return 'Backup pendente: há alterações desde o último backup. Clique para exportar (.zip).'
}

export function Layout({ children }: LayoutProps) {
  const location = useLocation()
  const [backupReminder, setBackupReminder] = useState<BackupReminderStatus | null>(null)
  const [collapsed, setCollapsed] = useState<boolean>(() => {
    try {
      return window.localStorage.getItem(SIDEBAR_STORAGE_KEY) === 'true'
    } catch {
      return false
    }
  })

  useEffect(() => {
    try {
      window.localStorage.setItem(SIDEBAR_STORAGE_KEY, String(collapsed))
    } catch {
      return
    }
  }, [collapsed])

  const refreshBackupStatus = useCallback(() => {
    api
      .getDashboard()
      .then((res) => setBackupReminder(res.backup_reminder ?? null))
      .catch(() => undefined)
  }, [])

  useEffect(() => {
    refreshBackupStatus()
  }, [refreshBackupStatus, location.pathname])

  const isImportRoute = location.pathname.startsWith('/import')
  const isReceiptsRoute = location.pathname.startsWith('/receipts')
  const isSettingsRoute = location.pathname.startsWith('/settings')
  const needsBackup = Boolean(backupReminder?.needs_backup)
  const tooltipMessage = backupTooltipText(backupReminder)

  return (
    <div className="relative min-h-screen lg:h-screen lg:overflow-hidden text-[#212529]">
      <WatercolorBackground />

      <div className="relative z-10 min-h-screen lg:h-screen flex flex-col lg:flex-row">
        <aside
          className={[
            'glass-sidebar no-print shrink-0 border-b lg:border-b-0 lg:border-r border-[#ced4da]/85 flex flex-col justify-between transition-[width] duration-200',
            collapsed ? 'lg:w-14' : 'lg:w-52',
          ].join(' ')}
        >
          <div>
            <div className="h-13 px-3 border-b border-[#ced4da]/70 flex items-center justify-between gap-2">
              {!collapsed ? (
                <Link
                  to="/"
                  className="inline-flex items-center gap-2.5 pl-1 text-base font-bold tracking-tight text-[#212529] truncate"
                >
                  <SkattLogo size={24} />
                  <span>Skatt</span>
                </Link>
              ) : (
                <button
                  type="button"
                  onClick={() => setCollapsed(false)}
                  aria-label="Expandir painel lateral"
                  title="Expandir painel lateral"
                  className="hidden lg:inline-flex items-center justify-center mx-auto cursor-pointer"
                >
                  <SkattLogo size={24} />
                </button>
              )}

              {!collapsed && (
                <button
                  type="button"
                  onClick={() => setCollapsed(true)}
                  aria-label="Recolher painel lateral"
                  title="Recolher painel lateral"
                  className="hidden lg:inline-flex items-center justify-center w-8 h-8 rounded-lg text-[#495057] hover:text-[#212529] hover:bg-white/75 transition-colors cursor-pointer"
                >
                  <svg width="15" height="15" viewBox="0 0 16 16" fill="none" aria-hidden="true">
                    <path
                      d="M10 3.5L5.5 8L10 12.5"
                      stroke="currentColor"
                      strokeWidth="1.6"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                    />
                  </svg>
                </button>
              )}

              <nav className="flex lg:hidden items-center gap-1 overflow-x-auto">
                {PRIMARY_NAV.map((item) => (
                  <NavLink
                    key={item.to}
                    to={item.to}
                    end={item.end}
                    className={({ isActive }) =>
                      [
                        'px-2.5 py-1.5 rounded-lg text-xs font-medium whitespace-nowrap transition-colors',
                        isActive
                          ? 'bg-[#212529] text-[#f8f9fa]'
                          : 'text-[#343a40] hover:text-[#212529] hover:bg-white/70',
                      ].join(' ')
                    }
                  >
                    {item.label}
                  </NavLink>
                ))}
                <NavLink
                  to="/settings"
                  aria-label="Configurações"
                  title="Configurações"
                  className={({ isActive }) =>
                    [
                      'p-1.5 rounded-lg transition-colors',
                      isActive
                        ? 'bg-[#212529] text-[#f8f9fa]'
                        : 'text-[#343a40] hover:text-[#212529] hover:bg-white/70',
                    ].join(' ')
                  }
                >
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true">
                    <path
                      d="M12 15.25A3.25 3.25 0 1 0 12 8.75a3.25 3.25 0 0 0 0 6.5Z"
                      stroke="currentColor"
                      strokeWidth="1.8"
                    />
                    <path
                      d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33h.08a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82v.08a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1Z"
                      stroke="currentColor"
                      strokeWidth="1.6"
                      strokeLinejoin="round"
                    />
                  </svg>
                </NavLink>
              </nav>
            </div>

            <div className="hidden lg:block px-2 py-4">
              <nav className="space-y-1">
                {PRIMARY_NAV.map((item) => (
                  <NavLink
                    key={item.to}
                    to={item.to}
                    end={item.end}
                    title={collapsed ? item.label : undefined}
                    className={({ isActive }) =>
                      [
                        'flex items-center rounded-lg text-sm font-medium transition-all',
                        collapsed ? 'justify-center h-9 px-0' : 'px-3 py-2',
                        isActive
                          ? 'bg-[#212529] text-[#f8f9fa] shadow-xs'
                          : 'text-[#343a40] hover:text-[#212529] hover:bg-white/65',
                      ].join(' ')
                    }
                  >
                    <span>{collapsed ? item.short : item.label}</span>
                  </NavLink>
                ))}
              </nav>
            </div>
          </div>

          <div
            className={[
              'hidden lg:flex items-center border-t border-[#ced4da]/70 px-2.5 py-2.5 gap-1.5',
              collapsed ? 'flex-col' : 'justify-between',
            ].join(' ')}
          >
            <NavLink
              to="/settings"
              aria-label="Configurações"
              title="Configurações"
              className={[
                'inline-flex items-center justify-center rounded-lg w-9 h-9 transition-colors',
                isSettingsRoute
                  ? 'bg-[#212529] text-[#f8f9fa]'
                  : 'text-[#495057] hover:text-[#212529] hover:bg-white/75',
              ].join(' ')}
            >
              <svg width="19" height="19" viewBox="0 0 24 24" fill="none" aria-hidden="true">
                <path
                  d="M12 15.25A3.25 3.25 0 1 0 12 8.75a3.25 3.25 0 0 0 0 6.5Z"
                  stroke="currentColor"
                  strokeWidth="1.8"
                />
                <path
                  d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33h.08a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82v.08a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1Z"
                  stroke="currentColor"
                  strokeWidth="1.6"
                  strokeLinejoin="round"
                />
              </svg>
            </NavLink>

            <div className="relative group">
              <a
                href={api.backupExportUrl(false)}
                download
                onClick={() => {
                  window.setTimeout(refreshBackupStatus, 1400)
                }}
                aria-label={tooltipMessage}
                className="relative inline-flex items-center justify-center rounded-lg w-9 h-9 text-[#495057] hover:text-[#212529] hover:bg-white/75 transition-colors"
              >
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true">
                  <path
                    d="M12 3.5v11m0 0-4-4m4 4 4-4M4.5 16.5v2a2 2 0 0 0 2 2h11a2 2 0 0 0 2-2v-2"
                    stroke="currentColor"
                    strokeWidth="1.8"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  />
                </svg>
                {needsBackup && (
                  <span className="absolute top-1.5 right-1.5 w-2 h-2 rounded-full bg-[#212529] ring-2 ring-[#f8f9fa]" />
                )}
              </a>
              <div
                role="tooltip"
                className="pointer-events-none opacity-0 group-hover:opacity-100 transition-opacity duration-150 absolute left-full bottom-1 ml-2 w-60 rounded-lg bg-[#212529] text-[#f8f9fa] px-2.5 py-2 text-xs font-medium shadow-lg z-50"
              >
                {tooltipMessage}
              </div>
            </div>
          </div>
        </aside>

        <main className="flex-1 min-w-0 lg:h-screen lg:overflow-y-auto px-4 py-4 sm:px-6 lg:px-7 lg:py-5 pb-20 lg:pb-16">
          {children ?? <Outlet />}
        </main>
      </div>

      <div className="no-print fixed bottom-4 right-5 z-30 flex items-center gap-2">
        <Link
          to="/receipts"
          aria-label="Gerar recibo"
          title="Gerar recibo"
          className={[
            'glass-fab w-10 h-10 rounded-full inline-flex items-center justify-center transition-all',
            isReceiptsRoute
              ? 'bg-[#343a40] text-[#f8f9fa] border-[#212529]'
              : 'text-[#212529] hover:bg-white',
          ].join(' ')}
        >
          <svg width="17" height="17" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path
              d="M6 3.5h12v17l-2.5-1.5L13 20.5 10.5 19 8 20.5 6 19.3V3.5Z"
              stroke="currentColor"
              strokeWidth="1.8"
              strokeLinejoin="round"
            />
            <path
              d="M9 8.5h6M9 12h6M9 15.5h3.5"
              stroke="currentColor"
              strokeWidth="1.8"
              strokeLinecap="round"
            />
          </svg>
        </Link>

        <Link
          to="/import"
          aria-label="Importar fatura"
          title="Importar fatura"
          className={[
            'w-10 h-10 rounded-full inline-flex items-center justify-center shadow-md transition-all',
            isImportRoute
              ? 'bg-[#495057] text-[#f8f9fa]'
              : 'bg-[#212529] text-[#f8f9fa] hover:bg-[#343a40]',
          ].join(' ')}
        >
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path
              d="M12 5v14M5 12h14"
              stroke="currentColor"
              strokeWidth="2.1"
              strokeLinecap="round"
            />
          </svg>
        </Link>
      </div>
    </div>
  )
}
