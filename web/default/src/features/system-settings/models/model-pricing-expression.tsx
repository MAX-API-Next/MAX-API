// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Textarea } from '@/components/ui/textarea'
import {
  combineBillingExpr,
  splitBillingExprAndRequestRules,
} from '@/features/pricing/lib/billing-expr'
import {
  createDefaultVisualConfig,
  generateExprFromVisualConfig,
  tryParseVisualConfig,
} from '@/features/pricing/lib/tier-expr'
import {
  generateTaskVisualExpression,
  parseTaskVisualExpression,
  type TaskVisualSchema,
} from './task-visual-expression'
import { TaskVisualForm } from './task-visual-form'
import { VisualEditor } from './tiered-pricing-visual-editor'

export function ModelPricingExpression(props: {
  id: string
  expression: string
  plugin: boolean
  schema?: TaskVisualSchema
  explicitTaskUnits: boolean
  onChange: (value: string) => void
}) {
  const { t } = useTranslation()
  const [unitWarning, setUnitWarning] = useState(false)
  const expression = props.expression
  const task = props.schema
    ? parseTaskVisualExpression(expression, props.schema)
    : null
  const tokenSource = expression.trim()
  const tokenPrefix = tokenSource.startsWith('v1:') ? 'v1:' : ''
  const tokenParts = splitBillingExprAndRequestRules(
    tokenSource.slice(tokenPrefix.length)
  )
  const canEditToken =
    !props.plugin && !props.schema && !/^v\d+:/.test(tokenParts.billingExpr)
  const tokenTiered = canEditToken
    ? tryParseVisualConfig(tokenParts.billingExpr)
    : null
  const plainWrapper = 'tier("base", '
  const tokenPlain =
    canEditToken && !tokenTiered && tokenParts.billingExpr
      ? tryParseVisualConfig(plainWrapper + tokenParts.billingExpr + ')')
      : null
  const token =
    tokenTiered ??
    tokenPlain ??
    (canEditToken && !expression ? createDefaultVisualConfig() : null)
  return (
    <div className='space-y-3'>
      {(task && props.schema) || token ? (
        <details className='rounded-lg border p-3' open>
          <summary className='mb-3 cursor-pointer text-sm font-medium'>
            {t('Visual editor')}
          </summary>
          {task && props.schema && (
            <TaskVisualForm
              config={task}
              schema={props.schema}
              onChange={(next) => {
                const hasUsage = next.tiers.some(
                  (tier) =>
                    tier.conditions.length > 0 ||
                    Object.keys(tier.rates).length > 0
                )
                if (!props.explicitTaskUnits && !hasUsage) {
                  setUnitWarning(true)
                  return
                }
                setUnitWarning(false)
                props.onChange(
                  generateTaskVisualExpression(next, props.schema ?? {})
                )
              }}
            />
          )}
          {token && (
            <VisualEditor
              visualConfig={token}
              onChange={(next) => {
                const generated = generateExprFromVisualConfig(next)
                const plain =
                  tokenPlain &&
                  generated.startsWith(plainWrapper) &&
                  next.tiers.length === 1 &&
                  !next.tiers[0].conditions.length
                const base = plain
                  ? generated.slice(plainWrapper.length, -1)
                  : generated
                props.onChange(
                  tokenPrefix +
                    combineBillingExpr(base, tokenParts.requestRuleExpr)
                )
              }}
            />
          )}
        </details>
      ) : (
        <p className='text-muted-foreground text-xs'>
          {t(
            'Visual editing is unavailable for this expression or source. You can edit the original code below.'
          )}
        </p>
      )}
      {unitWarning && (
        <p role='alert' className='text-destructive text-xs'>
          {t(
            'Keep a usage dimension in this default expression to retain task amount units.'
          )}
        </p>
      )}
      <label className='grid gap-1 text-sm' htmlFor={props.id}>
        {props.plugin
          ? t('Plugin expression override')
          : t('Default expression')}
        <Textarea
          className='font-mono'
          id={props.id}
          rows={4}
          value={expression}
          placeholder={
            props.plugin
              ? t(
                  'Empty inherits the default expression; 0 is explicitly free.'
                )
              : undefined
          }
          onChange={(event) => {
            setUnitWarning(false)
            props.onChange(event.target.value)
          }}
        />
      </label>
    </div>
  )
}
