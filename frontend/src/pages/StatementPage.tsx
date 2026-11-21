import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { BankLogo } from '../components/BankLogo'
import { PersonSwitcher } from '../components/PersonSwitcher'
import { SplitModal } from '../components/SplitModal'
import { api } from '../lib/api'
import { contrastTextColor, normalizeHexColor } from '../lib/colors'
import { bankLabel, formatBRL, formatDateBR, monthNamePT } from '../lib/format'
import type {
  Person,
  PersonTotal,
  SplitInput,
  StatementDetail,
  Transaction,
  TransactionSplit,
} from '../types/api'

type FilterMode = 'all' | 'unassigned' | 'ignored' | 'person'

function recomputeStatementDetail(
  prev: StatementDetail,
  nextTransactions: Transaction[],
  personById: Map<number, Person>,
): StatementDetail {
  let totalCents = 0
  let transactionCount = 0
  let classifiedCount = 0
  let unassignedCount = 0
  let unassignedTotalCents = 0
  const personTotalsMap = new Map<number, PersonTotal>()

  for (const tx of nextTransactions) {
    if (tx.is_ignored) {
      continue
    }
    transactionCount += 1
    totalCents += tx.amount_cents
    if (tx.splits.length === 0) {
      unassignedCount += 1
      unassignedTotalCents += tx.amount_cents
      continue
    }
    classifiedCount += 1
    for (const sp of tx.splits) {
      const existing = personTotalsMap.get(sp.person_id)
      if (existing) {
        existing.total_cents += sp.amount_cents
      } else {
        const p = personById.get(sp.person_id)
        personTotalsMap.set(sp.person_id, {
          person_id: sp.person_id,
          person_name: p?.name ?? sp.person_name,
          is_self: p?.is_self ?? false,
          color: normalizeHexColor(p?.color, '#495057'),
          total_cents: sp.amount_cents,
        })
      }
    }
  }

  const totalsByPerson = Array.from(personTotalsMap.values()).sort((a, b) => {
    if (a.is_self !== b.is_self) {
      return a.is_self ? -1 : 1
    }
    return a.person_name.localeCompare(b.person_name, 'pt-BR')
  })

  return {
    ...prev,
    statement: {
      ...prev.statement,
      total_cents: totalCents,
      transaction_count: transactionCount,
      classified_count: classifiedCount,
      unassigned_count: unassignedCount,
    },
    transactions: nextTransactions,
    totals_by_person: totalsByPerson,
    unassigned_total_cents: unassignedTotalCents,
  }
}

export function StatementPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const statementId = Number(id)

  const [detail, setDetail] = useState<StatementDetail | null>(null)
  const [people, setPeople] = useState<Person[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [statusMessage, setStatusMessage] = useState('')

  const [filterMode, setFilterMode] = useState<FilterMode>('all')
  const [filterPersonId, setFilterPersonId] = useState<number>(0)
  const [searchQuery, setSearchQuery] = useState('')

  const [busyTxIds, setBusyTxIds] = useState<Record<number, boolean>>({})
  const [batchBusy, setBatchBusy] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [splittingTx, setSplittingTx] = useState<Transaction | null>(null)

  const txVersionRef = useRef<Record<number, number>>({})

  const loadData = useCallback(
    async (showLoading: boolean) => {
      if (!Number.isInteger(statementId) || statementId <= 0) {
        setError('Identificador de fatura inválido.')
        setLoading(false)
        return
      }
      if (showLoading) {
        setLoading(true)
      }
      setError('')
      try {
        const [stDetail, peopleList] = await Promise.all([
          api.getStatement(statementId),
          api.listPeople(true),
        ])
        setDetail(stDetail)
        setPeople(peopleList)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Erro ao carregar fatura.')
      } finally {
        if (showLoading) {
          setLoading(false)
        }
      }
    },
    [statementId],
  )

  useEffect(() => {
    void loadData(true)
  }, [loadData])

  const activePeople = useMemo(() => people.filter((p) => !p.archived), [people])

  const selfPerson = useMemo(() => people.find((p) => p.is_self) ?? null, [people])

  const personById = useMemo(() => {
    const map = new Map<number, Person>()
    for (const p of people) {
      map.set(p.id, p)
    }
    return map
  }, [people])

  const filteredTransactions = useMemo(() => {
    if (!detail) {
      return []
    }
    const query = searchQuery.trim().toLowerCase()
    return detail.transactions.filter((tx) => {
      if (filterMode === 'unassigned') {
        if (tx.is_ignored || tx.splits.length > 0) {
          return false
        }
      } else if (filterMode === 'ignored') {
        if (!tx.is_ignored) {
          return false
        }
      } else if (filterMode === 'person' && filterPersonId > 0) {
        if (tx.is_ignored || !tx.splits.some((s) => s.person_id === filterPersonId)) {
          return false
        }
      }

      if (query !== '' && !tx.description.toLowerCase().includes(query)) {
        return false
      }
      return true
    })
  }, [detail, filterMode, filterPersonId, searchQuery])

  const suggestionCount = useMemo(() => {
    if (!detail) {
      return 0
    }
    return detail.transactions.filter(
      (tx) => !tx.is_ignored && tx.splits.length === 0 && tx.suggested_person_id !== null,
    ).length
  }, [detail])

  function setTxBusy(txId: number, busy: boolean) {
    setBusyTxIds((prev) => ({ ...prev, [txId]: busy }))
  }

  async function handleQuickSwitch(tx: Transaction, personId: number | null) {
    const nextVersion = (txVersionRef.current[tx.id] ?? 0) + 1
    txVersionRef.current[tx.id] = nextVersion

    setStatusMessage('')
    setError('')

    const splitsInput: SplitInput[] =
      personId === null ? [] : [{ person_id: personId, amount_cents: tx.amount_cents }]

    const optimisticSplits: TransactionSplit[] =
      personId === null
        ? []
        : [
            {
              id: 0,
              transaction_id: tx.id,
              person_id: personId,
              person_name: personById.get(personId)?.name ?? '',
              amount_cents: tx.amount_cents,
            },
          ]

    setDetail((prev) => {
      if (!prev) {
        return prev
      }
      const nextTxs = prev.transactions.map((item) =>
        item.id === tx.id ? { ...item, splits: optimisticSplits } : item,
      )
      return recomputeStatementDetail(prev, nextTxs, personById)
    })

    try {
      const updated = await api.setTransactionSplits(tx.id, splitsInput)
      if (txVersionRef.current[tx.id] !== nextVersion) {
        return
      }
      setDetail((prev) => {
        if (!prev) {
          return prev
        }
        const nextTxs = prev.transactions.map((item) => (item.id === tx.id ? updated : item))
        return recomputeStatementDetail(prev, nextTxs, personById)
      })
    } catch (err) {
      if (txVersionRef.current[tx.id] !== nextVersion) {
        return
      }
      setError(err instanceof Error ? err.message : 'Erro ao atualizar atribuição.')
      await loadData(false)
    }
  }

  async function handleSaveModalSplits(splits: SplitInput[]) {
    if (!splittingTx) {
      return
    }
    setStatusMessage('')
    setError('')
    await api.setTransactionSplits(splittingTx.id, splits)
    await loadData(false)
  }

  async function handleToggleIgnore(tx: Transaction) {
    setTxBusy(tx.id, true)
    setStatusMessage('')
    setError('')
    try {
      await api.updateTransaction(tx.id, { is_ignored: !tx.is_ignored })
      await loadData(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Erro ao atualizar lançamento.')
    } finally {
      setTxBusy(tx.id, false)
    }
  }

  async function handleApplySuggestions() {
    if (!detail || batchBusy) {
      return
    }
    setBatchBusy(true)
    setStatusMessage('')
    setError('')
    try {
      const res = await api.applySuggestions(detail.statement.id)
      setStatusMessage(
        res.updated === 0
          ? 'Nenhuma sugestão aplicável encontrada.'
          : `${res.updated} ${res.updated === 1 ? 'lançamento classificado' : 'lançamentos classificados'} pelas sugestões.`,
      )
      await loadData(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Erro ao aplicar sugestões.')
    } finally {
      setBatchBusy(false)
    }
  }

  async function handleAssignRemainingToSelf() {
    if (!detail || !selfPerson || batchBusy) {
      return
    }
    setBatchBusy(true)
    setStatusMessage('')
    setError('')
    try {
      const res = await api.assignRemaining(detail.statement.id, selfPerson.id)
      setStatusMessage(
        res.updated === 0
          ? 'Não há lançamentos pendentes.'
          : `${res.updated} ${res.updated === 1 ? 'lançamento atribuído' : 'lançamentos atribuídos'} para ${selfPerson.name}.`,
      )
      await loadData(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Erro ao atribuir lançamentos restantes.')
    } finally {
      setBatchBusy(false)
    }
  }

  async function handleDeleteStatement() {
    if (!detail || deleting) {
      return
    }
    setDeleting(true)
    setError('')
    try {
      await api.deleteStatement(detail.statement.id)
      navigate('/')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Erro ao excluir fatura.')
      setDeleting(false)
      setConfirmDelete(false)
    }
  }

  if (loading) {
    return <div className="py-8 text-sm text-[#6c757d]">Carregando fatura...</div>
  }

  if (!detail) {
    return (
      <div className="space-y-3">
        <p className="text-sm text-[#212529]">{error || 'Fatura não encontrada.'}</p>
        <Link to="/" className="text-sm text-[#212529] underline">
          Voltar para visão geral
        </Link>
      </div>
    )
  }

  const { statement } = detail

  return (
    <div className="space-y-5">
      <div className="pb-4 border-b border-[#dee2e6]">
        <div className="mb-2">
          <Link to="/" className="text-xs text-[#6c757d] hover:text-[#212529] underline">
            Voltar para visão geral
          </Link>
        </div>
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="flex items-start gap-3">
            <div className="mt-0.5">
              <BankLogo bank={statement.bank} size={28} />
            </div>
            <div>
              <h1 className="text-lg font-semibold text-[#212529]">
                {bankLabel(statement.bank)} — {monthNamePT(statement.reference_month)}/
                {statement.reference_year}
                {statement.card_label && (
                  <span className="ml-2 text-sm font-normal text-[#6c757d]">
                    ({statement.card_label})
                  </span>
                )}
              </h1>
              <div className="mt-1 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-[#6c757d] tabular-nums">
                <span>
                  Total:{' '}
                  <strong className="font-semibold text-[#212529]">
                    {formatBRL(statement.total_cents)}
                  </strong>
                </span>
                {statement.declared_total_cents !== null && (
                  <span>Declarado: {formatBRL(statement.declared_total_cents)}</span>
                )}
                <span>
                  {statement.classified_count} de {statement.transaction_count} classificadas
                </span>
              </div>
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <Link
              to="/people"
              className="px-3 py-1.5 rounded-lg text-xs font-medium text-[#495057] bg-white/80 hover:bg-white border border-[#ced4da] transition-colors"
            >
              Configurar cores
            </Link>
            {statement.stored_path !== '' && (
              <a
                href={api.statementFileUrl(statement.id)}
                download
                className="px-3 py-1.5 rounded-lg text-xs font-medium text-[#212529] bg-white/80 hover:bg-white border border-[#ced4da] transition-colors"
              >
                Baixar original
              </a>
            )}
            {!confirmDelete ? (
              <button
                type="button"
                onClick={() => setConfirmDelete(true)}
                className="px-3 py-1.5 rounded-lg text-xs font-medium text-[#6c757d] hover:text-[#212529] bg-white/80 border border-[#ced4da] cursor-pointer"
              >
                Excluir fatura
              </button>
            ) : (
              <div className="flex items-center gap-1.5 rounded-lg border border-[#ced4da] bg-white px-2.5 py-1">
                <span className="text-xs text-[#212529]">Confirmar exclusão?</span>
                <button
                  type="button"
                  onClick={handleDeleteStatement}
                  disabled={deleting}
                  className="px-2 py-0.5 rounded-md text-xs font-medium text-[#f8f9fa] bg-[#212529] hover:bg-[#343a40] disabled:opacity-50 cursor-pointer"
                >
                  {deleting ? 'Excluindo...' : 'Excluir'}
                </button>
                <button
                  type="button"
                  onClick={() => setConfirmDelete(false)}
                  disabled={deleting}
                  className="px-2 py-0.5 text-xs text-[#6c757d] hover:text-[#212529] cursor-pointer"
                >
                  Cancelar
                </button>
              </div>
            )}
          </div>
        </div>
      </div>

      <section className="glass-panel rounded-xl flex flex-wrap items-stretch divide-y sm:divide-y-0 sm:divide-x divide-[#dee2e6] overflow-hidden">
        {detail.totals_by_person.map((pt) => {
          const color = normalizeHexColor(personById.get(pt.person_id)?.color ?? pt.color, '#495057')
          const activeFilter = filterMode === 'person' && filterPersonId === pt.person_id
          return (
            <button
              key={pt.person_id}
              type="button"
              onClick={() => {
                if (activeFilter) {
                  setFilterMode('all')
                  setFilterPersonId(0)
                } else {
                  setFilterMode('person')
                  setFilterPersonId(pt.person_id)
                }
              }}
              className={[
                'px-4 py-2.5 min-w-[140px] text-left transition-colors cursor-pointer',
                activeFilter ? 'bg-[#e9ecef]' : 'hover:bg-white/70',
              ].join(' ')}
            >
              <div className="flex items-center gap-1.5 text-xs text-[#495057]">
                <span
                  className="w-2.5 h-2.5 rounded-full shrink-0"
                  style={{ backgroundColor: color }}
                />
                <span>{pt.person_name}</span>
              </div>
              <div className="mt-0.5 text-sm font-semibold text-[#212529] tabular-nums">
                {formatBRL(pt.total_cents)}
              </div>
            </button>
          )
        })}
        <button
          type="button"
          onClick={() => {
            if (filterMode === 'unassigned') {
              setFilterMode('all')
            } else {
              setFilterMode('unassigned')
              setFilterPersonId(0)
            }
          }}
          className={[
            'px-4 py-2.5 min-w-[155px] text-left transition-colors cursor-pointer',
            filterMode === 'unassigned' ? 'bg-[#e9ecef]' : 'hover:bg-white/70',
          ].join(' ')}
        >
          <div className="text-xs text-[#6c757d]">
            Não atribuídas ({statement.unassigned_count})
          </div>
          <div className="mt-0.5 text-sm font-semibold text-[#212529] tabular-nums">
            {formatBRL(detail.unassigned_total_cents)}
          </div>
        </button>
      </section>

      {error && (
        <div className="glass-panel rounded-xl px-4 py-2.5 text-sm text-[#212529] border-l-2 border-l-[#212529]">
          {error}
        </div>
      )}

      {statusMessage && (
        <div className="glass-panel rounded-xl px-4 py-2 text-xs text-[#212529]">
          {statusMessage}
        </div>
      )}

      <div className="glass-panel rounded-xl px-4 py-3 flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <div className="glass-switch inline-flex items-center rounded-xl p-0.5">
            <button
              type="button"
              onClick={() => {
                setFilterMode('all')
                setFilterPersonId(0)
              }}
              className={[
                'px-2.5 py-1 rounded-lg text-xs transition-all cursor-pointer',
                filterMode === 'all'
                  ? 'bg-[#212529] text-[#f8f9fa] font-medium shadow-xs'
                  : 'text-[#495057] hover:text-[#212529]',
              ].join(' ')}
            >
              Todas
            </button>
            <button
              type="button"
              onClick={() => {
                setFilterMode('unassigned')
                setFilterPersonId(0)
              }}
              className={[
                'px-2.5 py-1 rounded-lg text-xs tabular-nums transition-all cursor-pointer',
                filterMode === 'unassigned'
                  ? 'bg-[#212529] text-[#f8f9fa] font-medium shadow-xs'
                  : 'text-[#495057] hover:text-[#212529]',
              ].join(' ')}
            >
              Pendentes ({statement.unassigned_count})
            </button>
            <button
              type="button"
              onClick={() => {
                setFilterMode('ignored')
                setFilterPersonId(0)
              }}
              className={[
                'px-2.5 py-1 rounded-lg text-xs transition-all cursor-pointer',
                filterMode === 'ignored'
                  ? 'bg-[#212529] text-[#f8f9fa] font-medium shadow-xs'
                  : 'text-[#495057] hover:text-[#212529]',
              ].join(' ')}
            >
              Ignoradas
            </button>
          </div>

          <div className="glass-switch inline-flex flex-wrap items-center gap-0.5 rounded-xl p-0.5">
            {activePeople.map((p) => {
              const color = normalizeHexColor(p.color, '#495057')
              const active = filterMode === 'person' && filterPersonId === p.id
              return (
                <button
                  key={p.id}
                  type="button"
                  onClick={() => {
                    if (active) {
                      setFilterMode('all')
                      setFilterPersonId(0)
                    } else {
                      setFilterMode('person')
                      setFilterPersonId(p.id)
                    }
                  }}
                  style={
                    active
                      ? { backgroundColor: color, color: contrastTextColor(color) }
                      : undefined
                  }
                  className={[
                    'inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg text-xs transition-all cursor-pointer',
                    active ? 'font-medium shadow-xs' : 'text-[#495057] hover:text-[#212529]',
                  ].join(' ')}
                >
                  <span
                    className="w-2 h-2 rounded-full shrink-0"
                    style={{
                      backgroundColor: active ? contrastTextColor(color) : color,
                    }}
                  />
                  <span>{p.name}</span>
                </button>
              )
            })}
          </div>

          <input
            type="search"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="Buscar descrição..."
            className="rounded-xl border border-[#ced4da] bg-white/85 px-3 py-1.5 text-xs text-[#212529] w-48 focus:outline-none focus:border-[#495057]"
          />
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            onClick={handleApplySuggestions}
            disabled={batchBusy || suggestionCount === 0}
            className="px-3 py-1.5 rounded-lg text-xs font-medium text-[#212529] bg-[#e9ecef] hover:bg-[#dee2e6] border border-[#ced4da] disabled:opacity-40 cursor-pointer"
          >
            Aplicar sugestões{suggestionCount > 0 ? ` (${suggestionCount})` : ''}
          </button>
          <button
            type="button"
            onClick={handleAssignRemainingToSelf}
            disabled={batchBusy || !selfPerson || statement.unassigned_count === 0}
            className="px-3 py-1.5 rounded-lg text-xs font-medium text-[#f8f9fa] bg-[#212529] hover:bg-[#343a40] disabled:opacity-40 cursor-pointer"
          >
            Restantes para {selfPerson?.name ?? 'Eu'}
          </button>
        </div>
      </div>

      <div className="glass-panel rounded-xl overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-[#dee2e6] bg-[#f8f9fa]/75 text-left text-xs text-[#6c757d]">
              <th className="px-3 py-2.5 font-medium w-24">Data</th>
              <th className="px-3 py-2.5 font-medium">Descrição</th>
              <th className="px-3 py-2.5 font-medium text-right w-28">Valor</th>
              <th className="px-3 py-2.5 font-medium">Responsável (1 clique)</th>
              <th className="px-3 py-2.5 font-medium text-right w-32">Ações</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-[#e9ecef]">
            {filteredTransactions.length === 0 ? (
              <tr>
                <td colSpan={5} className="px-4 py-6 text-center text-sm text-[#6c757d]">
                  Nenhum lançamento encontrado para os filtros atuais.
                </td>
              </tr>
            ) : (
              filteredTransactions.map((tx) => {
                const isBusy = Boolean(busyTxIds[tx.id])
                const isMultiSplit = tx.splits.length >= 2
                const selectedPersonId =
                  !isMultiSplit && tx.splits.length === 1 ? tx.splits[0].person_id : null

                const rowPeople = activePeople.slice()
                for (const s of tx.splits) {
                  if (!rowPeople.some((p) => p.id === s.person_id)) {
                    const archived = personById.get(s.person_id)
                    if (archived) {
                      rowPeople.push(archived)
                    }
                  }
                }

                const singleColor =
                  selectedPersonId !== null
                    ? normalizeHexColor(personById.get(selectedPersonId)?.color, '#495057')
                    : null

                return (
                  <tr
                    key={tx.id}
                    className={
                      tx.is_ignored
                        ? 'bg-[#e9ecef]/45 text-[#adb5bd]'
                        : 'hover:bg-white/75 transition-colors'
                    }
                  >
                    <td className="px-3 py-2 tabular-nums whitespace-nowrap text-xs text-[#6c757d]">
                      <div className="flex items-center gap-2">
                        <span
                          className="w-1 h-5 rounded-full shrink-0"
                          style={{
                            backgroundColor: tx.is_ignored
                              ? '#ced4da'
                              : isMultiSplit
                                ? '#343a40'
                                : singleColor ?? '#dee2e6',
                          }}
                        />
                        <span>{formatDateBR(tx.purchase_date)}</span>
                      </div>
                    </td>
                    <td className="px-3 py-2">
                      <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
                        <span
                          className={
                            tx.is_ignored ? 'line-through text-[#6c757d]' : 'text-[#212529]'
                          }
                        >
                          {tx.description}
                        </span>
                        {tx.installment_current !== null && tx.installment_total !== null && (
                          <span className="text-xs text-[#6c757d] tabular-nums">
                            {String(tx.installment_current).padStart(2, '0')}/
                            {String(tx.installment_total).padStart(2, '0')}
                          </span>
                        )}
                        {tx.amount_cents < 0 && (
                          <span className="text-xs text-[#495057] font-medium">Estorno</span>
                        )}
                        {tx.is_ignored && (
                          <span className="text-xs text-[#6c757d]">Ignorada</span>
                        )}
                      </div>

                      {!tx.is_ignored && isMultiSplit && (
                        <div className="mt-1 flex flex-wrap items-center gap-x-2.5 gap-y-0.5 text-xs text-[#495057] tabular-nums">
                          {tx.splits.map((s) => {
                            const spColor = normalizeHexColor(
                              personById.get(s.person_id)?.color,
                              '#495057',
                            )
                            return (
                              <span key={s.person_id} className="inline-flex items-center gap-1">
                                <span
                                  className="w-2 h-2 rounded-full"
                                  style={{ backgroundColor: spColor }}
                                />
                                <span>
                                  {s.person_name}: {formatBRL(s.amount_cents)}
                                </span>
                              </span>
                            )
                          })}
                        </div>
                      )}
                    </td>
                    <td className="px-3 py-2 text-right tabular-nums whitespace-nowrap font-medium text-[#212529]">
                      <span className={tx.is_ignored ? 'line-through text-[#6c757d]' : ''}>
                        {formatBRL(tx.amount_cents)}
                      </span>
                    </td>
                    <td className="px-3 py-2">
                      <PersonSwitcher
                        people={rowPeople}
                        selectedPersonId={selectedPersonId}
                        splits={tx.splits}
                        suggestedPersonId={tx.suggested_person_id}
                        disabled={tx.is_ignored || isBusy}
                        onSelect={(pid) => void handleQuickSwitch(tx, pid)}
                        onOpenSplit={() => setSplittingTx(tx)}
                      />
                    </td>
                    <td className="px-3 py-2 text-right whitespace-nowrap">
                      <div className="inline-flex items-center gap-2">
                        <button
                          type="button"
                          disabled={tx.is_ignored || isBusy}
                          onClick={() => setSplittingTx(tx)}
                          className="px-2.5 py-1 rounded-lg text-xs text-[#212529] bg-white/80 hover:bg-[#e9ecef] border border-[#ced4da] disabled:opacity-40 cursor-pointer"
                        >
                          Dividir
                        </button>
                        <button
                          type="button"
                          disabled={isBusy}
                          onClick={() => void handleToggleIgnore(tx)}
                          className="text-xs text-[#6c757d] hover:text-[#212529] disabled:opacity-40 cursor-pointer"
                        >
                          {tx.is_ignored ? 'Reativar' : 'Ignorar'}
                        </button>
                      </div>
                    </td>
                  </tr>
                )
              })
            )}
          </tbody>
        </table>
      </div>

      {splittingTx && (
        <SplitModal
          transaction={splittingTx}
          people={people}
          onClose={() => setSplittingTx(null)}
          onSave={handleSaveModalSplits}
        />
      )}
    </div>
  )
}
