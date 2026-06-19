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
export const BASE_URL_REQUIRED_TYPES = new Set([3, 8, 36, 45])

export const OTHER_REQUIRED_TYPES = new Set([3, 18, 21, 39, 41, 49])

export const VIDEO_TASK_QUERY_PLACEHOLDERS = [
  '{task_id}',
  '{operation_name}',
  '{upstream_task_id}',
]

export function hasVideoTaskQueryPlaceholder(path: string): boolean {
  return VIDEO_TASK_QUERY_PLACEHOLDERS.some((placeholder) =>
    path.includes(placeholder)
  )
}

export function hasVertexDefaultRegion(value: unknown): boolean {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    return false
  }
  const defaultRegion = (value as Record<string, unknown>).default
  return typeof defaultRegion === 'string' && defaultRegion.trim().length > 0
}
