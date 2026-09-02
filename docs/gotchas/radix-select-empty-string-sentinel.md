---
type: Gotcha
title: Radix Select Empty String Sentinel
description: Radix Select rejects empty strings as item values; use a sentinel value instead
tags:
  - gotcha
  - radix
  - forms
  - ui-library
timestamp: 2026-09-02T07:07:53Z
---
Radix UI's `Select` component rejects empty strings (`""`) as item values. When a select field needs to represent a "no value" or "default" option — such as an empty `backend_url` meaning "same origin" — passing `""` as the value attribute silently breaks: the component fails to select the item, the display text never updates, and form submission sends stale data.

## Workaround

Use a sentinel value instead of the empty string:

```tsx
const SAME_ORIGIN = "__SAME_ORIGIN__";

// Map display → storage
function toStorageValue(display: string) {
  return display === "" ? SAME_ORIGIN : display;
}

// Map storage → display
function fromStorageValue(value: string) {
  return value === SAME_ORIGIN ? "" : value;
}
```

When reading from the API, map the empty string to the sentinel before rendering. When saving, map the sentinel back to the empty string before sending.

## When to apply

This pattern applies whenever a Radix Select (or Radix-based combobox) must represent an optional/free-text field that can legitimately be empty — e.g., optional backend URLs, default namespaces, or nullable dropdown fields that map to a "no preference" server value.

## Reference

Encountered during `BackendForm.tsx` implementation for the browser panel proxy configuration.