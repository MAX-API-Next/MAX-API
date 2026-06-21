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
import type { CSSProperties } from 'react'
import { Activity, Coins, KeyRound, Network, ShieldCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'

const ORBIT_NODES = [
  { label: 'OpenAI', angle: '18deg', delay: '0s' },
  { label: 'Claude', angle: '78deg', delay: '-1.4s' },
  { label: 'Gemini', angle: '144deg', delay: '-2.5s' },
  { label: 'DeepSeek', angle: '214deg', delay: '-3.8s' },
  { label: 'Qwen', angle: '288deg', delay: '-5.1s' },
  { label: 'Agents', angle: '336deg', delay: '-6.2s' },
] as const

const SPHERE_POINTS = [
  { x: '17%', y: '31%', delay: '0s' },
  { x: '28%', y: '68%', delay: '-0.8s' },
  { x: '44%', y: '23%', delay: '-1.6s' },
  { x: '82%', y: '42%', delay: '-2.1s' },
  { x: '69%', y: '27%', delay: '-2.8s' },
  { x: '78%', y: '63%', delay: '-3.5s' },
  { x: '31%', y: '38%', delay: '-4.2s' },
  { x: '61%', y: '76%', delay: '-4.9s' },
  { x: '23%', y: '49%', delay: '-5.5s' },
  { x: '72%', y: '47%', delay: '-6.1s' },
  { x: '49%', y: '82%', delay: '-6.8s' },
  { x: '58%', y: '18%', delay: '-7.3s' },
] as const

const SPHERE_PANEL_ITEMS = [
  {
    title: 'Ingress',
    value: 'Apps + Agents',
    icon: Network,
  },
  {
    title: 'Policy',
    value: 'users / groups',
    icon: KeyRound,
  },
  {
    title: 'Settlement',
    value: 'rate-card / expr',
    icon: Coins,
  },
  {
    title: 'Audit',
    value: 'logs / limits',
    icon: ShieldCheck,
  },
] as const

const SPHERE_ARCS = [
  { className: 'home-sphere-arc--primary', delay: '0s' },
  { className: 'home-sphere-arc--secondary', delay: '-1.6s' },
  { className: 'home-sphere-arc--tertiary', delay: '-3.1s' },
] as const

const TELEMETRY_ITEMS = [
  'Policy matched',
  'Route selected',
  'Quota reserved',
  'Task observed',
  'Audit sealed',
] as const

type NeuralSphereProps = {
  logo: string
  name: string
  variant?: 'full' | 'sphere'
}

function sphereStyle(values: {
  x: string
  y: string
  delay: string
}): CSSProperties {
  return {
    '--sphere-point-x': values.x,
    '--sphere-point-y': values.y,
    '--sphere-point-delay': values.delay,
  } as CSSProperties
}

function orbitStyle(values: { angle: string; delay: string }): CSSProperties {
  return {
    '--orbit-angle': values.angle,
    '--orbit-delay': values.delay,
  } as CSSProperties
}

function arcStyle(values: { delay: string }): CSSProperties {
  return {
    '--sphere-arc-delay': values.delay,
  } as CSSProperties
}

function SphereStage({
  logo,
  name,
  standalone = false,
}: Pick<NeuralSphereProps, 'logo' | 'name'> & { standalone?: boolean }) {
  return (
    <div
      className={cn(
        'home-sphere-stage',
        standalone && 'home-sphere-stage--standalone'
      )}
      aria-hidden={standalone || undefined}
    >
      <div className='home-sphere-backplane' aria-hidden='true' />
      <div className='home-orbit home-orbit--outer' aria-hidden='true' />
      <div className='home-orbit home-orbit--inner' aria-hidden='true' />
      <div className='home-orbit home-orbit--tilted' aria-hidden='true' />

      {ORBIT_NODES.map((node) => (
        <div
          key={node.label}
          className='home-orbit-node'
          style={orbitStyle(node)}
        >
          <span>{node.label}</span>
        </div>
      ))}

      <div className='home-sphere' aria-hidden='true'>
        <div className='home-sphere-grid home-sphere-grid--lat' />
        <div className='home-sphere-grid home-sphere-grid--lng' />
        <div className='home-sphere-halo home-sphere-halo--equator' />
        <div className='home-sphere-halo home-sphere-halo--meridian' />
        {SPHERE_ARCS.map((arc) => (
          <span
            key={arc.className}
            className={cn('home-sphere-arc', arc.className)}
            style={arcStyle(arc)}
          />
        ))}
        <div className='home-sphere-core'>
          <img src={logo} alt={name} className='home-sphere-logo' />
        </div>
        {SPHERE_POINTS.map((point, index) => (
          <span
            key={`${point.x}-${point.y}`}
            className='home-sphere-point'
            style={sphereStyle(point)}
            data-index={index}
          />
        ))}
      </div>
    </div>
  )
}

export function NeuralSphere({
  logo,
  name,
  variant = 'full',
}: NeuralSphereProps) {
  const { t } = useTranslation()

  if (variant === 'sphere') {
    return <SphereStage logo={logo} name={name} standalone />
  }

  return (
    <div
      className='home-visual-shell'
      aria-label={t('Live AI governance visualization')}
    >
      <div className='home-visual-topbar'>
        <div className='flex items-center gap-2'>
          <span className='home-window-dot' />
          <span className='home-window-dot home-window-dot--secondary' />
          <span className='home-window-dot home-window-dot--tertiary' />
        </div>
        <div className='flex items-center gap-2 text-xs text-slate-400'>
          <Activity aria-hidden='true' className='size-3.5' />
          {t('Governance fabric online')}
        </div>
      </div>

      <SphereStage logo={logo} name={name} />

      <div className='home-visual-panels'>
        {SPHERE_PANEL_ITEMS.map((item) => {
          const Icon = item.icon
          return (
            <div key={item.title} className='home-visual-panel'>
              <Icon aria-hidden='true' />
              <div>
                <span>{t(item.title)}</span>
                <strong>{t(item.value)}</strong>
              </div>
            </div>
          )
        })}
      </div>

      <div className='home-telemetry-band' aria-hidden='true'>
        <div className='home-telemetry-track'>
          {[...TELEMETRY_ITEMS, ...TELEMETRY_ITEMS].map((item, index) => (
            <span key={`${item}-${index}`}>{t(item)}</span>
          ))}
        </div>
      </div>
    </div>
  )
}
