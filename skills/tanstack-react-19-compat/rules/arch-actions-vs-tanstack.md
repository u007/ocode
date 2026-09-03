# arch-actions-vs-tanstack: React 19 Actions vs. TanStack Form/Query

## Priority: LOW — architectural guidance, not a compatibility bug

## Explanation

React 19's `useActionState`, `<form action={fn}>`, and `useFormStatus` are not incompatible with TanStack Form/Query — `@tanstack/react-form` has an official integration (`@tanstack/react-form-nextjs`'s `useTransform`/`mergeForm`) for merging server-action state into form state. The two are solving different problems, so pick per-case rather than treating one as a replacement for the other:

| Use React 19 Actions when... | Use TanStack Form/Query when... |
|---|---|
| Form state is local and simple (no persisted cache) | Form/query data must be cached, invalidated, or shared across components |
| No client-side validation beyond what the server enforces | Need schema validation (Zod), field-level state, or optimistic updates |
| A single server round-trip is enough (progressive enhancement) | Need retries, background refetch, mutation state (`isPending`, `isError`) surfaced to multiple consumers |
| No existing TanStack Query cache for the affected data | The submission should invalidate/update an existing `useQuery` cache entry |

## Good Example — combining them (server action result feeding a TanStack Form)

```tsx
import { useForm, mergeForm, useTransform } from '@tanstack/react-form'
import { useActionState } from 'react'

function ProfileForm({ initialFormState }: { initialFormState: FormState }) {
  const [state, submitAction] = useActionState(updateProfileAction, initialFormState)

  const form = useForm({
    transform: useTransform((baseForm) => mergeForm(baseForm, state), [state]),
    onSubmit: async ({ value }) => {
      // still goes through TanStack Query's mutation cache for optimistic UI
      await updateProfileMutation.mutateAsync(value)
    },
  })

  return <form action={submitAction}>{/* fields */}</form>
}
```

## Rule

- Don't migrate an existing TanStack Query mutation to a raw React 19 Action just because Actions are newer — only do it if the form has no caching/shared-state needs.
- Don't fight the two systems for control of the same submit handler; either let the Action own submission and feed its result into the form via `mergeForm`, or use `onSubmit` + a Query mutation and skip `useActionState` for that form.
