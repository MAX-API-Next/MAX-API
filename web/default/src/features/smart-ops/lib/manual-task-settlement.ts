/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues
*/
import { z } from 'zod'
import type { TFunction } from 'i18next'

type ManualTaskSettlementSchemaShape = {
  actualQuota: string
}

type ManualTaskSettlementSchema = z.ZodType<
  ManualTaskSettlementSchemaShape,
  ManualTaskSettlementSchemaShape
>

export function getManualTaskSettlementSchema(
  t: TFunction,
  maxQuota: number
): ManualTaskSettlementSchema {
  return z.object({
    actualQuota: z
      .string()
      .trim()
      .min(1, t('Enter the exact final quota.'))
      .refine((value) => {
        if (!/^\d+$/.test(value)) return false
        const quota = Number(value)
        return Number.isSafeInteger(quota) && quota >= 0
      }, t('Final quota must be a non-negative safe integer.'))
      .refine(
        (value) => Number(value) <= maxQuota,
        t('Final quota cannot exceed the reserved quota.')
      ),
  })
}

export type ManualTaskSettlementFormValues = z.infer<ManualTaskSettlementSchema>
