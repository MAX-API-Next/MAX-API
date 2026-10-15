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
import { useState, useEffect, useCallback } from 'react'
import i18next from 'i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { getSelf } from '@/lib/api'
import { getSafeErrorDebugInfo } from '@/lib/handle-server-error'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { useReadScope } from '@/hooks/use-read-scope'
import { useResettableState } from '@/hooks/use-resettable-state'
import { getAffiliateCode, transferAffiliateQuota } from '../api'
import { generateAffiliateLink } from '../lib'

// ============================================================================
// Affiliate Hook
// ============================================================================

export function useAffiliate() {
  const sessionRevision = useAuthStore((state) => state.sessionRevision)
  const beginRead = useReadScope(sessionRevision)
  const [affiliateCode, setAffiliateCode] = useResettableState(
    '',
    sessionRevision
  )
  const [affiliateLink, setAffiliateLink] = useResettableState(
    '',
    sessionRevision
  )
  const [loading, setLoading] = useResettableState(true, sessionRevision)
  const [transferring, setTransferring] = useState(false)
  const { copyToClipboard } = useCopyToClipboard()

  const loadAffiliateCode = useCallback(() => {
    const current = beginRead()
    if (!current()) return Promise.resolve()
    return getAffiliateCode()
      .then((response) => {
        if (current() && response.success && response.data) {
          setAffiliateCode(response.data)
          setAffiliateLink(generateAffiliateLink(response.data))
        }
      })
      .catch((error) => {
        if (!current()) return
        // eslint-disable-next-line no-console
        console.error(
          'Failed to fetch affiliate code:',
          getSafeErrorDebugInfo(error)
        )
      })
      .finally(() => {
        if (current()) setLoading(false)
      })
  }, [beginRead, setAffiliateCode, setAffiliateLink, setLoading])
  const fetchAffiliateCode = useCallback(() => {
    setLoading(true)
    return loadAffiliateCode()
  }, [loadAffiliateCode, setLoading])

  // Copy affiliate link
  const copyAffiliateLink = useCallback(() => {
    copyToClipboard(affiliateLink)
  }, [affiliateLink, copyToClipboard])

  // Transfer affiliate quota to balance
  const transferQuota = useCallback(async (quota: number): Promise<boolean> => {
    try {
      setTransferring(true)
      const response = await transferAffiliateQuota({ quota })

      if (response.success) {
        toast.success(response.message || i18next.t('Transfer successful'))
        await getSelf()
        return true
      }

      toast.error(response.message || i18next.t('Transfer failed'))
      return false
    } catch (_error) {
      toast.error(i18next.t('Transfer failed'))
      return false
    } finally {
      setTransferring(false)
    }
  }, [])

  useEffect(() => {
    void loadAffiliateCode()
  }, [loadAffiliateCode])

  return {
    affiliateCode,
    affiliateLink,
    loading,
    transferring,
    copyAffiliateLink,
    transferQuota,
    refetch: fetchAffiliateCode,
  }
}
