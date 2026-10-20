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
import { useCallback, useLayoutEffect, useRef, useState } from 'react'
import { useAuthStore } from '@/stores/auth-store'

/** Ignore obsolete reads; this does not cancel a request or undo a server mutation. */
export function useReadScope(scopeKey?: unknown) {
  const sessionRevision = useAuthStore((state) => state.sessionRevision)
  const revision = useRef(0)
  const mounted = useRef(false)
  const [owner, setOwner] = useState(() => ({ key: scopeKey, sessionRevision }))
  let scope = owner
  if (
    !Object.is(owner.key, scopeKey) ||
    owner.sessionRevision !== sessionRevision
  ) {
    scope = { key: scopeKey, sessionRevision }
    setOwner(scope)
  }
  const activeScope = useRef(scope)
  useLayoutEffect(() => {
    activeScope.current = scope
    mounted.current = true
    revision.current += 1
    return () => {
      mounted.current = false
      revision.current += 1
    }
  }, [scope])
  return useCallback(() => {
    if (
      !mounted.current ||
      activeScope.current !== scope ||
      useAuthStore.getState().sessionRevision !== scope.sessionRevision
    )
      return () => false
    const current = ++revision.current
    return () =>
      mounted.current &&
      revision.current === current &&
      activeScope.current === scope &&
      useAuthStore.getState().sessionRevision === scope.sessionRevision
  }, [scope])
}
