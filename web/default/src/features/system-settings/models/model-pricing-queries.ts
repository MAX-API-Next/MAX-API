// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import type { QueryClient } from '@tanstack/react-query'

export function invalidateModelPricing(client: QueryClient): void {
  for (const queryKey of [
    ['model-pricing-config'],
    ['system-options'],
    ['pricing'],
    ['models'],
  ]) {
    void client.invalidateQueries({ queryKey })
  }
}
