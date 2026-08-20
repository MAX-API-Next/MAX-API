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
import { ArrowDown, ArrowUp, ChevronsUpDown } from 'lucide-react'
import type { SortDirection } from './sort-direction'

export function SortDirectionIcon(props: {
  direction: SortDirection
}): React.ReactElement {
  if (props.direction === 'desc') {
    return <ArrowDown data-icon='inline-end' aria-hidden='true' />
  }
  if (props.direction === 'asc') {
    return <ArrowUp data-icon='inline-end' aria-hidden='true' />
  }
  return <ChevronsUpDown data-icon='inline-end' aria-hidden='true' />
}
