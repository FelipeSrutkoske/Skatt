import { PERSON_COLOR_PRESETS, normalizeHexColor } from '../lib/colors'

export interface ColorPickerProps {
  value: string
  onChange: (hex: string) => void
  compact?: boolean
}

export function ColorPicker({ value, onChange, compact = false }: ColorPickerProps) {
  const normalized = normalizeHexColor(value, '#212529')

  return (
    <div className="inline-flex flex-wrap items-center gap-1.5">
      {PERSON_COLOR_PRESETS.map((preset) => {
        const active = normalized === preset.hex
        return (
          <button
            key={preset.hex}
            type="button"
            title={preset.label}
            aria-label={`Cor ${preset.label}`}
            aria-pressed={active}
            onClick={(e) => {
              e.stopPropagation()
              onChange(preset.hex)
            }}
            style={{ backgroundColor: preset.hex }}
            className={[
              compact ? 'w-5 h-5' : 'w-6 h-6',
              'rounded-full transition-transform cursor-pointer border',
              active
                ? 'ring-2 ring-offset-2 ring-[#212529] border-white scale-105'
                : 'border-white/70 hover:scale-110',
            ].join(' ')}
          />
        )
      })}
      <label
        title="Escolher outra cor"
        onClick={(e) => e.stopPropagation()}
        className={[
          compact ? 'h-5 px-1.5 text-[10px]' : 'h-6 px-2 text-[11px]',
          'inline-flex items-center gap-1 rounded-full border border-[#ced4da] bg-white/85 text-[#495057] hover:text-[#212529] cursor-pointer',
        ].join(' ')}
      >
        <input
          type="color"
          aria-label="Cor personalizada"
          value={normalized}
          onChange={(e) => onChange(normalizeHexColor(e.target.value, normalized))}
          className="w-3.5 h-3.5 border-0 p-0 bg-transparent cursor-pointer"
        />
        <span>Outra</span>
      </label>
    </div>
  )
}
