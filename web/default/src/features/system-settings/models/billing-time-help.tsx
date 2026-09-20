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
import type { ReactElement } from 'react'
import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'
import { FieldDescription } from '@/components/ui/field'
import { TIME_FUNCS, type TimeFunc } from '@/features/pricing/lib/billing-expr'

function describeTimeValue(timeFunc: TimeFunc, t: TFunction): string {
  switch (timeFunc) {
    case 'hour':
      return t('Hour: 0–23 (24-hour clock); 0 is midnight.')
    case 'minute':
      return t('Minute: 0–59 within the current hour.')
    case 'weekday':
      return t(
        'Weekday: 0 = Sunday, 1 = Monday, 2 = Tuesday, 3 = Wednesday, 4 = Thursday, 5 = Friday, 6 = Saturday.'
      )
    case 'month':
      return t('Month: 1–12; January is 1.')
    case 'day':
      return t(
        'Day of month: 1–31, depending on the month and leap year; this is not a duration.'
      )
  }
}

type BillingTimeHelpProps = {
  id: string
  timeFunc?: TimeFunc
  mode: 'visual' | 'expression'
}

export function BillingTimeHelp(props: BillingTimeHelpProps): ReactElement {
  const { t } = useTranslation()
  const timeFunctions = props.timeFunc ? [props.timeFunc] : TIME_FUNCS

  return (
    <FieldDescription id={props.id} className='w-full min-w-0 break-words'>
      {timeFunctions.map((timeFunc) => (
        <span key={timeFunc} className='block'>
          {props.mode === 'expression' && <code>{timeFunc}(tz): </code>}
          {describeTimeValue(timeFunc, t)}
        </span>
      ))}
      {timeFunctions.includes('weekday') && (
        <span className='block'>
          {t(
            'Weekday < 6 includes Sunday through Friday. For Monday–Friday, use >= 1 AND <= 5 in the same condition group.'
          )}
        </span>
      )}
      <span className='block'>
        {t(
          'Time values use the configured timezone. Asia/Shanghai is China Standard Time (UTC+8).'
        )}
      </span>
      <span className='block'>
        {props.mode === 'visual'
          ? t(
              'An empty timezone in this visual editor uses Asia/Shanghai when generating the expression. Unrecognized timezones are evaluated as UTC.'
            )
          : t(
              'In expressions, an empty or unrecognized timezone is evaluated as UTC.'
            )}
      </span>
    </FieldDescription>
  )
}
