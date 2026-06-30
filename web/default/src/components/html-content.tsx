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
import { sanitizeHtmlContent } from '@/lib/html-sanitizer'
import { cn } from '@/lib/utils'

interface HtmlContentProps {
  content: string
  className?: string
}

export function HtmlContent(props: HtmlContentProps) {
  const html = useMemo(
    () => sanitizeHtmlContent(props.content),
    [props.content]
  )

  return (
    <div
      className={cn(
        'prose prose-neutral dark:prose-invert max-w-none',
        props.className
      )}
      dangerouslySetInnerHTML={{ __html: html }}
    />
  )
}
