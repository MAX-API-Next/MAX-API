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
import { useCallback, useMemo, useEffect } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  getChannelRoutingSnapshot,
  toChannelRoutingSnapshot,
  type ChannelReadObservation,
  type ChannelRoutingSnapshot,
} from '../api'
import {
  useChannelHealthRefresh,
  CHANNEL_HEALTH_REFRESH_MS,
} from '../hooks/use-channel-health-refresh'
import { channelsQueryKeys } from '../lib/channel-actions'
import type { Channel } from '../types'
import { ChannelHealthSnapshotContext } from './channel-health-snapshot-context'
import { ChannelRoutingHealthDetails } from './channel-routing-health'

// React Query uses this for BOTH setQueryData (details) and query completion
// (polling). Ordering only at the seeding effect leaves the reverse race open.
function acceptSnapshot(previous: unknown, incoming: unknown) {
  const current = previous as ChannelRoutingSnapshot | undefined
  const next = incoming as ChannelRoutingSnapshot
  if (
    current?.id === next.id &&
    current.observation?.requestOrder >= next.observation.requestOrder
  )
    return current
  return next
}

export function ChannelRoutingHealthCard(props: {
  initialChannel: Channel
  initialUpdatedAt: number
  initialObservation: ChannelReadObservation
}) {
  const { t } = useTranslation()
  const id = props.initialChannel.id
  const queryClient = useQueryClient()
  const query = useQuery({
    queryKey: channelsQueryKeys.runtime(id),
    queryFn: ({ signal }) => getChannelRoutingSnapshot(id, signal),
    initialData: () =>
      toChannelRoutingSnapshot(props.initialChannel, props.initialObservation),
    initialDataUpdatedAt: props.initialUpdatedAt,
    structuralSharing: acceptSnapshot,
    staleTime: CHANNEL_HEALTH_REFRESH_MS,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    retry: false,
  })
  useEffect(() => {
    const key = channelsQueryKeys.runtime(id)
    const cached = queryClient.getQueryData<ChannelRoutingSnapshot>(key)
    const incoming = toChannelRoutingSnapshot(
      props.initialChannel,
      props.initialObservation
    )
    if (acceptSnapshot(cached, incoming) === incoming) {
      queryClient.setQueryData(key, incoming, {
        updatedAt: props.initialUpdatedAt,
      })
    }
  }, [
    id,
    props.initialChannel,
    props.initialObservation,
    props.initialUpdatedAt,
    queryClient,
  ])
  const { refetch } = query
  const refresh = useCallback(
    () => refetch({ cancelRefetch: false }),
    [refetch]
  )
  const snapshots = useMemo(() => [query.data], [query.data])
  const receivedAt = query.data.observation.receivedAt
  const now = useChannelHealthRefresh({
    snapshots,
    receivedAt,
    refetch: refresh,
  })
  return (
    <section
      aria-label={t('Routing state snapshot')}
      className='space-y-3 rounded-md border p-3'
    >
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <h4 className='text-sm font-medium'>{t('Routing state snapshot')}</h4>
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={query.isFetching}
          onClick={() => void refresh()}
        >
          {t('Refresh runtime state')}
        </Button>
      </div>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Based on saved configuration. Unsaved edits are not reflected here.'
        )}
      </p>
      {query.isError && (
        <p role='status' className='text-destructive text-xs'>
          {t('Runtime refresh failed. The previous snapshot may be outdated.')}
        </p>
      )}
      <ChannelHealthSnapshotContext
        value={{ now, receivedAt, placeholder: false }}
      >
        <ChannelRoutingHealthDetails
          routingHealth={query.data.routing_health}
        />
      </ChannelHealthSnapshotContext>
      <Link
        to='/smart-ops/channel-health'
        className='text-primary text-xs underline'
      >
        {t('Open channel health settings')}
      </Link>
    </section>
  )
}
