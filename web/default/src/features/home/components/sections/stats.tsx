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
import { useCallback, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'

interface CounterProps {
  end: number
  suffix?: string
  duration?: number
}

function Counter(props: CounterProps) {
  const { end, suffix = '', duration = 1300 } = props
  const ref = useRef<HTMLSpanElement>(null)
  const startedRef = useRef(false)

  const animate = useCallback(() => {
    const el = ref.current
    if (!el) return
    const start = performance.now()
    const step = (now: number) => {
      const progress = Math.min((now - start) / duration, 1)
      const eased = 1 - Math.pow(1 - progress, 3)
      el.textContent = `${Math.round(eased * end).toLocaleString()}${suffix}`
      if (progress < 1) requestAnimationFrame(step)
    }
    requestAnimationFrame(step)
  }, [duration, end, suffix])

  useEffect(() => {
    const el = ref.current
    if (!el) return

    const mq = window.matchMedia('(prefers-reduced-motion: reduce)')
    if (mq.matches) {
      el.textContent = `${end.toLocaleString()}${suffix}`
      return
    }

    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting && !startedRef.current) {
          startedRef.current = true
          animate()
          observer.unobserve(el)
        }
      },
      { threshold: 0.45 }
    )

    observer.observe(el)
    return () => observer.disconnect()
  }, [animate, end, suffix])

  return (
    <span ref={ref} className='tabular-nums'>
      0{suffix}
    </span>
  )
}

export function Stats() {
  const { t } = useTranslation()
  const stats = [
    {
      value: 50,
      suffix: '+',
      label: t('upstream providers and compatible model platforms'),
    },
    {
      value: 11,
      suffix: '+',
      label: t('video task adapters, status mappings, and protocol paths'),
    },
    {
      value: 100,
      suffix: '+',
      label: t('model pricing entries, expressions, and rate-card rules'),
    },
    {
      value: 6,
      suffix: '',
      label: t('governance layers for Agent workloads'),
    },
  ]

  return (
    <section className='border-border bg-background relative z-10 border-y'>
      <div className='bg-border mx-auto grid max-w-7xl gap-px px-4 sm:px-6 lg:grid-cols-[0.92fr_1.08fr]'>
        <div className='bg-background flex flex-col justify-center py-10 pr-8'>
          <div className='flex items-center gap-3'>
            <span className='h-px w-8 bg-emerald-500/70' />
            <p className='text-muted-foreground text-[11px] font-semibold tracking-[0.28em] uppercase'>
              {t('Operational baseline')}
            </p>
          </div>
          <h2 className='mt-4 max-w-xl font-serif text-3xl leading-[1.1] font-black tracking-[-0.01em] md:text-[2.5rem]'>
            {t('Designed for the continuous operation of AI models and Agents')}
          </h2>
        </div>
        <div className='bg-border grid grid-cols-2 gap-px md:grid-cols-4'>
          {stats.map((stat, index) => (
            <div
              key={stat.label}
              className='group bg-background hover:bg-muted/40 px-5 py-9 transition-colors'
            >
              <span className='editorial-numeral text-muted-foreground/60 text-[11px] font-bold'>
                {String(index + 1).padStart(2, '0')}
              </span>
              <div className='editorial-numeral text-foreground mt-2 text-4xl leading-none font-black tracking-[-0.02em] md:text-5xl'>
                <Counter end={stat.value} suffix={stat.suffix} />
              </div>
              <p className='text-muted-foreground mt-3 text-xs leading-5'>
                {stat.label}
              </p>
            </div>
          ))}
        </div>
      </div>
    </section>
  )
}
