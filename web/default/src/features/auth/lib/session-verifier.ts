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
type SessionAuthState<TUser> = {
  user: TUser | null
  setUser: (user: TUser | null) => void
  reset: () => void
}

type SessionResponse<TUser> = {
  success?: boolean
  data?: TUser
}

type SessionVerifierDependencies<TUser> = {
  getAuthState: () => SessionAuthState<TUser>
  getSelf: () => Promise<SessionResponse<TUser>>
  getUserIdentity: (user: TUser) => string | number
  isUnauthorized: (error: unknown) => boolean
  now?: () => number
  revalidateAfterMs?: number
}

export const AUTH_SESSION_REVALIDATE_MS = 30_000

export function createSessionVerifier<TUser>(
  dependencies: SessionVerifierDependencies<TUser>
) {
  const now = dependencies.now ?? Date.now
  const revalidateAfterMs =
    dependencies.revalidateAfterMs ?? AUTH_SESSION_REVALIDATE_MS
  let verifiedAt = 0
  let verifiedUserIdentity: string | number | null = null
  let inFlight: {
    userIdentity: string | number | null
    promise: Promise<boolean>
  } | null = null

  const getCurrentUserIdentity = () => {
    const user = dependencies.getAuthState().user
    return user ? dependencies.getUserIdentity(user) : null
  }

  const verifyRemote = async (
    startingUserIdentity: string | number | null
  ): Promise<boolean> => {
    let response: SessionResponse<TUser>
    try {
      response = await dependencies.getSelf()
    } catch (error: unknown) {
      if (!dependencies.isUnauthorized(error)) throw error
      if (getCurrentUserIdentity() === startingUserIdentity) {
        dependencies.getAuthState().reset()
        verifiedAt = 0
        verifiedUserIdentity = null
      }
      return false
    }

    if (getCurrentUserIdentity() !== startingUserIdentity) {
      return false
    }

    const currentAuth = dependencies.getAuthState()
    if (response.success && response.data) {
      currentAuth.setUser(response.data)
      verifiedUserIdentity = dependencies.getUserIdentity(response.data)
      verifiedAt = now()
      return true
    }

    currentAuth.reset()
    verifiedAt = 0
    verifiedUserIdentity = null
    return false
  }

  const verify = async (): Promise<boolean> => {
    while (true) {
      const currentUserIdentity = getCurrentUserIdentity()
      if (
        currentUserIdentity !== null &&
        currentUserIdentity === verifiedUserIdentity &&
        verifiedAt > 0 &&
        now() - verifiedAt < revalidateAfterMs
      ) {
        return true
      }

      const active = inFlight
      if (active) {
        if (active.userIdentity === currentUserIdentity) {
          try {
            const result = await active.promise
            if (getCurrentUserIdentity() === currentUserIdentity) return result
          } catch (error: unknown) {
            if (getCurrentUserIdentity() === currentUserIdentity) throw error
          }
        } else {
          try {
            await active.promise
          } catch {
            // A request for another identity must not decide this verification.
          }
        }
        continue
      }

      const promise = verifyRemote(currentUserIdentity)
      const entry = { userIdentity: currentUserIdentity, promise }
      inFlight = entry
      try {
        const result = await promise
        if (getCurrentUserIdentity() === currentUserIdentity) return result
      } catch (error: unknown) {
        if (getCurrentUserIdentity() === currentUserIdentity) throw error
      } finally {
        if (inFlight === entry) inFlight = null
      }
    }
  }

  return { verify }
}
