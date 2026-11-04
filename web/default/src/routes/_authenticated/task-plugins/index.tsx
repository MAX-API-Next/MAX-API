// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import { createFileRoute, redirect } from '@tanstack/react-router'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { TaskPlugins } from '@/features/task-plugins'

export const Route = createFileRoute('/_authenticated/task-plugins/')({
  beforeLoad: () => {
    if (useAuthStore.getState().auth.user?.role !== ROLE.SUPER_ADMIN)
      throw redirect({ to: '/403' })
  },
  component: TaskPlugins,
})
