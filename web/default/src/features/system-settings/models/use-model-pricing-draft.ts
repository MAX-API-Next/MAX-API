// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import { useEffect, useMemo, useSyncExternalStore } from 'react'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import {
  modelPricingTransport,
  type ModelPricingTransport,
} from './model-pricing-api'
import { ModelPricingDraftSession } from './model-pricing-draft'

// React adapter for the existing editor's next integration step. Each auth
// revision owns a fresh draft session; network cancellation never undoes PATCH.
export function useModelPricingDraft(
  model: string,
  transport: ModelPricingTransport = modelPricingTransport,
  active = true
) {
  const revision = useAuthStore((state) => state.sessionRevision)
  const canEdit = useAuthStore(
    (state) => state.auth.user?.role === ROLE.SUPER_ADMIN
  )
  const session = useMemo(
    () =>
      new ModelPricingDraftSession(
        transport,
        () =>
          canEdit &&
          useAuthStore.getState().sessionRevision === revision &&
          useAuthStore.getState().auth.user?.role === ROLE.SUPER_ADMIN
      ),
    [transport, revision, canEdit]
  )
  const snapshot = useSyncExternalStore(
    session.subscribe,
    session.getSnapshot,
    session.getSnapshot
  )
  useEffect(() => {
    if (!canEdit || !model || !active) {
      session.suspend()
      return
    }
    session.resume()
    session.select(model)
    void session.refresh()
    return () => session.suspend()
  }, [session, model, canEdit, active])
  // Do not flash the previous model's values before the effect switches identity.
  return {
    session,
    canEdit,
    blockedModels: snapshot.blockedModels ?? [],
    cacheWarningModels: snapshot.cacheWarningModels ?? [],
    document:
      canEdit && snapshot.model === model ? snapshot.document : undefined,
  }
}
