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
import { useEffect, useRef, useState } from 'react'
import { readChannelRoutingHealth } from '../lib/channel-routing-health'
import type { Channel } from '../types'

export const CHANNEL_HEALTH_REFRESH_MS = 30_000

type RefreshOptions = {
  snapshots: Pick<Channel, 'id' | 'routing_health'>[]
  receivedAt?: number
  paused?: boolean
  enabled?: boolean
  refetch: () => Promise<unknown>
}

// A deadline requests a new server snapshot; it never changes effective values.
export function useChannelHealthRefresh(options: RefreshOptions) {
  const { enabled, paused, refetch, snapshots, receivedAt } = options
  const [now, setNow] = useState(() => performance.now())
  const handledDeadlines = useRef(new Set<string>())
  const lastRequest = useRef(-Infinity)
  const previouslyPaused = useRef(false)

  useEffect(() => {
    const wasPaused = previouslyPaused.current
    previouslyPaused.current = Boolean(paused)
    if (enabled === false || paused) return

    let disposed = false
    let deadlineTimer: ReturnType<typeof setTimeout> | undefined
    const deadlines: Array<{ key: string; at: number }> = []
    for (const snapshot of snapshots) {
      const observation = readChannelRoutingHealth(snapshot.routing_health)
      if (observation.kind !== 'known') continue
      const health = observation.health
      let until: string | null = null
      if (
        health.status === 'penalized' &&
        health.penalty_recovery_mode === 'automatic'
      )
        until = health.penalty_until
      if (
        health.status === 'runtime_disabled' &&
        health.disable_recovery_mode === 'probe_then_automatic'
      )
        until = health.disabled_until
      if (
        receivedAt === undefined ||
        !until ||
        Date.parse(until) <= Date.parse(health.evaluated_at)
      )
        continue
      deadlines.push({
        key: `${snapshot.id}:${receivedAt}:${until}`,
        at: receivedAt + Date.parse(until) - Date.parse(health.evaluated_at),
      })
    }
    const currentKeys = new Set(deadlines.map((deadline) => deadline.key))
    handledDeadlines.current = new Set(
      [...handledDeadlines.current].filter((key) => currentKeys.has(key))
    )

    const visible = () => document.visibilityState !== 'hidden'
    const refresh = (deduplicateForeground = false) => {
      if (disposed || !visible()) return
      const timestamp = performance.now()
      setNow(timestamp)
      // visibilitychange and focus can describe the same foreground transition.
      if (deduplicateForeground && timestamp - lastRequest.current < 250) return
      lastRequest.current = timestamp
      for (const deadline of deadlines) {
        if (deadline.at <= timestamp) handledDeadlines.current.add(deadline.key)
      }
      void refetch().catch(() => undefined)
    }
    const scheduleDeadline = () => {
      if (deadlineTimer) clearTimeout(deadlineTimer)
      if (!visible()) return
      const next = deadlines
        .filter((deadline) => !handledDeadlines.current.has(deadline.key))
        .sort((a, b) => a.at - b.at)[0]
      if (!next) return
      deadlineTimer = setTimeout(
        () => {
          if (!visible()) return
          // Timers may run slightly early. Only a reached deadline requests a
          // confirmation; otherwise reschedule without consuming this snapshot.
          if (performance.now() < next.at) {
            scheduleDeadline()
            return
          }
          refresh()
          scheduleDeadline()
        },
        Math.min(
          Math.max(Math.ceil(next.at - performance.now()), 0),
          2_147_483_647
        )
      )
    }
    const foreground = () => {
      refresh(true)
      scheduleDeadline()
    }
    const interval = setInterval(refresh, CHANNEL_HEALTH_REFRESH_MS)
    document.addEventListener('visibilitychange', foreground)
    window.addEventListener('focus', foreground)
    if (wasPaused) refresh()
    scheduleDeadline()
    return () => {
      disposed = true
      clearInterval(interval)
      clearTimeout(deadlineTimer)
      document.removeEventListener('visibilitychange', foreground)
      window.removeEventListener('focus', foreground)
    }
  }, [enabled, paused, refetch, snapshots, receivedAt])
  return now
}
