// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import type { PluginEntry } from './api'

export function packageCompatibilityMessage(entry: PluginEntry): string {
  if (entry.bom_prefixed && entry.format_version >= 2)
    return 'All catalog nodes support this package format and UTF-8 BOM packages. Older nodes may suspend their entire plugin host.'
  if (entry.bom_prefixed)
    return 'All catalog nodes support UTF-8 BOM packages. Older nodes may suspend their entire plugin host.'
  return 'All catalog nodes support this package format. Older nodes may suspend their entire plugin host.'
}
