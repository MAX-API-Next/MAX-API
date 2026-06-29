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
import DOMPurify from 'dompurify'

const htmlSanitizeOptions = {
  USE_PROFILES: { html: true },
} as const

type HtmlSanitizer = {
  isSupported?: boolean
  sanitize: (dirty: string, config?: typeof htmlSanitizeOptions) => string
}

type HtmlSanitizerFactory = (window: Window) => HtmlSanitizer

function isUsableHtmlSanitizer(value: unknown): value is HtmlSanitizer {
  if ((typeof value !== 'object' && typeof value !== 'function') || value === null) {
    return false
  }

  const sanitizer = value as Partial<HtmlSanitizer>
  return (
    sanitizer.isSupported !== false &&
    typeof sanitizer.sanitize === 'function'
  )
}

export function sanitizeHtmlContent(content: string): string {
  const purify = DOMPurify as unknown as HtmlSanitizer | HtmlSanitizerFactory

  try {
    if (isUsableHtmlSanitizer(purify)) {
      return purify.sanitize(content, htmlSanitizeOptions)
    }

    if (typeof window !== 'undefined' && typeof purify === 'function') {
      const browserSanitizer = purify(window)

      if (isUsableHtmlSanitizer(browserSanitizer)) {
        return browserSanitizer.sanitize(content, htmlSanitizeOptions)
      }
    }
  } catch {
    return ''
  }

  return ''
}
