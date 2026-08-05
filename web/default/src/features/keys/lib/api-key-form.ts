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
import { z } from 'zod'
import type { TFunction } from 'i18next'
import { DEFAULT_AUTO_ROUTE_KEY, isAutoRouteKey } from '@/lib/auto-routes'
import { parseQuotaFromDollars, quotaUnitsToDollars } from '@/lib/format'
import { DEFAULT_GROUP } from '../constants'
import {
  type ApiKeyFormData,
  type ApiKey,
  type TokenRoutingPolicy,
} from '../types'

export const MAX_MANUAL_ROUTING_GROUPS = 8

type ApiKeyRoutingAvailability = {
  smartRoutes?: readonly string[]
  manualGroups?: readonly string[]
  preservedSmartRoute?: string
  preservedManualGroups?: readonly string[]
}

// ============================================================================
// Form Schema
// ============================================================================

export function getApiKeyFormSchema(
  t: TFunction,
  availability: ApiKeyRoutingAvailability = {}
) {
  return z
    .object({
      name: z.string().min(1, t('Please enter a name')),
      remain_quota_dollars: z.number().optional(),
      expired_time: z.date().optional(),
      unlimited_quota: z.boolean(),
      model_limits: z.array(z.string()),
      allow_ips: z.string().optional(),
      routing_mode: z.enum(['smart', 'manual']),
      routing_route: z.string(),
      manual_groups: z.array(z.string()).max(MAX_MANUAL_ROUTING_GROUPS),
      cross_group_retry: z.boolean().optional(),
      tokenCount: z.number().min(1).optional(),
    })
    .superRefine((data, ctx) => {
      if (data.unlimited_quota) {
        // Routing validation still applies to unlimited keys.
      } else if (
        data.remain_quota_dollars === undefined ||
        data.remain_quota_dollars < 0
      ) {
        ctx.addIssue({
          code: 'custom',
          path: ['remain_quota_dollars'],
          message: t('Quota must be zero or greater'),
        })
      }

      if (data.routing_mode === 'smart' && !data.routing_route.trim()) {
        ctx.addIssue({
          code: 'custom',
          path: ['routing_route'],
          message: t('No automatic routing groups are currently available'),
        })
      }
      if (
        data.routing_mode === 'smart' &&
        availability.smartRoutes &&
        !availability.smartRoutes.includes(data.routing_route) &&
        availability.preservedSmartRoute !== data.routing_route
      ) {
        ctx.addIssue({
          code: 'custom',
          path: ['routing_route'],
          message: t('Select an available automatic routing group'),
        })
      }
      if (data.routing_mode === 'manual' && data.manual_groups.length === 0) {
        ctx.addIssue({
          code: 'custom',
          path: ['manual_groups'],
          message: t('Select at least one manual routing group'),
        })
      }
      if (
        data.routing_mode === 'manual' &&
        new Set(data.manual_groups).size !== data.manual_groups.length
      ) {
        ctx.addIssue({
          code: 'custom',
          path: ['manual_groups'],
          message: t('Manual routing groups must not contain duplicates'),
        })
      }
      if (
        data.routing_mode === 'manual' &&
        availability.manualGroups &&
        data.manual_groups.some(
          (group) =>
            !availability.manualGroups?.includes(group) &&
            !availability.preservedManualGroups?.includes(group)
        )
      ) {
        ctx.addIssue({
          code: 'custom',
          path: ['manual_groups'],
          message: t('Remove unavailable manual routing groups before saving'),
        })
      }
    })
}

export type ApiKeyFormValues = z.infer<ReturnType<typeof getApiKeyFormSchema>>

// ============================================================================
// Form Defaults
// ============================================================================

export const API_KEY_FORM_DEFAULT_VALUES: ApiKeyFormValues = {
  name: '',
  remain_quota_dollars: 10,
  expired_time: undefined,
  unlimited_quota: true,
  model_limits: [],
  allow_ips: '',
  routing_mode: 'smart',
  routing_route: DEFAULT_AUTO_ROUTE_KEY,
  manual_groups: [DEFAULT_GROUP],
  cross_group_retry: true,
  tokenCount: 1,
}

export function getApiKeyFormDefaultValues(
  defaultAutoRoute: string = DEFAULT_AUTO_ROUTE_KEY,
  defaultManualGroups: string[] = [DEFAULT_GROUP]
): ApiKeyFormValues {
  return {
    ...API_KEY_FORM_DEFAULT_VALUES,
    routing_route: defaultAutoRoute,
    manual_groups:
      defaultManualGroups.length > 0
        ? defaultManualGroups.slice(0, MAX_MANUAL_ROUTING_GROUPS)
        : [DEFAULT_GROUP],
  }
}

function buildRoutingPolicy(data: ApiKeyFormValues): TokenRoutingPolicy {
  if (data.routing_mode === 'manual') {
    return {
      version: 1,
      mode: 'manual',
      groups: data.manual_groups,
      retry_on_failure: !!data.cross_group_retry,
    }
  }
  return {
    version: 1,
    mode: 'smart',
    route: data.routing_route || DEFAULT_AUTO_ROUTE_KEY,
    retry_on_failure: !!data.cross_group_retry,
  }
}

type TransformFormDataOptions = {
  includeRouting?: boolean
}

export function shouldIncludeRoutingProjection(
  isUpdate: boolean,
  routingLegacy: boolean,
  routingChanged: boolean
): boolean {
  return !(isUpdate && routingLegacy && !routingChanged)
}

// ============================================================================
// Form Data Transformation
// ============================================================================

/**
 * Transform form data to API payload
 */
export function transformFormDataToPayload(
  data: ApiKeyFormValues,
  options: TransformFormDataOptions = {}
): ApiKeyFormData {
  const routing = buildRoutingPolicy(data)
  const projectedGroup =
    routing.mode === 'manual'
      ? routing.groups?.[0] || ''
      : routing.route || DEFAULT_AUTO_ROUTE_KEY
  const payload: ApiKeyFormData = {
    name: data.name,
    remain_quota: data.unlimited_quota
      ? 0
      : parseQuotaFromDollars(data.remain_quota_dollars || 0),
    expired_time: data.expired_time
      ? Math.floor(data.expired_time.getTime() / 1000)
      : -1,
    unlimited_quota: data.unlimited_quota,
    model_limits_enabled: data.model_limits.length > 0,
    model_limits: data.model_limits.join(','),
    allow_ips: data.allow_ips || '',
  }
  if (options.includeRouting === false) return payload
  return {
    ...payload,
    group: projectedGroup,
    cross_group_retry: !!data.cross_group_retry,
    routing,
  }
}

/**
 * Transform API key data to form defaults
 */
export function transformApiKeyToFormDefaults(
  apiKey: ApiKey,
  smartModeManualGroups: string[] = [DEFAULT_GROUP]
): ApiKeyFormValues {
  const routing = apiKey.routing
  const legacySmart = !routing && isAutoRouteKey(apiKey.group)
  const mode = routing?.mode ?? (legacySmart ? 'smart' : 'manual')
  const manualGroups =
    mode === 'manual'
      ? routing?.groups?.length
        ? routing.groups
        : [apiKey.group || DEFAULT_GROUP]
      : smartModeManualGroups.length > 0
        ? smartModeManualGroups.slice(0, MAX_MANUAL_ROUTING_GROUPS)
        : [DEFAULT_GROUP]
  return {
    name: apiKey.name,
    remain_quota_dollars: apiKey.unlimited_quota
      ? 0
      : quotaUnitsToDollars(apiKey.remain_quota),
    expired_time:
      apiKey.expired_time > 0
        ? new Date(apiKey.expired_time * 1000)
        : undefined,
    unlimited_quota: apiKey.unlimited_quota,
    model_limits: apiKey.model_limits
      ? apiKey.model_limits.split(',').filter(Boolean)
      : [],
    allow_ips: apiKey.allow_ips || '',
    routing_mode: mode,
    routing_route:
      mode === 'smart'
        ? routing?.route || apiKey.group || DEFAULT_AUTO_ROUTE_KEY
        : DEFAULT_AUTO_ROUTE_KEY,
    manual_groups: manualGroups,
    cross_group_retry: routing?.retry_on_failure ?? !!apiKey.cross_group_retry,
    tokenCount: 1,
  }
}
