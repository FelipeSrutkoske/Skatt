import type {
  BatchAssignResult,
  ConfirmImportRequest,
  DashboardSummary,
  OperationResult,
  Payment,
  PaymentInput,
  Person,
  PersonInput,
  PersonLedger,
  PreviewResult,
  Receipt,
  ReceiptMonthAvailability,
  ReceiptPreview,
  ReceiptPreviewRequest,
  SendReceiptRequest,
  SettingsView,
  SMTPTestRequest,
  SplitInput,
  Statement,
  StatementDetail,
  Transaction,
  UpdateSettingsRequest,
  UpdateTransactionRequest,
} from '../types/api'

export class ApiError extends Error {
  status: number
  code?: string

  constructor(status: number, message: string, code?: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, init)
  if (!response.ok) {
    let message = `Erro na requisição (${response.status})`
    let code: string | undefined
    const text = await response.text()
    if (text) {
      try {
        const parsed = JSON.parse(text) as { error?: string; message?: string; code?: string }
        if (parsed.error) {
          message = parsed.error
        } else if (parsed.message) {
          message = parsed.message
        }
        if (parsed.code) {
          code = parsed.code
        }
      } catch {
        message = text
      }
    }
    throw new ApiError(response.status, message, code)
  }

  if (response.status === 204) {
    return undefined as T
  }

  const text = await response.text()
  if (!text) {
    return undefined as T
  }
  return JSON.parse(text) as T
}

function jsonRequest<T>(path: string, method: string, body?: unknown): Promise<T> {
  return request<T>(path, {
    method,
    headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
}

export const api = {
  getDashboard(params?: { year?: number; month?: number; bank?: string }): Promise<DashboardSummary> {
    const query = new URLSearchParams()
    if (params?.year !== undefined && params.year > 0) {
      query.set('year', String(params.year))
    }
    if (params?.month !== undefined) {
      query.set('month', String(params.month))
    }
    if (params?.bank) {
      query.set('bank', params.bank)
    }
    const qs = query.toString()
    return request<DashboardSummary>(qs ? `/api/dashboard?${qs}` : '/api/dashboard')
  },

  listPeople(includeArchived = false): Promise<Person[]> {
    const qs = includeArchived ? '?include_archived=true' : ''
    return request<Person[]>(`/api/people${qs}`)
  },

  createPerson(input: PersonInput): Promise<Person> {
    return jsonRequest<Person>('/api/people', 'POST', input)
  },

  updatePerson(id: number, input: PersonInput): Promise<Person> {
    return jsonRequest<Person>(`/api/people/${id}`, 'PUT', input)
  },

  deletePerson(id: number): Promise<void> {
    return request<void>(`/api/people/${id}`, { method: 'DELETE' })
  },

  getPersonLedger(id: number): Promise<PersonLedger> {
    return request<PersonLedger>(`/api/people/${id}/ledger`)
  },

  createPayment(input: PaymentInput): Promise<Payment> {
    return jsonRequest<Payment>('/api/payments', 'POST', input)
  },

  deletePayment(id: number): Promise<void> {
    return request<void>(`/api/payments/${id}`, { method: 'DELETE' })
  },

  previewImport(file: File): Promise<PreviewResult> {
    const form = new FormData()
    form.append('file', file)
    return request<PreviewResult>('/api/imports/preview', {
      method: 'POST',
      body: form,
    })
  },

  unlockImport(stagingId: string, password: string): Promise<PreviewResult> {
    return jsonRequest<PreviewResult>(
      `/api/imports/preview/${encodeURIComponent(stagingId)}/unlock`,
      'POST',
      { password },
    )
  },

  aiExtractImport(stagingId: string): Promise<PreviewResult> {
    return jsonRequest<PreviewResult>(
      `/api/imports/preview/${encodeURIComponent(stagingId)}/ai-extract`,
      'POST',
    )
  },

  confirmImport(payload: ConfirmImportRequest): Promise<Statement> {
    return jsonRequest<Statement>('/api/imports/confirm', 'POST', payload)
  },

  getStatement(id: number): Promise<StatementDetail> {
    return request<StatementDetail>(`/api/statements/${id}`)
  },

  deleteStatement(id: number): Promise<void> {
    return request<void>(`/api/statements/${id}`, { method: 'DELETE' })
  },

  statementFileUrl(id: number): string {
    return `/api/statements/${id}/file`
  },

  setTransactionSplits(transactionId: number, splits: SplitInput[]): Promise<Transaction> {
    return jsonRequest<Transaction>(`/api/transactions/${transactionId}/splits`, 'PUT', { splits })
  },

  updateTransaction(transactionId: number, patch: UpdateTransactionRequest): Promise<Transaction> {
    return jsonRequest<Transaction>(`/api/transactions/${transactionId}`, 'PATCH', patch)
  },

  applySuggestions(statementId: number): Promise<BatchAssignResult> {
    return jsonRequest<BatchAssignResult>(`/api/statements/${statementId}/apply-suggestions`, 'POST')
  },

  assignRemaining(statementId: number, personId: number): Promise<BatchAssignResult> {
    return jsonRequest<BatchAssignResult>(`/api/statements/${statementId}/assign-remaining`, 'POST', {
      person_id: personId,
    })
  },

  getReceiptAvailability(
    personId: number,
    year: number,
    bank = '',
  ): Promise<ReceiptMonthAvailability[]> {
    const query = new URLSearchParams()
    query.set('person_id', String(personId))
    query.set('year', String(year))
    if (bank) {
      query.set('bank', bank)
    }
    return request<ReceiptMonthAvailability[]>(`/api/receipts/availability?${query.toString()}`)
  },

  previewReceipt(payload: ReceiptPreviewRequest): Promise<ReceiptPreview> {
    return jsonRequest<ReceiptPreview>('/api/receipts/preview', 'POST', payload)
  },

  sendReceipt(payload: SendReceiptRequest): Promise<Receipt> {
    return jsonRequest<Receipt>('/api/receipts/send', 'POST', payload)
  },

  listReceipts(): Promise<Receipt[]> {
    return request<Receipt[]>('/api/receipts')
  },

  receiptHtmlUrl(id: number): string {
    return `/api/receipts/${id}/html`
  },

  getSettings(): Promise<SettingsView> {
    return request<SettingsView>('/api/settings')
  },

  updateSettings(payload: UpdateSettingsRequest): Promise<SettingsView> {
    return jsonRequest<SettingsView>('/api/settings', 'PUT', payload)
  },

  testSMTP(payload: SMTPTestRequest): Promise<OperationResult> {
    return jsonRequest<OperationResult>('/api/settings/smtp/test', 'POST', payload)
  },

  backupExportUrl(includeSecrets = false): string {
    return `/api/backup/export?include_secrets=${includeSecrets ? 'true' : 'false'}`
  },

  restoreBackup(file: File): Promise<OperationResult> {
    const form = new FormData()
    form.append('file', file)
    return request<OperationResult>('/api/backup/restore', {
      method: 'POST',
      body: form,
    })
  },
}
