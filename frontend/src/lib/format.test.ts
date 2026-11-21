import { describe, expect, it } from 'vitest'
import {
  bankLabel,
  formatBRL,
  formatDateBR,
  monthNamePT,
  parseBRLInput,
  paymentStatusLabel,
} from './format'

describe('formatBRL', () => {
  it('formata valores positivos, zero e milhares', () => {
    expect(formatBRL(0)).toBe('R$ 0,00')
    expect(formatBRL(-0)).toBe('R$ 0,00')
    expect(formatBRL(5)).toBe('R$ 0,05')
    expect(formatBRL(5000)).toBe('R$ 50,00')
    expect(formatBRL(123456)).toBe('R$ 1.234,56')
    expect(formatBRL(100000000)).toBe('R$ 1.000.000,00')
  })

  it('formata valores negativos corretamente', () => {
    expect(formatBRL(-4590)).toBe('-R$ 45,90')
    expect(formatBRL(-5)).toBe('-R$ 0,05')
    expect(formatBRL(-123456)).toBe('-R$ 1.234,56')
  })
})

describe('parseBRLInput', () => {
  it('converte entradas brasileiras e internacionais para centavos', () => {
    expect(parseBRLInput('50,00')).toBe(5000)
    expect(parseBRLInput('50')).toBe(5000)
    expect(parseBRLInput('50,5')).toBe(5050)
    expect(parseBRLInput('1.234,56')).toBe(123456)
    expect(parseBRLInput('R$ 1.234,56')).toBe(123456)
    expect(parseBRLInput('120.50')).toBe(12050)
    expect(parseBRLInput('1,234.56')).toBe(123456)
    expect(parseBRLInput('1.000')).toBe(100000)
    expect(parseBRLInput('0,00')).toBe(0)
    expect(parseBRLInput('-0,00')).toBe(0)
  })

  it('converte valores negativos em formatos variados', () => {
    expect(parseBRLInput('-10,50')).toBe(-1050)
    expect(parseBRLInput('-R$ 45,90')).toBe(-4590)
    expect(parseBRLInput('R$ -45,90')).toBe(-4590)
    expect(parseBRLInput('45,90-')).toBe(-4590)
  })

  it('retorna null para entradas invalidas', () => {
    expect(parseBRLInput('')).toBeNull()
    expect(parseBRLInput('   ')).toBeNull()
    expect(parseBRLInput('abc')).toBeNull()
    expect(parseBRLInput('R$')).toBeNull()
    expect(parseBRLInput('12,345')).toBeNull()
    expect(parseBRLInput('12,34,56')).toBeNull()
    expect(parseBRLInput('12,ab')).toBeNull()
  })
})

describe('formatDateBR', () => {
  it('converte datas ISO para formato brasileiro', () => {
    expect(formatDateBR('2025-04-10')).toBe('10/04/2025')
    expect(formatDateBR('2026-11-20T14:30:00Z')).toBe('20/11/2026')
    expect(formatDateBR('2026-01-05 08:15:00')).toBe('05/01/2026')
    expect(formatDateBR('')).toBe('')
    expect(formatDateBR('invalid')).toBe('invalid')
  })
})

describe('monthNamePT', () => {
  it('retorna nomes dos meses em portugues', () => {
    expect(monthNamePT(1)).toBe('Janeiro')
    expect(monthNamePT(3)).toBe('Março')
    expect(monthNamePT(12)).toBe('Dezembro')
    expect(monthNamePT(0)).toBe('')
    expect(monthNamePT(13)).toBe('')
  })
})

describe('bankLabel', () => {
  it('retorna rotulos padronizados para bancos conhecidos e outros', () => {
    expect(bankLabel('nubank')).toBe('Nubank')
    expect(bankLabel('mercadopago')).toBe('Mercado Pago')
    expect(bankLabel('mercado_pago')).toBe('Mercado Pago')
    expect(bankLabel('inter')).toBe('Inter')
    expect(bankLabel('itau')).toBe('Itaú')
    expect(bankLabel('outro')).toBe('Outro')
    expect(bankLabel('')).toBe('Outro')
    expect(bankLabel('bradesco')).toBe('Bradesco')
  })
})

describe('paymentStatusLabel', () => {
  it('traduz status de pagamento corretamente', () => {
    expect(paymentStatusLabel('sem_gastos')).toBe('Sem gastos')
    expect(paymentStatusLabel('pendente')).toBe('Pendente')
    expect(paymentStatusLabel('pago_parcial')).toBe('Pago parcial')
    expect(paymentStatusLabel('pago')).toBe('Pago')
    expect(paymentStatusLabel('desconhecido')).toBe('desconhecido')
  })
})
