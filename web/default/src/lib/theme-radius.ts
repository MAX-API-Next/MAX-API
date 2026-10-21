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
import { useMemo, useSyncExternalStore } from 'react'

export function resolveThemeRadiusPx(
  cssVariable = '--radius-md'
): number | undefined {
  if (typeof document === 'undefined') return undefined

  const probe = document.createElement('div')
  probe.style.borderRadius = `var(${cssVariable})`
  probe.style.pointerEvents = 'none'
  probe.style.position = 'absolute'
  probe.style.visibility = 'hidden'

  document.documentElement.appendChild(probe)
  const resolvedRadius = getComputedStyle(probe).borderTopLeftRadius
  probe.remove()

  const parsedRadius = Number.parseFloat(resolvedRadius)
  return Number.isFinite(parsedRadius) ? parsedRadius : undefined
}

function createRadiusStore(options: {
  cssVariable: string
  refreshKey?: string
}) {
  let radius: number | undefined
  const listeners = new Set<() => void>()
  const measure = () => {
    const next = resolveThemeRadiusPx(options.cssVariable)
    if (Object.is(next, radius)) return
    radius = next
    for (const notify of listeners) notify()
  }
  return {
    getSnapshot: () => radius,
    getServerSnapshot: () => undefined,
    subscribe: (notify: () => void) => {
      listeners.add(notify)
      const observer = new MutationObserver(measure)
      observer.observe(document.documentElement, {
        attributes: true,
        attributeFilter: ['class', 'style', 'data-theme'],
      })
      window.addEventListener('resize', measure)
      measure()
      return () => {
        listeners.delete(notify)
        observer.disconnect()
        window.removeEventListener('resize', measure)
      }
    },
  }
}

export function useThemeRadiusPx(
  cssVariable = '--radius-md',
  refreshKey?: string
): number | undefined {
  // A changed theme key creates a fresh measurement after the DOM commits.
  const store = useMemo(
    () => createRadiusStore({ cssVariable, refreshKey }),
    [cssVariable, refreshKey]
  )
  return useSyncExternalStore(
    store.subscribe,
    store.getSnapshot,
    store.getServerSnapshot
  )
}
