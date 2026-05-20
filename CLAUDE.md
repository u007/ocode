# ocode Project Instructions

## File Reading

When reading files, do not show the entire file content. Show only the relevant excerpts needed for the current task.

## TUI Config Safety

The `Config.Ocode` struct field is a pointer and can be nil. Before accessing any `Config.Ocode` property in TUI components (model.go, views, handlers), always guard with `if c.Config.Ocode != nil { ... }` to prevent nil pointer panics. This applies even to read-only property accesses.
