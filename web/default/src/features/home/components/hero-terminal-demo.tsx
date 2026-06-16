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
import { useEffect, useMemo, useState } from 'react'
import {
  BadgeCheck,
  Cable,
  CircleDollarSign,
  Gauge,
  GitBranch,
  LockKeyhole,
  RadioTower,
  Route,
  ShieldCheck,
  Sparkles,
  Workflow,
  type LucideIcon,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'

type FlowTone = 'cyan' | 'amber' | 'emerald' | 'slate'

interface FlowRow {
  model: string
  vendor: string
  protocol: string
  status: string
  cost: string
  tone: FlowTone
}

interface SignalItem {
  label: string
  value: string
  icon: LucideIcon
  tone: FlowTone
}

const FLOW_ROWS: FlowRow[] = [
  {
    model: 'GPT-5',
    vendor: 'OpenAI',
    protocol: 'Responses',
    status: 'routed',
    cost: 'expr:p*1.25+c*10',
    tone: 'cyan',
  },
  {
    model: 'Claude Sonnet',
    vendor: 'Anthropic',
    protocol: 'Messages',
    status: 'cached',
    cost: 'cache hit',
    tone: 'emerald',
  },
  {
    model: 'DeepSeek-R1',
    vendor: 'DeepSeek',
    protocol: 'OpenAI Compatible',
    status: 'fallback',
    cost: 'group x channel',
    tone: 'slate',
  },
  {
    model: 'doubao-seedance',
    vendor: 'VolcEngine',
    protocol: 'video task',
    status: 'proxy',
    cost: 'rate-card: 720p/5s',
    tone: 'amber',
  },
]

const TONE_CLASSES: Record<
  FlowTone,
  { dot: string; text: string; border: string; background: string }
> = {
  cyan: {
    dot: 'bg-cyan-400',
    text: 'text-cyan-700 dark:text-cyan-300',
    border: 'border-cyan-400/30',
    background: 'bg-cyan-400/10',
  },
  amber: {
    dot: 'bg-amber-400',
    text: 'text-amber-700 dark:text-amber-300',
    border: 'border-amber-400/30',
    background: 'bg-amber-400/10',
  },
  emerald: {
    dot: 'bg-emerald-400',
    text: 'text-emerald-700 dark:text-emerald-300',
    border: 'border-emerald-400/30',
    background: 'bg-emerald-400/10',
  },
  slate: {
    dot: 'bg-slate-400',
    text: 'text-slate-700 dark:text-slate-300',
    border: 'border-slate-400/30',
    background: 'bg-slate-400/10',
  },
}

const CYCLE_INTERVAL_MS = 2800

interface HeroTerminalDemoProps {
  className?: string
}

export function HeroTerminalDemo(props: HeroTerminalDemoProps) {
  const { t } = useTranslation()
  const [activeRow, setActiveRow] = useState(0)

  useEffect(() => {
    const mq = window.matchMedia('(prefers-reduced-motion: reduce)')
    if (mq.matches) return

    const intervalId = window.setInterval(() => {
      setActiveRow((value) => (value + 1) % FLOW_ROWS.length)
    }, CYCLE_INTERVAL_MS)

    return () => window.clearInterval(intervalId)
  }, [])

  const signals = useMemo<SignalItem[]>(
    () => [
      {
        label: t('Model governance'),
        value: t('catalog + mapping'),
        icon: RadioTower,
        tone: 'cyan',
      },
      {
        label: t('AgentOps boundary'),
        value: t('token + audit'),
        icon: ShieldCheck,
        tone: 'emerald',
      },
      {
        label: t('Cost rule engine'),
        value: t('expression + rate-card'),
        icon: CircleDollarSign,
        tone: 'amber',
      },
    ],
    [t]
  )

  return (
    <div
      className={cn(
        'relative mx-auto w-full max-w-5xl overflow-hidden rounded-[1.25rem] border border-white/12 bg-[#0b1118]/95 text-slate-100 shadow-[0_24px_80px_-42px_rgba(0,0,0,0.88)]',
        props.className
      )}
    >
      <div
        aria-hidden='true'
        className='absolute inset-0 [background-image:linear-gradient(to_right,rgba(148,163,184,0.36)_1px,transparent_1px),linear-gradient(to_bottom,rgba(148,163,184,0.28)_1px,transparent_1px)] [background-size:34px_34px] opacity-[0.18]'
      />
      <div
        aria-hidden='true'
        className='absolute inset-x-0 top-0 h-px bg-linear-to-r from-transparent via-cyan-300/70 to-transparent'
      />

      <div className='relative grid min-h-[440px] grid-rows-[auto_minmax(0,1fr)_auto]'>
        <div className='flex flex-wrap items-center justify-between gap-3 border-b border-white/10 px-4 py-3 sm:px-5'>
          <div className='flex items-center gap-3'>
            <div className='flex size-9 items-center justify-center rounded-lg border border-cyan-300/20 bg-cyan-300/10'>
              <Route className='size-4 text-cyan-200' aria-hidden='true' />
            </div>
            <div>
              <div className='text-sm font-semibold'>
                {t('AI governance operations plane')}
              </div>
              <div className='text-xs text-slate-400'>
                {t(
                  'Model access, Agent tokens, billing rules, audit scope, and upstream health'
                )}
              </div>
            </div>
          </div>
          <div className='flex items-center gap-2 rounded-full border border-emerald-300/20 bg-emerald-300/10 px-3 py-1.5 text-xs font-medium text-emerald-200'>
            <span className='size-1.5 rounded-full bg-emerald-300 shadow-[0_0_12px_rgba(110,231,183,0.8)]' />
            {t('Service online')}
          </div>
        </div>

        <div className='grid gap-px bg-white/10 lg:grid-cols-[1fr_1.35fr_1fr]'>
          <ControlColumn
            title={t('Applications')}
            icon={Workflow}
            items={[
              'Agent workflow',
              'Research assistant',
              'Image pipeline',
              'Internal model console',
            ]}
          />

          <div className='bg-[#0b1118]/95 p-4 sm:p-5'>
            <div className='mb-4 flex items-center justify-between gap-3'>
              <div>
                <div className='text-xs font-medium tracking-[0.2em] text-slate-500 uppercase'>
                  {t('Routing matrix')}
                </div>
                <div className='mt-1 text-lg font-semibold'>
                  {t('One governance layer, many model protocols')}
                </div>
              </div>
              <div className='rounded-lg border border-white/10 bg-white/[0.04] px-2.5 py-1.5 font-mono text-[11px] text-slate-300'>
                p95 186ms
              </div>
            </div>

            <div className='space-y-2'>
              {FLOW_ROWS.map((row, index) => (
                <FlowRowItem
                  key={row.model}
                  row={row}
                  active={index === activeRow}
                />
              ))}
            </div>

            <div className='mt-4 grid grid-cols-3 gap-2'>
              {signals.map((signal) => (
                <SignalTile key={signal.label} signal={signal} />
              ))}
            </div>
          </div>

          <ControlColumn
            title={t('Upstream platforms')}
            icon={Cable}
            items={[
              'OpenAI',
              'Anthropic',
              'Google Gemini',
              'AWS',
              'Azure',
              'Vertex AI',
              'Ollama',
              'Codex',
              'Dify',
              'RAGFlow',
              'DeepSeek',
              'Alibaba Cloud Bailian',
              'VolcEngine',
              'Kling',
              'Seedance',
              'More compatible APIs',
            ]}
            compact
            align='right'
          />
        </div>

        <div className='grid gap-px border-t border-white/10 bg-white/10 sm:grid-cols-4'>
          <FooterMetric
            icon={GitBranch}
            label={t('Routing policy')}
            value={t('weighted + retry')}
          />
          <FooterMetric
            icon={LockKeyhole}
            label={t('Agent access')}
            value={t('token + model scope')}
          />
          <FooterMetric
            icon={Gauge}
            label={t('Cost operations')}
            value={t('quota + refund')}
          />
          <FooterMetric
            icon={Sparkles}
            label={t('Audit')}
            value={t('admin scoped')}
          />
        </div>
      </div>
    </div>
  )
}

function ControlColumn(props: {
  title: string
  icon: LucideIcon
  items: string[]
  align?: 'left' | 'right'
  translateItems?: boolean
  compact?: boolean
}) {
  const { t } = useTranslation()
  const Icon = props.icon
  const translateItems = props.translateItems ?? true

  return (
    <div className='bg-[#0d131c]/96 p-4 sm:p-5'>
      <div
        className={cn(
          'mb-4 flex items-center gap-2 text-xs font-medium tracking-[0.2em] text-slate-500 uppercase',
          props.align === 'right' && 'justify-end text-right'
        )}
      >
        <Icon className='size-3.5' aria-hidden='true' />
        {props.title}
      </div>
      <div
        className={cn(props.compact ? 'grid grid-cols-2 gap-2' : 'space-y-2')}
      >
        {props.items.map((item, index) => (
          <div
            key={item}
            className={cn(
              'flex items-center gap-2 rounded-lg border border-white/8 bg-white/[0.035] px-3 py-2 text-sm text-slate-300',
              props.compact && 'px-2.5 py-1.5 text-xs',
              props.align === 'right' && 'flex-row-reverse text-right'
            )}
          >
            <span
              className={cn(
                'size-1.5 shrink-0 rounded-full',
                index % 3 === 0 && 'bg-cyan-300',
                index % 3 === 1 && 'bg-emerald-300',
                index % 3 === 2 && 'bg-amber-300'
              )}
            />
            <span className='min-w-0 truncate'>
              {translateItems ? t(item) : item}
            </span>
          </div>
        ))}
      </div>
    </div>
  )
}

function FlowRowItem(props: { row: FlowRow; active: boolean }) {
  const { t } = useTranslation()
  const tone = TONE_CLASSES[props.row.tone]

  return (
    <div
      className={cn(
        'grid grid-cols-[minmax(0,1.05fr)_minmax(0,0.95fr)] gap-3 rounded-xl border px-3 py-3 transition-all duration-500 sm:grid-cols-[minmax(0,1fr)_minmax(0,0.9fr)_minmax(0,1fr)]',
        props.active
          ? `${tone.border} ${tone.background} shadow-[0_0_0_1px_rgba(255,255,255,0.04),0_12px_34px_-24px_rgba(34,211,238,0.65)]`
          : 'border-white/8 bg-white/[0.03]'
      )}
    >
      <div className='min-w-0'>
        <div className='flex items-center gap-2'>
          <span className={cn('size-1.5 rounded-full', tone.dot)} />
          <span className='truncate text-sm font-semibold'>
            {props.row.model}
          </span>
        </div>
        <div className='mt-1 truncate text-xs text-slate-500'>
          {props.row.vendor}
        </div>
      </div>
      <div className='min-w-0'>
        <div className='truncate text-xs text-slate-500'>{t('Protocol')}</div>
        <div className='mt-1 truncate font-mono text-xs text-slate-300'>
          {props.row.protocol}
        </div>
      </div>
      <div className='col-span-2 min-w-0 sm:col-span-1'>
        <div className='flex items-center justify-between gap-2'>
          <span
            className={cn(
              'rounded-md border px-1.5 py-0.5 text-[10px] font-semibold uppercase',
              tone.border,
              tone.text
            )}
          >
            {props.row.status}
          </span>
          <span className='truncate font-mono text-[11px] text-slate-400'>
            {props.row.cost}
          </span>
        </div>
      </div>
    </div>
  )
}

function SignalTile(props: { signal: SignalItem }) {
  const Icon = props.signal.icon
  const tone = TONE_CLASSES[props.signal.tone]

  return (
    <div className='rounded-xl border border-white/8 bg-white/[0.035] p-3'>
      <div className='mb-2 flex items-center justify-between gap-2'>
        <Icon className={cn('size-3.5', tone.text)} aria-hidden='true' />
        <BadgeCheck className='size-3.5 text-emerald-300' aria-hidden='true' />
      </div>
      <div className='truncate text-[11px] text-slate-500'>
        {props.signal.label}
      </div>
      <div className='mt-1 truncate text-xs font-semibold text-slate-200'>
        {props.signal.value}
      </div>
    </div>
  )
}

function FooterMetric(props: {
  icon: LucideIcon
  label: string
  value: string
}) {
  const Icon = props.icon

  return (
    <div className='bg-[#0d131c]/96 px-4 py-3'>
      <div className='flex items-center gap-2 text-[11px] text-slate-500'>
        <Icon className='size-3.5' aria-hidden='true' />
        {props.label}
      </div>
      <div className='mt-1 truncate text-xs font-semibold text-slate-200'>
        {props.value}
      </div>
    </div>
  )
}
