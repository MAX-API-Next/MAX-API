/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues
*/
import { useState, useEffect, useRef } from 'react'

/**
 * Ensures a loading skeleton is shown for at least `minimumTime` ms
 * to prevent flickering when data loads too quickly.
 */
export function useMinimumLoadingTime(
  loading: boolean,
  minimumTime = 1000
): boolean {
  const [period, setPeriod] = useState(() => ({
    loading,
    cycle: loading ? 1 : 0,
    elapsed: !loading,
  }))
  const started = useRef({ cycle: 0, time: 0 })
  if (period.loading !== loading) {
    setPeriod({
      loading,
      cycle: loading ? period.cycle + 1 : period.cycle,
      elapsed: loading ? false : period.elapsed,
    })
  }
  const cycle = period.cycle
  useEffect(() => {
    if (cycle === 0) return
    if (started.current.cycle !== cycle)
      started.current = { cycle, time: Date.now() }
    const remaining = Math.max(
      0,
      minimumTime - (Date.now() - started.current.time)
    )
    const timer = setTimeout(() => {
      setPeriod((previous) =>
        previous.cycle === cycle ? { ...previous, elapsed: true } : previous
      )
    }, remaining)
    return () => clearTimeout(timer)
  }, [cycle, minimumTime])
  return loading || (minimumTime > 0 && cycle > 0 && !period.elapsed)
}
