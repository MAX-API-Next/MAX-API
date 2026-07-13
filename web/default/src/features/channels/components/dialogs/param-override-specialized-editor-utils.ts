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
export type SyncTargetType = 'header' | 'json'

type SyncTarget = {
  type: string
  key: string
}

type SyncTargetEdit = {
  spec: string
  typeOverride: SyncTargetType | null
}

export function normalizeSyncTargetType(type: string): SyncTargetType {
  return type === 'header' ? 'header' : 'json'
}

export function buildSyncTargetSpec(type: string, key: string): string {
  const normalizedType = normalizeSyncTargetType(type)
  const normalizedKey = String(key ?? '').trim()
  if (!normalizedKey) return ''
  return `${normalizedType}:${normalizedKey}`
}

export function selectSyncTargetType(
  target: SyncTarget,
  type: string
): SyncTargetEdit {
  const nextType = normalizeSyncTargetType(type)
  if (!target.key.trim()) {
    return { spec: '', typeOverride: nextType }
  }
  return {
    spec: buildSyncTargetSpec(nextType, target.key),
    typeOverride: null,
  }
}

export function editSyncTargetKey(
  currentType: string,
  key: string
): SyncTargetEdit {
  const normalizedType = normalizeSyncTargetType(currentType)
  return {
    spec: buildSyncTargetSpec(normalizedType, key),
    typeOverride: key.trim() ? null : normalizedType,
  }
}
