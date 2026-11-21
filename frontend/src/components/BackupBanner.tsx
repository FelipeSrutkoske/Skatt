import { api } from '../lib/api'
import type { BackupReminderStatus } from '../types/api'

export interface BackupBannerProps {
  reminder: BackupReminderStatus | null | undefined
  onExported?: () => void
}

function buildReminderText(reminder: BackupReminderStatus): string {
  if (!reminder.last_backup_at) {
    return 'Há alterações sem backup. Exporte uma cópia de segurança (.zip).'
  }
  if (reminder.days_since_backup !== null && reminder.days_since_backup > 0) {
    const unit = reminder.days_since_backup === 1 ? 'dia' : 'dias'
    return `Alterações desde o último backup (${reminder.days_since_backup} ${unit} atrás).`
  }
  return 'Há alterações registradas desde o último backup.'
}

export function BackupBanner({ reminder, onExported }: BackupBannerProps) {
  if (!reminder || !reminder.needs_backup) {
    return null
  }

  return (
    <div
      role="status"
      className="no-print glass-panel rounded-xl px-3.5 py-2 flex flex-wrap items-center justify-between gap-3 text-xs font-medium text-[#343a40]"
    >
      <span>{buildReminderText(reminder)}</span>
      <a
        href={api.backupExportUrl(false)}
        download
        onClick={onExported}
        className="inline-flex items-center px-2.5 py-1 rounded-lg text-xs font-medium text-[#212529] bg-[#e9ecef] hover:bg-[#dee2e6] border border-[#ced4da] transition-colors shrink-0"
      >
        Exportar backup (.zip)
      </a>
    </div>
  )
}
