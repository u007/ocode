# Part 01 — cdp: `Settle` and a structured `Controls` index

Package: `internal/browse/cdp`. Files: new `pagedom_controls.go` (+ `_test.go`); read
`pagedom_read.go` (the `Snapshot` walker, `snapshotWriter`, `axNode`, `axStates`, `snapshotLine`),
`refs.go` (`RefTable.Mint`), `target_agent.go` (`ReadyState`) first and reuse them.

## 1. `Target.Settle(ctx)`

Waits for the page to be quiet after an action. Rule, copied from fastbrowse after they removed
their spinner heuristics: `document.readyState` is not `loading`, then 200 ms with no DOM mutation
(a `MutationObserver` on `document` in a `Runtime.evaluate` promise with `awaitPromise`), capped at
5 s overall. On the cap it returns normally with `settled=false` (a page still loading is read as it
stands; the caller reports it). It never guesses at spinner class names. Constants are package
consts next to the snapshot caps. A CDP transport error is returned as an error.

## 2. `Target.Controls(ctx, refs) (ControlsPage, error)`

A structured sibling of `Snapshot`, built from the same `Accessibility.getFullAXTree` read (same
`axMaxDepth`, same `axNodeBudget` refusal). It returns:

- page `URL`, `Title` (from the AX root or `Runtime.evaluate`), `Fingerprint` (sha-256 hex of url
  plus the concatenated control lines, short form), `ViewportText` (the visible text, capped at
  6 KB in UTF-16 units like `PageText` does, flagged when cut), `Settled bool` from a `Settle` call
  made first, and `Controls []Control`, `Omitted int`.
- One `Control` per interactive AX node, in document order: `Ref` (minted through the same
  `RefTable` so `@eN` is valid for every element action), `Role`, `Label` (AX name, clipped at 120
  runes), `Value`, `States` (the existing `snapshotStates` set), `Context` (the AX name of the
  nearest ancestor that has a name and whose role is heading, group, form, region, dialog, navigation,
  list or table; empty otherwise), `Operations []string` derived from the role: button, link, tab,
  menuitem, menuitemcheckbox, menuitemradio, option, treeitem → `click`; textbox, searchbox,
  combobox, spinbutton → `fill` and `click`; checkbox, radio, switch → `check` and `uncheck`.
  Nodes with the `disabled` state are kept but carry no operations.
- At most 160 controls; `Omitted` counts the rest. The whole-tree refusal stays as in `Snapshot`.

`Controls` does not write the human-readable snapshot; it is JSON-shaped for the loop. Keep the
walker logic shared (extract the transparent-node and ancestor handling into helpers used by both).

## Tests

- Unit tests with a stubbed AX tree (the package already feeds `axNode` slices to the writer in
  `pagedom_test.go`; follow that pattern): operations per role, context from the nearest named
  ancestor, the 160 cap and `Omitted`, refs minted in the shared table, disabled nodes without
  operations, fingerprint stable across two identical reads and different when a label changes.
- `Settle`: a real-Chrome test gated like `TestPageDOMRealChrome` (skipped without
  `OCODE_CHROME_PATH`) that settles a page which mutates the DOM for 300 ms then stops, and one that
  hits the 5 s cap with `settled=false`.

## Verify

```
go build ./... && go vet ./internal/browse/cdp/ && go test ./internal/browse/cdp/ -run 'Controls|Settle|PageDOM' -count=1
```
