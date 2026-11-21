// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
export async function readPluginFile(
  file: Pick<File, 'arrayBuffer'>
): Promise<string> {
  // File.text() strips a leading BOM. Preserve it so a publisher's pin still
  // describes the submitted UTF-8 text, without normalizing line endings.
  return new TextDecoder('utf-8', { ignoreBOM: true, fatal: true }).decode(
    await file.arrayBuffer()
  )
}

export function isJSONPluginUpload(document: string): boolean {
  // Go's bytes.TrimSpace uses Unicode White_Space, which excludes U+FEFF.
  const json = document.replace(/^\uFEFF/, '').replace(/^\p{White_Space}*/u, '')
  if (!json.startsWith('{')) return false
  try {
    JSON.parse(json)
    return true
  } catch {
    return false
  }
}
