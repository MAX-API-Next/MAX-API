// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
// Layout reference: QuantumNous/new-api ce8fc49e plugin cards and list toolbar.
// Independently composed with MAX's retained-version catalog and existing controls.
import { useState } from 'react'
import { LayoutGrid, List, Package, Search, ChevronRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import type { PluginEntry } from './api'

type Props = {
  onSearchReady: (element: HTMLInputElement | null) => void
  entries: PluginEntry[]
  loading: boolean
  available: boolean
  status: (entry: PluginEntry) => string
  onDetails: (entry: PluginEntry) => void
}

export function PluginStatusBadge(props: {
  entry: PluginEntry
  children: string
}) {
  const enabled =
    props.entry.active && props.entry.enabled && !props.entry.revoked
  return (
    <Badge
      variant={props.entry.revoked ? 'destructive' : 'outline'}
      className={cn(
        'gap-1.5 font-normal',
        enabled &&
          'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900 dark:bg-emerald-950/40 dark:text-emerald-400'
      )}
    >
      <span
        aria-hidden='true'
        className={cn(
          'size-1.5 rounded-full bg-current',
          !enabled && 'opacity-50'
        )}
      />
      {props.children}
    </Badge>
  )
}

function PluginIdentity(props: { entry: PluginEntry }) {
  return (
    <div className='flex min-w-0 items-center gap-3'>
      <div className='bg-muted/60 text-muted-foreground flex size-11 shrink-0 items-center justify-center rounded-xl border'>
        <Package className='size-5' aria-hidden='true' />
      </div>
      <div className='min-w-0'>
        <h3 className='truncate text-sm font-semibold' title={props.entry.name}>
          {props.entry.name}
        </h3>
        <p
          className='text-muted-foreground mt-0.5 truncate font-mono text-xs'
          title={props.entry.key}
        >
          {props.entry.key}
        </p>
      </div>
    </div>
  )
}

function PluginCard(props: {
  entry: PluginEntry
  status: string
  onDetails: () => void
}) {
  const { t } = useTranslation()
  return (
    <article
      aria-label={`${props.entry.name} ${props.entry.version}`}
      className='bg-card flex min-w-0 flex-col rounded-xl border transition-shadow hover:shadow-sm'
    >
      <div className='flex items-start justify-between gap-3 p-5 pb-3'>
        <PluginIdentity entry={props.entry} />
        <PluginStatusBadge entry={props.entry}>
          {props.status}
        </PluginStatusBadge>
      </div>
      <div className='flex flex-1 flex-col gap-4 px-5 pb-5'>
        <div className='flex min-w-0 flex-wrap items-center gap-1.5'>
          <Badge
            variant='secondary'
            className='max-w-full font-mono font-normal'
            title={props.entry.version}
          >
            <span className='truncate'>{props.entry.version}</span>
          </Badge>
          {props.entry.protocols.slice(0, 3).map((p) => (
            <Badge
              key={p.name}
              variant='outline'
              className='max-w-full font-normal'
              title={p.name}
            >
              <span className='truncate'>{p.name}</span>
            </Badge>
          ))}
          {props.entry.protocols.length > 3 && (
            <span className='text-muted-foreground text-xs'>
              +{props.entry.protocols.length - 3}
            </span>
          )}
        </div>
        <div className='space-y-2'>
          <p className='text-muted-foreground text-xs'>
            {t('Declared models')}{' '}
            <span className='tabular-nums'>· {props.entry.models.length}</span>
          </p>
          <div className='flex min-h-12 flex-wrap content-start gap-1.5'>
            {props.entry.models.slice(0, 4).map((model) => (
              <Badge
                key={model}
                variant='secondary'
                className='max-w-full rounded-md font-mono text-[11px] font-normal'
                title={model}
              >
                <span className='truncate'>{model}</span>
              </Badge>
            ))}
            {props.entry.models.length > 4 && (
              <button
                type='button'
                aria-label={`${t('Declared models')}: +${props.entry.models.length - 4} ${props.entry.key}@${props.entry.version}`}
                className='text-muted-foreground hover:text-foreground rounded-md border px-2 text-xs focus-visible:outline-2'
                onClick={props.onDetails}
              >
                +{props.entry.models.length - 4}
              </button>
            )}
            {props.entry.models.length === 0 && (
              <span className='text-muted-foreground text-xs'>—</span>
            )}
          </div>
        </div>
      </div>
      <div className='flex items-center justify-between gap-3 border-t px-5 py-3'>
        <span className='text-muted-foreground text-xs'>
          {t('Package format')} {props.entry.format_version}
        </span>
        <Button
          variant='ghost'
          size='sm'
          aria-label={`${t('Details')} ${props.entry.key}@${props.entry.version}`}
          onClick={props.onDetails}
        >
          {t('Details')}
          <ChevronRight className='size-3.5' aria-hidden='true' />
        </Button>
      </div>
    </article>
  )
}

// Isolate the callback passed to ref so the React compiler does not treat the
// remaining render props as references.
export function PluginCatalog({ onSearchReady, ...props }: Props) {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const [filter, setFilter] = useState('all')
  const [view, setView] = useState<'cards' | 'table'>('cards')
  const rows = props.entries.filter((entry) => {
    const matchesSearch =
      `${entry.key} ${entry.name} ${entry.version} ${entry.models.join(' ')}`
        .toLowerCase()
        .includes(search.trim().toLowerCase())
    let matchesStatus = true
    if (filter === 'enabled')
      matchesStatus = entry.active && entry.enabled && !entry.revoked
    if (filter === 'disabled')
      matchesStatus = entry.active && !entry.enabled && !entry.revoked
    if (filter === 'historical') matchesStatus = !entry.active && !entry.revoked
    if (filter === 'revoked') matchesStatus = entry.revoked
    return matchesSearch && matchesStatus
  })
  const isEmpty = !props.loading && props.available && rows.length === 0
  const emptyTitle = props.entries.length
    ? t('No matching plugins')
    : t('No task plugin packages found')
  const emptyHint = props.entries.length
    ? t('Try a different search or status filter.')
    : t('Install your first plugin package to get started.')
  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap items-center justify-between gap-3 border-b pb-4'>
        <div className='flex items-center gap-2 text-sm font-medium'>
          {t('Plugin catalog')}
          <Badge
            variant='secondary'
            className='font-mono font-normal tabular-nums'
          >
            {props.entries.length}
          </Badge>
        </div>
        <div className='flex w-full flex-wrap items-center gap-2 sm:w-auto'>
          <div className='relative min-w-40 flex-1 sm:w-64'>
            <Search
              className='text-muted-foreground pointer-events-none absolute top-2.5 left-3 size-4'
              aria-hidden='true'
            />
            <Input
              ref={onSearchReady}
              aria-label={t('Search task plugins')}
              className='pl-9'
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder={t('Search task plugins')}
            />
          </div>
          <select
            aria-label={t('Filter by status')}
            className='border-input bg-background h-9 max-w-full rounded-md border px-3 text-sm'
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          >
            <option value='all'>{t('All statuses')}</option>
            <option value='enabled'>{t('Enabled')}</option>
            <option value='disabled'>{t('Disabled')}</option>
            <option value='historical'>{t('Historical version')}</option>
            <option value='revoked'>{t('Revoked')}</option>
          </select>
          <div className='flex items-center gap-0.5 rounded-lg border p-0.5'>
            <Button
              variant={view === 'cards' ? 'secondary' : 'ghost'}
              size='icon-sm'
              aria-label={t('Card view')}
              aria-pressed={view === 'cards'}
              onClick={() => setView('cards')}
            >
              <LayoutGrid className='size-4' aria-hidden='true' />
            </Button>
            <Button
              variant={view === 'table' ? 'secondary' : 'ghost'}
              size='icon-sm'
              aria-label={t('Table view')}
              aria-pressed={view === 'table'}
              onClick={() => setView('table')}
            >
              <List className='size-4' aria-hidden='true' />
            </Button>
          </div>
        </div>
      </div>
      <p
        role='status'
        aria-label={t('Plugin catalog')}
        aria-atomic='true'
        className='sr-only'
      >
        {isEmpty ? `${emptyTitle} ${emptyHint}` : ''}
      </p>
      {props.loading && (
        <p
          role='status'
          className='text-muted-foreground py-12 text-center text-sm'
        >
          {t('Loading...')}
        </p>
      )}
      {isEmpty && (
        <div className='flex flex-col items-center rounded-xl border border-dashed px-6 py-16 text-center'>
          <Package
            className='text-muted-foreground mb-4 size-8'
            aria-hidden='true'
          />
          <h3 className='text-sm font-medium'>{emptyTitle}</h3>
          <p className='text-muted-foreground mt-2 text-sm'>{emptyHint}</p>
        </div>
      )}
      {!props.loading &&
        rows.length > 0 &&
        (view === 'cards' ? (
          <div className='grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3'>
            {rows.map((entry) => (
              <PluginCard
                key={`${entry.key}@${entry.version}`}
                entry={entry}
                status={props.status(entry)}
                onDetails={() => props.onDetails(entry)}
              />
            ))}
          </div>
        ) : (
          <div className='overflow-hidden rounded-xl border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('API extension')}</TableHead>
                  <TableHead>{t('Version')}</TableHead>
                  <TableHead>{t('Status')}</TableHead>
                  <TableHead>{t('Models')}</TableHead>
                  <TableHead>{t('Protocols')}</TableHead>
                  <TableHead className='text-right'>{t('Actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((entry) => (
                  <TableRow key={`${entry.key}@${entry.version}`}>
                    <TableCell className='min-w-52 py-4'>
                      <PluginIdentity entry={entry} />
                    </TableCell>
                    <TableCell className='font-mono text-xs'>
                      {entry.version}
                    </TableCell>
                    <TableCell>
                      <PluginStatusBadge entry={entry}>
                        {props.status(entry)}
                      </PluginStatusBadge>
                    </TableCell>
                    <TableCell className='tabular-nums'>
                      {entry.models.length}
                    </TableCell>
                    <TableCell className='max-w-64 truncate text-xs'>
                      {entry.protocols.map((p) => p.name).join(', ') || '—'}
                    </TableCell>
                    <TableCell className='text-right'>
                      <Button
                        size='sm'
                        variant='outline'
                        aria-label={`${t('Details')} ${entry.key}@${entry.version}`}
                        onClick={() => props.onDetails(entry)}
                      >
                        {t('Details')}
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        ))}
    </div>
  )
}
