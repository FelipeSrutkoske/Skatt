import { describe, expect, it } from 'vitest'
import { splitEvenlyCents, validateSplits } from './SplitModal'

describe('splitEvenlyCents', () => {
  it('divide valores exatos igualmente sem residual', () => {
    expect(splitEvenlyCents(12000, 3)).toEqual([4000, 4000, 4000])
  })

  it('distribui centavo residual deterministicamente para valores positivos', () => {
    const parts = splitEvenlyCents(10000, 3)
    expect(parts).toEqual([3334, 3333, 3333])
    expect(parts.reduce((a, b) => a + b, 0)).toBe(10000)
  })

  it('distribui centavo residual deterministicamente para estornos negativos', () => {
    const parts = splitEvenlyCents(-10000, 3)
    expect(parts).toEqual([-3334, -3333, -3333])
    expect(parts.reduce((a, b) => a + b, 0)).toBe(-10000)
  })

  it('retorna lista vazia para contagem invalida', () => {
    expect(splitEvenlyCents(10000, 0)).toEqual([])
    expect(splitEvenlyCents(10000, -2)).toEqual([])
  })
})

describe('validateSplits', () => {
  it('aceita soma exata com pessoas distintas', () => {
    const result = validateSplits(12000, [
      { personId: 1, amountCents: 5000 },
      { personId: 2, amountCents: 7000 },
    ])
    expect(result.valid).toBe(true)
    expect(result.remainingCents).toBe(0)
    expect(result.error).toBe('')
  })

  it('aceita estorno negativo com soma exata', () => {
    const result = validateSplits(-4500, [
      { personId: 1, amountCents: -2000 },
      { personId: 2, amountCents: -2500 },
    ])
    expect(result.valid).toBe(true)
    expect(result.remainingCents).toBe(0)
  })

  it('rejeita soma divergente em 1 centavo', () => {
    const result = validateSplits(12000, [
      { personId: 1, amountCents: 5000 },
      { personId: 2, amountCents: 6999 },
    ])
    expect(result.valid).toBe(false)
    expect(result.remainingCents).toBe(1)
    expect(result.error).not.toBe('')
  })

  it('rejeita pessoa duplicada mesmo quando a soma fecha', () => {
    const result = validateSplits(12000, [
      { personId: 1, amountCents: 6000 },
      { personId: 1, amountCents: 6000 },
    ])
    expect(result.valid).toBe(false)
    expect(result.remainingCents).toBe(0)
    expect(result.error).not.toBe('')
  })

  it('rejeita lista vazia ou pessoa nao selecionada', () => {
    expect(validateSplits(5000, []).valid).toBe(false)
    expect(validateSplits(5000, [{ personId: 0, amountCents: 5000 }]).valid).toBe(false)
  })
})
