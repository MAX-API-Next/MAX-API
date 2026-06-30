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
interface SelectionOption {
  value: string
}

export function getAvailableOptionValue<T extends SelectionOption>(
  options: T[],
  currentValue: string,
  preferredValue?: string
): string {
  if (options.length === 0) return ''

  if (options.some((option) => option.value === currentValue)) {
    return currentValue
  }

  return (
    options.find((option) => option.value === preferredValue)?.value ??
    options[0].value
  )
}

export function isOptionValueAvailable<T extends SelectionOption>(
  options: T[] | undefined,
  value: string
): boolean {
  return Boolean(value && options?.some((option) => option.value === value))
}
