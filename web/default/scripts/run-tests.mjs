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
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const scriptDirectory = path.dirname(fileURLToPath(import.meta.url))
const projectRoot = path.resolve(scriptDirectory, '..')
const testPatterns = [
  'src/**/*.test.ts',
  'src/**/*.test.tsx',
  'src/**/*.spec.ts',
  'src/**/*.spec.tsx',
  'tests/**/*.test.ts',
  'tests/**/*.test.tsx',
  'tests/**/*.spec.ts',
  'tests/**/*.spec.tsx',
]

const testFiles = new Set()
for (const pattern of testPatterns) {
  const glob = new Bun.Glob(pattern)
  for await (const file of glob.scan({ cwd: projectRoot, onlyFiles: true })) {
    testFiles.add(file)
  }
}

const sharedFiles = []
const isolatedFiles = []
for (const file of [...testFiles].sort()) {
  const source = await Bun.file(path.join(projectRoot, file)).text()
  const usesBrowserEnvironment =
    file.endsWith('.tsx') ||
    source.includes('createReactTestEnvironment') ||
    source.includes("from 'jsdom'") ||
    source.includes('from "jsdom"')

  if (usesBrowserEnvironment) {
    isolatedFiles.push(file)
  } else {
    sharedFiles.push(file)
  }
}

function runTestFiles(files, label) {
  if (files.length === 0) return

  console.log(`\n==> ${label} (${files.length} file${files.length === 1 ? '' : 's'})`)
  const result = Bun.spawnSync({
    cmd: [process.execPath, 'test', '--max-concurrency=1', ...files],
    cwd: projectRoot,
    env: process.env,
    stdout: 'inherit',
    stderr: 'inherit',
  })
  if (result.exitCode !== 0) {
    process.exit(result.exitCode)
  }
}

runTestFiles(sharedFiles, 'Shared-state-safe tests')
for (const file of isolatedFiles) {
  runTestFiles([file], `Isolated browser test: ${file}`)
}
