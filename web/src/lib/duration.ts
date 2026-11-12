// Time settings are stored in the unit the node declares (milliseconds,
// seconds, minutes); the editor shows and accepts friendlier units.
export type DurationUnit = 'ms' | 's' | 'min' | 'h'
const SIZE: Record<DurationUnit, number> = { ms: 1, s: 1000, min: 60_000, h: 3_600_000 }
const BASE: Record<string, DurationUnit> = { milliseconds: 'ms', seconds: 's', minutes: 'min', hours: 'h' }
const ORDER: DurationUnit[] = ['ms', 's', 'min', 'h']

export function isDurationUnit(unit: string | undefined): boolean {
  return !!unit && unit in BASE
}

export function durationUnits(base: string): DurationUnit[] {
  return ORDER.slice(ORDER.indexOf(BASE[base] ?? 'ms'))
}

export function splitDuration(value: number | undefined, base: string): { amount: number | undefined; unit: DurationUnit } {
  const units = durationUnits(base)
  if (value === undefined || !Number.isFinite(value)) return { amount: undefined, unit: units[0] }
  const ms = value * SIZE[units[0]]
  for (const unit of [...units].reverse()) {
    if (ms !== 0 && Number.isInteger(ms / SIZE[unit])) return { amount: ms / SIZE[unit], unit }
  }
  return { amount: value, unit: units[0] }
}

export function joinDuration(amount: number | undefined, unit: DurationUnit, base: string): number | undefined {
  if (amount === undefined || !Number.isFinite(amount)) return undefined
  const value = amount * SIZE[unit] / SIZE[durationUnits(base)[0]]
  return Math.round(value * 1000) / 1000
}

export function formatDuration(value: number, base: string): string {
  const { amount, unit } = splitDuration(value, base)
  return `${amount} ${unit}`
}
