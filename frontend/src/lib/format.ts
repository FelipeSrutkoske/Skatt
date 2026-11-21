const MONTHS_PT = [
  'Janeiro',
  'Fevereiro',
  'Março',
  'Abril',
  'Maio',
  'Junho',
  'Julho',
  'Agosto',
  'Setembro',
  'Outubro',
  'Novembro',
  'Dezembro',
]

export function formatBRL(cents: number): string {
  const safeCents = Number.isFinite(cents) ? Math.round(cents) : 0
  const negative = safeCents < 0
  const abs = Math.abs(safeCents)
  const reais = Math.floor(abs / 100)
  const centavos = abs % 100
  const intStr = String(reais).replace(/\B(?=(\d{3})+(?!\d))/g, '.')
  const fracStr = String(centavos).padStart(2, '0')
  return `${negative ? '-' : ''}R$ ${intStr},${fracStr}`
}

export function parseBRLInput(raw: string): number | null {
  let s = raw.trim()
  if (s === '') {
    return null
  }

  let negative = false
  if (s.startsWith('-')) {
    negative = true
    s = s.slice(1).trim()
  } else if (s.startsWith('+')) {
    s = s.slice(1).trim()
  }

  if (s.startsWith('R$') || s.startsWith('r$')) {
    s = s.slice(2).trim()
  }

  if (s.startsWith('-')) {
    negative = true
    s = s.slice(1).trim()
  } else if (s.startsWith('+')) {
    s = s.slice(1).trim()
  }

  if (s.endsWith('-')) {
    negative = true
    s = s.slice(0, -1).trim()
  }

  s = s.replace(/\s+/g, '')
  if (s === '') {
    return null
  }

  const lastComma = s.lastIndexOf(',')
  const lastDot = s.lastIndexOf('.')

  let decimalSep = ''
  if (lastComma >= 0 && lastDot >= 0) {
    if (lastComma > lastDot) {
      decimalSep = ','
      s = s.replaceAll('.', '')
    } else {
      decimalSep = '.'
      s = s.replaceAll(',', '')
    }
  } else if (lastComma >= 0) {
    decimalSep = ','
  } else if (lastDot >= 0) {
    const afterDot = s.length - lastDot - 1
    if (afterDot === 1 || afterDot === 2) {
      decimalSep = '.'
    } else {
      s = s.replaceAll('.', '')
    }
  }

  let intPart: string
  let fracPart: string
  if (decimalSep !== '') {
    const parts = s.split(decimalSep)
    if (parts.length !== 2) {
      return null
    }
    intPart = parts[0]
    fracPart = parts[1]
  } else {
    intPart = s
    fracPart = '00'
  }

  if (intPart === '') {
    intPart = '0'
  }
  if (fracPart.length === 0) {
    fracPart = '00'
  } else if (fracPart.length === 1) {
    fracPart += '0'
  } else if (fracPart.length > 2) {
    return null
  }

  if (!/^\d+$/.test(intPart) || !/^\d+$/.test(fracPart)) {
    return null
  }

  const reais = Number(intPart)
  const centavos = Number(fracPart)
  if (!Number.isSafeInteger(reais) || !Number.isSafeInteger(centavos)) {
    return null
  }

  const total = reais * 100 + centavos
  if (!Number.isSafeInteger(total)) {
    return null
  }
  if (total === 0) {
    return 0
  }
  return negative ? -total : total
}

export function formatDateBR(iso: string): string {
  const trimmed = iso.trim()
  const match = /^(\d{4})-(\d{2})-(\d{2})/.exec(trimmed)
  if (!match) {
    return trimmed
  }
  return `${match[3]}/${match[2]}/${match[1]}`
}

export function monthNamePT(month: number): string {
  if (!Number.isInteger(month) || month < 1 || month > 12) {
    return ''
  }
  return MONTHS_PT[month - 1]
}

export function bankLabel(bank: string): string {
  const trimmed = bank.trim()
  const normalized = trimmed.toLowerCase().replace(/[\s_-]+/g, '')
  if (normalized === '') {
    return 'Outro'
  }
  if (normalized.includes('nubank') || normalized.includes('nupagamentos')) {
    return 'Nubank'
  }
  if (normalized.includes('mercadopago') || normalized.includes('mpago')) {
    return 'Mercado Pago'
  }
  if (normalized.includes('inter')) {
    return 'Inter'
  }
  if (normalized.includes('itau') || normalized.includes('itaú')) {
    return 'Itaú'
  }
  if (normalized === 'outro') {
    return 'Outro'
  }
  return trimmed.charAt(0).toUpperCase() + trimmed.slice(1)
}

export function paymentStatusLabel(status: string): string {
  switch (status) {
    case 'sem_gastos':
      return 'Sem gastos'
    case 'pendente':
      return 'Pendente'
    case 'pago_parcial':
      return 'Pago parcial'
    case 'pago':
      return 'Pago'
    default:
      return status
  }
}
