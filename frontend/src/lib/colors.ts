export interface PersonColorPreset {
  hex: string
  label: string
}

export const PERSON_COLOR_PRESETS: PersonColorPreset[] = [
  { hex: '#212529', label: 'Grafite' },
  { hex: '#1d4e89', label: 'Azul petróleo' },
  { hex: '#2d6a4f', label: 'Verde musgo' },
  { hex: '#b5452e', label: 'Terracota' },
  { hex: '#6d3b7d', label: 'Ameixa' },
  { hex: '#a66315', label: 'Âmbar' },
  { hex: '#0f6d7a', label: 'Ciano escuro' },
  { hex: '#9b2242', label: 'Carmim' },
  { hex: '#5f6f3a', label: 'Oliva' },
  { hex: '#6c757d', label: 'Cinza neutro' },
]

export function normalizeHexColor(raw: string | undefined | null, fallback = '#495057'): string {
  const trimmed = (raw ?? '').trim()
  if (/^#[0-9a-fA-F]{6}$/.test(trimmed)) {
    return trimmed.toLowerCase()
  }
  if (/^#[0-9a-fA-F]{3}$/.test(trimmed)) {
    const r = trimmed[1]
    const g = trimmed[2]
    const b = trimmed[3]
    return `#${r}${r}${g}${g}${b}${b}`.toLowerCase()
  }
  return fallback
}

export function contrastTextColor(hex: string): '#f8f9fa' | '#212529' {
  const normalized = normalizeHexColor(hex, '#495057')
  const r = parseInt(normalized.slice(1, 3), 16)
  const g = parseInt(normalized.slice(3, 5), 16)
  const b = parseInt(normalized.slice(5, 7), 16)
  const luminance = (0.299 * r + 0.587 * g + 0.114 * b) / 255
  return luminance > 0.62 ? '#212529' : '#f8f9fa'
}

export function hexToRgba(hex: string, alpha: number): string {
  const normalized = normalizeHexColor(hex, '#495057')
  const r = parseInt(normalized.slice(1, 3), 16)
  const g = parseInt(normalized.slice(3, 5), 16)
  const b = parseInt(normalized.slice(5, 7), 16)
  return `rgba(${r}, ${g}, ${b}, ${alpha})`
}
