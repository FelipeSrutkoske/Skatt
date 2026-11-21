import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { ApiError, api } from '../lib/api'
import { formatDateBR } from '../lib/format'
import type { SettingsView, UpdateSettingsRequest } from '../types/api'

function extractErrorMessage(err: unknown, fallback: string): string {
  if (err instanceof ApiError && err.message) {
    return err.message
  }
  if (err instanceof Error && err.message) {
    return err.message
  }
  return fallback
}

function formatDateTimeBR(raw: string): string {
  const trimmed = raw.trim()
  if (!trimmed) {
    return 'Nunca'
  }
  const match = /^(\d{4})-(\d{2})-(\d{2})[T ](\d{2}):(\d{2})/.exec(trimmed)
  if (!match) {
    return formatDateBR(trimmed)
  }
  return `${match[3]}/${match[2]}/${match[1]} ${match[4]}:${match[5]}`
}

export function SettingsPage() {
  const [settings, setSettings] = useState<SettingsView | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')

  const [includeSecrets, setIncludeSecrets] = useState(false)
  const [backupReminderDays, setBackupReminderDays] = useState('7')
  const [savingReminder, setSavingReminder] = useState(false)
  const [backupFeedback, setBackupFeedback] = useState('')
  const [backupError, setBackupError] = useState('')

  const [restoreFile, setRestoreFile] = useState<File | null>(null)
  const [confirmRestore, setConfirmRestore] = useState(false)
  const [restoring, setRestoring] = useState(false)
  const fileInputRef = useRef<HTMLInputElement | null>(null)

  const [smtpHost, setSmtpHost] = useState('')
  const [smtpPort, setSmtpPort] = useState('587')
  const [smtpUser, setSmtpUser] = useState('')
  const [smtpPass, setSmtpPass] = useState('')
  const [clearSmtpPass, setClearSmtpPass] = useState(false)
  const [smtpFromName, setSmtpFromName] = useState('')
  const [smtpTlsMode, setSmtpTlsMode] = useState('starttls')
  const [savingSmtp, setSavingSmtp] = useState(false)
  const [smtpFeedback, setSmtpFeedback] = useState('')
  const [smtpError, setSmtpError] = useState('')

  const [testEmailTo, setTestEmailTo] = useState('')
  const [testingSmtp, setTestingSmtp] = useState(false)
  const [testFeedback, setTestFeedback] = useState('')
  const [testError, setTestError] = useState('')

  const [aiModel, setAiModel] = useState('gemini-2.5-flash')
  const [aiApiKey, setAiApiKey] = useState('')
  const [clearAiApiKey, setClearAiApiKey] = useState(false)
  const [savingAi, setSavingAi] = useState(false)
  const [aiFeedback, setAiFeedback] = useState('')
  const [aiError, setAiError] = useState('')

  const applySettings = useCallback((data: SettingsView) => {
    setSettings(data)
    setBackupReminderDays(String(data.backup_reminder_days || 7))
    setSmtpHost(data.smtp_host ?? '')
    setSmtpPort(String(data.smtp_port || 587))
    setSmtpUser(data.smtp_user ?? '')
    setSmtpFromName(data.smtp_from_name ?? '')
    setSmtpTlsMode(data.smtp_tls_mode || 'starttls')
    setSmtpPass('')
    setClearSmtpPass(false)
    setTestEmailTo((prev) => prev || data.smtp_user || '')
    setAiModel(data.ai_model || 'gemini-2.5-flash')
    setAiApiKey('')
    setClearAiApiKey(false)
  }, [])

  const loadSettings = useCallback(async () => {
    setLoading(true)
    setLoadError('')
    try {
      const data = await api.getSettings()
      applySettings(data)
    } catch (err) {
      setLoadError(extractErrorMessage(err, 'Não foi possível carregar as configurações.'))
    } finally {
      setLoading(false)
    }
  }, [applySettings])

  useEffect(() => {
    void loadSettings()
  }, [loadSettings])

  function handleExportClick() {
    setBackupFeedback('Download do backup iniciado.')
    setBackupError('')
    window.setTimeout(() => {
      void api
        .getSettings()
        .then((data) => {
          setSettings(data)
        })
        .catch(() => undefined)
    }, 1500)
  }

  async function handleSaveReminderDays(e: FormEvent) {
    e.preventDefault()
    const days = Number(backupReminderDays)
    if (!Number.isInteger(days) || days < 0 || days > 365) {
      setBackupError('Informe um número de dias entre 0 e 365.')
      return
    }
    setSavingReminder(true)
    setBackupError('')
    setBackupFeedback('')
    try {
      const updated = await api.updateSettings({ backup_reminder_days: days })
      setSettings(updated)
      setBackupReminderDays(String(updated.backup_reminder_days))
      setBackupFeedback('Intervalo de lembrete atualizado.')
    } catch (err) {
      setBackupError(extractErrorMessage(err, 'Erro ao atualizar lembrete de backup.'))
    } finally {
      setSavingReminder(false)
    }
  }

  async function handleRestoreSubmit(e: FormEvent) {
    e.preventDefault()
    if (!restoreFile) {
      setBackupError('Selecione um arquivo .zip de backup.')
      return
    }
    if (!confirmRestore) {
      setBackupError('Marque a confirmação explícita antes de restaurar o backup.')
      return
    }
    setRestoring(true)
    setBackupError('')
    setBackupFeedback('')
    try {
      const res = await api.restoreBackup(restoreFile)
      setRestoreFile(null)
      setConfirmRestore(false)
      if (fileInputRef.current) {
        fileInputRef.current.value = ''
      }
      setBackupFeedback(res?.message || 'Backup restaurado com sucesso.')
      await loadSettings()
    } catch (err) {
      setBackupError(extractErrorMessage(err, 'Falha ao restaurar o arquivo de backup.'))
    } finally {
      setRestoring(false)
    }
  }

  async function handleSaveSmtp(e: FormEvent) {
    e.preventDefault()
    const portNum = Number(smtpPort)
    if (!Number.isInteger(portNum) || portNum < 1 || portNum > 65535) {
      setSmtpError('Informe uma porta SMTP válida (1 a 65535).')
      return
    }
    setSavingSmtp(true)
    setSmtpError('')
    setSmtpFeedback('')
    try {
      const payload: UpdateSettingsRequest = {
        smtp_host: smtpHost.trim(),
        smtp_port: portNum,
        smtp_user: smtpUser.trim(),
        smtp_from_name: smtpFromName.trim(),
        smtp_tls_mode: smtpTlsMode,
      }
      if (clearSmtpPass) {
        payload.clear_smtp_pass = true
      } else if (smtpPass !== '') {
        payload.smtp_pass = smtpPass
      }
      const updated = await api.updateSettings(payload)
      applySettings(updated)
      setSmtpFeedback('Configurações de SMTP salvas.')
    } catch (err) {
      setSmtpError(extractErrorMessage(err, 'Erro ao salvar configurações de SMTP.'))
    } finally {
      setSavingSmtp(false)
    }
  }

  async function handleTestSmtp(e: FormEvent) {
    e.preventDefault()
    const target = testEmailTo.trim()
    if (!target) {
      setTestError('Informe um e-mail de destino para o teste.')
      return
    }
    setTestingSmtp(true)
    setTestError('')
    setTestFeedback('')
    try {
      const res = await api.testSMTP({ to: target })
      setTestFeedback(res?.message || `E-mail de teste enviado para ${target}.`)
    } catch (err) {
      setTestError(extractErrorMessage(err, 'Falha ao enviar e-mail de teste.'))
    } finally {
      setTestingSmtp(false)
    }
  }

  async function handleSaveAi(e: FormEvent) {
    e.preventDefault()
    setSavingAi(true)
    setAiError('')
    setAiFeedback('')
    try {
      const payload: UpdateSettingsRequest = {
        ai_provider: 'gemini',
        ai_model: aiModel.trim() || 'gemini-2.5-flash',
      }
      if (clearAiApiKey) {
        payload.clear_ai_api_key = true
      } else if (aiApiKey !== '') {
        payload.ai_api_key = aiApiKey.trim()
      }
      const updated = await api.updateSettings(payload)
      applySettings(updated)
      setAiFeedback('Configurações de IA salvas.')
    } catch (err) {
      setAiError(extractErrorMessage(err, 'Erro ao salvar configurações de IA.'))
    } finally {
      setSavingAi(false)
    }
  }

  if (loading) {
    return (
      <div className="space-y-4">
        <div className="border-b border-[#dee2e6] pb-4">
          <h1 className="text-xl font-semibold tracking-tight text-[#212529]">
            Configurações e backup
          </h1>
        </div>
        <p className="text-sm text-[#6c757d]">Carregando configurações...</p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="border-b border-[#dee2e6] pb-4">
        <h1 className="text-xl font-semibold tracking-tight text-[#212529]">
          Configurações e backup
        </h1>
        <p className="mt-1 text-xs text-[#6c757d]">
          Backup local em arquivo compactado, credenciais de envio por e-mail e extração opcional
          sob demanda.
        </p>
      </div>

      {loadError && (
        <div
          role="alert"
          className="glass-panel rounded-xl border-l-2 border-l-[#212529] px-4 py-2.5 text-sm text-[#212529]"
        >
          {loadError}
        </div>
      )}

      <section className="glass-panel rounded-xl overflow-hidden">
        <div className="px-4 py-3 border-b border-[#dee2e6]">
          <h2 className="text-sm font-semibold text-[#212529]">Backup e restauração</h2>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 border-b border-[#dee2e6] divide-y sm:divide-y-0 sm:divide-x divide-[#dee2e6]">
          <div className="px-4 py-3">
            <div className="text-xs text-[#6c757d]">Último backup exportado</div>
            <div className="mt-1 text-sm font-medium tabular-nums text-[#212529]">
              {formatDateTimeBR(settings?.last_backup_at ?? '')}
            </div>
          </div>
          <div className="px-4 py-3">
            <div className="text-xs text-[#6c757d]">Última alteração nos dados</div>
            <div className="mt-1 text-sm font-medium tabular-nums text-[#212529]">
              {formatDateTimeBR(settings?.last_mutation_at ?? '')}
            </div>
          </div>
        </div>

        {backupError && (
          <div
            role="alert"
            className="border-b border-[#dee2e6] px-4 py-2.5 text-sm text-[#212529] bg-[#e9ecef]/70"
          >
            {backupError}
          </div>
        )}

        {backupFeedback && (
          <div
            role="status"
            className="border-b border-[#dee2e6] px-4 py-2.5 text-sm text-[#212529] bg-[#f8f9fa]"
          >
            {backupFeedback}
          </div>
        )}

        <div className="px-4 py-4 border-b border-[#dee2e6] flex flex-wrap items-center justify-between gap-4">
          <div className="space-y-1">
            <label className="inline-flex items-center gap-2 text-sm text-[#212529] cursor-pointer select-none">
              <input
                type="checkbox"
                checked={includeSecrets}
                onChange={(e) => setIncludeSecrets(e.target.checked)}
                className="border-[#ced4da]"
              />
              Incluir credenciais SMTP/IA no backup
            </label>
            <p className="text-xs text-[#6c757d]">
              Por padrão, senha SMTP e chave da API de IA são omitidas para armazenamento seguro em
              nuvem.
            </p>
          </div>

          <a
            href={api.backupExportUrl(includeSecrets)}
            download
            onClick={handleExportClick}
            className="inline-flex items-center h-9 px-4 rounded-xl text-sm font-medium text-[#f8f9fa] bg-[#212529] hover:bg-[#343a40] transition-colors shrink-0"
          >
            Exportar backup (.zip)
          </a>
        </div>

        <form
          onSubmit={handleSaveReminderDays}
          className="px-4 py-3 border-b border-[#dee2e6] bg-[#f8f9fa]/70 flex flex-wrap items-end gap-3"
        >
          <div className="w-48">
            <label
              htmlFor="backup-reminder-days"
              className="block text-xs font-medium text-[#495057] mb-1"
            >
              Lembrete de backup (dias)
            </label>
            <input
              id="backup-reminder-days"
              type="number"
              min={0}
              max={365}
              value={backupReminderDays}
              onChange={(e) => setBackupReminderDays(e.target.value)}
              className="w-full h-9 px-2.5 rounded-lg text-sm tabular-nums bg-white border border-[#ced4da] text-[#212529] focus:outline-none focus:border-[#495057]"
            />
          </div>
          <button
            type="submit"
            disabled={savingReminder}
            className="h-9 px-3 rounded-xl text-sm text-[#212529] bg-white border border-[#ced4da] hover:bg-[#e9ecef] disabled:opacity-50 transition-colors cursor-pointer"
          >
            {savingReminder ? 'Salvando...' : 'Salvar lembrete'}
          </button>
        </form>

        <form onSubmit={handleRestoreSubmit} className="px-4 py-4 space-y-3">
          <div>
            <h3 className="text-xs font-semibold uppercase tracking-wider text-[#6c757d]">
              Restaurar backup (.zip)
            </h3>
            <p className="mt-1 text-xs text-[#6c757d]">
              Substitui o banco de dados e os arquivos de faturas atuais pelo conteúdo do backup.
              Uma cópia pré-restauração do banco atual é salva automaticamente em disco.
            </p>
          </div>

          <div className="flex flex-wrap items-center gap-3">
            <input
              ref={fileInputRef}
              type="file"
              accept=".zip,application/zip"
              onChange={(e) => {
                const file = e.target.files?.[0] ?? null
                setRestoreFile(file)
                setConfirmRestore(false)
                setBackupError('')
              }}
              className="text-sm text-[#495057] file:mr-3 file:py-1.5 file:px-3 file:rounded-lg file:border file:border-[#ced4da] file:bg-[#f8f9fa] file:text-xs file:font-medium file:text-[#212529] hover:file:bg-[#e9ecef] cursor-pointer"
            />
          </div>

          <label className="flex items-start gap-2 text-sm text-[#212529] cursor-pointer select-none">
            <input
              type="checkbox"
              checked={confirmRestore}
              disabled={!restoreFile}
              onChange={(e) => setConfirmRestore(e.target.checked)}
              className="mt-1 border-[#ced4da]"
            />
            <span>
              Confirmo a restauração e entendo que os dados atuais serão substituídos pelo arquivo
              selecionado.
            </span>
          </label>

          <div>
            <button
              type="submit"
              disabled={!restoreFile || !confirmRestore || restoring}
              className="h-9 px-4 rounded-xl text-sm font-medium text-[#212529] bg-[#e9ecef] border border-[#ced4da] hover:bg-[#dee2e6] disabled:opacity-50 transition-colors cursor-pointer"
            >
              {restoring ? 'Restaurando...' : 'Restaurar backup (.zip)'}
            </button>
          </div>
        </form>
      </section>

      <section className="glass-panel rounded-xl overflow-hidden">
        <div className="px-4 py-3 border-b border-[#dee2e6]">
          <h2 className="text-sm font-semibold text-[#212529]">E-mail (SMTP)</h2>
        </div>

        {smtpError && (
          <div
            role="alert"
            className="border-b border-[#dee2e6] px-4 py-2.5 text-sm text-[#212529] bg-[#e9ecef]/70"
          >
            {smtpError}
          </div>
        )}

        {smtpFeedback && (
          <div
            role="status"
            className="border-b border-[#dee2e6] px-4 py-2.5 text-sm text-[#212529] bg-[#f8f9fa]"
          >
            {smtpFeedback}
          </div>
        )}

        <form onSubmit={handleSaveSmtp} className="px-4 py-4 border-b border-[#dee2e6] space-y-4">
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
            <div>
              <label htmlFor="smtp-host" className="block text-xs font-medium text-[#495057] mb-1">
                Host
              </label>
              <input
                id="smtp-host"
                type="text"
                value={smtpHost}
                onChange={(e) => setSmtpHost(e.target.value)}
                placeholder="smtp.gmail.com"
                className="w-full h-9 px-2.5 rounded-lg text-sm bg-white border border-[#ced4da] text-[#212529] focus:outline-none focus:border-[#495057]"
              />
            </div>

            <div>
              <label htmlFor="smtp-port" className="block text-xs font-medium text-[#495057] mb-1">
                Porta
              </label>
              <input
                id="smtp-port"
                type="number"
                min={1}
                max={65535}
                value={smtpPort}
                onChange={(e) => setSmtpPort(e.target.value)}
                className="w-full h-9 px-2.5 rounded-lg text-sm tabular-nums bg-white border border-[#ced4da] text-[#212529] focus:outline-none focus:border-[#495057]"
              />
            </div>

            <div>
              <label
                htmlFor="smtp-tls-mode"
                className="block text-xs font-medium text-[#495057] mb-1"
              >
                Modo TLS
              </label>
              <select
                id="smtp-tls-mode"
                value={smtpTlsMode}
                onChange={(e) => setSmtpTlsMode(e.target.value)}
                className="w-full h-9 px-2.5 rounded-lg text-sm bg-white border border-[#ced4da] text-[#212529] focus:outline-none focus:border-[#495057]"
              >
                <option value="starttls">STARTTLS (porta 587)</option>
                <option value="tls">TLS implícito (porta 465)</option>
                <option value="none">Sem criptografia (none)</option>
              </select>
            </div>

            <div>
              <label htmlFor="smtp-user" className="block text-xs font-medium text-[#495057] mb-1">
                Usuário
              </label>
              <input
                id="smtp-user"
                type="text"
                value={smtpUser}
                onChange={(e) => setSmtpUser(e.target.value)}
                placeholder="seu-email@gmail.com"
                className="w-full h-9 px-2.5 rounded-lg text-sm bg-white border border-[#ced4da] text-[#212529] focus:outline-none focus:border-[#495057]"
              />
            </div>

            <div>
              <label htmlFor="smtp-pass" className="block text-xs font-medium text-[#495057] mb-1">
                Senha
              </label>
              <input
                id="smtp-pass"
                type="password"
                autoComplete="new-password"
                value={smtpPass}
                disabled={clearSmtpPass}
                onChange={(e) => setSmtpPass(e.target.value)}
                placeholder={
                  settings?.has_smtp_pass
                    ? 'Configurada (em branco mantém atual)'
                    : 'Senha ou Senha de App'
                }
                className="w-full h-9 px-2.5 rounded-lg text-sm bg-white border border-[#ced4da] text-[#212529] disabled:bg-[#f8f9fa] focus:outline-none focus:border-[#495057]"
              />
              {settings?.has_smtp_pass && (
                <label className="mt-1.5 inline-flex items-center gap-1.5 text-xs text-[#495057] cursor-pointer select-none">
                  <input
                    type="checkbox"
                    checked={clearSmtpPass}
                    onChange={(e) => {
                      setClearSmtpPass(e.target.checked)
                      if (e.target.checked) {
                        setSmtpPass('')
                      }
                    }}
                    className="border-[#ced4da]"
                  />
                  Limpar senha salva
                </label>
              )}
            </div>

            <div>
              <label
                htmlFor="smtp-from-name"
                className="block text-xs font-medium text-[#495057] mb-1"
              >
                Nome do remetente
              </label>
              <input
                id="smtp-from-name"
                type="text"
                value={smtpFromName}
                onChange={(e) => setSmtpFromName(e.target.value)}
                placeholder="Skatt"
                className="w-full h-9 px-2.5 rounded-lg text-sm bg-white border border-[#ced4da] text-[#212529] focus:outline-none focus:border-[#495057]"
              />
            </div>
          </div>

          <div>
            <button
              type="submit"
              disabled={savingSmtp}
              className="h-9 px-4 rounded-xl text-sm font-medium text-[#f8f9fa] bg-[#212529] hover:bg-[#343a40] disabled:opacity-50 transition-colors cursor-pointer"
            >
              {savingSmtp ? 'Salvando...' : 'Salvar configurações'}
            </button>
          </div>
        </form>

        <form onSubmit={handleTestSmtp} className="px-4 py-3 bg-[#f8f9fa]/70 space-y-2">
          <div className="flex flex-wrap items-end gap-3">
            <div className="flex-1 min-w-[220px] max-w-md">
              <label
                htmlFor="smtp-test-to"
                className="block text-xs font-medium text-[#495057] mb-1"
              >
                E-mail de destino para teste
              </label>
              <input
                id="smtp-test-to"
                type="email"
                value={testEmailTo}
                onChange={(e) => setTestEmailTo(e.target.value)}
                placeholder="destino@exemplo.com"
                className="w-full h-9 px-2.5 rounded-lg text-sm bg-white border border-[#ced4da] text-[#212529] focus:outline-none focus:border-[#495057]"
              />
            </div>
            <button
              type="submit"
              disabled={testingSmtp}
              className="h-9 px-4 rounded-xl text-sm text-[#212529] bg-white border border-[#ced4da] hover:bg-[#e9ecef] disabled:opacity-50 transition-colors cursor-pointer"
            >
              {testingSmtp ? 'Enviando teste...' : 'Enviar e-mail de teste'}
            </button>
          </div>
          {testError && (
            <p role="alert" className="text-xs font-medium text-[#212529]">
              {testError}
            </p>
          )}
          {testFeedback && (
            <p role="status" className="text-xs text-[#212529]">
              {testFeedback}
            </p>
          )}
        </form>
      </section>

      <section className="glass-panel rounded-xl overflow-hidden">
        <div className="px-4 py-3 border-b border-[#dee2e6]">
          <h2 className="text-sm font-semibold text-[#212529]">IA sob demanda (Gemini)</h2>
        </div>

        <div className="px-4 py-2.5 border-b border-[#dee2e6] bg-[#f8f9fa]/70 text-xs text-[#495057]">
          A extração via IA nunca é acionada automaticamente. Ela só é executada quando você clicar
          explicitamente na opção de extração na tela de importação, enviando exclusivamente o texto
          extraído já mascarado localmente (sem CPF, números de cartão ou endereço).
        </div>

        {aiError && (
          <div
            role="alert"
            className="border-b border-[#dee2e6] px-4 py-2.5 text-sm text-[#212529] bg-[#e9ecef]/70"
          >
            {aiError}
          </div>
        )}

        {aiFeedback && (
          <div
            role="status"
            className="border-b border-[#dee2e6] px-4 py-2.5 text-sm text-[#212529] bg-[#f8f9fa]"
          >
            {aiFeedback}
          </div>
        )}

        <form onSubmit={handleSaveAi} className="px-4 py-4 space-y-4">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div>
              <label
                htmlFor="ai-api-key"
                className="block text-xs font-medium text-[#495057] mb-1"
              >
                API Key
              </label>
              <input
                id="ai-api-key"
                type="password"
                autoComplete="new-password"
                value={aiApiKey}
                disabled={clearAiApiKey}
                onChange={(e) => setAiApiKey(e.target.value)}
                placeholder={
                  settings?.has_ai_api_key
                    ? 'Chave configurada (em branco mantém atual)'
                    : 'Chave da Gemini API'
                }
                className="w-full h-9 px-2.5 rounded-lg text-sm bg-white border border-[#ced4da] text-[#212529] disabled:bg-[#f8f9fa] focus:outline-none focus:border-[#495057]"
              />
              {settings?.has_ai_api_key && (
                <label className="mt-1.5 inline-flex items-center gap-1.5 text-xs text-[#495057] cursor-pointer select-none">
                  <input
                    type="checkbox"
                    checked={clearAiApiKey}
                    onChange={(e) => {
                      setClearAiApiKey(e.target.checked)
                      if (e.target.checked) {
                        setAiApiKey('')
                      }
                    }}
                    className="border-[#ced4da]"
                  />
                  Limpar chave salva
                </label>
              )}
            </div>

            <div>
              <label htmlFor="ai-model" className="block text-xs font-medium text-[#495057] mb-1">
                Modelo
              </label>
              <input
                id="ai-model"
                type="text"
                value={aiModel}
                onChange={(e) => setAiModel(e.target.value)}
                placeholder="gemini-2.5-flash"
                className="w-full h-9 px-2.5 rounded-lg text-sm bg-white border border-[#ced4da] text-[#212529] focus:outline-none focus:border-[#495057]"
              />
            </div>
          </div>

          <div>
            <button
              type="submit"
              disabled={savingAi}
              className="h-9 px-4 rounded-xl text-sm font-medium text-[#f8f9fa] bg-[#212529] hover:bg-[#343a40] disabled:opacity-50 transition-colors cursor-pointer"
            >
              {savingAi ? 'Salvando...' : 'Salvar configurações de IA'}
            </button>
          </div>
        </form>
      </section>
    </div>
  )
}
