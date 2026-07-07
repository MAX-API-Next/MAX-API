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
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  QUOTA_FILTER_ALL_VALUE,
  QUOTA_FILTER_VALUES,
  QUOTA_FILTERS,
} from '../constants'

export type QuotaFilterValue = (typeof QUOTA_FILTER_VALUES)[number]

export function isQuotaFilterValue(value: string): value is QuotaFilterValue {
  return (QUOTA_FILTER_VALUES as readonly string[]).includes(value)
}

interface QuotaFilterSelectProps {
  value: QuotaFilterValue
  onValueChange: (value: QuotaFilterValue) => void
}

export function QuotaFilterSelect(props: QuotaFilterSelectProps) {
  const { t } = useTranslation()
  const items = useMemo(
    () =>
      QUOTA_FILTERS.map((filter) => ({
        value: filter.value,
        label: t(filter.label),
      })),
    [t]
  )
  const label =
    items.find((filter) => filter.value === props.value)?.label ??
    t('All Billing')

  return (
    <Select
      items={items}
      value={props.value}
      onValueChange={(value) => {
        props.onValueChange(
          value !== null && isQuotaFilterValue(value)
            ? value
            : QUOTA_FILTER_ALL_VALUE
        )
      }}
    >
      <SelectTrigger>
        <SelectValue>{label}</SelectValue>
      </SelectTrigger>
      <SelectContent alignItemWithTrigger={false}>
        <SelectGroup>
          {QUOTA_FILTERS.map((filter) => (
            <SelectItem key={filter.value} value={filter.value}>
              {t(filter.label)}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  )
}
