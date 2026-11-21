import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { useSearchParams } from 'react-router-dom'
import { BankSwitcher, DEFAULT_BANK_CODES } from '../components/BankLogo'
import { ApiError, api } from '../lib/api'
import { bankLabel, formatBRL, formatDateBR, monthNamePT } from '../lib/format'
import type {
  Person,
  Receipt,
  ReceiptMonthAvailability,
  ReceiptPreview,
} from '../types/api'

const BASE_YEARS = [2024, 2025, 2026]
const SHORT_MONTHS = ['Jan', 'Fev', 'Mar', 'Abr', 'Mai', 'Jun', 'Jul', 'Ago', 'Set', 'Out', 'Nov', 'Dez']

function extractErrorMessage(err: unknown, fallback: string): string {
  if (err instanceof ApiError && err.message) {
    return err.message
  }
  if (err instanceof Error && err.message) {
    return err.message
  }
  return fallback
}

function formatDateTimeBR(raw: string | null): string {
  if (!raw) {
    return '—'
  }
  const trimmed = raw.trim()
  const match = /^(\d{4})-(\d{2})-(\d{2})[T ](\d{2}):(\d{2})/.exec(trimmed)
  if (!match) {
    return formatDateBR(trimmed)
  }
  return `${match[3]}/${match[2]}/${match[1]} ${match[4]}:${match[5]}`
}

function deliveryStatusLabel(status: string): string {
  switch (status) {
    case 'sent':
      return 'Enviado'
    case 'failed':
      return 'Falha no envio'
    case 'generated':
    case 'draft':
    case 'preview':
      return 'Gerado'
    default:
      return status || '—'
  }
}

export function ReceiptsPage() {
  const [searchParams] = useSearchParams()
  const now = new Date()

  const initialYearParam = Number(searchParams.get('year'))
  const initialMonthParam = Number(searchParams.get('month'))
  const initialPersonParam = Number(searchParams.get('person_id'))
  const initialBankParam = searchParams.get('bank') ?? ''

  const [people, setPeople] = useState<Person[]>([])
  const [availableYears, setAvailableYears] = useState<number[]>(BASE_YEARS)
  const [availableBanks, setAvailableBanks] = useState<string[]>([...DEFAULT_BANK_CODES])
  const [history, setHistory] = useState<Receipt[]>([])
  const [loadingInit, setLoadingInit] = useState(true)

  const [personId, setPersonId] = useState<number | ''>(
    Number.isInteger(initialPersonParam) && initialPersonParam > 0 ? initialPersonParam : '',
  )
  const [year, setYear] = useState<number>(
    Number.isInteger(initialYearParam) && initialYearParam >= 2000
      ? initialYearParam
      : now.getFullYear(),
  )
  const [month, setMonth] = useState<number>(
    Number.isInteger(initialMonthParam) && initialMonthParam >= 1 && initialMonthParam <= 12
      ? initialMonthParam
      : now.getMonth() + 1,
  )
  const [bank, setBank] = useState<string>(initialBankParam)
  const [includePriorBalance, setIncludePriorBalance] = useState<boolean>(false)

  const [availability, setAvailability] = useState<ReceiptMonthAvailability[]>([])
  const [loadingAvailability, setLoadingAvailability] = useState(false)

  const [preview, setPreview] = useState<ReceiptPreview | null>(null)
  const [loadingPreview, setLoadingPreview] = useState(false)
  const [recipientEmail, setRecipientEmail] = useState('')
  const [subject, setSubject] = useState('')
  const [sending, setSending] = useState(false)

  const [errorMessage, setErrorMessage] = useState('')
  const [statusMessage, setStatusMessage] = useState('')
  const [copyFeedback, setCopyFeedback] = useState('')

  const iframeRef = useRef<HTMLIFrameElement | null>(null)

  const loadHistory = useCallback(async () => {
    try {
      const receipts = await api.listReceipts()
      setHistory(receipts ?? [])
    } catch (err) {
      setErrorMessage(extractErrorMessage(err, 'Não foi possível carregar o histórico de recibos.'))
    }
  }, [])

  useEffect(() => {
    let active = true
    async function loadInitial() {
      setLoadingInit(true)
      setErrorMessage('')
      try {
        const [peopleRes, dashboardRes, receiptsRes] = await Promise.all([
          api.listPeople(true),
          api.getDashboard(),
          api.listReceipts(),
        ])
        if (!active) {
          return
        }
        const peopleList = peopleRes ?? []
        setPeople(peopleList)
        setHistory(receiptsRes ?? [])

        const yearSet = new Set<number>([...BASE_YEARS, ...(dashboardRes.available_years ?? [])])
        yearSet.add(now.getFullYear())
        if (year >= 2000) {
          yearSet.add(year)
        }
        setAvailableYears(Array.from(yearSet).sort((a, b) => a - b))

        const bankSet = new Set<string>([
          ...DEFAULT_BANK_CODES,
          ...(dashboardRes.available_banks ?? []),
        ])
        setAvailableBanks(Array.from(bankSet))

        setPersonId((prev) => {
          if (prev !== '' && peopleList.some((p) => p.id === prev)) {
            return prev
          }
          const preferred = peopleList.find((p) => !p.is_self && !p.archived) ?? peopleList[0]
          return preferred ? preferred.id : ''
        })
      } catch (err) {
        if (active) {
          setErrorMessage(extractErrorMessage(err, 'Erro ao carregar dados iniciais de recibos.'))
        }
      } finally {
        if (active) {
          setLoadingInit(false)
        }
      }
    }
    void loadInitial()
    return () => {
      active = false
    }
  }, [])

  useEffect(() => {
    if (personId === '') {
      setAvailability([])
      return
    }
    let active = true
    setLoadingAvailability(true)
    api
      .getReceiptAvailability(personId, year, bank)
      .then((res) => {
        if (active) {
          setAvailability(res ?? [])
        }
      })
      .catch(() => {
        if (active) {
          setAvailability([])
        }
      })
      .finally(() => {
        if (active) {
          setLoadingAvailability(false)
        }
      })
    return () => {
      active = false
    }
  }, [personId, year, bank])

  const yearOptions = useMemo(() => {
    const set = new Set<number>(availableYears)
    set.add(year)
    return Array.from(set).sort((a, b) => a - b)
  }, [availableYears, year])

  const availabilityByMonth = useMemo(() => {
    const map = new Map<number, ReceiptMonthAvailability>()
    for (const item of availability) {
      map.set(item.month, item)
    }
    return map
  }, [availability])

  const requestPreview = useCallback(
    async (targetPerson: number, targetYear: number, targetMonth: number, targetBank: string, prior: boolean) => {
      setLoadingPreview(true)
      setErrorMessage('')
      setStatusMessage('')
      setCopyFeedback('')
      try {
        const effectivePrior = targetBank === '' ? prior : false
        const result = await api.previewReceipt({
          person_id: targetPerson,
          reference_year: targetYear,
          reference_month: targetMonth,
          bank_filter: targetBank,
          include_prior_balance: effectivePrior,
        })
        setPreview(result)
        setRecipientEmail(result.person_email ?? '')
        setSubject(result.subject ?? '')
      } catch (err) {
        setErrorMessage(
          extractErrorMessage(err, 'Não foi possível gerar a pré-visualização do recibo.'),
        )
      } finally {
        setLoadingPreview(false)
      }
    },
    [],
  )

  function handleBankChange(nextBank: string) {
    setBank(nextBank)
    if (nextBank !== '') {
      setIncludePriorBalance(false)
    }
  }

  function handleSelectMonth(nextMonth: number) {
    setMonth(nextMonth)
    if (personId !== '') {
      void requestPreview(personId, year, nextMonth, bank, includePriorBalance)
    }
  }

  async function handleGeneratePreview(e: FormEvent) {
    e.preventDefault()
    if (personId === '') {
      setErrorMessage('Selecione uma pessoa para gerar o recibo.')
      return
    }
    await requestPreview(personId, year, month, bank, includePriorBalance)
  }

  async function handleSendEmail(e: FormEvent) {
    e.preventDefault()
    if (!preview) {
      return
    }
    const trimmedEmail = recipientEmail.trim()
    if (!trimmedEmail) {
      setErrorMessage('Informe o e-mail de destino antes de enviar.')
      return
    }
    setSending(true)
    setErrorMessage('')
    setStatusMessage('')
    try {
      const sent = await api.sendReceipt({
        person_id: preview.person_id,
        reference_year: preview.reference_year,
        reference_month: preview.reference_month,
        bank_filter: preview.bank_filter,
        include_prior_balance: preview.include_prior_balance,
        recipient_email: trimmedEmail,
        subject: subject.trim() || preview.subject,
      })
      if (sent.delivery_status === 'failed') {
        setErrorMessage(sent.error_message || 'Falha ao enviar o recibo por e-mail.')
      } else {
        setStatusMessage(`Recibo enviado para ${sent.recipient_email}.`)
      }
      await loadHistory()
    } catch (err) {
      setErrorMessage(extractErrorMessage(err, 'Erro ao enviar recibo por e-mail.'))
      await loadHistory()
    } finally {
      setSending(false)
    }
  }

  function handleDownloadHTML() {
    if (!preview) {
      return
    }
    const blob = new Blob([preview.html_body], { type: 'text/html;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const slug = preview.person_name
      .toLowerCase()
      .normalize('NFD')
      .replace(/[\u0300-\u036f]/g, '')
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-+|-+$/g, '')
    const monthStr = String(preview.reference_month).padStart(2, '0')
    const bankSuffix = preview.bank_filter ? `-${preview.bank_filter}` : ''
    const link = document.createElement('a')
    link.href = url
    link.download = `recibo-${slug || 'pessoa'}-${preview.reference_year}-${monthStr}${bankSuffix}.html`
    document.body.appendChild(link)
    link.click()
    link.remove()
    URL.revokeObjectURL(url)
  }

  function handlePrintReceipt() {
    const win = iframeRef.current?.contentWindow
    if (!win) {
      return
    }
    win.focus()
    win.print()
  }

  async function handleCopyWhatsApp() {
    if (!preview) {
      return
    }
    try {
      await navigator.clipboard.writeText(preview.whatsapp_text)
      setCopyFeedback('Resumo copiado.')
      window.setTimeout(() => {
        setCopyFeedback('')
      }, 3000)
    } catch {
      setCopyFeedback('Não foi possível copiar para a área de transferência.')
    }
  }

  return (
    <div className="space-y-5">
      <div className="border-b border-[#ced4da] pb-3">
        <h1 className="text-lg font-semibold tracking-tight text-[#212529]">Recibos</h1>
        <p className="mt-0.5 text-xs font-medium text-[#495057]">
          Selecione pessoa, ano, banco e clique diretamente no mês com valor disponível.
        </p>
      </div>

      {errorMessage && (
        <div
          role="alert"
          className="glass-panel rounded-xl border-l-2 border-l-[#212529] px-4 py-2.5 text-sm font-medium text-[#212529]"
        >
          {errorMessage}
        </div>
      )}

      {statusMessage && (
        <div
          role="status"
          className="glass-panel rounded-xl px-4 py-2.5 text-sm font-medium text-[#212529]"
        >
          {statusMessage}
        </div>
      )}

      <form onSubmit={handleGeneratePreview} className="glass-panel rounded-xl px-4 py-4 space-y-4">
        <div className="flex flex-wrap items-center gap-3">
          <div className="min-w-[180px]">
            <label htmlFor="receipt-person" className="sr-only">
              Pessoa
            </label>
            <select
              id="receipt-person"
              value={personId}
              disabled={loadingInit}
              onChange={(e) => setPersonId(e.target.value ? Number(e.target.value) : '')}
              className="w-full h-9 px-3 rounded-xl text-sm font-medium bg-white/90 border border-[#ced4da] text-[#212529] focus:outline-none focus:border-[#495057]"
            >
              {people.length === 0 && <option value="">Nenhuma pessoa</option>}
              {people.map((person) => (
                <option key={person.id} value={person.id}>
                  {person.name}
                  {person.archived ? ' (Arquivada)' : ''}
                </option>
              ))}
            </select>
          </div>

          <div className="glass-switch inline-flex items-center gap-1 rounded-xl p-1">
            {yearOptions.map((y) => (
              <button
                key={y}
                type="button"
                onClick={() => setYear(y)}
                className={[
                  'px-3 py-1 rounded-lg text-xs font-semibold tabular-nums transition-all cursor-pointer',
                  year === y
                    ? 'bg-[#212529] text-[#f8f9fa] shadow-xs'
                    : 'text-[#343a40] hover:text-[#212529]',
                ].join(' ')}
              >
                {y}
              </button>
            ))}
          </div>

          <BankSwitcher
            value={bank}
            banks={availableBanks}
            includeAll
            allLabel="Todos os bancos"
            onChange={handleBankChange}
          />
        </div>

        <div className="space-y-1.5">
          <div className="flex items-center justify-between text-xs font-medium text-[#495057]">
            <span>Meses de {year} (clique em um mês para gerar o recibo)</span>
            {loadingAvailability && <span className="text-[#6c757d]">Consultando valores...</span>}
          </div>

          <div className="grid grid-cols-3 sm:grid-cols-4 lg:grid-cols-6 gap-2">
            {SHORT_MONTHS.map((label, idx) => {
              const m = idx + 1
              const info = availabilityByMonth.get(m)
              const hasItems = (info?.item_count ?? 0) > 0
              const active = month === m

              return (
                <button
                  key={m}
                  type="button"
                  onClick={() => handleSelectMonth(m)}
                  className={[
                    'rounded-xl px-2.5 py-2 text-left border transition-all cursor-pointer',
                    active
                      ? 'bg-[#212529] text-[#f8f9fa] border-[#212529] shadow-xs'
                      : hasItems
                        ? 'bg-white/90 text-[#212529] border-[#adb5bd] hover:bg-white'
                        : 'bg-[#e9ecef]/65 text-[#6c757d] border-[#dee2e6] hover:bg-[#e9ecef]',
                  ].join(' ')}
                >
                  <div className="flex items-baseline justify-between gap-1">
                    <span className="text-xs font-semibold">{label}</span>
                    {hasItems && (
                      <span
                        className={[
                          'text-[10px] font-medium tabular-nums',
                          active ? 'text-[#ced4da]' : 'text-[#6c757d]',
                        ].join(' ')}
                      >
                        {info?.item_count} {info?.item_count === 1 ? 'item' : 'itens'}
                      </span>
                    )}
                  </div>
                  <div
                    className={[
                      'mt-0.5 text-xs font-semibold tabular-nums truncate',
                      active
                        ? 'text-[#f8f9fa]'
                        : hasItems
                          ? 'text-[#212529]'
                          : 'text-[#6c757d] font-normal',
                    ].join(' ')}
                  >
                    {hasItems ? formatBRL(info?.total_cents ?? 0) : 'Sem gastos'}
                  </div>
                </button>
              )
            })}
          </div>
        </div>

        <div className="pt-1 flex flex-wrap items-center justify-between gap-3">
          <div>
            <label
              className={[
                'inline-flex items-center gap-2 text-sm font-medium select-none',
                bank !== '' ? 'text-[#6c757d] cursor-not-allowed' : 'text-[#212529] cursor-pointer',
              ].join(' ')}
            >
              <input
                type="checkbox"
                checked={bank === '' && includePriorBalance}
                disabled={bank !== ''}
                onChange={(e) => setIncludePriorBalance(e.target.checked)}
                className="border-[#ced4da]"
              />
              Incluir saldo devedor de meses anteriores
            </label>
            {bank !== '' && (
              <p className="text-xs text-[#6c757d] mt-0.5">
                Disponível apenas quando o filtro de banco está em "Todos os bancos".
              </p>
            )}
          </div>

          <button
            type="submit"
            disabled={loadingPreview || loadingInit || personId === ''}
            className="h-9 px-4 rounded-xl text-xs font-semibold text-[#f8f9fa] bg-[#212529] hover:bg-[#343a40] disabled:opacity-50 transition-colors cursor-pointer"
          >
            {loadingPreview ? 'Gerando...' : 'Atualizar pré-visualização'}
          </button>
        </div>
      </form>

      {preview && (
        <section className="glass-panel rounded-xl overflow-hidden">
          <div className="px-4 py-3 border-b border-[#dee2e6] flex flex-wrap items-baseline justify-between gap-2">
            <h2 className="text-sm font-semibold text-[#212529]">
              Pré-visualização — {preview.person_name} ({monthNamePT(preview.reference_month)}/
              {preview.reference_year}
              {preview.bank_filter ? `, ${bankLabel(preview.bank_filter)}` : ', Todos os bancos'})
            </h2>
            <span className="text-xs font-medium text-[#495057] tabular-nums">
              {(preview.items ?? []).length}{' '}
              {(preview.items ?? []).length === 1 ? 'lançamento' : 'lançamentos'}
            </span>
          </div>

          <div
            className={[
              'grid grid-cols-1 border-b border-[#dee2e6] divide-y sm:divide-y-0 sm:divide-x divide-[#dee2e6]',
              preview.bank_filter === ''
                ? preview.include_prior_balance
                  ? 'sm:grid-cols-4'
                  : 'sm:grid-cols-3'
                : 'sm:grid-cols-2',
            ].join(' ')}
          >
            <div className="px-4 py-3">
              <div className="text-xs font-medium text-[#6c757d]">Subtotal do mês</div>
              <div className="mt-1 text-base font-semibold tabular-nums text-[#212529]">
                {formatBRL(preview.subtotal_cents)}
              </div>
            </div>

            {preview.bank_filter === '' && (
              <div className="px-4 py-3">
                <div className="text-xs font-medium text-[#6c757d]">Pagamentos abatidos</div>
                <div className="mt-1 text-base font-semibold tabular-nums text-[#343a40]">
                  {formatBRL(preview.paid_cents)}
                </div>
              </div>
            )}

            {preview.bank_filter === '' && preview.include_prior_balance && (
              <div className="px-4 py-3">
                <div className="text-xs font-medium text-[#6c757d]">Saldo anterior</div>
                <div className="mt-1 text-base font-semibold tabular-nums text-[#343a40]">
                  {formatBRL(preview.prior_balance_cents)}
                </div>
              </div>
            )}

            <div className="px-4 py-3">
              <div className="text-xs font-medium text-[#6c757d]">
                {preview.bank_filter === '' ? 'Total a pagar' : 'Total do banco'}
              </div>
              <div className="mt-1 text-base font-semibold tabular-nums text-[#212529]">
                {formatBRL(preview.total_cents)}
              </div>
            </div>
          </div>

          <form
            onSubmit={handleSendEmail}
            className="px-4 py-3 border-b border-[#dee2e6] bg-[#f8f9fa]/70 space-y-3"
          >
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
              <div>
                <label
                  htmlFor="receipt-recipient"
                  className="block text-xs font-medium text-[#495057] mb-1"
                >
                  Destinatário
                </label>
                <input
                  id="receipt-recipient"
                  type="email"
                  value={recipientEmail}
                  onChange={(e) => setRecipientEmail(e.target.value)}
                  placeholder="destinatario@exemplo.com"
                  className="w-full h-9 px-2.5 rounded-lg text-sm bg-white border border-[#ced4da] text-[#212529] focus:outline-none focus:border-[#495057]"
                />
              </div>
              <div>
                <label
                  htmlFor="receipt-subject"
                  className="block text-xs font-medium text-[#495057] mb-1"
                >
                  Assunto
                </label>
                <input
                  id="receipt-subject"
                  type="text"
                  value={subject}
                  onChange={(e) => setSubject(e.target.value)}
                  className="w-full h-9 px-2.5 rounded-lg text-sm bg-white border border-[#ced4da] text-[#212529] focus:outline-none focus:border-[#495057]"
                />
              </div>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              <button
                type="submit"
                disabled={sending || !recipientEmail.trim()}
                className="h-9 px-4 rounded-xl text-xs font-semibold text-[#f8f9fa] bg-[#212529] hover:bg-[#343a40] disabled:opacity-50 transition-colors cursor-pointer"
              >
                {sending ? 'Enviando...' : 'Enviar por e-mail'}
              </button>
              <button
                type="button"
                onClick={handleDownloadHTML}
                className="h-9 px-3 rounded-xl text-xs font-medium text-[#212529] bg-white border border-[#ced4da] hover:bg-[#e9ecef] transition-colors cursor-pointer"
              >
                Baixar HTML
              </button>
              <button
                type="button"
                onClick={handlePrintReceipt}
                className="h-9 px-3 rounded-xl text-xs font-medium text-[#212529] bg-white border border-[#ced4da] hover:bg-[#e9ecef] transition-colors cursor-pointer"
              >
                Imprimir / Salvar PDF
              </button>
              <button
                type="button"
                onClick={() => void handleCopyWhatsApp()}
                className="h-9 px-3 rounded-xl text-xs font-medium text-[#212529] bg-white border border-[#ced4da] hover:bg-[#e9ecef] transition-colors cursor-pointer"
              >
                Copiar resumo (WhatsApp)
              </button>
              {copyFeedback && (
                <span role="status" className="text-xs font-medium text-[#343a40] ml-1">
                  {copyFeedback}
                </span>
              )}
            </div>
          </form>

          <div className="p-4 bg-[#f8f9fa]/60">
            <iframe
              ref={iframeRef}
              title="Pré-visualização do recibo"
              srcDoc={preview.html_body}
              className="w-full h-[520px] rounded-lg bg-white border border-[#dee2e6]"
            />
          </div>
        </section>
      )}

      <section className="glass-panel rounded-xl overflow-hidden">
        <div className="px-4 py-2.5 border-b border-[#dee2e6] flex items-center justify-between">
          <h2 className="text-sm font-semibold text-[#212529]">Histórico de recibos</h2>
          <span className="text-xs font-medium text-[#495057] tabular-nums">
            {history.length} registros
          </span>
        </div>

        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-[#dee2e6] bg-[#f8f9fa]/75 text-xs font-semibold text-[#495057]">
                <th className="text-left py-2.5 px-4">Data</th>
                <th className="text-left py-2.5 px-4">Pessoa</th>
                <th className="text-left py-2.5 px-4">Referência</th>
                <th className="text-left py-2.5 px-4">Escopo</th>
                <th className="text-left py-2.5 px-4">Destinatário</th>
                <th className="text-right py-2.5 px-4">Total</th>
                <th className="text-left py-2.5 px-4">Status</th>
                <th className="text-right py-2.5 px-4">Arquivo</th>
              </tr>
            </thead>
            <tbody>
              {loadingInit ? (
                <tr>
                  <td colSpan={8} className="py-6 px-4 text-center text-[#6c757d]">
                    Carregando histórico...
                  </td>
                </tr>
              ) : history.length === 0 ? (
                <tr>
                  <td colSpan={8} className="py-6 px-4 text-center text-[#6c757d]">
                    Nenhum recibo enviado ou salvo até o momento.
                  </td>
                </tr>
              ) : (
                history.map((item) => (
                  <tr key={item.id} className="border-b border-[#e9ecef] hover:bg-white/75">
                    <td className="py-2.5 px-4 tabular-nums text-[#343a40] whitespace-nowrap">
                      {formatDateTimeBR(item.sent_at || item.created_at)}
                    </td>
                    <td className="py-2.5 px-4 font-semibold text-[#212529] whitespace-nowrap">
                      {item.person_name}
                    </td>
                    <td className="py-2.5 px-4 text-[#343a40] whitespace-nowrap">
                      {monthNamePT(item.reference_month)}/{item.reference_year}
                    </td>
                    <td className="py-2.5 px-4 text-[#343a40] whitespace-nowrap">
                      {item.bank_filter ? bankLabel(item.bank_filter) : 'Todos os bancos'}
                      {item.include_prior_balance ? ' (+ saldo anterior)' : ''}
                    </td>
                    <td className="py-2.5 px-4 text-[#343a40]">{item.recipient_email || '—'}</td>
                    <td className="py-2.5 px-4 text-right tabular-nums font-semibold text-[#212529] whitespace-nowrap">
                      {formatBRL(item.total_cents)}
                    </td>
                    <td className="py-2.5 px-4">
                      <div
                        className={
                          item.delivery_status === 'failed'
                            ? 'text-[#212529] font-semibold'
                            : 'text-[#343a40] font-medium'
                        }
                      >
                        {deliveryStatusLabel(item.delivery_status)}
                      </div>
                      {item.error_message && (
                        <div className="text-xs text-[#6c757d] mt-0.5">{item.error_message}</div>
                      )}
                    </td>
                    <td className="py-2.5 px-4 text-right whitespace-nowrap">
                      <a
                        href={api.receiptHtmlUrl(item.id)}
                        target="_blank"
                        rel="noreferrer"
                        className="text-xs font-medium text-[#212529] underline underline-offset-2 hover:text-[#495057]"
                      >
                        Abrir HTML
                      </a>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  )
}
