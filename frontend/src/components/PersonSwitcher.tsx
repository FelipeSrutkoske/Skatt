import { contrastTextColor, hexToRgba, normalizeHexColor } from '../lib/colors'
import type { Person, TransactionSplit } from '../types/api'

export interface PersonSwitcherProps {
  people: Person[]
  selectedPersonId: number | null
  splits?: TransactionSplit[]
  suggestedPersonId?: number | null
  disabled?: boolean
  onSelect: (personId: number | null) => void
  onOpenSplit?: () => void
}

export function PersonSwitcher({
  people,
  selectedPersonId,
  splits,
  suggestedPersonId = null,
  disabled = false,
  onSelect,
  onOpenSplit,
}: PersonSwitcherProps) {
  const isMultiSplit = (splits?.length ?? 0) >= 2

  return (
    <div className="flex flex-wrap items-center gap-1">
      <div
        role="group"
        aria-label="Selecionar responsável"
        className="inline-flex flex-wrap items-center gap-1 rounded-lg bg-[#e9ecef]/85 p-0.5 border border-[#ced4da]/80"
      >
        {people.map((person) => {
          const color = normalizeHexColor(person.color, '#495057')
          const isSelected = !isMultiSplit && selectedPersonId === person.id
          const isSuggested =
            !isMultiSplit && selectedPersonId === null && suggestedPersonId === person.id

          const buttonStyle = isSelected
            ? {
                backgroundColor: color,
                color: contrastTextColor(color),
                borderColor: color,
              }
            : isSuggested
              ? {
                  backgroundColor: hexToRgba(color, 0.1),
                  color: '#212529',
                  borderColor: hexToRgba(color, 0.6),
                }
              : undefined

          return (
            <button
              key={person.id}
              type="button"
              disabled={disabled}
              onClick={() => onSelect(isSelected ? null : person.id)}
              title={
                isSelected
                  ? `${person.name} (clique novamente para limpar)`
                  : isSuggested
                    ? `${person.name} (sugerido pelo histórico)`
                    : `Atribuir para ${person.name}`
              }
              style={buttonStyle}
              className={[
                'inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-xs font-medium border transition-all cursor-pointer disabled:opacity-45 disabled:cursor-not-allowed',
                isSelected
                  ? 'shadow-xs'
                  : isSuggested
                    ? 'border-dashed'
                    : 'bg-white/80 text-[#495057] border-transparent hover:bg-white hover:text-[#212529]',
              ].join(' ')}
            >
              <span
                className="w-2 h-2 rounded-full shrink-0"
                style={{
                  backgroundColor: isSelected ? contrastTextColor(color) : color,
                  opacity: isSelected ? 0.9 : 1,
                }}
              />
              <span className="max-w-[96px] truncate">{person.name}</span>
            </button>
          )
        })}
      </div>

      {isMultiSplit && onOpenSplit && (
        <button
          type="button"
          disabled={disabled}
          onClick={onOpenSplit}
          className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-[11px] font-medium bg-[#343a40] text-[#f8f9fa] hover:bg-[#212529] transition-colors cursor-pointer disabled:opacity-45"
        >
          <span>Dividido ({splits?.length})</span>
        </button>
      )}
    </div>
  )
}
