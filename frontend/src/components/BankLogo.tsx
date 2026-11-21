import interLogo from '../assets/banks/inter.svg'
import itauLogo from '../assets/banks/itau.svg'
import mercadopagoLogo from '../assets/banks/mercadopago.svg'
import nubankLogo from '../assets/banks/nubank.svg'
import { bankLabel } from '../lib/format'

export interface BankLogoProps {
  bank: string
  size?: number
}

export const DEFAULT_BANK_CODES = ['nubank', 'mercadopago', 'inter', 'itau'] as const

const BANK_ASSETS: Record<string, { src: string; pad: string }> = {
  nubank: { src: nubankLogo, pad: 'p-[2px]' },
  mercadopago: { src: mercadopagoLogo, pad: 'p-[2px]' },
  inter: { src: interLogo, pad: 'px-[2px] py-[4px]' },
  itau: { src: itauLogo, pad: 'p-0' },
}

export function BankLogo({ bank, size = 18 }: BankLogoProps) {
  const code = bank.trim().toLowerCase()
  const asset = BANK_ASSETS[code]

  if (asset) {
    return (
      <span
        style={{ width: size, height: size }}
        className={[
          'inline-flex items-center justify-center shrink-0 rounded-[4px] bg-white/95 overflow-hidden border border-[#dee2e6]',
          asset.pad,
        ].join(' ')}
      >
        <img
          src={asset.src}
          alt={bankLabel(code)}
          className="w-full h-full object-contain block"
          draggable={false}
        />
      </span>
    )
  }

  const initial = (bankLabel(code).slice(0, 2) || 'B').toUpperCase()
  return (
    <span
      style={{ width: size, height: size, fontSize: Math.max(8, Math.round(size * 0.44)) }}
      className="inline-flex items-center justify-center shrink-0 rounded-[4px] bg-[#495057] text-[#f8f9fa] font-semibold leading-none"
    >
      {initial}
    </span>
  )
}

export interface BankSwitcherProps {
  value: string
  banks: string[]
  includeAll?: boolean
  allLabel?: string
  disabled?: boolean
  onChange: (bank: string) => void
}

export function BankSwitcher({
  value,
  banks,
  includeAll = true,
  allLabel = 'Todos',
  disabled = false,
  onChange,
}: BankSwitcherProps) {
  return (
    <div
      role="group"
      aria-label="Filtrar por banco"
      className="glass-switch inline-flex flex-wrap items-center gap-1 rounded-xl p-1"
    >
      {includeAll && (
        <button
          type="button"
          disabled={disabled}
          onClick={() => onChange('')}
          className={[
            'inline-flex items-center gap-1.5 rounded-lg px-2.5 py-1 text-xs font-medium transition-all cursor-pointer disabled:opacity-50',
            value === ''
              ? 'bg-[#212529] text-[#f8f9fa] shadow-xs'
              : 'text-[#343a40] hover:text-[#212529] hover:bg-white/60',
          ].join(' ')}
        >
          <span>{allLabel}</span>
        </button>
      )}
      {banks.map((bank) => {
        const active = value === bank
        return (
          <button
            key={bank}
            type="button"
            disabled={disabled}
            onClick={() => onChange(bank)}
            className={[
              'inline-flex items-center gap-1.5 rounded-lg px-2.5 py-1 text-xs font-medium transition-all cursor-pointer disabled:opacity-50',
              active
                ? 'bg-[#212529] text-[#f8f9fa] shadow-xs'
                : 'text-[#343a40] hover:text-[#212529] hover:bg-white/60',
            ].join(' ')}
          >
            <BankLogo bank={bank} size={17} />
            <span>{bankLabel(bank)}</span>
          </button>
        )
      })}
    </div>
  )
}
