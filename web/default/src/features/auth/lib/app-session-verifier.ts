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
import { useAuthStore } from '@/stores/auth-store'
import { getSelf } from '@/lib/api'
import { isStaleAuthResponse } from '@/lib/auth-errors'
import { createSessionVerifier } from './session-verifier'

async function getSelfForVerification() {
  while (true) {
    const starting = useAuthStore.getState()
    try {
      const response = await getSelf()
      const current = useAuthStore.getState()
      // Authenticated identities include the revision below. When both are
      // anonymous, the verifier cannot see a reset, so refresh this pure read.
      if (
        current.sessionRevision === starting.sessionRevision ||
        starting.auth.user ||
        current.auth.user
      )
        return response
    } catch (error) {
      const current = useAuthStore.getState()
      if (
        isStaleAuthResponse(error) &&
        !starting.auth.user &&
        !current.auth.user
      )
        continue
      throw error
    }
  }
}

export const appSessionVerifier = createSessionVerifier({
  getAuthState: () => useAuthStore.getState().auth,
  getSelf: getSelfForVerification,
  getUserIdentity: (user) =>
    `${useAuthStore.getState().sessionRevision}:${user.id}`,
  isUnauthorized: (error: unknown) =>
    (error as { response?: { status?: number } })?.response?.status === 401,
})
