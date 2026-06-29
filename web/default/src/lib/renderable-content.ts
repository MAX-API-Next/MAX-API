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
export type RenderableContentKind = 'url' | 'html' | 'markdown'

export function getRenderableContentKind(value: string): RenderableContentKind {
  const content = value.trim()

  if (isHttpUrl(content)) {
    return 'url'
  }

  if (isLikelyHtml(content)) {
    return 'html'
  }

  return 'markdown'
}

export function getSafeIframeEmbedSrc(content: string): string | null {
  const trimmed = content.trim()

  if (isHttpUrl(trimmed)) return trimmed
  if (!/<iframe[\s>]/i.test(content)) return null
  if (typeof DOMParser === 'undefined') return null

  const document = new DOMParser().parseFromString(trimmed, 'text/html')
  const iframes = document.querySelectorAll('iframe')

  if (iframes.length !== 1) return null

  const iframe = iframes[0]
  iframe.remove()

  if (document.body.textContent?.trim()) return null
  if (!hasOnlyEmptyWrapperElements(document.body)) return null

  const src = iframe.getAttribute('src')?.trim()
  if (!src) return null

  return isHttpUrl(src) ? src : null
}

function hasOnlyEmptyWrapperElements(body: HTMLElement): boolean {
  const allowedWrapperTags = new Set(['ARTICLE', 'DIV', 'MAIN', 'SECTION'])

  return [...body.querySelectorAll('*')].every((element) =>
    allowedWrapperTags.has(element.tagName)
  )
}

function isHttpUrl(value: string): boolean {
  try {
    const url = new URL(value)
    return url.protocol === 'http:' || url.protocol === 'https:'
  } catch {
    return false
  }
}

function isLikelyHtml(value: string): boolean {
  return /<\/?[a-z][\s\S]*>/i.test(value)
}
