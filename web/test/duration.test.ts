import { expect, test } from 'bun:test'
import { durationUnits, splitDuration, joinDuration, formatDuration, isDurationUnit } from '../src/lib/duration'

test('only time units are durations', () => {
  expect(isDurationUnit('milliseconds')).toBe(true)
  expect(isDurationUnit('seconds')).toBe(true)
  expect(isDurationUnit('bytes')).toBe(false)
  expect(isDurationUnit(undefined)).toBe(false)
})
test('the units offered never go below the setting unit', () => {
  expect(durationUnits('milliseconds')).toEqual(['ms', 's', 'min', 'h'])
  expect(durationUnits('seconds')).toEqual(['s', 'min', 'h'])
  expect(durationUnits('minutes')).toEqual(['min', 'h'])
})
test('a value is shown in the largest unit that divides it', () => {
  expect(splitDuration(1000, 'milliseconds')).toEqual({ amount: 1, unit: 's' })
  expect(splitDuration(1500, 'milliseconds')).toEqual({ amount: 1500, unit: 'ms' })
  expect(splitDuration(7200, 'seconds')).toEqual({ amount: 2, unit: 'h' })
  expect(splitDuration(-30, 'minutes')).toEqual({ amount: -30, unit: 'min' })
  expect(splitDuration(undefined, 'seconds')).toEqual({ amount: undefined, unit: 's' })
})
test('an amount in a unit is stored in the setting unit', () => {
  expect(joinDuration(5, 'min', 'milliseconds')).toBe(300000)
  expect(joinDuration(2, 'h', 'seconds')).toBe(7200)
  expect(joinDuration(undefined, 'min', 'seconds')).toBeUndefined()
})
test('durations read naturally', () => {
  expect(formatDuration(1000, 'milliseconds')).toBe('1 s')
  expect(formatDuration(90, 'seconds')).toBe('90 s')
  expect(formatDuration(300000, 'milliseconds')).toBe('5 min')
})
