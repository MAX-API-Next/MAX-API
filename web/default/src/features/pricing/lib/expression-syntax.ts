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
// Split only outside calls and quoted strings; both pricing parsers share this contract.
export function splitTopLevelExpression(
  source: string,
  operator: ':' | '&&' | '||',
  preserveEmpty: boolean = false
): string[] {
  const parts: string[] = []
  let start = 0
  let depth = 0
  let quote = ''
  let escaped = false
  for (let index = 0; index < source.length; index += 1) {
    const char = source[index]
    if (quote) {
      if (escaped) escaped = false
      else if (char === '\\') escaped = true
      else if (char === quote) quote = ''
      continue
    }
    if (char === '"' || char === "'") {
      quote = char
      continue
    }
    if (char === '(') depth += 1
    else if (char === ')') depth = Math.max(0, depth - 1)
    if (depth === 0 && source.startsWith(operator, index)) {
      parts.push(source.slice(start, index).trim())
      start = index + operator.length
      index += operator.length - 1
    }
  }
  parts.push(source.slice(start).trim())
  return preserveEmpty ? parts : parts.filter(Boolean)
}

// Reject non-finite values, unsafe integer magnitudes, and nonzero underflow.
export function isSupportedNumericLiteral(source: string): boolean {
  if (!/^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?$/.test(source))
    return false
  const value = Number(source)
  return (
    Number.isFinite(value) &&
    (!Number.isInteger(value) || Number.isSafeInteger(value)) &&
    (value !== 0 || !/[1-9]/.test(source.split(/[eE]/)[0]))
  )
}

// Remove enclosing parentheses without interpreting quoted punctuation.
export function unwrapConditionParens(source: string): string {
  let value = source.trim()
  while (value.startsWith('(') && value.endsWith(')')) {
    let depth = 0
    let quote = ''
    let closesAtEnd = false
    let escaped = false
    for (let index = 0; index < value.length; index += 1) {
      const char = value[index]
      if (quote) {
        if (escaped) escaped = false
        else if (char === '\\') escaped = true
        else if (char === quote) quote = ''
        continue
      }
      if (char === '"' || char === "'") {
        quote = char
        continue
      }
      if (char === '(') depth += 1
      else if (char === ')') depth -= 1
      if (depth === 0) {
        closesAtEnd = index === value.length - 1
        break
      }
    }
    if (!closesAtEnd) break
    value = value.slice(1, -1).trim()
  }
  return value
}
