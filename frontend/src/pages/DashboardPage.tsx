import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { BankLogo, BankSwitcher, DEFAULT_BANK_CODES } from '../components/BankLogo'
import { api } from '../lib/api'
import { normalizeHexColor } from '../lib/colors'
import { bankLabel, formatBRL, monthNamePT } from '../lib/format'
import type { DashboardSummary, MonthDashboardSummary } from '../types/api'

const BASE_YEARS = [2024, 2025, 2026]

type DashboardView = 'months' | 'people'

interface PersonSummaryRow {
  personId: number
  personName: string
  color: string
  isSelf: boolean
  periodCents: number
  totalSpentCents: number
  totalPaidCents: number
  balanceCents: number
}

function defaultDashboardYear(): number {
  const current = new Date().getFullYear()
  return current >= 2024 && current <= 2030 ? current : 2026
}

function formatPercent(part: number, total: number): string {
  if (total <= 0) {
    return '0%'
  }
  const pct = (part / total) * 100
  return `${pct.toFixed(1).replace('.', ',')}%`
}

export function DashboardPage() {
  const navigate = useNavigate()
  const [data, setData] = useState<DashboardSummary | null>(null)
  const [selectedYear, setSelectedYear] = useState<number>(defaultDashboardYear)
  const [selectedBank, setSelectedBank] = useState<string>('')
  const [view, setView] = useState<DashboardView>('months')
  const [modalMonth, setModalMonth] = useState<MonthDashboardSummary | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const loadDashboard = useCallback(async (year: number, bank: string) => {
    setLoading(true)
    setError('')
    try {
      const res = await api.getDashboard({ year, month: 0, bank })
      setData(res)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Erro ao carregar visão geral.')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadDashboard(selectedYear, selectedBank)
  }, [loadDashboard, selectedYear, selectedBank])

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        setModalMonth(null)
      }
    }
    if (modalMonth) {
      window.addEventListener('keydown', onKeyDown)
      return () => window.removeEventListener('keydown', onKeyDown)
    }
  }, [modalMonth])

  const yearOptions = useMemo(() => {
    const years = new Set<number>(BASE_YEARS)
    for (const y of data?.available_years ?? []) {
      if (y > 0) {
        years.add(y)
      }
    }
    years.add(selectedYear)
    return Array.from(years).sort((a, b) => a - b)
  }, [data?.available_years, selectedYear])

  const bankOptions = useMemo(() => {
    const banks = new Set<string>(DEFAULT_BANK_CODES)
    for (const b of data?.available_banks ?? []) {
      if (b) {
        banks.add(b)
      }
    }
    if (selectedBank) {
      banks.add(selectedBank)
    }
    const defaultsOrder = new Map<string, number>(DEFAULT_BANK_CODES.map((b, idx) => [b, idx]))
    return Array.from(banks).sort((a, b) => {
      const ia = defaultsOrder.get(a) ?? 99
      const ib = defaultsOrder.get(b) ?? 99
      if (ia !== ib) {
        return ia - ib
      }
      return a.localeCompare(b, 'pt-BR')
    })
  }, [data?.available_banks, selectedBank])

  const monthsGrid = useMemo<MonthDashboardSummary[]>(() => {
    if (data?.months && data.months.length === 12) {
      return data.months
    }
    return Array.from({ length: 12 }, (_, idx) => ({
      month: idx + 1,
      total_cents: 0,
      self_total_cents: 0,
      others_total_cents: 0,
      unassigned_total_cents: 0,
      unassigned_count: 0,
      transaction_count: 0,
      classified_count: 0,
      statement_count: 0,
      totals_by_person: [],
      statements: [],
    }))
  }, [data?.months])

  const yearStats = useMemo(() => {
    const activeMonths = monthsGrid.filter((m) => m.statement_count > 0)
    const statementCount = activeMonths.reduce((acc, m) => acc + m.statement_count, 0)
    const avgPerActiveMonth =
      activeMonths.length > 0 && data ? Math.round(data.total_cents / activeMonths.length) : 0
    const openDebtOthers = (data?.debt_by_person ?? [])
      .filter((p) => !p.is_self && p.balance_cents > 0)
      .reduce((acc, p) => acc + p.balance_cents, 0)
    return {
      activeMonthCount: activeMonths.length,
      statementCount,
      avgPerActiveMonth,
      openDebtOthers,
    }
  }, [monthsGrid, data])

  const personRows = useMemo<PersonSummaryRow[]>(() => {
    if (!data) {
      return []
    }
    const map = new Map<number, PersonSummaryRow>()
    for (const debt of data.debt_by_person) {
      map.set(debt.person_id, {
        personId: debt.person_id,
        personName: debt.person_name,
        color: normalizeHexColor(debt.color, '#495057'),
        isSelf: debt.is_self,
        periodCents: 0,
        totalSpentCents: debt.total_spent_cents,
        totalPaidCents: debt.total_paid_cents,
        balanceCents: debt.balance_cents,
      })
    }
    for (const pt of data.totals_by_person) {
      const existing = map.get(pt.person_id)
      if (existing) {
        existing.periodCents = pt.total_cents
      } else {
        map.set(pt.person_id, {
          personId: pt.person_id,
          personName: pt.person_name,
          color: normalizeHexColor(pt.color, '#495057'),
          isSelf: pt.is_self,
          periodCents: pt.total_cents,
          totalSpentCents: pt.total_cents,
          totalPaidCents: 0,
          balanceCents: pt.is_self ? 0 : pt.total_cents,
        })
      }
    }
    return Array.from(map.values()).sort((a, b) => {
      if (a.isSelf !== b.isSelf) {
        return a.isSelf ? -1 : 1
      }
      if (b.periodCents !== a.periodCents) {
        return b.periodCents - a.periodCents
      }
      return a.personName.localeCompare(b.personName, 'pt-BR')
    })
  }, [data])

  function importUrlForMonth(month: number): string {
    const params = new URLSearchParams()
    params.set('year', String(selectedYear))
    params.set('month', String(month))
    if (selectedBank) {
      params.set('bank', selectedBank)
    }
    return `/import?${params.toString()}`
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2.5">
        <div className="flex flex-wrap items-center gap-2.5">
          <div
            role="group"
            aria-label="Selecionar ano"
            className="glass-switch inline-flex items-center gap-0.5 rounded-xl p-1"
          >
            {yearOptions.map((y) => {
              const active = selectedYear === y
              return (
                <button
                  key={y}
                  type="button"
                  onClick={() => setSelectedYear(y)}
                  className={[
                    'px-3 py-1 rounded-lg text-xs font-semibold tabular-nums transition-all cursor-pointer',
                    active
                      ? 'bg-[#212529] text-[#f8f9fa] shadow-xs'
                      : 'text-[#343a40] hover:text-[#212529] hover:bg-white/60',
                  ].join(' ')}
                >
                  {y}
                </button>
              )
            })}
          </div>

          <BankSwitcher
            value={selectedBank}
            banks={bankOptions}
            includeAll
            allLabel="Todos"
            onChange={setSelectedBank}
          />
        </div>

        <div className="flex items-center gap-2">
          {loading && <span className="text-xs font-medium text-[#495057]">Atualizando...</span>}
          <div className="glass-switch inline-flex items-center rounded-xl p-1">
            <button
              type="button"
              onClick={() => setView('months')}
              className={[
                'px-3 py-1 rounded-lg text-xs font-medium transition-all cursor-pointer',
                view === 'months'
                  ? 'bg-[#212529] text-[#f8f9fa] shadow-xs'
                  : 'text-[#343a40] hover:text-[#212529]',
              ].join(' ')}
            >
              Meses
            </button>
            <button
              type="button"
              onClick={() => setView('people')}
              className={[
                'px-3 py-1 rounded-lg text-xs font-medium transition-all cursor-pointer',
                view === 'people'
                  ? 'bg-[#212529] text-[#f8f9fa] shadow-xs'
                  : 'text-[#343a40] hover:text-[#212529]',
              ].join(' ')}
            >
              Pessoas
            </button>
          </div>
        </div>
      </div>

      {error && (
        <div className="glass-panel rounded-xl px-4 py-2.5 text-sm font-medium text-[#212529] border-l-2 border-l-[#212529]">
          {error}
        </div>
      )}

      {data && (
        <section className="glass-panel rounded-2xl grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 divide-y sm:divide-y-0 sm:divide-x divide-[#ced4da]/75">
          <div className="px-5 py-4">
            <div className="text-xs font-semibold uppercase tracking-wider text-[#6c757d]">
              Total {selectedYear}
            </div>
            <div className="mt-1.5 text-2xl font-bold tracking-tight text-[#212529] tabular-nums">
              {formatBRL(data.total_cents)}
            </div>
            <div className="mt-1 text-xs font-medium text-[#495057] tabular-nums">
              {yearStats.statementCount}{' '}
              {yearStats.statementCount === 1 ? 'fatura' : 'faturas'}
              {yearStats.activeMonthCount > 0
                ? ` · média ${formatBRL(yearStats.avgPerActiveMonth)}/mês`
                : ' · sem faturas no ano'}
            </div>
          </div>

          <div className="px-5 py-4">
            <div className="text-xs font-semibold uppercase tracking-wider text-[#6c757d]">
              Meu total (Eu)
            </div>
            <div className="mt-1.5 text-2xl font-bold tracking-tight text-[#212529] tabular-nums">
              {formatBRL(data.self_total_cents)}
            </div>
            <div className="mt-1 text-xs font-medium text-[#495057] tabular-nums">
              {formatPercent(data.self_total_cents, data.total_cents)} do total
              {selectedBank ? ` em ${bankLabel(selectedBank)}` : ' no ano'}
            </div>
          </div>

          <div className="px-5 py-4">
            <div className="text-xs font-semibold uppercase tracking-wider text-[#6c757d]">
              Total de outros
            </div>
            <div className="mt-1.5 text-2xl font-bold tracking-tight text-[#212529] tabular-nums">
              {formatBRL(data.others_total_cents)}
            </div>
            <div className="mt-1 text-xs font-medium text-[#495057] tabular-nums">
              {formatPercent(data.others_total_cents, data.total_cents)} do total ·{' '}
              {formatBRL(yearStats.openDebtOthers)} em aberto
            </div>
          </div>

          <div className="px-5 py-4">
            <div className="text-xs font-semibold uppercase tracking-wider text-[#6c757d]">
              Não atribuídas
            </div>
            <div className="mt-1.5 text-2xl font-bold tracking-tight text-[#212529] tabular-nums">
              {formatBRL(data.unassigned_total_cents)}
            </div>
            <div className="mt-1 text-xs font-medium text-[#495057] tabular-nums">
              {data.unassigned_count === 0
                ? 'Todos os lançamentos classificados'
                : `${data.unassigned_count} ${data.unassigned_count === 1 ? 'lançamento sem responsável' : 'lançamentos sem responsável'}`}
            </div>
          </div>
        </section>
      )}

      {view === 'months' ? (
        <section
          aria-label="Meses do ano"
          className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3"
        >
          {monthsGrid.map((m) => {
            const hasStatements = m.statement_count > 0
            const singleStatement = m.statements.length === 1 ? m.statements[0] : null
            const monthTitle = monthNamePT(m.month)
            const monthNum = String(m.month).padStart(2, '0')
            const personTotals = (m.totals_by_person ?? []).filter((p) => p.total_cents !== 0)
            const positiveSum = personTotals.reduce(
              (acc, p) => acc + Math.max(0, p.total_cents),
              0,
            )

            return (
              <article
                key={m.month}
                className="glass-card rounded-xl px-4 py-3 h-[168px] flex flex-col justify-between"
              >
                <div className="space-y-2">
                  <div className="flex items-baseline justify-between gap-2">
                    <div className="flex items-baseline gap-1.5 min-w-0">
                      <span className="text-sm font-semibold text-[#212529] truncate">
                        {monthTitle}
                      </span>
                      <span className="text-xs font-medium text-[#6c757d] tabular-nums">
                        {monthNum}/{selectedYear}
                      </span>
                    </div>
                    <span className="text-xs font-medium text-[#495057] tabular-nums shrink-0">
                      {hasStatements ? (
                        m.unassigned_count > 0 ? (
                          <span className="text-[#212529] font-semibold">
                            {m.unassigned_count} pend.
                          </span>
                        ) : (
                          `${m.transaction_count} ${m.transaction_count === 1 ? 'item' : 'itens'}`
                        )
                      ) : (
                        <span className="text-[#6c757d]">Vazio</span>
                      )}
                    </span>
                  </div>

                  <div>
                    <div
                      className={[
                        'text-lg font-bold tracking-tight tabular-nums leading-tight',
                        hasStatements ? 'text-[#212529]' : 'text-[#6c757d]',
                      ].join(' ')}
                    >
                      {formatBRL(m.total_cents)}
                    </div>

                    <div className="mt-1.5 h-1.5 w-full rounded-full bg-[#dee2e6] overflow-hidden flex">
                      {hasStatements && positiveSum > 0 ? (
                        personTotals
                          .filter((p) => p.total_cents > 0)
                          .map((p) => (
                            <span
                              key={p.person_id}
                              title={`${p.person_name}: ${formatBRL(p.total_cents)}`}
                              style={{
                                width: `${(p.total_cents / positiveSum) * 100}%`,
                                backgroundColor: normalizeHexColor(p.color, '#495057'),
                              }}
                              className="h-full"
                            />
                          ))
                      ) : (
                        <span className="h-full w-0" />
                      )}
                    </div>
                  </div>

                  <div className="h-10 flex flex-col justify-center">
                    {hasStatements ? (
                      <div className="space-y-1">
                        {personTotals.length > 0 && (
                          <div className="flex items-center gap-2.5 text-xs font-medium text-[#343a40] tabular-nums overflow-hidden">
                            {personTotals.slice(0, 2).map((p) => (
                              <span
                                key={p.person_id}
                                className="inline-flex items-center gap-1 min-w-0 truncate"
                              >
                                <span
                                  className="w-2 h-2 rounded-full shrink-0"
                                  style={{ backgroundColor: normalizeHexColor(p.color, '#495057') }}
                                />
                                <span className="truncate">{p.person_name}</span>
                                <span className="text-[#212529]">{formatBRL(p.total_cents)}</span>
                              </span>
                            ))}
                            {personTotals.length > 2 && (
                              <span className="text-[#6c757d] shrink-0">
                                +{personTotals.length - 2}
                              </span>
                            )}
                          </div>
                        )}
                        <div className="flex items-center gap-1.5 overflow-hidden">
                          {m.statements.slice(0, 3).map((st) => (
                            <Link
                              key={st.id}
                              to={`/statements/${st.id}`}
                              className="inline-flex items-center gap-1 rounded-md bg-white/85 hover:bg-white border border-[#ced4da] px-1.5 py-0.5 text-[11px] font-medium text-[#212529] transition-colors shrink-0"
                            >
                              <BankLogo bank={st.bank} size={13} />
                              <span className="truncate max-w-[76px]">
                                {st.card_label || bankLabel(st.bank)}
                              </span>
                            </Link>
                          ))}
                          {m.statements.length > 3 && (
                            <button
                              type="button"
                              onClick={() => setModalMonth(m)}
                              className="text-[11px] font-medium text-[#495057] hover:text-[#212529] cursor-pointer"
                            >
                              +{m.statements.length - 3}
                            </button>
                          )}
                        </div>
                      </div>
                    ) : (
                      <span className="text-xs text-[#6c757d]">Sem faturas importadas</span>
                    )}
                  </div>
                </div>

                <div className="pt-2 border-t border-[#dee2e6] flex items-center justify-between gap-2">
                  {singleStatement ? (
                    <button
                      type="button"
                      onClick={() => navigate(`/statements/${singleStatement.id}`)}
                      className="px-2.5 py-1 rounded-lg text-xs font-medium text-[#212529] bg-[#e9ecef] hover:bg-[#dee2e6] transition-colors cursor-pointer"
                    >
                      Abrir
                    </button>
                  ) : hasStatements ? (
                    <button
                      type="button"
                      onClick={() => setModalMonth(m)}
                      className="px-2.5 py-1 rounded-lg text-xs font-medium text-[#212529] bg-[#e9ecef] hover:bg-[#dee2e6] transition-colors cursor-pointer"
                    >
                      Ver faturas ({m.statement_count})
                    </button>
                  ) : (
                    <span className="text-xs text-[#adb5bd]">—</span>
                  )}

                  <Link
                    to={importUrlForMonth(m.month)}
                    className="px-2 py-1 rounded-lg text-xs font-medium text-[#495057] hover:text-[#212529] hover:bg-[#e9ecef]/70 transition-colors"
                  >
                    Importar
                  </Link>
                </div>
              </article>
            )
          })}
        </section>
      ) : (
        <section className="space-y-2">
          <div className="flex items-center justify-between">
            <h2 className="text-xs font-semibold uppercase tracking-wider text-[#495057]">
              Resumo por pessoa ({selectedYear}
              {selectedBank ? ` · ${bankLabel(selectedBank)}` : ''}) e saldo acumulado
            </h2>
            <Link
              to="/people"
              className="text-xs font-medium text-[#343a40] hover:text-[#212529] underline"
            >
              Gerenciar pessoas, cores e pagamentos
            </Link>
          </div>
          <div className="glass-panel rounded-xl overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-[#ced4da] bg-[#f8f9fa]/80 text-left text-xs font-semibold text-[#495057]">
                  <th className="px-4 py-2.5">Pessoa</th>
                  <th className="px-4 py-2.5 text-right">Em {selectedYear}</th>
                  <th className="px-4 py-2.5 text-right">Gasto histórico</th>
                  <th className="px-4 py-2.5 text-right">Pago histórico</th>
                  <th className="px-4 py-2.5 text-right">Saldo devedor acumulado</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-[#dee2e6]">
                {personRows.length === 0 ? (
                  <tr>
                    <td colSpan={5} className="px-4 py-6 text-center text-sm text-[#6c757d]">
                      Nenhum gasto atribuído até o momento.
                    </td>
                  </tr>
                ) : (
                  personRows.map((row) => (
                    <tr key={row.personId} className="hover:bg-white/75 transition-colors">
                      <td className="px-4 py-2.5 font-medium text-[#212529]">
                        <span className="inline-flex items-center gap-2">
                          <span
                            className="w-2.5 h-2.5 rounded-full shrink-0"
                            style={{ backgroundColor: row.color }}
                          />
                          <span>{row.personName}</span>
                          {row.isSelf && (
                            <span className="text-xs font-medium text-[#6c757d]">Titular</span>
                          )}
                        </span>
                      </td>
                      <td className="px-4 py-2.5 text-right tabular-nums font-medium text-[#212529]">
                        {formatBRL(row.periodCents)}
                      </td>
                      <td className="px-4 py-2.5 text-right tabular-nums text-[#343a40]">
                        {formatBRL(row.totalSpentCents)}
                      </td>
                      <td className="px-4 py-2.5 text-right tabular-nums text-[#343a40]">
                        {row.isSelf ? '—' : formatBRL(row.totalPaidCents)}
                      </td>
                      <td className="px-4 py-2.5 text-right tabular-nums font-semibold text-[#212529]">
                        {row.isSelf ? '—' : formatBRL(row.balanceCents)}
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </section>
      )}

      {modalMonth && (
        <div
          role="dialog"
          aria-modal="true"
          className="fixed inset-0 z-50 flex items-center justify-center bg-[#212529]/35 backdrop-blur-xs p-4"
          onClick={() => setModalMonth(null)}
        >
          <div
            className="w-full max-w-lg rounded-2xl bg-[#f8f9fa]/95 backdrop-blur-xl border border-white/90 shadow-2xl overflow-hidden"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="px-5 py-3.5 border-b border-[#dee2e6] flex items-center justify-between">
              <div>
                <h2 className="text-base font-semibold text-[#212529]">
                  {monthNamePT(modalMonth.month)} de {selectedYear}
                </h2>
                <p className="text-xs font-medium text-[#495057] tabular-nums">
                  Total: {formatBRL(modalMonth.total_cents)} · {modalMonth.statement_count} faturas
                </p>
              </div>
              <button
                type="button"
                onClick={() => setModalMonth(null)}
                className="px-2.5 py-1 rounded-lg text-xs font-medium text-[#495057] bg-white border border-[#ced4da] hover:text-[#212529] cursor-pointer"
              >
                Fechar
              </button>
            </div>
            <div className="divide-y divide-[#e9ecef] max-h-[60vh] overflow-y-auto">
              {modalMonth.statements.map((st) => (
                <button
                  key={st.id}
                  type="button"
                  onClick={() => {
                    setModalMonth(null)
                    navigate(`/statements/${st.id}`)
                  }}
                  className="w-full px-5 py-3 text-left flex items-center justify-between gap-4 hover:bg-white transition-colors cursor-pointer"
                >
                  <div className="flex items-center gap-2.5 min-w-0">
                    <BankLogo bank={st.bank} size={20} />
                    <div className="min-w-0">
                      <div className="text-sm font-semibold text-[#212529] truncate">
                        {bankLabel(st.bank)}
                        {st.card_label ? ` — ${st.card_label}` : ''}
                      </div>
                      <div className="text-xs font-medium text-[#495057] tabular-nums">
                        {st.classified_count}/{st.transaction_count} classificadas
                        {st.unassigned_count > 0 ? ` · ${st.unassigned_count} pendentes` : ''}
                      </div>
                    </div>
                  </div>
                  <div className="text-sm font-semibold text-[#212529] tabular-nums shrink-0">
                    {formatBRL(st.total_cents)}
                  </div>
                </button>
              ))}
            </div>
            <div className="px-5 py-3 border-t border-[#dee2e6] bg-[#e9ecef]/60 flex justify-end">
              <Link
                to={importUrlForMonth(modalMonth.month)}
                onClick={() => setModalMonth(null)}
                className="px-3.5 py-1.5 rounded-xl text-xs font-medium text-[#f8f9fa] bg-[#212529] hover:bg-[#343a40]"
              >
                Importar nova fatura neste mês
              </Link>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
