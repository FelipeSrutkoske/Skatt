import { useEffect, useMemo, useState } from 'react'
import { normalizeHexColor } from '../lib/colors'
import { formatBRL, formatDateBR, parseBRLInput } from '../lib/format'
import type { Person, SplitInput, Transaction } from '../types/api'

export interface SplitValidationRow {
  personId: number
  amountCents: number
}

export interface SplitValidationResult {
  valid: boolean
  remainingCents: number
  error: string
}

export function splitEvenlyCents(totalCents: number, count: number): number[] {
  if (!Number.isInteger(count) || count <= 0) {
    return []
  }
  const safeTotal = Math.round(totalCents)
  const base = Math.trunc(safeTotal / count)
  const remainder = safeTotal - base * count
  const step = remainder >= 0 ? 1 : -1
  const absRem = Math.abs(remainder)
  const result: number[] = []
  for (let i = 0; i < count; i++) {
    const val = i < absRem ? base + step : base
    result.push(val === 0 ? 0 : val)
  }
  return result
}

export function validateSplits(
  totalCents: number,
  rows: SplitValidationRow[],
): SplitValidationResult {
  const safeTotal = Math.round(totalCents)
  if (rows.length === 0) {
    return {
      valid: false,
      remainingCents: safeTotal,
      error: 'Selecione ao menos uma pessoa para dividir.',
    }
  }

  let sum = 0
  let hasInvalidAmount = false
  let hasInvalidPerson = false
  let hasDuplicatePerson = false
  const seen = new Set<number>()

  for (const row of rows) {
    if (!Number.isInteger(row.personId) || row.personId <= 0) {
      hasInvalidPerson = true
    } else if (seen.has(row.personId)) {
      hasDuplicatePerson = true
    } else {
      seen.add(row.personId)
    }

    if (!Number.isFinite(row.amountCents) || !Number.isInteger(row.amountCents)) {
      hasInvalidAmount = true
    } else {
      sum += row.amountCents
    }
  }

  const remainingCents = safeTotal - sum

  if (hasInvalidPerson) {
    return {
      valid: false,
      remainingCents,
      error: 'Selecione uma pessoa em todas as linhas.',
    }
  }

  if (hasDuplicatePerson) {
    return {
      valid: false,
      remainingCents,
      error: 'Não é permitido repetir a mesma pessoa na divisão.',
    }
  }

  if (hasInvalidAmount) {
    return {
      valid: false,
      remainingCents,
      error: 'Informe valores válidos em todas as linhas.',
    }
  }

  if (remainingCents !== 0) {
    return {
      valid: false,
      remainingCents,
      error: `A soma dos valores difere do total em ${formatBRL(remainingCents)}.`,
    }
  }

  return {
    valid: true,
    remainingCents: 0,
    error: '',
  }
}

function centsToInputValue(cents: number): string {
  return formatBRL(cents).replace(/^(-?)R\$\s*/, '$1')
}

interface DraftRow {
  key: number
  personId: number
  amountText: string
}

export interface SplitModalProps {
  transaction: Transaction
  people: Person[]
  onClose: () => void
  onSave: (splits: SplitInput[]) => Promise<void>
}

export function SplitModal({ transaction, people, onClose, onSave }: SplitModalProps) {
  const selectablePeople = useMemo(() => {
    const existingIds = new Set(transaction.splits.map((s) => s.person_id))
    return people.filter((p) => !p.archived || existingIds.has(p.id))
  }, [people, transaction.splits])

  const personColorById = useMemo(() => {
    const map = new Map<number, string>()
    for (const p of people) {
      map.set(p.id, normalizeHexColor(p.color, '#495057'))
    }
    return map
  }, [people])

  const [rows, setRows] = useState<DraftRow[]>(() => {
    if (transaction.splits.length >= 2) {
      return transaction.splits.map((s, idx) => ({
        key: idx + 1,
        personId: s.person_id,
        amountText: centsToInputValue(s.amount_cents),
      }))
    }
    if (transaction.splits.length === 1) {
      const firstId = transaction.splits[0].person_id
      const second = selectablePeople.find((p) => p.id !== firstId)
      const initial: DraftRow[] = [
        {
          key: 1,
          personId: firstId,
          amountText: centsToInputValue(transaction.splits[0].amount_cents),
        },
      ]
      if (second) {
        initial.push({
          key: 2,
          personId: second.id,
          amountText: centsToInputValue(0),
        })
      }
      return initial
    }
    const count = Math.min(2, selectablePeople.length)
    const even = splitEvenlyCents(transaction.amount_cents, count)
    return even.map((amount, idx) => ({
      key: idx + 1,
      personId: selectablePeople[idx]?.id ?? 0,
      amountText: centsToInputValue(amount),
    }))
  })

  const [nextKey, setNextKey] = useState(() => rows.length + 1)
  const [saving, setSaving] = useState(false)
  const [submitError, setSubmitError] = useState('')

  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape' && !saving) {
        onClose()
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [onClose, saving])

  const parsedRows = useMemo(() => {
    return rows.map((row) => ({
      personId: row.personId,
      parsed: parseBRLInput(row.amountText),
    }))
  }, [rows])

  const hasParseError = parsedRows.some((r) => r.parsed === null)

  const validation = useMemo(() => {
    return validateSplits(
      transaction.amount_cents,
      parsedRows.map((r) => ({
        personId: r.personId,
        amountCents: r.parsed === null ? Number.NaN : r.parsed,
      })),
    )
  }, [parsedRows, transaction.amount_cents])

  const currentSum = useMemo(() => {
    return parsedRows.reduce((acc, r) => acc + (r.parsed ?? 0), 0)
  }, [parsedRows])

  function handleSplitEvenly() {
    if (rows.length === 0) {
      return
    }
    const parts = splitEvenlyCents(transaction.amount_cents, rows.length)
    setRows((prev) =>
      prev.map((row, idx) => ({
        ...row,
        amountText: centsToInputValue(parts[idx] ?? 0),
      })),
    )
    setSubmitError('')
  }

  function handleAddRow() {
    const usedIds = new Set(rows.map((r) => r.personId))
    const available = selectablePeople.find((p) => !usedIds.has(p.id))
    const remaining = validation.remainingCents
    setRows((prev) => [
      ...prev,
      {
        key: nextKey,
        personId: available?.id ?? 0,
        amountText: centsToInputValue(remaining),
      },
    ])
    setNextKey((k) => k + 1)
    setSubmitError('')
  }

  function handleRemoveRow(key: number) {
    setRows((prev) => prev.filter((r) => r.key !== key))
    setSubmitError('')
  }

  function handleChangePerson(key: number, personId: number) {
    setRows((prev) => prev.map((r) => (r.key === key ? { ...r, personId } : r)))
    setSubmitError('')
  }

  function handleChangeAmount(key: number, amountText: string) {
    setRows((prev) => prev.map((r) => (r.key === key ? { ...r, amountText } : r)))
    setSubmitError('')
  }

  async function handleSubmit(event: React.FormEvent) {
    event.preventDefault()
    if (!validation.valid || hasParseError || saving) {
      return
    }
    setSaving(true)
    setSubmitError('')
    try {
      const payload: SplitInput[] = parsedRows.map((r) => ({
        person_id: r.personId,
        amount_cents: r.parsed as number,
      }))
      await onSave(payload)
      onClose()
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : 'Erro ao salvar divisão.')
    } finally {
      setSaving(false)
    }
  }

  const canAddMore = rows.length < selectablePeople.length

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="split-modal-title"
      className="fixed inset-0 z-50 flex items-center justify-center bg-[#212529]/35 backdrop-blur-xs p-4"
      onClick={() => {
        if (!saving) {
          onClose()
        }
      }}
    >
      <div
        className="w-full max-w-lg rounded-2xl bg-[#f8f9fa]/95 backdrop-blur-xl border border-white/90 shadow-2xl overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-start justify-between gap-4 border-b border-[#dee2e6] px-5 py-4">
          <div className="min-w-0">
            <h2 id="split-modal-title" className="text-base font-semibold text-[#212529]">
              Dividir lançamento
            </h2>
            <p className="mt-0.5 text-sm text-[#495057] truncate">{transaction.description}</p>
            <p className="mt-1 text-xs text-[#6c757d] tabular-nums">
              {formatDateBR(transaction.purchase_date)} · Total:{' '}
              <span className="font-medium text-[#212529]">
                {formatBRL(transaction.amount_cents)}
              </span>
            </p>
          </div>
          <button
            type="button"
            onClick={onClose}
            disabled={saving}
            className="text-xs text-[#495057] hover:text-[#212529] rounded-lg border border-[#ced4da] bg-white px-2.5 py-1 disabled:opacity-50 cursor-pointer"
          >
            Fechar
          </button>
        </div>

        <form onSubmit={handleSubmit}>
          <div className="px-5 py-4 space-y-4">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <span className="text-xs uppercase tracking-wider text-[#6c757d] font-medium">
                Distribuição por valor exato
              </span>
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={handleSplitEvenly}
                  disabled={rows.length === 0 || saving}
                  className="px-2.5 py-1 rounded-lg text-xs font-medium text-[#212529] bg-[#e9ecef] hover:bg-[#dee2e6] border border-[#ced4da] disabled:opacity-50 cursor-pointer"
                >
                  Dividir igualmente
                </button>
                <button
                  type="button"
                  onClick={handleAddRow}
                  disabled={!canAddMore || saving}
                  className="px-2.5 py-1 rounded-lg text-xs font-medium text-[#212529] bg-white hover:bg-[#e9ecef] border border-[#ced4da] disabled:opacity-50 cursor-pointer"
                >
                  Adicionar pessoa
                </button>
              </div>
            </div>

            <div className="rounded-xl border border-[#dee2e6] bg-white/85 overflow-hidden">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b border-[#dee2e6] bg-[#f8f9fa] text-left text-xs text-[#6c757d]">
                    <th className="px-3 py-2 font-medium">Pessoa</th>
                    <th className="px-3 py-2 font-medium text-right w-36">Valor (R$)</th>
                    <th className="px-3 py-2 font-medium text-right w-20">Ação</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-[#e9ecef]">
                  {rows.map((row) => {
                    const rowParsed = parseBRLInput(row.amountText)
                    const dotColor = personColorById.get(row.personId) ?? '#adb5bd'
                    return (
                      <tr key={row.key}>
                        <td className="px-3 py-2">
                          <div className="flex items-center gap-2">
                            <span
                              className="w-2.5 h-2.5 rounded-full shrink-0"
                              style={{ backgroundColor: dotColor }}
                            />
                            <select
                              aria-label="Pessoa"
                              value={row.personId}
                              onChange={(e) => handleChangePerson(row.key, Number(e.target.value))}
                              disabled={saving}
                              className="w-full rounded-lg border border-[#ced4da] bg-white px-2 py-1 text-sm text-[#212529] focus:outline-none focus:border-[#495057]"
                            >
                              <option value={0}>Selecione...</option>
                              {selectablePeople.map((person) => (
                                <option key={person.id} value={person.id}>
                                  {person.name}
                                </option>
                              ))}
                            </select>
                          </div>
                        </td>
                        <td className="px-3 py-2">
                          <input
                            type="text"
                            inputMode="decimal"
                            aria-label="Valor em reais"
                            value={row.amountText}
                            onChange={(e) => handleChangeAmount(row.key, e.target.value)}
                            disabled={saving}
                            className={[
                              'w-full rounded-lg border px-2 py-1 text-sm text-right tabular-nums focus:outline-none',
                              rowParsed === null
                                ? 'border-[#212529] bg-[#e9ecef] text-[#212529]'
                                : 'border-[#ced4da] bg-white text-[#212529] focus:border-[#495057]',
                            ].join(' ')}
                          />
                        </td>
                        <td className="px-3 py-2 text-right">
                          <button
                            type="button"
                            onClick={() => handleRemoveRow(row.key)}
                            disabled={rows.length <= 1 || saving}
                            className="text-xs text-[#6c757d] hover:text-[#212529] disabled:opacity-40 cursor-pointer"
                          >
                            Remover
                          </button>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>

            <div className="border-t border-b border-[#dee2e6] py-2.5 grid grid-cols-3 gap-3 text-xs">
              <div>
                <div className="text-[#6c757d]">Total do lançamento</div>
                <div className="mt-0.5 text-sm font-medium text-[#212529] tabular-nums">
                  {formatBRL(transaction.amount_cents)}
                </div>
              </div>
              <div>
                <div className="text-[#6c757d]">Soma informada</div>
                <div className="mt-0.5 text-sm font-medium text-[#212529] tabular-nums">
                  {formatBRL(currentSum)}
                </div>
              </div>
              <div className="text-right">
                <div className="text-[#6c757d]">Diferença restante</div>
                <div
                  className={[
                    'mt-0.5 text-sm font-semibold tabular-nums',
                    validation.remainingCents === 0 && !hasParseError
                      ? 'text-[#212529]'
                      : 'text-[#495057] underline',
                  ].join(' ')}
                >
                  {formatBRL(validation.remainingCents)}
                </div>
              </div>
            </div>

            {!validation.valid && (
              <p className="text-xs font-medium text-[#212529]">{validation.error}</p>
            )}

            {submitError && <p className="text-xs font-medium text-[#212529]">{submitError}</p>}
          </div>

          <div className="flex items-center justify-end gap-2 border-t border-[#dee2e6] bg-[#e9ecef]/60 px-5 py-3">
            <button
              type="button"
              onClick={onClose}
              disabled={saving}
              className="px-3.5 py-1.5 rounded-xl text-sm text-[#495057] bg-white hover:bg-[#e9ecef] border border-[#ced4da] disabled:opacity-50 cursor-pointer"
            >
              Cancelar
            </button>
            <button
              type="submit"
              disabled={!validation.valid || hasParseError || saving}
              className="px-4 py-1.5 rounded-xl text-sm font-medium text-[#f8f9fa] bg-[#212529] hover:bg-[#343a40] disabled:opacity-40 cursor-pointer"
            >
              {saving ? 'Salvando...' : 'Salvar divisão'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
