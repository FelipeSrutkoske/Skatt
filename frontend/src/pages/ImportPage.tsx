import { useEffect, useMemo, useRef, useState } from 'react'
import type { DragEvent, FormEvent } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { BankSwitcher } from '../components/BankLogo'
import { PersonSwitcher } from '../components/PersonSwitcher'
import { ApiError, api } from '../lib/api'
import { bankLabel, formatBRL, monthNamePT, parseBRLInput } from '../lib/format'
import type {
  ConfirmImportTransactionInput,
  DuplicateWarning,
  Person,
  PreviewResult,
  PreviewTransaction,
  Statement,
} from '../types/api'

type ImportMode = 'file' | 'manual'
type ImportStep = 'upload' | 'needs_password' | 'needs_fallback' | 'preview'
type BankCode = 'nubank' | 'mercadopago' | 'inter' | 'itau' | 'outro'
type DuplicateAction = 'none' | 'additional' | 'replace'

interface EditableRow {
  rowId: string
  purchaseDate: string
  description: string
  amountInput: string
  personId: number | null
  suggestedPersonId: number | null
  isIgnored: boolean
}

const BANK_CODES: BankCode[] = ['nubank', 'mercadopago', 'inter', 'itau', 'outro']

const MONTH_NUMBERS = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12]

const ACCEPTED_EXTENSIONS = ['.csv', '.pdf', '.ofx']

function normalizeBankCode(raw: string, fallback: BankCode = 'outro'): BankCode {
  const normalized = raw.trim().toLowerCase().replace(/[\s_-]+/g, '')
  if (normalized === '') {
    return fallback
  }
  if (normalized.includes('nubank') || normalized.includes('nupagamentos')) {
    return 'nubank'
  }
  if (normalized.includes('mercadopago') || normalized.includes('mpago')) {
    return 'mercadopago'
  }
  if (normalized.includes('inter')) {
    return 'inter'
  }
  if (normalized.includes('itau') || normalized.includes('itaú')) {
    return 'itau'
  }
  return 'outro'
}

function formatCentsForInput(cents: number): string {
  const safe = Number.isFinite(cents) ? Math.round(cents) : 0
  const negative = safe < 0
  const abs = Math.abs(safe)
  const reais = Math.floor(abs / 100)
  const centavos = abs % 100
  const intStr = String(reais).replace(/\B(?=(\d{3})+(?!\d))/g, '.')
  const fracStr = String(centavos).padStart(2, '0')
  return `${negative ? '-' : ''}${intStr},${fracStr}`
}

function defaultDateForPeriod(year: number, month: number): string {
  const now = new Date()
  const safeYear = Number.isInteger(year) && year >= 2000 && year <= 2100 ? year : now.getFullYear()
  const safeMonth = Number.isInteger(month) && month >= 1 && month <= 12 ? month : now.getMonth() + 1
  const day =
    safeYear === now.getFullYear() && safeMonth === now.getMonth() + 1 ? now.getDate() : 1
  return `${String(safeYear).padStart(4, '0')}-${String(safeMonth).padStart(2, '0')}-${String(day).padStart(2, '0')}`
}

function isValidISODate(raw: string): boolean {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(raw.trim())
  if (!match) {
    return false
  }
  const year = Number(match[1])
  const month = Number(match[2])
  const day = Number(match[3])
  if (month < 1 || month > 12 || day < 1 || day > 31) {
    return false
  }
  const check = new Date(Date.UTC(year, month - 1, day))
  return (
    check.getUTCFullYear() === year &&
    check.getUTCMonth() === month - 1 &&
    check.getUTCDate() === day
  )
}

function hasAllowedExtension(filename: string): boolean {
  const lower = filename.trim().toLowerCase()
  return ACCEPTED_EXTENSIONS.some((ext) => lower.endsWith(ext))
}

function collectDuplicateCandidates(warning: DuplicateWarning | null): Statement[] {
  if (!warning) {
    return []
  }
  const list: Statement[] = []
  const seen = new Set<number>()
  if (warning.exact_file_match) {
    seen.add(warning.exact_file_match.id)
    list.push(warning.exact_file_match)
  }
  for (const item of warning.period_bank_matches ?? []) {
    if (!seen.has(item.id)) {
      seen.add(item.id)
      list.push(item)
    }
  }
  return list
}

function formatStatementSummary(st: Statement): string {
  const period = `${monthNamePT(st.reference_month)}/${st.reference_year}`
  const card = st.card_label ? ` — ${st.card_label}` : ''
  return `#${st.id} ${bankLabel(st.bank)}${card} (${period}, ${formatBRL(st.total_cents)}, ${st.transaction_count} lançamentos)`
}

function toEditableRow(tx: PreviewTransaction, rowId: string): EditableRow {
  const initialPersonId =
    tx.splits && tx.splits.length === 1
      ? tx.splits[0].person_id
      : tx.suggested_person_id ?? null

  return {
    rowId,
    purchaseDate: tx.purchase_date,
    description: tx.description,
    amountInput: formatCentsForInput(tx.amount_cents),
    personId: initialPersonId,
    suggestedPersonId: tx.suggested_person_id ?? null,
    isIgnored: Boolean(tx.is_ignored),
  }
}

function getErrorMessage(err: unknown, fallback: string): string {
  if (err instanceof ApiError) {
    return err.message || fallback
  }
  if (err instanceof Error && err.message) {
    return err.message
  }
  return fallback
}

export function ImportPage() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const fileInputRef = useRef<HTMLInputElement | null>(null)
  const rowCounterRef = useRef(0)

  const now = new Date()
  const paramYear = Number(searchParams.get('year'))
  const paramMonth = Number(searchParams.get('month'))
  const paramBank = searchParams.get('bank') ?? ''

  const defaultYear =
    Number.isInteger(paramYear) && paramYear >= 2000 && paramYear <= 2100
      ? paramYear
      : now.getFullYear()
  const defaultMonth =
    Number.isInteger(paramMonth) && paramMonth >= 1 && paramMonth <= 12
      ? paramMonth
      : now.getMonth() + 1
  const defaultBank = normalizeBankCode(paramBank, 'nubank')

  const nextRowId = () => {
    rowCounterRef.current += 1
    return `row-${rowCounterRef.current}`
  }

  const createBlankRow = (year: number, month: number): EditableRow => ({
    rowId: nextRowId(),
    purchaseDate: defaultDateForPeriod(year, month),
    description: '',
    amountInput: '',
    personId: null,
    suggestedPersonId: null,
    isIgnored: false,
  })

  const [mode, setMode] = useState<ImportMode>('file')
  const [step, setStep] = useState<ImportStep>('upload')
  const [people, setPeople] = useState<Person[]>([])
  const [peopleError, setPeopleError] = useState<string | null>(null)

  const [isDragging, setIsDragging] = useState(false)
  const [isBusy, setIsBusy] = useState(false)
  const [busyLabel, setBusyLabel] = useState('')
  const [errorMessage, setErrorMessage] = useState<string | null>(null)

  const [stagingId, setStagingId] = useState('')
  const [originalFilename, setOriginalFilename] = useState('')
  const [fileFormat, setFileFormat] = useState('')
  const [parserUsed, setParserUsed] = useState('')
  const [fallbackReason, setFallbackReason] = useState('')
  const [hasExtractableText, setHasExtractableText] = useState(false)
  const [textPreview, setTextPreview] = useState('')

  const [pdfPassword, setPdfPassword] = useState('')
  const [passwordError, setPasswordError] = useState<string | null>(null)

  const [bank, setBank] = useState<BankCode>(defaultBank)
  const [referenceYear, setReferenceYear] = useState<number>(defaultYear)
  const [referenceMonth, setReferenceMonth] = useState<number>(defaultMonth)
  const [cardLabel, setCardLabel] = useState('')
  const [declaredTotalCents, setDeclaredTotalCents] = useState<number | null>(null)
  const [duplicateWarning, setDuplicateWarning] = useState<DuplicateWarning | null>(null)
  const [duplicateAction, setDuplicateAction] = useState<DuplicateAction>('none')
  const [replaceStatementId, setReplaceStatementId] = useState<number | null>(null)
  const [rows, setRows] = useState<EditableRow[]>([])

  useEffect(() => {
    let active = true
    api
      .listPeople(false)
      .then((list) => {
        if (active) {
          setPeople(list)
          setPeopleError(null)
        }
      })
      .catch((err) => {
        if (active) {
          setPeopleError(getErrorMessage(err, 'Não foi possível carregar a lista de pessoas.'))
        }
      })
    return () => {
      active = false
    }
  }, [])

  const duplicateCandidates = useMemo(
    () => collectDuplicateCandidates(duplicateWarning),
    [duplicateWarning],
  )
  const hasDuplicateWarning = duplicateCandidates.length > 0

  const totals = useMemo(() => {
    let computedCents = 0
    let activeCount = 0
    let ignoredCount = 0
    let invalidAmountCount = 0

    for (const row of rows) {
      const parsed = parseBRLInput(row.amountInput)
      if (row.isIgnored) {
        ignoredCount += 1
      } else {
        activeCount += 1
        if (parsed === null) {
          invalidAmountCount += 1
        } else {
          computedCents += parsed
        }
      }
    }

    return {
      computedCents,
      activeCount,
      ignoredCount,
      invalidAmountCount,
    }
  }, [rows])

  const resetFileFlow = () => {
    setMode('file')
    setStep('upload')
    setErrorMessage(null)
    setPasswordError(null)
    setPdfPassword('')
    setStagingId('')
    setOriginalFilename('')
    setFileFormat('')
    setParserUsed('')
    setFallbackReason('')
    setHasExtractableText(false)
    setTextPreview('')
    setDeclaredTotalCents(null)
    setDuplicateWarning(null)
    setDuplicateAction('none')
    setReplaceStatementId(null)
    setCardLabel('')
    setRows([])
  }

  const openManualMode = () => {
    setMode('manual')
    setStep('preview')
    setErrorMessage(null)
    setPasswordError(null)
    setPdfPassword('')
    setStagingId('')
    setOriginalFilename('')
    setFileFormat('manual')
    setParserUsed('manual')
    setFallbackReason('')
    setHasExtractableText(false)
    setTextPreview('')
    setBank(defaultBank)
    setReferenceYear(defaultYear)
    setReferenceMonth(defaultMonth)
    setCardLabel('')
    setDeclaredTotalCents(null)
    setDuplicateWarning(null)
    setDuplicateAction('none')
    setReplaceStatementId(null)
    setRows([createBlankRow(defaultYear, defaultMonth)])
  }

  const applyPreviewResult = (result: PreviewResult) => {
    setStagingId(result.staging_id ?? '')
    setOriginalFilename(result.original_filename ?? '')
    setFileFormat(result.file_format ?? '')
    setParserUsed(result.parser_used ?? '')
    setFallbackReason(result.reason ?? '')
    setHasExtractableText(Boolean(result.has_extractable_text))
    setTextPreview(result.text_preview ?? '')
    setDeclaredTotalCents(result.declared_total_cents ?? null)
    setDuplicateWarning(result.duplicate_warning ?? null)

    const candidates = collectDuplicateCandidates(result.duplicate_warning ?? null)
    setDuplicateAction('none')
    setReplaceStatementId(candidates.length > 0 ? candidates[0].id : null)

    const validYear =
      result.reference_year >= 2000 && result.reference_year <= 2100
        ? result.reference_year
        : defaultYear
    const validMonth =
      result.reference_month >= 1 && result.reference_month <= 12
        ? result.reference_month
        : defaultMonth

    setBank(normalizeBankCode(result.bank ?? '', defaultBank))
    setReferenceYear(validYear)
    setReferenceMonth(validMonth)
    setCardLabel('')

    if (result.status === 'needs_password') {
      setPasswordError(null)
      setPdfPassword('')
      setStep('needs_password')
      return
    }

    if (result.status === 'needs_fallback') {
      setStep('needs_fallback')
      return
    }

    const mappedRows = (result.transactions ?? []).map((tx) => toEditableRow(tx, nextRowId()))
    setRows(mappedRows)
    setStep('preview')
  }

  const handleFileUpload = async (file: File) => {
    if (!hasAllowedExtension(file.name)) {
      setErrorMessage('Formato não suportado. Envie um arquivo .csv, .pdf ou .ofx.')
      return
    }

    setErrorMessage(null)
    setPasswordError(null)
    setIsBusy(true)
    setBusyLabel('Analisando arquivo com parsers locais...')
    try {
      const result = await api.previewImport(file)
      applyPreviewResult(result)
    } catch (err) {
      setErrorMessage(getErrorMessage(err, 'Falha ao processar o arquivo enviado.'))
    } finally {
      setIsBusy(false)
      setBusyLabel('')
    }
  }

  const handleFileInputChange = (files: FileList | null) => {
    if (!files || files.length === 0) {
      return
    }
    const file = files[0]
    if (fileInputRef.current) {
      fileInputRef.current.value = ''
    }
    void handleFileUpload(file)
  }

  const handleDrop = (event: DragEvent<HTMLDivElement>) => {
    event.preventDefault()
    setIsDragging(false)
    if (isBusy) {
      return
    }
    const file = event.dataTransfer.files?.[0]
    if (file) {
      void handleFileUpload(file)
    }
  }

  const handleUnlockSubmit = async (event: FormEvent) => {
    event.preventDefault()
    if (!stagingId || !pdfPassword) {
      setPasswordError('Informe a senha do PDF para continuar.')
      return
    }

    setPasswordError(null)
    setErrorMessage(null)
    setIsBusy(true)
    setBusyLabel('Desbloqueando PDF em memória...')
    try {
      const result = await api.unlockImport(stagingId, pdfPassword)
      setPdfPassword('')
      applyPreviewResult(result)
    } catch (err) {
      if (err instanceof ApiError && err.status === 422) {
        setPasswordError(
          err.message && err.message !== 'invalid_password'
            ? err.message
            : 'Senha incorreta. Verifique a senha do PDF e tente novamente.',
        )
      } else {
        setPasswordError(getErrorMessage(err, 'Não foi possível desbloquear o PDF.'))
      }
    } finally {
      setIsBusy(false)
      setBusyLabel('')
    }
  }

  const handleAiExtract = async () => {
    if (!stagingId || !hasExtractableText || isBusy) {
      return
    }
    setErrorMessage(null)
    setIsBusy(true)
    setBusyLabel('Extraindo lançamentos com IA sobre texto mascarado...')
    try {
      const result = await api.aiExtractImport(stagingId)
      applyPreviewResult(result)
    } catch (err) {
      setErrorMessage(
        getErrorMessage(
          err,
          'Não foi possível extrair os dados com IA. Verifique a chave de API em Configurações.',
        ),
      )
    } finally {
      setIsBusy(false)
      setBusyLabel('')
    }
  }

  const handleFallbackManual = () => {
    setErrorMessage(null)
    setParserUsed('manual')
    setRows([createBlankRow(referenceYear, referenceMonth)])
    setStep('preview')
  }

  const updateRow = (rowId: string, patch: Partial<EditableRow>) => {
    setRows((prev) => prev.map((row) => (row.rowId === rowId ? { ...row, ...patch } : row)))
  }

  const handleAmountBlur = (rowId: string, raw: string) => {
    const parsed = parseBRLInput(raw)
    if (parsed !== null) {
      updateRow(rowId, { amountInput: formatCentsForInput(parsed) })
    }
  }

  const handleRemoveRow = (rowId: string) => {
    setRows((prev) => prev.filter((row) => row.rowId !== rowId))
  }

  const handleAddRow = () => {
    setRows((prev) => [...prev, createBlankRow(referenceYear, referenceMonth)])
  }

  const handleConfirmImport = async () => {
    setErrorMessage(null)

    if (!Number.isInteger(referenceYear) || referenceYear < 2000 || referenceYear > 2100) {
      setErrorMessage('Informe um ano de referência válido (entre 2000 e 2100).')
      return
    }
    if (!Number.isInteger(referenceMonth) || referenceMonth < 1 || referenceMonth > 12) {
      setErrorMessage('Informe um mês de referência válido (1 a 12).')
      return
    }
    if (hasDuplicateWarning && duplicateAction === 'none') {
      setErrorMessage(
        'Há uma fatura duplicada detectada. Escolha entre importar como cartão adicional, substituir a fatura anterior ou cancelar.',
      )
      return
    }
    if (duplicateAction === 'replace' && replaceStatementId === null) {
      setErrorMessage('Selecione qual fatura anterior deseja substituir.')
      return
    }
    if (rows.length === 0) {
      setErrorMessage('Adicione pelo menos um lançamento antes de confirmar a importação.')
      return
    }

    const transactions: ConfirmImportTransactionInput[] = []
    for (let i = 0; i < rows.length; i += 1) {
      const row = rows[i]
      const lineNum = i + 1
      if (!isValidISODate(row.purchaseDate)) {
        setErrorMessage(`Linha ${lineNum}: informe uma data válida no formato AAAA-MM-DD.`)
        return
      }
      const description = row.description.trim()
      if (!description) {
        setErrorMessage(`Linha ${lineNum}: informe a descrição do lançamento.`)
        return
      }
      const amountCents = parseBRLInput(row.amountInput)
      if (amountCents === null) {
        setErrorMessage(`Linha ${lineNum}: informe um valor em R$ válido (ex: 120,50 ou -45,90).`)
        return
      }

      transactions.push({
        purchase_date: row.purchaseDate.trim(),
        description,
        amount_cents: amountCents,
        is_ignored: row.isIgnored,
        splits:
          !row.isIgnored && row.personId !== null
            ? [{ person_id: row.personId, amount_cents: amountCents }]
            : [],
      })
    }

    setIsBusy(true)
    setBusyLabel('Salvando fatura...')
    try {
      const created = await api.confirmImport({
        staging_id: stagingId,
        bank,
        card_label: cardLabel.trim(),
        reference_year: referenceYear,
        reference_month: referenceMonth,
        declared_total_cents: declaredTotalCents,
        parser_used: parserUsed || 'manual',
        replace_statement_id: duplicateAction === 'replace' ? replaceStatementId : null,
        transactions,
      })
      navigate(`/statements/${created.id}`)
    } catch (err) {
      setErrorMessage(getErrorMessage(err, 'Erro ao confirmar a importação da fatura.'))
    } finally {
      setIsBusy(false)
      setBusyLabel('')
    }
  }

  const totalDiffCents =
    declaredTotalCents !== null ? totals.computedCents - declaredTotalCents : null

  return (
    <div className="space-y-6">
      <div className="border-b border-[#dee2e6] pb-4 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold tracking-tight text-[#212529]">Importar fatura</h1>
          <p className="mt-1 text-xs text-[#6c757d]">
            Envie um arquivo de fatura para leitura local ou registre os lançamentos manualmente.
          </p>
        </div>

        <div
          role="tablist"
          aria-label="Modo de importação"
          className="glass-switch inline-flex rounded-xl p-1"
        >
          <button
            type="button"
            role="tab"
            aria-selected={mode === 'file'}
            disabled={isBusy}
            onClick={resetFileFlow}
            className={[
              'px-3.5 py-1.5 rounded-lg text-xs transition-all cursor-pointer disabled:opacity-50',
              mode === 'file'
                ? 'bg-[#212529] text-[#f8f9fa] font-medium shadow-xs'
                : 'text-[#495057] hover:text-[#212529]',
            ].join(' ')}
          >
            Importar arquivo (.csv, .pdf, .ofx)
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={mode === 'manual'}
            disabled={isBusy}
            onClick={openManualMode}
            className={[
              'px-3.5 py-1.5 rounded-lg text-xs transition-all cursor-pointer disabled:opacity-50',
              mode === 'manual'
                ? 'bg-[#212529] text-[#f8f9fa] font-medium shadow-xs'
                : 'text-[#495057] hover:text-[#212529]',
            ].join(' ')}
          >
            Criar fatura manual
          </button>
        </div>
      </div>

      {errorMessage && (
        <div
          role="alert"
          className="glass-panel rounded-xl border-l-2 border-l-[#212529] px-4 py-3 text-sm text-[#212529]"
        >
          {errorMessage}
        </div>
      )}

      {peopleError && (
        <div role="status" className="glass-panel rounded-xl px-4 py-2.5 text-xs text-[#6c757d]">
          {peopleError}
        </div>
      )}

      {step === 'upload' && (
        <section className="glass-panel rounded-xl p-6">
          <input
            ref={fileInputRef}
            type="file"
            accept=".csv,.pdf,.ofx"
            className="hidden"
            onChange={(e) => handleFileInputChange(e.target.files)}
          />

          <div
            onDragOver={(e) => {
              e.preventDefault()
              if (!isBusy) {
                setIsDragging(true)
              }
            }}
            onDragLeave={() => setIsDragging(false)}
            onDrop={handleDrop}
            onClick={() => {
              if (!isBusy) {
                fileInputRef.current?.click()
              }
            }}
            role="button"
            tabIndex={0}
            onKeyDown={(e) => {
              if (!isBusy && (e.key === 'Enter' || e.key === ' ')) {
                e.preventDefault()
                fileInputRef.current?.click()
              }
            }}
            className={[
              'rounded-xl border border-dashed px-6 py-12 text-center transition-colors cursor-pointer',
              isDragging
                ? 'border-[#212529] bg-[#e9ecef]/80'
                : 'border-[#adb5bd] bg-[#f8f9fa]/70 hover:border-[#495057] hover:bg-white/80',
              isBusy ? 'opacity-60 pointer-events-none' : '',
            ].join(' ')}
          >
            <p className="text-sm font-medium text-[#212529]">
              {isBusy ? busyLabel : 'Arraste o arquivo da fatura aqui ou clique para selecionar'}
            </p>
            <p className="mt-2 text-xs text-[#6c757d]">
              Formatos aceitos: .csv (Nubank, Inter, Itaú), .pdf (Nubank, Mercado Pago, Inter, Itaú
              — inclusive com senha) e .ofx.
            </p>
            <div className="mt-4">
              <span className="inline-block px-4 py-1.5 rounded-lg text-xs font-medium text-[#212529] bg-white border border-[#ced4da]">
                Selecionar arquivo
              </span>
            </div>
          </div>

          <p className="mt-4 text-xs text-[#6c757d]">
            O processamento inicial é 100% local. Nenhum arquivo é enviado para serviços externos
            sem a sua autorização explícita.
          </p>
        </section>
      )}

      {step === 'needs_password' && (
        <section className="glass-panel rounded-xl p-6 max-w-xl">
          <h2 className="text-base font-semibold text-[#212529]">PDF protegido por senha</h2>
          <p className="mt-1 text-sm text-[#495057]">
            Arquivo: <span className="font-medium text-[#212529]">{originalFilename}</span>
          </p>
          <p className="mt-2 text-xs text-[#495057] border-l-2 border-[#adb5bd] pl-3">
            A senha é usada apenas em memória para leitura e não é salva.
          </p>

          {passwordError && (
            <div
              role="alert"
              className="mt-4 rounded-lg border border-[#ced4da] border-l-2 border-l-[#212529] bg-white/80 px-3.5 py-2.5 text-sm text-[#212529]"
            >
              {passwordError}
            </div>
          )}

          <form onSubmit={handleUnlockSubmit} className="mt-4 space-y-4">
            <div>
              <label
                htmlFor="pdf-password"
                className="block text-xs font-medium text-[#495057] mb-1"
              >
                Senha do PDF
              </label>
              <input
                id="pdf-password"
                type="password"
                autoFocus
                autoComplete="off"
                value={pdfPassword}
                onChange={(e) => setPdfPassword(e.target.value)}
                disabled={isBusy}
                placeholder="Digite a senha para desbloquear"
                className="w-full rounded-lg border border-[#ced4da] bg-white px-3 py-2 text-sm text-[#212529] focus:outline-none focus:border-[#495057]"
              />
            </div>

            <div className="flex items-center gap-3">
              <button
                type="submit"
                disabled={isBusy}
                className="px-4 py-2 rounded-xl text-sm font-medium bg-[#212529] text-[#f8f9fa] hover:bg-[#343a40] disabled:opacity-50 cursor-pointer"
              >
                {isBusy ? busyLabel || 'Desbloqueando...' : 'Desbloquear PDF'}
              </button>
              <button
                type="button"
                disabled={isBusy}
                onClick={resetFileFlow}
                className="px-3.5 py-2 rounded-xl text-sm text-[#495057] bg-white border border-[#ced4da] hover:text-[#212529] disabled:opacity-50 cursor-pointer"
              >
                Cancelar
              </button>
            </div>
          </form>
        </section>
      )}

      {step === 'needs_fallback' && (
        <section className="glass-panel rounded-xl p-6 space-y-5">
          <div>
            <h2 className="text-base font-semibold text-[#212529]">
              Formato não reconhecido pelos parsers locais
            </h2>
            <p className="mt-1 text-sm text-[#495057]">
              Arquivo: <span className="font-medium text-[#212529]">{originalFilename}</span>
              {fileFormat ? ` (${fileFormat.toUpperCase()})` : ''}
            </p>
            <p className="mt-2 text-sm text-[#495057]">
              {fallbackReason
                ? `Motivo: ${fallbackReason}`
                : 'Os parsers locais não conseguiram identificar automaticamente os lançamentos deste arquivo.'}
            </p>
            <p className="mt-1.5 text-xs text-[#6c757d]">
              Nenhuma informação foi enviada para serviços externos. Escolha abaixo como deseja
              prosseguir.
            </p>
          </div>

          {!hasExtractableText && (
            <div
              role="status"
              className="rounded-lg border border-[#ced4da] border-l-2 border-l-[#212529] bg-[#f8f9fa]/80 px-4 py-3 text-sm text-[#212529]"
            >
              Este PDF parece ser uma imagem escaneada sem camada de texto extraível. A extração
              com IA está indisponível para este arquivo; utilize a opção Preencher manualmente para
              digitar os lançamentos mantendo o arquivo original arquivado.
            </div>
          )}

          {hasExtractableText && (
            <p className="text-xs text-[#495057] border-l-2 border-[#adb5bd] pl-3">
              Ao optar pela extração com IA, CPF, números de cartão e dados de endereço/CEP são
              mascarados localmente antes do envio. O resultado volta para a tela de revisão antes
              de ser salvo.
            </p>
          )}

          {textPreview.trim() !== '' && (
            <details className="border-t border-[#dee2e6] pt-3">
              <summary className="text-xs font-medium text-[#495057] cursor-pointer select-none hover:text-[#212529]">
                Ver trecho do texto extraído localmente
              </summary>
              <pre className="mt-2 max-h-48 overflow-auto rounded-lg bg-[#f8f9fa] border border-[#dee2e6] p-3 text-xs text-[#343a40] whitespace-pre-wrap font-mono">
                {textPreview}
              </pre>
            </details>
          )}

          <div className="flex flex-wrap items-center gap-3 pt-1">
            <button
              type="button"
              disabled={!hasExtractableText || isBusy}
              onClick={() => void handleAiExtract()}
              className="px-4 py-2 rounded-xl text-sm font-medium bg-[#212529] text-[#f8f9fa] hover:bg-[#343a40] disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer"
            >
              {isBusy ? busyLabel || 'Extraindo...' : 'Extrair com IA (envia apenas texto mascarado)'}
            </button>
            <button
              type="button"
              disabled={isBusy}
              onClick={handleFallbackManual}
              className="px-4 py-2 rounded-xl text-sm font-medium text-[#212529] bg-white border border-[#ced4da] hover:bg-[#e9ecef] disabled:opacity-50 cursor-pointer"
            >
              Preencher manualmente
            </button>
            <button
              type="button"
              disabled={isBusy}
              onClick={resetFileFlow}
              className="px-3.5 py-2 text-sm text-[#495057] hover:text-[#212529] disabled:opacity-50 cursor-pointer"
            >
              Enviar outro arquivo
            </button>
          </div>
        </section>
      )}

      {step === 'preview' && (
        <div className="space-y-5">
          <div className="flex flex-wrap items-center justify-between gap-3 text-xs text-[#6c757d]">
            <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
              <span>
                Origem:{' '}
                <strong className="font-medium text-[#212529]">
                  {originalFilename || 'Fatura manual (sem arquivo)'}
                </strong>
              </span>
              <span>
                Formato:{' '}
                <strong className="font-medium text-[#212529]">{fileFormat || 'manual'}</strong>
              </span>
              <span>
                Parser:{' '}
                <strong className="font-medium text-[#212529]">{parserUsed || 'manual'}</strong>
              </span>
            </div>

            {mode === 'file' && (
              <button
                type="button"
                disabled={isBusy}
                onClick={resetFileFlow}
                className="text-xs text-[#495057] underline underline-offset-2 hover:text-[#212529] cursor-pointer"
              >
                Escolher outro arquivo
              </button>
            )}
          </div>

          {hasDuplicateWarning && (
            <section
              role="alert"
              className="glass-panel rounded-xl border-l-2 border-l-[#212529] p-4 space-y-3"
            >
              <div>
                <h2 className="text-sm font-semibold text-[#212529]">
                  Possível fatura duplicada detectada
                </h2>
                {duplicateWarning?.exact_file_match && (
                  <p className="mt-1 text-sm text-[#495057]">
                    Este mesmo arquivo já foi importado anteriormente:{' '}
                    <span className="font-medium text-[#212529] tabular-nums">
                      {formatStatementSummary(duplicateWarning.exact_file_match)}
                    </span>
                    .
                  </p>
                )}
                {duplicateWarning && duplicateWarning.period_bank_matches.length > 0 && (
                  <p className="mt-1 text-sm text-[#495057]">
                    Já existe fatura para este banco e período:{' '}
                    <span className="font-medium text-[#212529] tabular-nums">
                      {duplicateWarning.period_bank_matches
                        .map((item) => formatStatementSummary(item))
                        .join('; ')}
                    </span>
                    .
                  </p>
                )}
              </div>

              <div className="flex flex-wrap items-center gap-3 pt-1">
                <label className="inline-flex items-center gap-2 text-sm text-[#212529] cursor-pointer">
                  <input
                    type="radio"
                    name="duplicate-action"
                    checked={duplicateAction === 'additional'}
                    onChange={() => setDuplicateAction('additional')}
                  />
                  <span>Importar como cartão adicional</span>
                </label>

                <label className="inline-flex items-center gap-2 text-sm text-[#212529] cursor-pointer">
                  <input
                    type="radio"
                    name="duplicate-action"
                    checked={duplicateAction === 'replace'}
                    onChange={() => {
                      setDuplicateAction('replace')
                      if (replaceStatementId === null && duplicateCandidates.length > 0) {
                        setReplaceStatementId(duplicateCandidates[0].id)
                      }
                    }}
                  />
                  <span>Substituir fatura anterior</span>
                </label>

                <button
                  type="button"
                  onClick={resetFileFlow}
                  className="ml-auto px-3 py-1 rounded-lg text-xs text-[#495057] bg-white border border-[#ced4da] hover:text-[#212529] cursor-pointer"
                >
                  Cancelar
                </button>
              </div>

              {duplicateAction === 'additional' && (
                <p className="text-xs text-[#495057]">
                  Preencha o campo Rótulo do cartão abaixo para diferenciar esta fatura das demais
                  do mesmo período.
                </p>
              )}

              {duplicateAction === 'replace' && (
                <div className="space-y-2 pt-2 border-t border-[#dee2e6]">
                  <div className="flex flex-wrap items-center gap-3">
                    <label
                      htmlFor="replace-statement-select"
                      className="text-xs font-medium text-[#495057]"
                    >
                      Fatura a substituir:
                    </label>
                    <select
                      id="replace-statement-select"
                      value={replaceStatementId ?? ''}
                      onChange={(e) =>
                        setReplaceStatementId(e.target.value ? Number(e.target.value) : null)
                      }
                      className="rounded-lg border border-[#ced4da] bg-white px-2.5 py-1 text-xs text-[#212529] tabular-nums"
                    >
                      {duplicateCandidates.map((item) => (
                        <option key={item.id} value={item.id}>
                          {formatStatementSummary(item)}
                        </option>
                      ))}
                    </select>
                  </div>
                  <p className="text-xs font-medium text-[#212529]">
                    Aviso: a substituição remove a fatura anterior e todos os splits manuais já
                    configurados nela. A nova importação manterá apenas as sugestões aprendidas.
                  </p>
                </div>
              )}
            </section>
          )}

          <section className="glass-panel rounded-xl p-4 space-y-4">
            <div>
              <div className="text-xs font-medium text-[#495057] mb-1.5">Banco</div>
              <BankSwitcher
                value={bank}
                banks={BANK_CODES}
                includeAll={false}
                disabled={isBusy}
                onChange={(next) => setBank(next as BankCode)}
              />
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
              <div>
                <label
                  htmlFor="import-year"
                  className="block text-xs font-medium text-[#495057] mb-1"
                >
                  Ano
                </label>
                <input
                  id="import-year"
                  type="number"
                  min={2000}
                  max={2100}
                  value={referenceYear}
                  onChange={(e) => setReferenceYear(Number(e.target.value))}
                  disabled={isBusy}
                  className="w-full rounded-lg border border-[#ced4da] bg-white px-2.5 py-1.5 text-sm text-[#212529] tabular-nums focus:outline-none focus:border-[#495057]"
                />
              </div>

              <div>
                <label
                  htmlFor="import-month"
                  className="block text-xs font-medium text-[#495057] mb-1"
                >
                  Mês
                </label>
                <select
                  id="import-month"
                  value={referenceMonth}
                  onChange={(e) => setReferenceMonth(Number(e.target.value))}
                  disabled={isBusy}
                  className="w-full rounded-lg border border-[#ced4da] bg-white px-2.5 py-1.5 text-sm text-[#212529] focus:outline-none focus:border-[#495057]"
                >
                  {MONTH_NUMBERS.map((m) => (
                    <option key={m} value={m}>
                      {String(m).padStart(2, '0')} — {monthNamePT(m)}
                    </option>
                  ))}
                </select>
              </div>

              <div>
                <label
                  htmlFor="import-card-label"
                  className="block text-xs font-medium text-[#495057] mb-1"
                >
                  Rótulo do cartão (opcional)
                </label>
                <input
                  id="import-card-label"
                  type="text"
                  value={cardLabel}
                  onChange={(e) => setCardLabel(e.target.value)}
                  disabled={isBusy}
                  placeholder="Ex: Final 1234, Virtual"
                  className="w-full rounded-lg border border-[#ced4da] bg-white px-2.5 py-1.5 text-sm text-[#212529] focus:outline-none focus:border-[#495057]"
                />
              </div>
            </div>
          </section>

          <section className="glass-panel rounded-xl px-4 py-3 flex flex-wrap items-center justify-between gap-4">
            <div className="flex flex-wrap items-baseline gap-x-8 gap-y-2">
              <div>
                <div className="text-xs text-[#6c757d]">Total calculado</div>
                <div className="text-base font-semibold text-[#212529] tabular-nums">
                  {formatBRL(totals.computedCents)}
                </div>
              </div>

              {declaredTotalCents !== null && (
                <div>
                  <div className="text-xs text-[#6c757d]">Total declarado da fatura</div>
                  <div className="text-base font-semibold text-[#212529] tabular-nums">
                    {formatBRL(declaredTotalCents)}
                  </div>
                </div>
              )}

              {totalDiffCents !== null && (
                <div>
                  <div className="text-xs text-[#6c757d]">Conferência</div>
                  <div
                    className={[
                      'text-sm font-medium tabular-nums',
                      totalDiffCents === 0 ? 'text-[#212529]' : 'text-[#212529] underline',
                    ].join(' ')}
                  >
                    {totalDiffCents === 0
                      ? 'Conferido (diferença R$ 0,00)'
                      : `Divergência de ${formatBRL(totalDiffCents)}`}
                  </div>
                </div>
              )}
            </div>

            <div className="text-xs text-[#6c757d] tabular-nums">
              {totals.activeCount}{' '}
              {totals.activeCount === 1 ? 'lançamento ativo' : 'lançamentos ativos'}
              {totals.ignoredCount > 0 ? ` · ${totals.ignoredCount} ignorado(s)` : ''}
              {totals.invalidAmountCount > 0
                ? ` · ${totals.invalidAmountCount} valor(es) inválido(s)`
                : ''}
            </div>
          </section>

          <section className="glass-panel rounded-xl overflow-hidden">
            <div className="px-4 py-3 border-b border-[#dee2e6] flex flex-wrap items-center justify-between gap-3">
              <h2 className="text-sm font-semibold text-[#212529]">
                Lançamentos da fatura ({rows.length})
              </h2>
              <button
                type="button"
                disabled={isBusy}
                onClick={handleAddRow}
                className="px-3 py-1 rounded-lg text-xs font-medium text-[#212529] bg-white border border-[#ced4da] hover:bg-[#e9ecef] disabled:opacity-50 cursor-pointer"
              >
                Adicionar lançamento
              </button>
            </div>

            <div className="overflow-x-auto">
              <table className="w-full text-left border-collapse">
                <thead>
                  <tr className="border-b border-[#dee2e6] bg-[#f8f9fa]/75 text-xs font-medium text-[#6c757d]">
                    <th className="py-2.5 px-3 w-36">Data</th>
                    <th className="py-2.5 px-3">Descrição</th>
                    <th className="py-2.5 px-3 w-32 text-right">Valor (R$)</th>
                    <th className="py-2.5 px-3">Responsável (1 clique)</th>
                    <th className="py-2.5 px-3 w-16 text-center">Ignorar</th>
                    <th className="py-2.5 px-3 w-20 text-right">Ação</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-[#e9ecef] text-sm">
                  {rows.length === 0 ? (
                    <tr>
                      <td colSpan={6} className="py-8 px-4 text-center text-sm text-[#6c757d]">
                        Nenhum lançamento na pré-visualização. Clique em Adicionar lançamento para
                        incluir itens.
                      </td>
                    </tr>
                  ) : (
                    rows.map((row) => {
                      const amountInvalid =
                        row.amountInput.trim() !== '' && parseBRLInput(row.amountInput) === null
                      const dateInvalid =
                        row.purchaseDate.trim() !== '' && !isValidISODate(row.purchaseDate)

                      return (
                        <tr
                          key={row.rowId}
                          className={row.isIgnored ? 'bg-[#e9ecef]/45 text-[#adb5bd]' : ''}
                        >
                          <td className="py-1.5 px-3 align-middle">
                            <input
                              type="date"
                              value={row.purchaseDate}
                              onChange={(e) =>
                                updateRow(row.rowId, { purchaseDate: e.target.value })
                              }
                              disabled={isBusy}
                              aria-label="Data do lançamento"
                              className={[
                                'w-full rounded-lg border px-2 py-1 text-xs tabular-nums bg-white text-[#212529] focus:outline-none focus:border-[#495057]',
                                dateInvalid ? 'border-[#212529]' : 'border-[#ced4da]',
                                row.isIgnored ? 'opacity-60' : '',
                              ].join(' ')}
                            />
                          </td>

                          <td className="py-1.5 px-3 align-middle">
                            <input
                              type="text"
                              value={row.description}
                              onChange={(e) =>
                                updateRow(row.rowId, { description: e.target.value })
                              }
                              disabled={isBusy}
                              placeholder="Descrição da compra ou estorno"
                              aria-label="Descrição do lançamento"
                              className={[
                                'w-full rounded-lg border border-[#ced4da] px-2 py-1 text-sm bg-white text-[#212529] focus:outline-none focus:border-[#495057]',
                                row.isIgnored ? 'line-through opacity-60' : '',
                              ].join(' ')}
                            />
                          </td>

                          <td className="py-1.5 px-3 align-middle">
                            <input
                              type="text"
                              inputMode="decimal"
                              value={row.amountInput}
                              onChange={(e) =>
                                updateRow(row.rowId, { amountInput: e.target.value })
                              }
                              onBlur={(e) => handleAmountBlur(row.rowId, e.target.value)}
                              disabled={isBusy}
                              placeholder="0,00"
                              aria-label="Valor em reais"
                              className={[
                                'w-full rounded-lg border px-2 py-1 text-sm text-right tabular-nums bg-white text-[#212529] focus:outline-none focus:border-[#495057]',
                                amountInvalid ? 'border-[#212529] bg-[#e9ecef]' : 'border-[#ced4da]',
                                row.isIgnored ? 'line-through opacity-60' : '',
                              ].join(' ')}
                            />
                          </td>

                          <td className="py-1.5 px-3 align-middle">
                            <PersonSwitcher
                              people={people}
                              selectedPersonId={row.personId}
                              suggestedPersonId={row.suggestedPersonId}
                              disabled={isBusy || row.isIgnored}
                              onSelect={(pid) => updateRow(row.rowId, { personId: pid })}
                            />
                          </td>

                          <td className="py-1.5 px-3 align-middle text-center">
                            <input
                              type="checkbox"
                              checked={row.isIgnored}
                              onChange={(e) =>
                                updateRow(row.rowId, { isIgnored: e.target.checked })
                              }
                              disabled={isBusy}
                              aria-label="Ignorar lançamento"
                              className="cursor-pointer"
                            />
                          </td>

                          <td className="py-1.5 px-3 align-middle text-right">
                            <button
                              type="button"
                              disabled={isBusy}
                              onClick={() => handleRemoveRow(row.rowId)}
                              className="text-xs text-[#6c757d] hover:text-[#212529] disabled:opacity-50 cursor-pointer"
                            >
                              Remover
                            </button>
                          </td>
                        </tr>
                      )
                    })
                  )}
                </tbody>
              </table>
            </div>

            <div className="px-4 py-3 border-t border-[#dee2e6] bg-[#f8f9fa]/70 flex flex-wrap items-center justify-between gap-3">
              <button
                type="button"
                disabled={isBusy}
                onClick={handleAddRow}
                className="px-3 py-1.5 rounded-lg text-xs font-medium text-[#212529] bg-white border border-[#ced4da] hover:bg-[#e9ecef] disabled:opacity-50 cursor-pointer"
              >
                Adicionar lançamento
              </button>

              <div className="flex items-center gap-3">
                <button
                  type="button"
                  disabled={isBusy}
                  onClick={resetFileFlow}
                  className="px-3.5 py-1.5 rounded-xl text-sm text-[#495057] bg-white border border-[#ced4da] hover:text-[#212529] disabled:opacity-50 cursor-pointer"
                >
                  Cancelar
                </button>
                <button
                  type="button"
                  disabled={isBusy || rows.length === 0}
                  onClick={() => void handleConfirmImport()}
                  className="px-4 py-1.5 rounded-xl text-sm font-medium bg-[#212529] text-[#f8f9fa] hover:bg-[#343a40] disabled:opacity-50 cursor-pointer"
                >
                  {isBusy ? busyLabel || 'Salvando...' : 'Confirmar importação'}
                </button>
              </div>
            </div>
          </section>
        </div>
      )}
    </div>
  )
}
