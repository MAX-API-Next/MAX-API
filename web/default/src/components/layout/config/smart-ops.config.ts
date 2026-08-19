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
import { type TFunction } from 'i18next'
import { Bot, Gauge, ServerCog } from 'lucide-react'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import type { NavGroup, SidebarView } from '../types'

export function buildSmartOpsNavGroups(
  t: TFunction,
  userRole: number | undefined
): NavGroup[] {
  const isSuperAdmin = userRole === ROLE.SUPER_ADMIN
  const items = [
    {
      title: t('Channel performance'),
      url: '/smart-ops/channel-performance' as const,
      icon: Gauge,
    },
    {
      title: t('Model performance'),
      url: '/smart-ops/model-performance' as const,
      icon: Bot,
    },
    ...(isSuperAdmin
      ? [
          {
            title: t('System Info'),
            url: '/smart-ops/system-info' as const,
            icon: ServerCog,
          },
        ]
      : []),
  ]

  return [
    {
      id: 'smart-operations',
      title: t('Smart Operations Center'),
      items,
    },
  ]
}

function getSmartOpsNavGroups(t: TFunction): NavGroup[] {
  return buildSmartOpsNavGroups(t, useAuthStore.getState().auth.user?.role)
}

export const SMART_OPS_VIEW: SidebarView = {
  id: 'smart-ops',
  pathPattern: /^\/smart-ops(\/|$)/,
  parent: {
    to: '/dashboard/overview',
    label: 'Back to Dashboard',
  },
  getNavGroups: getSmartOpsNavGroups,
}
