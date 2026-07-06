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
import { useState, useMemo, useEffect, useCallback, memo } from 'react'
import { Pencil, Plus, Trash2, GripVertical, ChevronDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  getDefaultAutoRouteGroups,
  isValidAutoRouteKey,
  parseAutoGroupRoutesConfig,
  stringifyAutoGroupRoutesConfig,
  type AutoGroupRoute,
  type AutoGroupRoutesConfig,
} from '@/lib/auto-routes'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { safeJsonParse } from '../utils/json-parser'

type GroupRatioVisualEditorProps = {
  groupRatio: string
  topupGroupRatio: string
  userUsableGroups: string
  groupGroupRatio: string
  autoGroups: string
  autoGroupRoutes: string
  onChange: (field: string, value: string) => void
}

type SimpleGroup = {
  name: string
  value: string
}

type GroupPricingRow = {
  _id: string
  name: string
  ratio: number
  selectable: boolean
  description: string
}

type GroupOverride = {
  targetGroup: string
  ratio: number
}

type AutoRouteDialogData = {
  key: string
  name: string
  groupsText: string
}

const sectionCardClassName =
  'relative shadow-sm ring-0 before:pointer-events-none before:absolute before:inset-0 before:rounded-xl before:border before:border-border/90'
const sectionHeaderClassName = 'border-b bg-muted/20'

let groupPricingIdCounter = 0
function createGroupPricingId() {
  groupPricingIdCounter += 1
  return `gpr_${groupPricingIdCounter}`
}

function normalizeRatio(value: unknown): number {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : 1
}

function buildGroupPricingRows(
  groupRatio: string,
  userUsableGroups: string
): GroupPricingRow[] {
  const ratioMap = safeJsonParse<Record<string, number>>(groupRatio, {
    fallback: {},
    context: 'group ratios',
  })
  const usableMap = safeJsonParse<Record<string, string>>(userUsableGroups, {
    fallback: {},
    context: 'user usable groups',
  })
  const names = new Set([...Object.keys(ratioMap), ...Object.keys(usableMap)])

  return Array.from(names).map((name) => ({
    _id: createGroupPricingId(),
    name,
    ratio: normalizeRatio(ratioMap[name]),
    selectable: Object.prototype.hasOwnProperty.call(usableMap, name),
    description: String(usableMap[name] ?? ''),
  }))
}

function serializeGroupPricingRows(rows: GroupPricingRow[]) {
  const groupRatio: Record<string, number> = {}
  const userUsableGroups: Record<string, string> = {}

  for (const row of rows) {
    const name = row.name.trim()
    if (!name) continue
    groupRatio[name] = normalizeRatio(row.ratio)
    if (row.selectable) {
      userUsableGroups[name] = row.description
    }
  }

  return {
    GroupRatio: JSON.stringify(groupRatio, null, 2),
    UserUsableGroups: JSON.stringify(userUsableGroups, null, 2),
  }
}

function groupPricingSignature(rows: GroupPricingRow[]): string {
  const serialized = serializeGroupPricingRows(rows)
  return JSON.stringify({
    groupRatio: safeJsonParse(serialized.GroupRatio, {
      fallback: {},
      silent: true,
    }),
    userUsableGroups: safeJsonParse(serialized.UserUsableGroups, {
      fallback: {},
      silent: true,
    }),
  })
}

function sourceGroupPricingSignature(
  groupRatio: string,
  userUsableGroups: string
): string {
  return JSON.stringify({
    groupRatio: safeJsonParse(groupRatio, { fallback: {}, silent: true }),
    userUsableGroups: safeJsonParse(userUsableGroups, {
      fallback: {},
      silent: true,
    }),
  })
}

function routeGroupsToText(groups: string[]) {
  return groups.join('\n')
}

function parseRouteGroupsText(value: string) {
  const seen = new Set<string>()
  const groups: string[] = []
  for (const part of value.split(/[\n,]/)) {
    const group = part.trim()
    if (!group || seen.has(group)) continue
    seen.add(group)
    groups.push(group)
  }
  return groups
}

function cloneAutoRoutesConfig(
  config: AutoGroupRoutesConfig
): AutoGroupRoutesConfig {
  return {
    version: config.version,
    default_route: config.default_route,
    routes: config.routes.map((route) => ({
      ...route,
      groups: [...route.groups],
    })),
  }
}

export const GroupRatioVisualEditor = memo(function GroupRatioVisualEditor({
  groupRatio,
  topupGroupRatio,
  userUsableGroups,
  groupGroupRatio,
  autoGroups,
  autoGroupRoutes,
  onChange,
}: GroupRatioVisualEditorProps) {
  const { t } = useTranslation()
  const [simpleDialogOpen, setSimpleDialogOpen] = useState(false)
  const [simpleDialogType, setSimpleDialogType] = useState<
    'groupRatio' | 'topupGroupRatio' | null
  >(null)
  const [simpleEditData, setSimpleEditData] = useState<SimpleGroup | null>(null)

  const [autoRouteDialogOpen, setAutoRouteDialogOpen] = useState(false)
  const [autoRouteEditKey, setAutoRouteEditKey] = useState<string | null>(null)
  const [autoRouteDialogData, setAutoRouteDialogData] =
    useState<AutoRouteDialogData>({
      key: '',
      name: '',
      groupsText: '',
    })

  const [groupOverrideDialogOpen, setGroupOverrideDialogOpen] = useState(false)
  const [groupOverrideUserGroup, setGroupOverrideUserGroup] = useState<
    string | null
  >(null)
  const [groupOverrideEditData, setGroupOverrideEditData] =
    useState<GroupOverride | null>(null)

  const [userGroupDialogOpen, setUserGroupDialogOpen] = useState(false)
  const [userGroupInput, setUserGroupInput] = useState('')

  // Parse topup group ratios
  const topupRatioList = useMemo(() => {
    const map = safeJsonParse<Record<string, number>>(topupGroupRatio, {
      fallback: {},
      context: 'topup group ratios',
    })
    return Object.entries(map).map(([name, value]) => ({
      name,
      value: String(value),
    }))
  }, [topupGroupRatio])

  const autoRoutesConfig = useMemo(
    () => parseAutoGroupRoutesConfig(autoGroupRoutes, autoGroups),
    [autoGroupRoutes, autoGroups]
  )

  // Parse group-group ratios
  const groupGroupRatioList = useMemo(() => {
    const map = safeJsonParse<Record<string, Record<string, number>>>(
      groupGroupRatio,
      {
        fallback: {},
        context: 'group-group ratios',
      }
    )
    return Object.entries(map).map(([userGroup, overrides]) => ({
      userGroup,
      overrides: Object.entries(overrides).map(([targetGroup, ratio]) => ({
        targetGroup,
        ratio,
      })),
    }))
  }, [groupGroupRatio])

  // Simple group handlers (for groupRatio and topupGroupRatio)
  const handleSimpleAdd = (type: 'groupRatio' | 'topupGroupRatio') => {
    setSimpleDialogType(type)
    setSimpleEditData(null)
    setSimpleDialogOpen(true)
  }

  const handleSimpleEdit = (
    type: 'groupRatio' | 'topupGroupRatio',
    group: SimpleGroup
  ) => {
    setSimpleDialogType(type)
    setSimpleEditData(group)
    setSimpleDialogOpen(true)
  }

  const handleSimpleSave = (name: string, value: string) => {
    if (!simpleDialogType) return

    const fieldName =
      simpleDialogType === 'groupRatio' ? groupRatio : topupGroupRatio
    const map = safeJsonParse<Record<string, number>>(fieldName, {
      fallback: {},
      silent: true,
    })

    if (simpleEditData && simpleEditData.name !== name) {
      delete map[simpleEditData.name]
    }

    map[name] = parseFloat(value)

    const field =
      simpleDialogType === 'groupRatio' ? 'GroupRatio' : 'TopupGroupRatio'
    onChange(field, JSON.stringify(map, null, 2))
    setSimpleDialogOpen(false)
  }

  const handleSimpleDelete = (
    type: 'groupRatio' | 'topupGroupRatio',
    name: string
  ) => {
    const fieldName = type === 'groupRatio' ? groupRatio : topupGroupRatio
    const map = safeJsonParse<Record<string, number>>(fieldName, {
      fallback: {},
      silent: true,
    })
    delete map[name]

    const field = type === 'groupRatio' ? 'GroupRatio' : 'TopupGroupRatio'
    onChange(field, JSON.stringify(map, null, 2))
  }

  const emitAutoRoutesConfig = useCallback(
    (nextConfig: AutoGroupRoutesConfig) => {
      const serialized = stringifyAutoGroupRoutesConfig(nextConfig, 2)
      const normalized = parseAutoGroupRoutesConfig(serialized)
      onChange('AutoGroupRoutes', stringifyAutoGroupRoutesConfig(normalized, 2))
      onChange(
        'AutoGroups',
        JSON.stringify(getDefaultAutoRouteGroups(normalized), null, 2)
      )
    },
    [onChange]
  )

  const handleAutoRouteAdd = () => {
    setAutoRouteEditKey(null)
    setAutoRouteDialogData({
      key: 'auto:fast',
      name: '',
      groupsText: '',
    })
    setAutoRouteDialogOpen(true)
  }

  const handleAutoRouteEdit = (route: AutoGroupRoute) => {
    setAutoRouteEditKey(route.key)
    setAutoRouteDialogData({
      key: route.key,
      name: route.name ?? '',
      groupsText: routeGroupsToText(route.groups),
    })
    setAutoRouteDialogOpen(true)
  }

  const handleAutoRouteSave = () => {
    const key = autoRouteDialogData.key.trim()
    const groups = parseRouteGroupsText(autoRouteDialogData.groupsText)
    if (!isValidAutoRouteKey(key)) {
      toast.error(t('Invalid auto route key'))
      return
    }
    if (groups.length === 0) {
      toast.error(t('Route is required'))
      return
    }

    const nextConfig = cloneAutoRoutesConfig(autoRoutesConfig)
    const duplicateKeyIndex = nextConfig.routes.findIndex(
      (item) => item.key === key
    )
    if (
      duplicateKeyIndex >= 0 &&
      (!autoRouteEditKey || key !== autoRouteEditKey)
    ) {
      toast.error(t('Auto route key already exists'))
      return
    }
    const route: AutoGroupRoute = {
      key,
      name: autoRouteDialogData.name.trim() || key,
      enabled: true,
      user_selectable: true,
      groups,
    }
    const existingIndex = autoRouteEditKey
      ? nextConfig.routes.findIndex((item) => item.key === autoRouteEditKey)
      : -1
    if (existingIndex >= 0) {
      route.enabled = nextConfig.routes[existingIndex].enabled
      route.user_selectable = nextConfig.routes[existingIndex].user_selectable
      nextConfig.routes[existingIndex] = route
    } else {
      nextConfig.routes.push(route)
    }
    if (!nextConfig.default_route) {
      nextConfig.default_route = key
    }
    emitAutoRoutesConfig(nextConfig)
    setAutoRouteDialogOpen(false)
  }

  const handleAutoRouteDelete = (routeKey: string) => {
    const nextConfig = cloneAutoRoutesConfig(autoRoutesConfig)
    if (nextConfig.routes.length <= 1) return
    nextConfig.routes = nextConfig.routes.filter(
      (route) => route.key !== routeKey
    )
    if (nextConfig.default_route === routeKey) {
      nextConfig.default_route = nextConfig.routes[0]?.key ?? 'auto'
    }
    emitAutoRoutesConfig(nextConfig)
  }

  const handleAutoRouteToggle = (
    routeKey: string,
    field: 'enabled' | 'user_selectable',
    checked: boolean
  ) => {
    if (
      field === 'enabled' &&
      !checked &&
      routeKey === autoRoutesConfig.default_route
    ) {
      toast.error(t('Default auto route must be enabled'))
      return
    }
    const nextConfig = cloneAutoRoutesConfig(autoRoutesConfig)
    nextConfig.routes = nextConfig.routes.map((route) =>
      route.key === routeKey ? { ...route, [field]: checked } : route
    )
    emitAutoRoutesConfig(nextConfig)
  }

  const handleDefaultAutoRouteChange = (routeKey: string) => {
    const route = autoRoutesConfig.routes.find((item) => item.key === routeKey)
    if (!route?.enabled) {
      toast.error(t('Default auto route must be enabled'))
      return
    }
    emitAutoRoutesConfig({
      ...cloneAutoRoutesConfig(autoRoutesConfig),
      default_route: routeKey,
    })
  }

  const handleAutoRouteGroupDelete = (routeKey: string, index: number) => {
    const nextConfig = cloneAutoRoutesConfig(autoRoutesConfig)
    const route = nextConfig.routes.find((item) => item.key === routeKey)
    if (!route || route.groups.length <= 1) return
    route.groups = route.groups.filter((_, itemIndex) => itemIndex !== index)
    emitAutoRoutesConfig(nextConfig)
  }

  const handleAutoRouteGroupMove = (
    routeKey: string,
    index: number,
    direction: 'up' | 'down'
  ) => {
    const nextConfig = cloneAutoRoutesConfig(autoRoutesConfig)
    const route = nextConfig.routes.find((item) => item.key === routeKey)
    if (!route) return
    const nextIndex = direction === 'up' ? index - 1 : index + 1
    if (nextIndex < 0 || nextIndex >= route.groups.length) return
    ;[route.groups[index], route.groups[nextIndex]] = [
      route.groups[nextIndex],
      route.groups[index],
    ]
    emitAutoRoutesConfig(nextConfig)
  }

  // Group-group ratio handlers
  const handleUserGroupAdd = () => {
    setUserGroupInput('')
    setUserGroupDialogOpen(true)
  }

  const handleUserGroupSave = () => {
    if (!userGroupInput.trim()) return

    const map = safeJsonParse<Record<string, Record<string, number>>>(
      groupGroupRatio,
      {
        fallback: {},
        silent: true,
      }
    )

    if (!map[userGroupInput.trim()]) {
      map[userGroupInput.trim()] = {}
    }

    onChange('GroupGroupRatio', JSON.stringify(map, null, 2))
    setUserGroupDialogOpen(false)
  }

  const handleUserGroupDelete = (userGroup: string) => {
    const map = safeJsonParse<Record<string, Record<string, number>>>(
      groupGroupRatio,
      {
        fallback: {},
        silent: true,
      }
    )
    delete map[userGroup]
    onChange('GroupGroupRatio', JSON.stringify(map, null, 2))
  }

  const handleOverrideAdd = (userGroup: string) => {
    setGroupOverrideUserGroup(userGroup)
    setGroupOverrideEditData(null)
    setGroupOverrideDialogOpen(true)
  }

  const handleOverrideEdit = (userGroup: string, override: GroupOverride) => {
    setGroupOverrideUserGroup(userGroup)
    setGroupOverrideEditData(override)
    setGroupOverrideDialogOpen(true)
  }

  const handleOverrideSave = (
    targetGroup: string,
    ratio: number,
    oldTargetGroup?: string
  ) => {
    if (!groupOverrideUserGroup) return

    const map = safeJsonParse<Record<string, Record<string, number>>>(
      groupGroupRatio,
      {
        fallback: {},
        silent: true,
      }
    )

    if (!map[groupOverrideUserGroup]) {
      map[groupOverrideUserGroup] = {}
    }

    if (oldTargetGroup && oldTargetGroup !== targetGroup) {
      delete map[groupOverrideUserGroup][oldTargetGroup]
    }

    map[groupOverrideUserGroup][targetGroup] = ratio

    onChange('GroupGroupRatio', JSON.stringify(map, null, 2))
    setGroupOverrideDialogOpen(false)
  }

  const handleOverrideDelete = (userGroup: string, targetGroup: string) => {
    const map = safeJsonParse<Record<string, Record<string, number>>>(
      groupGroupRatio,
      {
        fallback: {},
        silent: true,
      }
    )

    if (map[userGroup]) {
      delete map[userGroup][targetGroup]
      if (Object.keys(map[userGroup]).length === 0) {
        delete map[userGroup]
      }
    }

    onChange('GroupGroupRatio', JSON.stringify(map, null, 2))
  }

  return (
    <div className='space-y-4'>
      <GroupPricingTable
        groupRatio={groupRatio}
        userUsableGroups={userUsableGroups}
        onChange={onChange}
      />

      {/* Topup Group Ratios */}
      <Card className={sectionCardClassName}>
        <CardHeader className={sectionHeaderClassName}>
          <CardTitle>{t('Top-up group ratios')}</CardTitle>
          <CardDescription>
            {t('Multipliers for recharge pricing based on user groups.')}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className='space-y-4'>
            <Button
              onClick={() => handleSimpleAdd('topupGroupRatio')}
              size='sm'
            >
              <Plus className='mr-2 h-4 w-4' />
              {t('Add group')}
            </Button>
            {topupRatioList.length > 0 && (
              <div className='rounded-md border'>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('Group name')}</TableHead>
                      <TableHead>{t('Multiplier')}</TableHead>
                      <TableHead className='text-right'>
                        {t('Actions')}
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {topupRatioList.map((group) => (
                      <TableRow key={group.name}>
                        <TableCell className='font-medium'>
                          {group.name}
                        </TableCell>
                        <TableCell>{group.value}</TableCell>
                        <TableCell className='text-right'>
                          <div className='flex justify-end gap-2'>
                            <Button
                              variant='ghost'
                              size='sm'
                              onClick={() =>
                                handleSimpleEdit('topupGroupRatio', group)
                              }
                            >
                              <Pencil className='h-4 w-4' />
                            </Button>
                            <Button
                              variant='ghost'
                              size='sm'
                              onClick={() =>
                                handleSimpleDelete(
                                  'topupGroupRatio',
                                  group.name
                                )
                              }
                            >
                              <Trash2 className='h-4 w-4' />
                            </Button>
                          </div>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}
          </div>
        </CardContent>
      </Card>

      {/* Inter-group ratio overrides */}
      <Card className={sectionCardClassName}>
        <CardHeader className={sectionHeaderClassName}>
          <CardTitle>{t('Inter-group ratio overrides')}</CardTitle>
          <CardDescription>
            {t(
              'Custom multipliers when specific user groups use specific token groups. Example: VIP users get 0.9x rate when using "edit_this" group tokens.'
            )}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className='space-y-4'>
            <Button onClick={handleUserGroupAdd} size='sm'>
              <Plus className='mr-2 h-4 w-4' />
              {t('Add user group')}
            </Button>
            {groupGroupRatioList.length > 0 && (
              <div className='space-y-3'>
                {groupGroupRatioList.map((userGroupData) => (
                  <Collapsible key={userGroupData.userGroup}>
                    <div className='rounded-lg border'>
                      <div className='flex items-center justify-between p-4'>
                        <div className='flex items-center gap-2'>
                          <CollapsibleTrigger
                            render={<Button variant='ghost' size='sm' />}
                          >
                            <ChevronDown className='h-4 w-4' />
                          </CollapsibleTrigger>
                          <span className='font-semibold'>
                            {userGroupData.userGroup}
                          </span>
                          <span className='text-muted-foreground text-sm'>
                            {t('{{count}} override', {
                              count: userGroupData.overrides.length,
                            })}
                          </span>
                        </div>
                        <div className='flex gap-2'>
                          <Button
                            variant='ghost'
                            size='sm'
                            onClick={() =>
                              handleOverrideAdd(userGroupData.userGroup)
                            }
                          >
                            <Plus className='h-4 w-4' />
                          </Button>
                          <Button
                            variant='ghost'
                            size='sm'
                            onClick={() =>
                              handleUserGroupDelete(userGroupData.userGroup)
                            }
                          >
                            <Trash2 className='h-4 w-4' />
                          </Button>
                        </div>
                      </div>
                      <CollapsibleContent>
                        {userGroupData.overrides.length > 0 && (
                          <div className='border-t'>
                            <Table>
                              <TableHeader>
                                <TableRow>
                                  <TableHead>{t('Target group')}</TableHead>
                                  <TableHead>{t('Ratio')}</TableHead>
                                  <TableHead className='text-right'>
                                    {t('Actions')}
                                  </TableHead>
                                </TableRow>
                              </TableHeader>
                              <TableBody>
                                {userGroupData.overrides.map((override) => (
                                  <TableRow key={override.targetGroup}>
                                    <TableCell className='font-medium'>
                                      {override.targetGroup}
                                    </TableCell>
                                    <TableCell>{override.ratio}</TableCell>
                                    <TableCell className='text-right'>
                                      <div className='flex justify-end gap-2'>
                                        <Button
                                          variant='ghost'
                                          size='sm'
                                          onClick={() =>
                                            handleOverrideEdit(
                                              userGroupData.userGroup,
                                              override
                                            )
                                          }
                                        >
                                          <Pencil className='h-4 w-4' />
                                        </Button>
                                        <Button
                                          variant='ghost'
                                          size='sm'
                                          onClick={() =>
                                            handleOverrideDelete(
                                              userGroupData.userGroup,
                                              override.targetGroup
                                            )
                                          }
                                        >
                                          <Trash2 className='h-4 w-4' />
                                        </Button>
                                      </div>
                                    </TableCell>
                                  </TableRow>
                                ))}
                              </TableBody>
                            </Table>
                          </div>
                        )}
                      </CollapsibleContent>
                    </div>
                  </Collapsible>
                ))}
              </div>
            )}
          </div>
        </CardContent>
      </Card>

      {/* Auto Routes */}
      <Card className={sectionCardClassName}>
        <CardHeader className={sectionHeaderClassName}>
          <CardTitle>{t('Auto route chains')}</CardTitle>
          <CardDescription>
            {t(
              'Configure named automatic routes. Tokens can use auto, auto:fast, auto:cheap, or any custom route key.'
            )}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className='space-y-4'>
            <Button onClick={handleAutoRouteAdd} size='sm'>
              <Plus className='mr-2 h-4 w-4' />
              {t('Add auto route')}
            </Button>
            <div className='space-y-3'>
              {autoRoutesConfig.routes.map((route) => (
                <div key={route.key} className='rounded-lg border p-4'>
                  <div className='flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between'>
                    <div className='min-w-0'>
                      <div className='flex flex-wrap items-center gap-2'>
                        <span className='font-semibold'>{route.key}</span>
                        {route.key === autoRoutesConfig.default_route && (
                          <Badge variant='secondary'>{t('Default')}</Badge>
                        )}
                        {!route.enabled && (
                          <Badge variant='outline'>{t('Disabled')}</Badge>
                        )}
                        {!route.user_selectable && (
                          <Badge variant='outline'>{t('Admin only')}</Badge>
                        )}
                      </div>
                      <p className='text-muted-foreground mt-1 text-sm'>
                        {route.name || route.key}
                      </p>
                    </div>
                    <div className='flex flex-wrap gap-2'>
                      <Button
                        variant={
                          route.key === autoRoutesConfig.default_route
                            ? 'secondary'
                            : 'outline'
                        }
                        size='sm'
                        disabled={
                          route.key === autoRoutesConfig.default_route ||
                          !route.enabled
                        }
                        onClick={() => handleDefaultAutoRouteChange(route.key)}
                      >
                        {t('Set default')}
                      </Button>
                      <Button
                        variant='outline'
                        size='sm'
                        onClick={() => handleAutoRouteEdit(route)}
                      >
                        <Pencil className='h-4 w-4' />
                      </Button>
                      <Button
                        variant='outline'
                        size='sm'
                        disabled={autoRoutesConfig.routes.length <= 1}
                        onClick={() => handleAutoRouteDelete(route.key)}
                      >
                        <Trash2 className='h-4 w-4' />
                      </Button>
                    </div>
                  </div>
                  <div className='mt-4 grid gap-3 sm:grid-cols-2'>
                    <label className='flex items-center gap-2 text-sm'>
                      <Checkbox
                        checked={route.enabled}
                        disabled={
                          route.key === autoRoutesConfig.default_route &&
                          route.enabled
                        }
                        onCheckedChange={(checked) =>
                          handleAutoRouteToggle(
                            route.key,
                            'enabled',
                            checked === true
                          )
                        }
                      />
                      {t('Enabled')}
                    </label>
                    <label className='flex items-center gap-2 text-sm'>
                      <Checkbox
                        checked={route.user_selectable}
                        onCheckedChange={(checked) =>
                          handleAutoRouteToggle(
                            route.key,
                            'user_selectable',
                            checked === true
                          )
                        }
                      />
                      {t('User selectable')}
                    </label>
                  </div>
                  <div className='mt-4 space-y-2'>
                    <p className='text-muted-foreground text-xs font-medium'>
                      {t('Route group order')}
                    </p>
                    {route.groups.map((group, index) => (
                      <div
                        key={`${route.key}-${group}-${index}`}
                        className='flex items-center gap-2 rounded-md border px-3 py-2'
                      >
                        <GripVertical className='text-muted-foreground h-4 w-4' />
                        <span className='min-w-0 flex-1 truncate font-medium'>
                          {group}
                        </span>
                        <Button
                          variant='ghost'
                          size='sm'
                          disabled={index === 0}
                          onClick={() =>
                            handleAutoRouteGroupMove(route.key, index, 'up')
                          }
                          aria-label={t('Move up')}
                        >
                          <span aria-hidden='true'>↑</span>
                        </Button>
                        <Button
                          variant='ghost'
                          size='sm'
                          disabled={index === route.groups.length - 1}
                          onClick={() =>
                            handleAutoRouteGroupMove(route.key, index, 'down')
                          }
                          aria-label={t('Move down')}
                        >
                          <span aria-hidden='true'>↓</span>
                        </Button>
                        <Button
                          variant='ghost'
                          size='sm'
                          disabled={route.groups.length <= 1}
                          onClick={() =>
                            handleAutoRouteGroupDelete(route.key, index)
                          }
                          aria-label={t('Delete')}
                        >
                          <Trash2 className='h-4 w-4' aria-hidden='true' />
                        </Button>
                      </div>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Simple Group Dialog */}
      <SimpleGroupDialog
        open={simpleDialogOpen}
        onOpenChange={setSimpleDialogOpen}
        onSave={handleSimpleSave}
        editData={simpleEditData}
        type={simpleDialogType}
      />

      {/* Auto Route Dialog */}
      <Dialog open={autoRouteDialogOpen} onOpenChange={setAutoRouteDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {autoRouteEditKey ? t('Edit auto route') : t('Add auto route')}
            </DialogTitle>
            <DialogDescription>
              {t(
                'Use route keys like auto, auto:fast, or auto:cheap. Groups must be real billing groups.'
              )}
            </DialogDescription>
          </DialogHeader>
          <div className='space-y-4 py-4'>
            <div className='space-y-2'>
              <Label>{t('Route key')}</Label>
              <Input
                value={autoRouteDialogData.key}
                disabled={!!autoRouteEditKey}
                onChange={(e) =>
                  setAutoRouteDialogData((current) => ({
                    ...current,
                    key: e.target.value,
                  }))
                }
                placeholder='auto:fast'
              />
            </div>
            <div className='space-y-2'>
              <Label>{t('Display name')}</Label>
              <Input
                value={autoRouteDialogData.name}
                onChange={(e) =>
                  setAutoRouteDialogData((current) => ({
                    ...current,
                    name: e.target.value,
                  }))
                }
                placeholder={t('Fast route')}
              />
            </div>
            <div className='space-y-2'>
              <Label>{t('Route groups')}</Label>
              <Textarea
                rows={5}
                value={autoRouteDialogData.groupsText}
                onChange={(e) =>
                  setAutoRouteDialogData((current) => ({
                    ...current,
                    groupsText: e.target.value,
                  }))
                }
                placeholder={'default\nvip\nsvip'}
              />
              <p className='text-muted-foreground text-xs'>
                {t(
                  'Enter one real group per line, or separate groups by comma.'
                )}
              </p>
            </div>
          </div>
          <DialogFooter>
            <Button
              variant='outline'
              onClick={() => setAutoRouteDialogOpen(false)}
            >
              {t('Cancel')}
            </Button>
            <Button onClick={handleAutoRouteSave}>
              {autoRouteEditKey ? t('Update') : t('Add')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* User Group Dialog */}
      <Dialog open={userGroupDialogOpen} onOpenChange={setUserGroupDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('Add user group')}</DialogTitle>
            <DialogDescription>
              {t('Create a new user group to configure ratio overrides for.')}
            </DialogDescription>
          </DialogHeader>
          <div className='space-y-4 py-4'>
            <div className='space-y-2'>
              <Label>{t('User group name')}</Label>
              <Input
                value={userGroupInput}
                onChange={(e) => setUserGroupInput(e.target.value)}
                placeholder={t('vip')}
              />
            </div>
          </div>
          <DialogFooter>
            <Button
              variant='outline'
              onClick={() => setUserGroupDialogOpen(false)}
            >
              {t('Cancel')}
            </Button>
            <Button onClick={handleUserGroupSave}>{t('Add')}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Group Override Dialog */}
      <GroupOverrideDialog
        open={groupOverrideDialogOpen}
        onOpenChange={setGroupOverrideDialogOpen}
        onSave={handleOverrideSave}
        editData={groupOverrideEditData}
        userGroup={groupOverrideUserGroup}
      />
    </div>
  )
})

type GroupPricingTableProps = {
  groupRatio: string
  userUsableGroups: string
  onChange: (field: string, value: string) => void
}

function GroupPricingTable({
  groupRatio,
  userUsableGroups,
  onChange,
}: GroupPricingTableProps) {
  const { t } = useTranslation()
  const [rows, setRows] = useState<GroupPricingRow[]>(() =>
    buildGroupPricingRows(groupRatio, userUsableGroups)
  )

  useEffect(() => {
    const incomingSignature = sourceGroupPricingSignature(
      groupRatio,
      userUsableGroups
    )
    /* eslint-disable react-hooks/set-state-in-effect -- Keep the visual rows synced when the JSON fields are edited directly. */
    setRows((currentRows) => {
      if (groupPricingSignature(currentRows) === incomingSignature) {
        return currentRows
      }
      return buildGroupPricingRows(groupRatio, userUsableGroups)
    })
    /* eslint-enable react-hooks/set-state-in-effect */
  }, [groupRatio, userUsableGroups])

  const emitRows = useCallback(
    (nextRows: GroupPricingRow[]) => {
      setRows(nextRows)
      const serialized = serializeGroupPricingRows(nextRows)
      onChange('GroupRatio', serialized.GroupRatio)
      onChange('UserUsableGroups', serialized.UserUsableGroups)
    },
    [onChange]
  )

  const updateRow = useCallback(
    (
      id: string,
      field: Exclude<keyof GroupPricingRow, '_id'>,
      value: string | number | boolean
    ) => {
      emitRows(
        rows.map((row) => (row._id === id ? { ...row, [field]: value } : row))
      )
    },
    [emitRows, rows]
  )

  const addRow = useCallback(() => {
    const existingNames = new Set(rows.map((row) => row.name))
    let index = 1
    let name = `group_${index}`
    while (existingNames.has(name)) {
      index += 1
      name = `group_${index}`
    }
    emitRows([
      ...rows,
      {
        _id: createGroupPricingId(),
        name,
        ratio: 1,
        selectable: true,
        description: '',
      },
    ])
  }, [emitRows, rows])

  const removeRow = useCallback(
    (id: string) => {
      emitRows(rows.filter((row) => row._id !== id))
    },
    [emitRows, rows]
  )

  const duplicateNames = useMemo(() => {
    const counts = new Map<string, number>()
    for (const row of rows) {
      const name = row.name.trim()
      if (!name) continue
      counts.set(name, (counts.get(name) ?? 0) + 1)
    }
    return Array.from(counts.entries())
      .filter(([, count]) => count > 1)
      .map(([name]) => name)
  }, [rows])

  return (
    <Card className={sectionCardClassName}>
      <CardHeader className={sectionHeaderClassName}>
        <div className='flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
          <div>
            <CardTitle>{t('Pricing groups')}</CardTitle>
            <CardDescription>
              {t(
                'Edit billing ratios and user-selectable groups in one table.'
              )}
            </CardDescription>
          </div>
          <Button onClick={addRow} size='sm' className='sm:self-start'>
            <Plus className='mr-2 h-4 w-4' />
            {t('Add group')}
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        <div className='space-y-3'>
          <div className='overflow-hidden rounded-md border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className='min-w-40'>{t('Group name')}</TableHead>
                  <TableHead className='w-28'>{t('Ratio')}</TableHead>
                  <TableHead className='w-28 text-center'>
                    {t('User selectable')}
                  </TableHead>
                  <TableHead className='min-w-56'>{t('Description')}</TableHead>
                  <TableHead className='w-16 text-right'>
                    {t('Actions')}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.length === 0 ? (
                  <TableRow>
                    <TableCell
                      colSpan={5}
                      className='text-muted-foreground h-20 text-center text-sm'
                    >
                      {t('No groups yet. Add a group to get started.')}
                    </TableCell>
                  </TableRow>
                ) : (
                  rows.map((row) => (
                    <TableRow key={row._id}>
                      <TableCell>
                        <Input
                          value={row.name}
                          onChange={(event) =>
                            updateRow(row._id, 'name', event.target.value)
                          }
                          aria-invalid={duplicateNames.includes(
                            row.name.trim()
                          )}
                        />
                      </TableCell>
                      <TableCell>
                        <Input
                          type='number'
                          min={0}
                          step={0.1}
                          value={String(row.ratio)}
                          onChange={(event) =>
                            updateRow(
                              row._id,
                              'ratio',
                              normalizeRatio(event.target.value)
                            )
                          }
                        />
                      </TableCell>
                      <TableCell>
                        <div className='flex justify-center'>
                          <Checkbox
                            checked={row.selectable}
                            onCheckedChange={(checked) =>
                              updateRow(row._id, 'selectable', checked === true)
                            }
                            aria-label={t('User selectable')}
                          />
                        </div>
                      </TableCell>
                      <TableCell>
                        {row.selectable ? (
                          <Input
                            value={row.description}
                            placeholder={t('Group description')}
                            onChange={(event) =>
                              updateRow(
                                row._id,
                                'description',
                                event.target.value
                              )
                            }
                          />
                        ) : (
                          <span className='text-muted-foreground px-3 text-sm'>
                            -
                          </span>
                        )}
                      </TableCell>
                      <TableCell className='text-right'>
                        <Button
                          variant='ghost'
                          size='sm'
                          onClick={() => removeRow(row._id)}
                          aria-label={t('Delete')}
                        >
                          <Trash2 className='h-4 w-4' />
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>

          {duplicateNames.length > 0 && (
            <p className='text-destructive text-sm'>
              {t('Duplicate group names: {{names}}', {
                names: duplicateNames.join(', '),
              })}
            </p>
          )}
        </div>
      </CardContent>
    </Card>
  )
}

// Simple Group Dialog Component
type SimpleGroupDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: (name: string, value: string) => void
  editData: SimpleGroup | null
  type: 'groupRatio' | 'topupGroupRatio' | null
}

function SimpleGroupDialog({
  open,
  onOpenChange,
  onSave,
  editData,
  type,
}: SimpleGroupDialogProps) {
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const [value, setValue] = useState('')

  const title = type === 'groupRatio' ? t('group ratio') : t('top-up ratio')

  useEffect(() => {
    /* eslint-disable react-hooks/set-state-in-effect -- Reset dialog draft state when a different group is opened. */
    if (!open) {
      setName('')
      setValue('')
      return
    }

    setName(editData?.name ?? '')
    setValue(editData?.value ?? '')
    /* eslint-enable react-hooks/set-state-in-effect */
  }, [editData, open])

  const handleSave = () => {
    if (!name.trim() || !value.trim()) return
    onSave(name.trim(), value.trim())
    setName('')
    setValue('')
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {editData
              ? t('Edit {{title}}', { title })
              : t('Add {{title}}', { title })}
          </DialogTitle>
          <DialogDescription>
            {t('Configure the ratio for this group.')}
          </DialogDescription>
        </DialogHeader>
        <div className='space-y-4 py-4'>
          <div className='space-y-2'>
            <Label>{t('Group name')}</Label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t('default')}
              disabled={!!editData}
            />
          </div>
          <div className='space-y-2'>
            <Label>{t('Ratio')}</Label>
            <Input
              value={value}
              onChange={(e) => {
                const val = e.target.value
                if (val === '' || !isNaN(parseFloat(val))) {
                  setValue(val)
                }
              }}
              placeholder='1.0'
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button onClick={handleSave}>
            {editData ? t('Update') : t('Add')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// Group Override Dialog Component
type GroupOverrideDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: (targetGroup: string, ratio: number, oldTargetGroup?: string) => void
  editData: GroupOverride | null
  userGroup: string | null
}

function GroupOverrideDialog({
  open,
  onOpenChange,
  onSave,
  editData,
  userGroup,
}: GroupOverrideDialogProps) {
  const { t } = useTranslation()
  const [targetGroup, setTargetGroup] = useState('')
  const [ratio, setRatio] = useState('')

  useEffect(() => {
    /* eslint-disable react-hooks/set-state-in-effect -- Reset dialog draft state when a different override is opened. */
    if (!open) {
      setTargetGroup('')
      setRatio('')
      return
    }

    setTargetGroup(editData?.targetGroup ?? '')
    setRatio(editData ? String(editData.ratio) : '')
    /* eslint-enable react-hooks/set-state-in-effect */
  }, [editData, open])

  const handleSave = () => {
    if (!targetGroup.trim() || !ratio.trim()) return
    const parsedRatio = parseFloat(ratio)
    if (isNaN(parsedRatio)) return

    onSave(targetGroup.trim(), parsedRatio, editData?.targetGroup)
    setTargetGroup('')
    setRatio('')
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {editData ? t('Edit ratio override') : t('Add ratio override')}
          </DialogTitle>
          <DialogDescription>
            {userGroup
              ? t(
                  'Configure a custom ratio for "{{userGroup}}" users when using a specific token group.',
                  { userGroup }
                )
              : t(
                  'Configure a custom ratio for when users use a specific token group.'
                )}
          </DialogDescription>
        </DialogHeader>
        <div className='space-y-4 py-4'>
          <div className='space-y-2'>
            <Label>{t('Target group')}</Label>
            <Input
              value={targetGroup}
              onChange={(e) => setTargetGroup(e.target.value)}
              placeholder={t('edit_this')}
              disabled={!!editData}
            />
            <p className='text-muted-foreground text-xs'>
              {t('The token group that will have a custom ratio')}
            </p>
          </div>
          <div className='space-y-2'>
            <Label>{t('Ratio')}</Label>
            <Input
              value={ratio}
              onChange={(e) => {
                const val = e.target.value
                if (val === '' || !isNaN(parseFloat(val))) {
                  setRatio(val)
                }
              }}
              placeholder='0.9'
            />
            <p className='text-muted-foreground text-xs'>
              {t('Multiplier applied when {{userGroup}} uses {{targetGroup}}', {
                userGroup: userGroup || t('this user group'),
                targetGroup: targetGroup || t('this token group'),
              })}
            </p>
          </div>
        </div>
        <DialogFooter>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button onClick={handleSave}>
            {editData ? t('Update') : t('Add')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
