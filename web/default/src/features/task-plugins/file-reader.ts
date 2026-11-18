// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
export async function readPluginFile(
  file: Pick<File, 'arrayBuffer'>
): Promise<string> {
  // File.text() strips a leading BOM. Preserve it so a publisher's pin still
  // describes the submitted UTF-8 text, without normalizing line endings.
  return new TextDecoder('utf-8', { ignoreBOM: true }).decode(
    await file.arrayBuffer()
  )
}
