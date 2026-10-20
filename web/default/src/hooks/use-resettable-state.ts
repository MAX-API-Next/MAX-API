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
import {
  useCallback,
  useState,
  type Dispatch,
  type SetStateAction,
} from 'react'

/** Keep an editable draft until its source identity changes, before children commit. */
export function useResettableState<T>(
  initialValue: T | (() => T),
  resetKey: unknown
): [T, Dispatch<SetStateAction<T>>] {
  const [draft, setDraft] = useState(() => ({
    key: resetKey,
    scope: {},
    value:
      typeof initialValue === 'function'
        ? (initialValue as () => T)()
        : initialValue,
  }))
  let current = draft
  if (!Object.is(draft.key, resetKey)) {
    const nextValue =
      typeof initialValue === 'function'
        ? (initialValue as () => T)()
        : initialValue
    current = { key: resetKey, scope: {}, value: nextValue }
    setDraft(current)
  }
  const scope = current.scope
  const setValue = useCallback<Dispatch<SetStateAction<T>>>(
    (action) => {
      setDraft((previous) =>
        previous.scope !== scope
          ? previous
          : {
              ...previous,
              value:
                typeof action === 'function'
                  ? (action as (value: T) => T)(previous.value)
                  : action,
            }
      )
    },
    [scope]
  )
  return [current.value, setValue]
}
