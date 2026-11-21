import { describe, expect, it } from 'vitest'
import { contrastTextColor, hexToRgba, normalizeHexColor } from './colors'

describe('normalizeHexColor', () => {
  it('normaliza hex de 6 e 3 digitos', () => {
    expect(normalizeHexColor('#1D4E89')).toBe('#1d4e89')
    expect(normalizeHexColor('#abc')).toBe('#aabbcc')
    expect(normalizeHexColor('invalido', '#212529')).toBe('#212529')
  })
})

describe('contrastTextColor', () => {
  it('retorna texto claro para cores escuras e texto escuro para cores claras', () => {
    expect(contrastTextColor('#212529')).toBe('#f8f9fa')
    expect(contrastTextColor('#1d4e89')).toBe('#f8f9fa')
    expect(contrastTextColor('#f8f9fa')).toBe('#212529')
    expect(contrastTextColor('#dee2e6')).toBe('#212529')
  })
})

describe('hexToRgba', () => {
  it('converte hex para rgba corretamente', () => {
    expect(hexToRgba('#212529', 0.2)).toBe('rgba(33, 37, 41, 0.2)')
  })
})
