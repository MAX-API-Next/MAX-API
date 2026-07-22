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
import { readdir, readFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const scriptDirectory = path.dirname(fileURLToPath(import.meta.url))
const cssDirectory = path.resolve(scriptDirectory, '../dist/static/css')

async function findCssFiles(directory) {
  const entries = await readdir(directory, { withFileTypes: true })
  const files = await Promise.all(
    entries.map((entry) => {
      const entryPath = path.join(directory, entry.name)
      return entry.isDirectory()
        ? findCssFiles(entryPath)
        : Promise.resolve(entry.name.endsWith('.css') ? [entryPath] : [])
    })
  )

  return files.flat()
}

const cssFiles = await findCssFiles(cssDirectory)
const css = (
  await Promise.all(cssFiles.map((file) => readFile(file, 'utf8')))
).join('\n')

const requiredRules = [
  {
    name: 'desktop homepage hero grid',
    pattern:
      /grid-template-columns\s*:\s*minmax\(0,\s*0?\.94fr\)\s+minmax\(440px,\s*1\.06fr\)/,
  },
]

const missingRules = requiredRules.filter(({ pattern }) => !pattern.test(css))

if (missingRules.length > 0) {
  console.error(
    `Tailwind build verification failed: missing ${missingRules
      .map(({ name }) => name)
      .join(', ')}`
  )
  process.exit(1)
}

console.log(`Tailwind build verification passed (${cssFiles.length} CSS files)`)
