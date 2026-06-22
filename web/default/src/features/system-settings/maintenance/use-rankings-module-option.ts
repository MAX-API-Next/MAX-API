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
import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { getSystemOptions } from '../api'
import { useUpdateOption } from '../hooks/use-update-option'
import type { SystemOptionsResponse } from '../types'
import {
  parseHeaderNavAccessModule,
  parseHeaderNavModules,
  serializeHeaderNavAccessModule,
  type HeaderNavAccessConfig,
} from './config'

const getOptionValue = (
  data: SystemOptionsResponse | undefined,
  key: string
): string => data?.data.find((option) => option.key === key)?.value ?? ''

type RankingsModuleUpdater = (
  current: HeaderNavAccessConfig
) => HeaderNavAccessConfig

export function useRankingsModuleOption() {
  const queryClient = useQueryClient()
  const updateOption = useUpdateOption()
  const [isMerging, setIsMerging] = useState(false)

  const updateRankingsModule = async (updater: RankingsModuleUpdater) => {
    setIsMerging(true)
    try {
      const latestOptions = await queryClient.fetchQuery({
        queryKey: ['system-options'],
        queryFn: getSystemOptions,
        staleTime: 0,
      })
      const headerNavConfig = parseHeaderNavModules(
        getOptionValue(latestOptions, 'HeaderNavModules')
      )
      const currentConfig = parseHeaderNavAccessModule(
        getOptionValue(latestOptions, 'RankingsModule'),
        headerNavConfig.rankings
      )
      const currentSerialized = serializeHeaderNavAccessModule(currentConfig)
      const nextConfig = updater(currentConfig)
      const nextSerialized = serializeHeaderNavAccessModule(nextConfig)

      if (nextSerialized === currentSerialized) {
        return
      }

      await updateOption.mutateAsync({
        key: 'RankingsModule',
        value: nextSerialized,
      })
    } finally {
      setIsMerging(false)
    }
  }

  return {
    isPending: isMerging || updateOption.isPending,
    updateRankingsModule,
  }
}
