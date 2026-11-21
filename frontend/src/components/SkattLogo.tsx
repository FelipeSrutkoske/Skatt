export interface SkattLogoProps {
  size?: number
}

export function SkattLogo({ size = 24 }: SkattLogoProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 32 32"
      fill="none"
      aria-hidden="true"
      className="shrink-0"
    >
      <rect width="32" height="32" rx="8" fill="#212529" />
      <path
        d="M11 23V9.5L21 6.5M11 15.5L20 12.8"
        stroke="#f8f9fa"
        strokeWidth="2.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <circle cx="20.5" cy="21" r="2.2" fill="#adb5bd" />
    </svg>
  )
}
