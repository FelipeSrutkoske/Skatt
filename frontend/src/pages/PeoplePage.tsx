import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react'
import { ColorPicker } from '../components/ColorPicker'
import { ApiError, api } from '../lib/api'
import {
  PERSON_COLOR_PRESETS,
  contrastTextColor,
  normalizeHexColor,
} from '../lib/colors'
import {
  formatBRL,
  formatDateBR,
  monthNamePT,
  parseBRLInput,
  paymentStatusLabel,
} from '../lib/format'
import type { Person, PersonDebtSummary, PersonLedger } from '../types/api'

const MONTHS = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12]

function todayISO(): string {
  const now = new Date()
  const y = now.getFullYear()
  const m = String(now.getMonth() + 1).padStart(2, '0')
  const d = String(now.getDate()).padStart(2, '0')
  return `${y}-${m}-${d}`
}

function centsToInputValue(cents: number): string {
  const abs = Math.abs(Math.round(cents))
  const reais = Math.floor(abs / 100)
  const centavos = String(abs % 100).padStart(2, '0')
  return `${reais},${centavos}`
}

function extractErrorMessage(err: unknown, fallback: string): string {
  if (err instanceof ApiError && err.message) {
    return err.message
  }
  if (err instanceof Error && err.message) {
    return err.message
  }
  return fallback
}

function nextUnusedColor(people: Person[]): string {
  const used = new Set(people.map((p) => normalizeHexColor(p.color, '')))
  const free = PERSON_COLOR_PRESETS.find((preset) => !used.has(preset.hex))
  return free ? free.hex : PERSON_COLOR_PRESETS[1].hex
}

export function PeoplePage() {
  const now = new Date()
  const [people, setPeople] = useState<Person[]>([])
  const [debtMap, setDebtMap] = useState<Record<number, PersonDebtSummary>>({})
  const [showArchived, setShowArchived] = useState(false)
  const [loadingList, setLoadingList] = useState(true)
  const [pageError, setPageError] = useState('')
  const [pageNotice, setPageNotice] = useState('')

  const [newName, setNewName] = useState('')
  const [newEmail, setNewEmail] = useState('')
  const [newColor, setNewColor] = useState(PERSON_COLOR_PRESETS[1].hex)
  const [creating, setCreating] = useState(false)

  const [editingId, setEditingId] = useState<number | null>(null)
  const [editName, setEditName] = useState('')
  const [editEmail, setEditEmail] = useState('')
  const [editColor, setEditColor] = useState(PERSON_COLOR_PRESETS[0].hex)
  const [savingEdit, setSavingEdit] = useState(false)
  const [confirmDeletePersonId, setConfirmDeletePersonId] = useState<number | null>(null)

  const [selectedPersonId, setSelectedPersonId] = useState<number | null>(null)
  const [ledger, setLedger] = useState<PersonLedger | null>(null)
  const [loadingLedger, setLoadingLedger] = useState(false)
  const [ledgerError, setLedgerError] = useState('')

  const [payMonth, setPayMonth] = useState<number>(now.getMonth() + 1)
  const [payYear, setPayYear] = useState<string>(String(now.getFullYear()))
  const [payAmount, setPayAmount] = useState('')
  const [payDate, setPayDate] = useState(todayISO())
  const [payNote, setPayNote] = useState('')
  const [savingPayment, setSavingPayment] = useState(false)
  const [paymentFeedback, setPaymentFeedback] = useState('')
  const [confirmDeletePaymentId, setConfirmDeletePaymentId] = useState<number | null>(null)

  const loadPeopleAndSummary = useCallback(async () => {
    setLoadingList(true)
    setPageError('')
    try {
      const [peopleRes, dashboardRes] = await Promise.all([
        api.listPeople(true),
        api.getDashboard(),
      ])
      const list = peopleRes ?? []
      const map: Record<number, PersonDebtSummary> = {}
      for (const item of dashboardRes.debt_by_person ?? []) {
        map[item.person_id] = item
      }
      setPeople(list)
      setDebtMap(map)
      setNewColor((prev) => (prev ? prev : nextUnusedColor(list)))
      setSelectedPersonId((prev) => {
        if (prev !== null && list.some((p) => p.id === prev)) {
          return prev
        }
        const preferred = list.find((p) => !p.is_self && !p.archived) ?? list[0]
        return preferred ? preferred.id : null
      })
    } catch (err) {
      setPageError(extractErrorMessage(err, 'Não foi possível carregar a lista de pessoas.'))
    } finally {
      setLoadingList(false)
    }
  }, [])

  const loadLedger = useCallback(async (personId: number) => {
    setLoadingLedger(true)
    setLedgerError('')
    try {
      const data = await api.getPersonLedger(personId)
      setLedger(data)
    } catch (err) {
      setLedger(null)
      setLedgerError(extractErrorMessage(err, 'Não foi possível carregar o extrato da pessoa.'))
    } finally {
      setLoadingLedger(false)
    }
  }, [])

  useEffect(() => {
    void loadPeopleAndSummary()
  }, [loadPeopleAndSummary])

  useEffect(() => {
    if (selectedPersonId === null) {
      setLedger(null)
      return
    }
    setPaymentFeedback('')
    setConfirmDeletePaymentId(null)
    void loadLedger(selectedPersonId)
  }, [selectedPersonId, loadLedger])

  const visiblePeople = useMemo(() => {
    return people.filter((p) => showArchived || !p.archived)
  }, [people, showArchived])

  const archivedCount = useMemo(() => {
    return people.filter((p) => p.archived).length
  }, [people])

  const consolidatedTotals = useMemo(() => {
    let spent = 0
    let paid = 0
    let balance = 0
    for (const p of visiblePeople) {
      const summary = debtMap[p.id]
      if (summary) {
        spent += summary.total_spent_cents
        paid += summary.total_paid_cents
        balance += summary.balance_cents
      }
    }
    return { spent, paid, balance }
  }, [visiblePeople, debtMap])

  async function handleCreatePerson(e: FormEvent) {
    e.preventDefault()
    const trimmedName = newName.trim()
    if (!trimmedName) {
      setPageError('Informe o nome da pessoa.')
      return
    }
    setCreating(true)
    setPageError('')
    setPageNotice('')
    try {
      const created = await api.createPerson({
        name: trimmedName,
        email: newEmail.trim(),
        color: normalizeHexColor(newColor, '#495057'),
      })
      setNewName('')
      setNewEmail('')
      setPageNotice(`Pessoa "${created.name}" cadastrada.`)
      await loadPeopleAndSummary()
      setNewColor(nextUnusedColor([...people, created]))
      setSelectedPersonId(created.id)
    } catch (err) {
      setPageError(extractErrorMessage(err, 'Erro ao cadastrar pessoa.'))
    } finally {
      setCreating(false)
    }
  }

  function startEdit(person: Person) {
    setEditingId(person.id)
    setEditName(person.name)
    setEditEmail(person.email)
    setEditColor(normalizeHexColor(person.color, '#495057'))
    setConfirmDeletePersonId(null)
    setPageError('')
    setPageNotice('')
  }

  function cancelEdit() {
    setEditingId(null)
    setEditName('')
    setEditEmail('')
  }

  async function handleSaveEdit(person: Person) {
    const trimmedName = editName.trim()
    if (!trimmedName) {
      setPageError('O nome da pessoa não pode ficar vazio.')
      return
    }
    setSavingEdit(true)
    setPageError('')
    setPageNotice('')
    try {
      await api.updatePerson(person.id, {
        name: trimmedName,
        email: editEmail.trim(),
        color: normalizeHexColor(editColor, '#495057'),
        archived: person.archived,
      })
      setEditingId(null)
      setPageNotice(`Dados e cor de "${trimmedName}" atualizados.`)
      await loadPeopleAndSummary()
      if (selectedPersonId === person.id) {
        await loadLedger(person.id)
      }
    } catch (err) {
      setPageError(extractErrorMessage(err, 'Erro ao atualizar pessoa.'))
    } finally {
      setSavingEdit(false)
    }
  }

  async function handleToggleArchive(person: Person) {
    if (person.is_self) {
      return
    }
    setPageError('')
    setPageNotice('')
    try {
      await api.updatePerson(person.id, {
        name: person.name,
        email: person.email,
        color: person.color,
        archived: !person.archived,
      })
      setPageNotice(
        person.archived ? `"${person.name}" reativada.` : `"${person.name}" arquivada.`,
      )
      await loadPeopleAndSummary()
      if (selectedPersonId === person.id) {
        await loadLedger(person.id)
      }
    } catch (err) {
      setPageError(extractErrorMessage(err, 'Erro ao alterar arquivamento da pessoa.'))
    }
  }

  async function handleDeletePerson(person: Person) {
    if (person.is_self) {
      return
    }
    setPageError('')
    setPageNotice('')
    try {
      await api.deletePerson(person.id)
      setConfirmDeletePersonId(null)
      setPageNotice(`"${person.name}" excluída.`)
      await loadPeopleAndSummary()
    } catch (err) {
      setConfirmDeletePersonId(null)
      setPageError(
        extractErrorMessage(
          err,
          'Não foi possível excluir esta pessoa. Caso possua histórico, utilize a opção Arquivar.',
        ),
      )
    }
  }

  function handlePrefillPayment(year: number, month: number, balanceCents: number) {
    setPayYear(String(year))
    setPayMonth(month)
    if (balanceCents > 0) {
      setPayAmount(centsToInputValue(balanceCents))
    }
    setPaymentFeedback('')
  }

  async function handleCreatePayment(e: FormEvent) {
    e.preventDefault()
    if (selectedPersonId === null) {
      return
    }
    const parsedYear = Number(payYear)
    const amountCents = parseBRLInput(payAmount)
    if (!Number.isInteger(parsedYear) || parsedYear < 2000 || parsedYear > 2100) {
      setLedgerError('Informe um ano de referência válido.')
      return
    }
    if (amountCents === null || amountCents <= 0) {
      setLedgerError('Informe um valor de pagamento positivo em reais (ex: 150,00).')
      return
    }
    if (!payDate.trim()) {
      setLedgerError('Informe a data do pagamento.')
      return
    }

    setSavingPayment(true)
    setLedgerError('')
    setPaymentFeedback('')
    try {
      await api.createPayment({
        person_id: selectedPersonId,
        reference_year: parsedYear,
        reference_month: payMonth,
        amount_cents: amountCents,
        paid_at: payDate,
        note: payNote.trim(),
      })
      setPayAmount('')
      setPayNote('')
      setPaymentFeedback(
        `Pagamento de ${formatBRL(amountCents)} registrado em ${monthNamePT(payMonth)}/${parsedYear}.`,
      )
      await Promise.all([loadPeopleAndSummary(), loadLedger(selectedPersonId)])
    } catch (err) {
      setLedgerError(extractErrorMessage(err, 'Erro ao registrar pagamento.'))
    } finally {
      setSavingPayment(false)
    }
  }

  async function handleDeletePayment(paymentId: number) {
    if (selectedPersonId === null) {
      return
    }
    setLedgerError('')
    setPaymentFeedback('')
    try {
      await api.deletePayment(paymentId)
      setConfirmDeletePaymentId(null)
      setPaymentFeedback('Pagamento excluído.')
      await Promise.all([loadPeopleAndSummary(), loadLedger(selectedPersonId)])
    } catch (err) {
      setLedgerError(extractErrorMessage(err, 'Erro ao excluir pagamento.'))
    }
  }

  function statusTextClass(status: string): string {
    switch (status) {
      case 'pendente':
        return 'font-medium text-[#212529]'
      case 'pago_parcial':
        return 'font-medium text-[#495057]'
      case 'pago':
      case 'sem_gastos':
      default:
        return 'text-[#6c757d]'
    }
  }

  const previewColor = normalizeHexColor(newColor, '#495057')

  return (
    <div className="space-y-6">
      <div className="border-b border-[#dee2e6] pb-4 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold tracking-tight text-[#212529]">
            Pessoas, cores e pagamentos
          </h1>
          <p className="mt-1 text-xs text-[#6c757d]">
            Defina a cor de cada pessoa para o seletor rápido de compras e acompanhe o saldo devedor mensal.
          </p>
        </div>
        {archivedCount > 0 && (
          <label className="inline-flex items-center gap-2 text-xs text-[#495057] cursor-pointer select-none">
            <input
              type="checkbox"
              checked={showArchived}
              onChange={(e) => setShowArchived(e.target.checked)}
              className="border-[#ced4da]"
            />
            Mostrar arquivadas ({archivedCount})
          </label>
        )}
      </div>

      {pageError && (
        <div
          role="alert"
          className="glass-panel rounded-xl px-4 py-2.5 text-sm text-[#212529] border-l-2 border-l-[#212529]"
        >
          {pageError}
        </div>
      )}

      {pageNotice && (
        <div
          role="status"
          className="glass-panel rounded-xl px-4 py-2.5 text-sm text-[#212529]"
        >
          {pageNotice}
        </div>
      )}

      <section>
        <form
          onSubmit={handleCreatePerson}
          className="glass-panel rounded-xl px-4 py-4 space-y-3"
        >
          <div className="flex flex-wrap items-end gap-3">
            <div className="flex-1 min-w-[180px]">
              <label
                htmlFor="new-person-name"
                className="block text-xs font-medium text-[#495057] mb-1"
              >
                Nova pessoa
              </label>
              <input
                id="new-person-name"
                type="text"
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                placeholder="Nome (ex: Regina)"
                className="w-full h-9 px-3 rounded-lg text-sm bg-white/90 border border-[#ced4da] text-[#212529] focus:outline-none focus:border-[#495057]"
              />
            </div>
            <div className="flex-1 min-w-[220px]">
              <label
                htmlFor="new-person-email"
                className="block text-xs font-medium text-[#495057] mb-1"
              >
                E-mail (opcional)
              </label>
              <input
                id="new-person-email"
                type="email"
                value={newEmail}
                onChange={(e) => setNewEmail(e.target.value)}
                placeholder="pessoa@exemplo.com"
                className="w-full h-9 px-3 rounded-lg text-sm bg-white/90 border border-[#ced4da] text-[#212529] focus:outline-none focus:border-[#495057]"
              />
            </div>
            <button
              type="submit"
              disabled={creating}
              className="h-9 px-4 rounded-xl text-sm font-medium text-[#f8f9fa] bg-[#212529] hover:bg-[#343a40] disabled:opacity-50 transition-colors cursor-pointer"
            >
              {creating ? 'Adicionando...' : 'Adicionar pessoa'}
            </button>
          </div>

          <div className="pt-2 border-t border-[#e9ecef] flex flex-wrap items-center justify-between gap-3">
            <div className="flex flex-wrap items-center gap-3">
              <span className="text-xs font-medium text-[#495057]">Cor no switcher:</span>
              <ColorPicker value={newColor} onChange={setNewColor} />
            </div>
            <span
              className="inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium"
              style={{
                backgroundColor: previewColor,
                color: contrastTextColor(previewColor),
              }}
            >
              <span
                className="w-2 h-2 rounded-full"
                style={{ backgroundColor: contrastTextColor(previewColor), opacity: 0.9 }}
              />
              <span>{newName.trim() || 'Prévia'}</span>
            </span>
          </div>
        </form>
      </section>

      <section className="glass-panel rounded-xl overflow-hidden">
        <div className="px-4 py-3 border-b border-[#dee2e6] flex items-center justify-between">
          <h2 className="text-sm font-semibold text-[#212529]">Participantes e saldo consolidado</h2>
          <span className="text-xs text-[#6c757d]">
            Clique em Editar para alterar nome, e-mail ou cor
          </span>
        </div>

        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-[#dee2e6] bg-[#f8f9fa]/75 text-xs font-medium text-[#6c757d]">
                <th className="text-left py-2.5 px-4">Pessoa e cor</th>
                <th className="text-left py-2.5 px-4">E-mail</th>
                <th className="text-right py-2.5 px-4">Total gasto</th>
                <th className="text-right py-2.5 px-4">Total pago</th>
                <th className="text-right py-2.5 px-4">Saldo devedor</th>
                <th className="text-right py-2.5 px-4">Ações</th>
              </tr>
            </thead>
            <tbody>
              {loadingList ? (
                <tr>
                  <td colSpan={6} className="py-6 px-4 text-center text-[#6c757d]">
                    Carregando pessoas...
                  </td>
                </tr>
              ) : visiblePeople.length === 0 ? (
                <tr>
                  <td colSpan={6} className="py-6 px-4 text-center text-[#6c757d]">
                    Nenhuma pessoa encontrada.
                  </td>
                </tr>
              ) : (
                visiblePeople.map((person) => {
                  const summary = debtMap[person.id]
                  const spentCents = summary ? summary.total_spent_cents : 0
                  const paidCents = summary ? summary.total_paid_cents : 0
                  const balanceCents = summary ? summary.balance_cents : 0
                  const isSelected = selectedPersonId === person.id
                  const isEditing = editingId === person.id
                  const isConfirmingDelete = confirmDeletePersonId === person.id
                  const personColor = normalizeHexColor(person.color, '#495057')

                  return (
                    <tr
                      key={person.id}
                      onClick={() => {
                        if (!isEditing) {
                          setSelectedPersonId(person.id)
                        }
                      }}
                      className={[
                        'border-b border-[#e9ecef] transition-colors',
                        isSelected ? 'bg-[#e9ecef]/70' : 'hover:bg-white/75',
                        isEditing ? '' : 'cursor-pointer',
                      ].join(' ')}
                    >
                      <td className="py-2.5 px-4 align-middle">
                        {isEditing ? (
                          <div className="space-y-2" onClick={(e) => e.stopPropagation()}>
                            <input
                              type="text"
                              value={editName}
                              onChange={(e) => setEditName(e.target.value)}
                              className="w-full min-w-[140px] h-8 px-2.5 rounded-lg text-sm bg-white border border-[#ced4da] text-[#212529] focus:outline-none focus:border-[#495057]"
                            />
                            <ColorPicker value={editColor} onChange={setEditColor} compact />
                          </div>
                        ) : (
                          <div className="flex items-center gap-2.5">
                            <span
                              className="inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-medium"
                              style={{
                                backgroundColor: personColor,
                                color: contrastTextColor(personColor),
                              }}
                            >
                              <span
                                className="w-1.5 h-1.5 rounded-full"
                                style={{
                                  backgroundColor: contrastTextColor(personColor),
                                  opacity: 0.9,
                                }}
                              />
                              <span>{person.name}</span>
                            </span>
                            {person.is_self && (
                              <span className="text-xs text-[#6c757d]">Titular</span>
                            )}
                            {person.archived && (
                              <span className="text-xs text-[#6c757d]">Arquivada</span>
                            )}
                          </div>
                        )}
                      </td>
                      <td className="py-2.5 px-4 align-middle text-[#495057]">
                        {isEditing ? (
                          <input
                            type="email"
                            value={editEmail}
                            onChange={(e) => setEditEmail(e.target.value)}
                            onClick={(e) => e.stopPropagation()}
                            placeholder="Sem e-mail"
                            className="w-full min-w-[180px] h-8 px-2.5 rounded-lg text-sm bg-white border border-[#ced4da] text-[#212529] focus:outline-none focus:border-[#495057]"
                          />
                        ) : (
                          <span>{person.email || '—'}</span>
                        )}
                      </td>
                      <td className="py-2.5 px-4 text-right tabular-nums text-[#212529]">
                        {formatBRL(spentCents)}
                      </td>
                      <td className="py-2.5 px-4 text-right tabular-nums text-[#495057]">
                        {formatBRL(paidCents)}
                      </td>
                      <td
                        className={[
                          'py-2.5 px-4 text-right tabular-nums',
                          balanceCents > 0 ? 'font-semibold text-[#212529]' : 'text-[#6c757d]',
                        ].join(' ')}
                      >
                        {formatBRL(balanceCents)}
                      </td>
                      <td
                        className="py-2.5 px-4 text-right whitespace-nowrap"
                        onClick={(e) => e.stopPropagation()}
                      >
                        {isEditing ? (
                          <div className="inline-flex items-center gap-2">
                            <button
                              type="button"
                              disabled={savingEdit}
                              onClick={() => void handleSaveEdit(person)}
                              className="px-3 py-1 rounded-lg text-xs font-medium text-[#f8f9fa] bg-[#212529] hover:bg-[#343a40] disabled:opacity-50 cursor-pointer"
                            >
                              Salvar
                            </button>
                            <button
                              type="button"
                              onClick={cancelEdit}
                              className="px-2.5 py-1 rounded-lg text-xs text-[#495057] border border-[#ced4da] hover:bg-white cursor-pointer"
                            >
                              Cancelar
                            </button>
                          </div>
                        ) : isConfirmingDelete ? (
                          <div className="inline-flex items-center gap-2">
                            <span className="text-xs text-[#212529]">Confirmar exclusão?</span>
                            <button
                              type="button"
                              onClick={() => void handleDeletePerson(person)}
                              className="px-2 py-1 rounded-lg text-xs font-medium text-[#f8f9fa] bg-[#212529] hover:bg-[#343a40] cursor-pointer"
                            >
                              Excluir
                            </button>
                            <button
                              type="button"
                              onClick={() => setConfirmDeletePersonId(null)}
                              className="px-2 py-1 rounded-lg text-xs text-[#495057] border border-[#ced4da] hover:bg-white cursor-pointer"
                            >
                              Cancelar
                            </button>
                          </div>
                        ) : (
                          <div className="inline-flex items-center gap-3 text-xs">
                            <button
                              type="button"
                              onClick={() => setSelectedPersonId(person.id)}
                              className="text-[#212529] underline underline-offset-2 hover:text-[#495057] cursor-pointer"
                            >
                              Extrato
                            </button>
                            <button
                              type="button"
                              onClick={() => startEdit(person)}
                              className="text-[#495057] hover:text-[#212529] cursor-pointer"
                            >
                              Editar
                            </button>
                            {!person.is_self && (
                              <>
                                <button
                                  type="button"
                                  onClick={() => void handleToggleArchive(person)}
                                  className="text-[#495057] hover:text-[#212529] cursor-pointer"
                                >
                                  {person.archived ? 'Desarquivar' : 'Arquivar'}
                                </button>
                                <button
                                  type="button"
                                  onClick={() => setConfirmDeletePersonId(person.id)}
                                  className="text-[#6c757d] hover:text-[#212529] cursor-pointer"
                                >
                                  Excluir
                                </button>
                              </>
                            )}
                          </div>
                        )}
                      </td>
                    </tr>
                  )
                })
              )}
            </tbody>
            {!loadingList && visiblePeople.length > 0 && (
              <tfoot>
                <tr className="bg-[#f8f9fa]/80 text-xs font-semibold text-[#212529]">
                  <td colSpan={2} className="py-2.5 px-4">
                    Total listado ({visiblePeople.length})
                  </td>
                  <td className="py-2.5 px-4 text-right tabular-nums">
                    {formatBRL(consolidatedTotals.spent)}
                  </td>
                  <td className="py-2.5 px-4 text-right tabular-nums text-[#495057]">
                    {formatBRL(consolidatedTotals.paid)}
                  </td>
                  <td className="py-2.5 px-4 text-right tabular-nums">
                    {formatBRL(consolidatedTotals.balance)}
                  </td>
                  <td className="py-2.5 px-4" />
                </tr>
              </tfoot>
            )}
          </table>
        </div>
      </section>

      {selectedPersonId !== null && (
        <section className="glass-panel rounded-xl overflow-hidden">
          <div className="px-4 py-3 border-b border-[#dee2e6] flex flex-wrap items-baseline justify-between gap-2">
            <div>
              <h2 className="text-base font-semibold text-[#212529]">
                Extrato mensal{ledger ? ` — ${ledger.person.name}` : ''}
              </h2>
              {ledger && ledger.person.email && (
                <p className="text-xs text-[#6c757d] mt-0.5">{ledger.person.email}</p>
              )}
            </div>
            {ledger && ledger.person.is_self && (
              <span className="text-xs text-[#6c757d]">
                Participante titular (gastos próprios)
              </span>
            )}
          </div>

          {ledgerError && (
            <div
              role="alert"
              className="border-b border-[#dee2e6] px-4 py-2.5 text-sm text-[#212529] bg-[#f8f9fa]"
            >
              {ledgerError}
            </div>
          )}

          {paymentFeedback && (
            <div
              role="status"
              className="border-b border-[#dee2e6] px-4 py-2.5 text-sm text-[#212529] bg-[#f8f9fa]"
            >
              {paymentFeedback}
            </div>
          )}

          {loadingLedger ? (
            <div className="py-8 px-4 text-center text-sm text-[#6c757d]">
              Carregando extrato...
            </div>
          ) : ledger ? (
            <>
              <div className="grid grid-cols-1 sm:grid-cols-3 border-b border-[#dee2e6] divide-y sm:divide-y-0 sm:divide-x divide-[#dee2e6]">
                <div className="px-4 py-3">
                  <div className="text-xs text-[#6c757d]">Total histórico gasto</div>
                  <div className="mt-1 text-base font-semibold tabular-nums text-[#212529]">
                    {formatBRL(ledger.total_spent_cents)}
                  </div>
                </div>
                <div className="px-4 py-3">
                  <div className="text-xs text-[#6c757d]">Total histórico pago</div>
                  <div className="mt-1 text-base font-semibold tabular-nums text-[#212529]">
                    {formatBRL(ledger.total_paid_cents)}
                  </div>
                </div>
                <div className="px-4 py-3">
                  <div className="text-xs text-[#6c757d]">Saldo devedor atual</div>
                  <div className="mt-1 text-base font-semibold tabular-nums text-[#212529]">
                    {formatBRL(ledger.balance_cents)}
                  </div>
                  <div className="text-xs text-[#6c757d] mt-0.5">
                    {ledger.balance_cents > 0
                      ? 'Valor em aberto'
                      : ledger.balance_cents < 0
                        ? 'Crédito a favor da pessoa'
                        : 'Sem pendências'}
                  </div>
                </div>
              </div>

              <form
                onSubmit={handleCreatePayment}
                className="px-4 py-3 border-b border-[#dee2e6] bg-[#f8f9fa]/70 flex flex-wrap items-end gap-3"
              >
                <div className="w-36">
                  <label htmlFor="pay-month" className="block text-xs font-medium text-[#495057] mb-1">
                    Mês de referência
                  </label>
                  <select
                    id="pay-month"
                    value={payMonth}
                    onChange={(e) => setPayMonth(Number(e.target.value))}
                    className="w-full h-9 px-2.5 rounded-lg text-sm bg-white border border-[#ced4da] text-[#212529] focus:outline-none focus:border-[#495057]"
                  >
                    {MONTHS.map((m) => (
                      <option key={m} value={m}>
                        {monthNamePT(m)}
                      </option>
                    ))}
                  </select>
                </div>

                <div className="w-24">
                  <label htmlFor="pay-year" className="block text-xs font-medium text-[#495057] mb-1">
                    Ano
                  </label>
                  <input
                    id="pay-year"
                    type="number"
                    min={2000}
                    max={2100}
                    value={payYear}
                    onChange={(e) => setPayYear(e.target.value)}
                    className="w-full h-9 px-2.5 rounded-lg text-sm tabular-nums bg-white border border-[#ced4da] text-[#212529] focus:outline-none focus:border-[#495057]"
                  />
                </div>

                <div className="w-36">
                  <label htmlFor="pay-amount" className="block text-xs font-medium text-[#495057] mb-1">
                    Valor (R$)
                  </label>
                  <input
                    id="pay-amount"
                    type="text"
                    inputMode="decimal"
                    value={payAmount}
                    onChange={(e) => setPayAmount(e.target.value)}
                    placeholder="0,00"
                    className="w-full h-9 px-2.5 rounded-lg text-sm text-right tabular-nums bg-white border border-[#ced4da] text-[#212529] focus:outline-none focus:border-[#495057]"
                  />
                </div>

                <div className="w-40">
                  <label htmlFor="pay-date" className="block text-xs font-medium text-[#495057] mb-1">
                    Data do pagamento
                  </label>
                  <input
                    id="pay-date"
                    type="date"
                    value={payDate}
                    onChange={(e) => setPayDate(e.target.value)}
                    className="w-full h-9 px-2.5 rounded-lg text-sm tabular-nums bg-white border border-[#ced4da] text-[#212529] focus:outline-none focus:border-[#495057]"
                  />
                </div>

                <div className="flex-1 min-w-[180px]">
                  <label htmlFor="pay-note" className="block text-xs font-medium text-[#495057] mb-1">
                    Observação (opcional)
                  </label>
                  <input
                    id="pay-note"
                    type="text"
                    value={payNote}
                    onChange={(e) => setPayNote(e.target.value)}
                    placeholder="Ex: Pix recebido"
                    className="w-full h-9 px-2.5 rounded-lg text-sm bg-white border border-[#ced4da] text-[#212529] focus:outline-none focus:border-[#495057]"
                  />
                </div>

                <button
                  type="submit"
                  disabled={savingPayment}
                  className="h-9 px-4 rounded-xl text-sm font-medium text-[#f8f9fa] bg-[#212529] hover:bg-[#343a40] disabled:opacity-50 transition-colors cursor-pointer"
                >
                  {savingPayment ? 'Registrando...' : 'Registrar pagamento'}
                </button>
              </form>

              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-[#dee2e6] bg-[#f8f9fa]/75 text-xs font-medium text-[#6c757d]">
                      <th className="text-left py-2.5 px-4">Mês/Ano</th>
                      <th className="text-right py-2.5 px-4">Gastos</th>
                      <th className="text-right py-2.5 px-4">Pago</th>
                      <th className="text-right py-2.5 px-4">Saldo do mês</th>
                      <th className="text-left py-2.5 px-4">Status</th>
                      <th className="text-left py-2.5 px-4">Pagamentos</th>
                      <th className="text-right py-2.5 px-4">Ação</th>
                    </tr>
                  </thead>
                  <tbody>
                    {(ledger.months ?? []).length === 0 ? (
                      <tr>
                        <td colSpan={7} className="py-6 px-4 text-center text-[#6c757d]">
                          Nenhum gasto ou pagamento registrado para esta pessoa.
                        </td>
                      </tr>
                    ) : (
                      (ledger.months ?? []).map((row) => {
                        const payments = row.payments ?? []
                        return (
                          <tr
                            key={`${row.year}-${row.month}`}
                            className="border-b border-[#e9ecef] align-top"
                          >
                            <td className="py-2.5 px-4 font-medium text-[#212529] whitespace-nowrap">
                              {monthNamePT(row.month)}/{row.year}
                            </td>
                            <td className="py-2.5 px-4 text-right tabular-nums text-[#212529] whitespace-nowrap">
                              {formatBRL(row.spent_cents)}
                            </td>
                            <td className="py-2.5 px-4 text-right tabular-nums text-[#495057] whitespace-nowrap">
                              {formatBRL(row.paid_cents)}
                            </td>
                            <td
                              className={[
                                'py-2.5 px-4 text-right tabular-nums whitespace-nowrap',
                                row.month_balance_cents > 0
                                  ? 'font-semibold text-[#212529]'
                                  : 'text-[#6c757d]',
                              ].join(' ')}
                            >
                              {formatBRL(row.month_balance_cents)}
                            </td>
                            <td className="py-2.5 px-4 whitespace-nowrap">
                              <span className={statusTextClass(row.status)}>
                                {paymentStatusLabel(row.status)}
                              </span>
                            </td>
                            <td className="py-2.5 px-4">
                              {payments.length === 0 ? (
                                <span className="text-xs text-[#6c757d]">—</span>
                              ) : (
                                <ul className="space-y-1">
                                  {payments.map((payment) => {
                                    const confirming = confirmDeletePaymentId === payment.id
                                    return (
                                      <li
                                        key={payment.id}
                                        className="flex flex-wrap items-center justify-between gap-2 text-xs"
                                      >
                                        <span className="text-[#495057]">
                                          <span className="tabular-nums font-medium text-[#212529]">
                                            {formatBRL(payment.amount_cents)}
                                          </span>
                                          {' em '}
                                          <span className="tabular-nums">
                                            {formatDateBR(payment.paid_at)}
                                          </span>
                                          {payment.note ? ` — ${payment.note}` : ''}
                                        </span>
                                        {confirming ? (
                                          <span className="inline-flex items-center gap-1.5">
                                            <button
                                              type="button"
                                              onClick={() => void handleDeletePayment(payment.id)}
                                              className="text-[#212529] font-medium hover:underline cursor-pointer"
                                            >
                                              Confirmar
                                            </button>
                                            <button
                                              type="button"
                                              onClick={() => setConfirmDeletePaymentId(null)}
                                              className="text-[#6c757d] hover:text-[#212529] cursor-pointer"
                                            >
                                              Cancelar
                                            </button>
                                          </span>
                                        ) : (
                                          <button
                                            type="button"
                                            onClick={() => setConfirmDeletePaymentId(payment.id)}
                                            className="text-[#6c757d] hover:text-[#212529] cursor-pointer"
                                          >
                                            Excluir
                                          </button>
                                        )}
                                      </li>
                                    )
                                  })}
                                </ul>
                              )}
                            </td>
                            <td className="py-2.5 px-4 text-right whitespace-nowrap">
                              <button
                                type="button"
                                onClick={() =>
                                  handlePrefillPayment(row.year, row.month, row.month_balance_cents)
                                }
                                className="text-xs text-[#495057] hover:text-[#212529] underline underline-offset-2 cursor-pointer"
                              >
                                Preencher
                              </button>
                            </td>
                          </tr>
                        )
                      })
                    )}
                  </tbody>
                </table>
              </div>
            </>
          ) : null}
        </section>
      )}
    </div>
  )
}
