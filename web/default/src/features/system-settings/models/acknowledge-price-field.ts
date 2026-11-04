// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import type {
  FieldPath,
  FieldPathValue,
  FieldValues,
  UseFormReturn,
} from 'react-hook-form'

// Acknowledge only this successful Option write. A later edit and every other
// dirty field remain local work; failed/ambiguous writes must not call this.
export function acknowledgePriceField<
  T extends FieldValues,
  K extends FieldPath<T>,
>(
  form: UseFormReturn<T>,
  name: K,
  submitted: FieldPathValue<T, K>,
  confirmed: FieldPathValue<T, K>
) {
  const current = form.getValues(name)
  // The visual editors also set values for fields without a mounted Controller.
  // resetField requires registration, so register that existing settings field.
  form.register(name)
  form.resetField(name, {
    defaultValue: confirmed,
    keepTouched: true,
    keepError: true,
  })
  if (current !== submitted && current !== confirmed)
    form.setValue(name, current, { shouldDirty: true })
}
