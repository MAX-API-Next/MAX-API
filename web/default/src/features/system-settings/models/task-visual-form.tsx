// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
// UI reference: QuantumNous/new-api 8542c9d6, task-usage-pricing-editor.tsx
// (AGPL-3.0-or-later).
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type { TaskUsageField } from './task-pricing-contract'
import {
  emptyTaskTier,
  taskConditionFor,
  type TaskCondition,
  type TaskTier,
  type TaskVisualExpression,
  type TaskVisualSchema,
} from './task-visual-expression'

const selectClass = 'bg-background rounded-md border p-2'
const numericOperators = ['==', '!=', '>', '>=', '<', '<='] as const
const scalarOperators = ['==', '!='] as const

function AmountInput(props: {
  value: number | undefined
  onChange: (value: number | undefined) => void
  placeholder?: string
  blankZero?: boolean
}) {
  const [enteredZero, setEnteredZero] = useState(false)
  const blank = props.blankZero && props.value === 0 && !enteredZero
  return (
    <Input
      type='number'
      min={0}
      max={Number.MAX_SAFE_INTEGER}
      step='any'
      placeholder={props.placeholder}
      value={blank ? '' : (props.value ?? '')}
      onBlur={() => setEnteredZero(false)}
      onChange={(event) => {
        if (event.target.validity.badInput) return
        if (props.blankZero) setEnteredZero(event.target.valueAsNumber === 0)
        if (event.target.value === '') props.onChange(undefined)
        else if (
          event.target.validity.valid &&
          Number.isFinite(event.target.valueAsNumber)
        ) {
          props.onChange(event.target.valueAsNumber)
        }
      }}
    />
  )
}

function ConditionEditor(props: {
  condition: TaskCondition
  fields: [string, TaskUsageField][]
  onChange: (value: TaskCondition) => void
  remove?: () => void
}) {
  const { t } = useTranslation()
  const condition = props.condition
  const definition = props.fields.find(
    ([name]) => name === condition.field
  )?.[1]
  if (!definition) return null
  const numeric = 'unit' in definition
  return (
    <div className='flex flex-wrap items-end gap-2'>
      <label className='grid gap-1 text-xs'>
        {t('Usage dimension')}
        <select
          className={selectClass}
          value={condition.field}
          onChange={(event) => {
            const field = props.fields.find(
              ([name]) => name === event.target.value
            )
            if (field) props.onChange(taskConditionFor(field[0], field[1]))
          }}
        >
          {props.fields.map(([name]) => (
            <option key={name}>{name}</option>
          ))}
        </select>
      </label>
      <label className='grid gap-1 text-xs'>
        {t('Operator')}
        <select
          className={selectClass}
          value={condition.operator}
          onChange={(event) =>
            props.onChange({
              ...condition,
              operator: event.target.value as TaskCondition['operator'],
            })
          }
        >
          {(numeric ? numericOperators : scalarOperators).map((operator) => (
            <option key={operator}>{operator}</option>
          ))}
        </select>
      </label>
      <label className='grid max-w-40 gap-1 text-xs'>
        {t('Value')}
        {numeric ? (
          <AmountInput
            value={Number(condition.value)}
            onChange={(value) =>
              props.onChange({ ...condition, value: value ?? 0 })
            }
          />
        ) : (
          <select
            className={selectClass}
            value={JSON.stringify(condition.value)}
            onChange={(event) =>
              props.onChange({
                ...condition,
                value: JSON.parse(event.target.value),
              })
            }
          >
            {(definition.type === 'boolean'
              ? [true, false]
              : definition.enum
            ).map((value) => (
              <option key={String(value)} value={JSON.stringify(value)}>
                {String(value)}
              </option>
            ))}
          </select>
        )}
      </label>
      <Button
        type='button'
        size='sm'
        variant='ghost'
        disabled={!props.remove}
        onClick={props.remove}
      >
        {t('Remove condition')}
      </Button>
    </div>
  )
}

export function TaskVisualForm(props: {
  config: TaskVisualExpression
  schema: TaskVisualSchema
  onChange: (value: TaskVisualExpression) => void
}) {
  const { t } = useTranslation()
  const tiers = props.config.tiers
  const fields = Object.entries(props.schema)
  const firstField = fields[0]
  const updateTiers = (next: TaskTier[]) =>
    props.onChange({ ...props.config, tiers: next })
  const updateTier = (index: number, tier: TaskTier) =>
    updateTiers(tiers.map((current, i) => (i === index ? tier : current)))
  return (
    <div className='space-y-3'>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Task expression result is total USD. Blank usage rates omit that dimension. A zero rate still reads the usage value.'
        )}
      </p>
      {tiers.map((tier, index) => {
        const fallback = index === tiers.length - 1
        return (
          <fieldset className='space-y-3 rounded-lg border p-3' key={index}>
            <legend className='px-1 text-sm'>
              {fallback
                ? t('Fallback tier')
                : t('Tier {{number}}', { number: index + 1 })}
            </legend>
            <div className='grid gap-3 sm:grid-cols-2'>
              <label className='grid gap-1 text-xs'>
                {t('Tier name')}
                <Input
                  value={tier.label}
                  onChange={(event) =>
                    updateTier(index, { ...tier, label: event.target.value })
                  }
                />
              </label>
              <label className='grid gap-1 text-xs'>
                {t('Base fee (USD)')}
                <AmountInput
                  value={tier.flat}
                  blankZero
                  placeholder='0'
                  onChange={(value) =>
                    updateTier(index, { ...tier, flat: value ?? 0 })
                  }
                />
              </label>
              {fields
                .filter(([, definition]) => 'unit' in definition)
                .map(([name, definition]) => (
                  <label className='grid gap-1 text-xs' key={name}>
                    {name} (
                    {'unit' in definition &&
                      (definition.unit === 'token'
                        ? t('USD / 1M tokens')
                        : t('USD / {{unit}}', { unit: definition.unit }))}
                    )
                    <AmountInput
                      value={
                        Object.hasOwn(tier.rates, name)
                          ? tier.rates[name]
                          : undefined
                      }
                      onChange={(value) => {
                        const rates =
                          value === undefined
                            ? { ...tier.rates }
                            : { ...tier.rates, [name]: value }
                        if (value === undefined) delete rates[name]
                        updateTier(index, { ...tier, rates })
                      }}
                    />
                  </label>
                ))}
            </div>
            {tier.conditions.map((condition, ci) => (
              <ConditionEditor
                key={ci}
                condition={condition}
                fields={fields}
                onChange={(next) =>
                  updateTier(index, {
                    ...tier,
                    conditions: tier.conditions.map((c, i) =>
                      i === ci ? next : c
                    ),
                  })
                }
                remove={
                  tier.conditions.length > 1
                    ? () => {
                        updateTier(index, {
                          ...tier,
                          conditions: tier.conditions.filter(
                            (_, i) => i !== ci
                          ),
                        })
                      }
                    : undefined
                }
              />
            ))}
            {!fallback && firstField && (
              <div className='flex flex-wrap gap-2'>
                <Button
                  type='button'
                  size='sm'
                  variant='outline'
                  onClick={() =>
                    updateTier(index, {
                      ...tier,
                      conditions: [
                        ...tier.conditions,
                        taskConditionFor(firstField[0], firstField[1]),
                      ],
                    })
                  }
                >
                  {t('Add condition')}
                </Button>
                <Button
                  type='button'
                  size='sm'
                  variant='ghost'
                  onClick={() =>
                    updateTiers(tiers.filter((_, i) => i !== index))
                  }
                >
                  {t('Remove tier')}
                </Button>
              </div>
            )}
          </fieldset>
        )
      })}
      {firstField && (
        <Button
          type='button'
          size='sm'
          variant='outline'
          onClick={() => {
            const tier = emptyTaskTier(`tier-${tiers.length}`)
            tier.conditions = [taskConditionFor(firstField[0], firstField[1])]
            updateTiers([...tiers.slice(0, -1), tier, tiers[tiers.length - 1]])
          }}
        >
          {t('Add tier')}
        </Button>
      )}
    </div>
  )
}
