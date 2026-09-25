# Plan Part 4: Documentation and Deferred Surfaces

## Scope

Document the shipped web list-dialog keyboard contract and keep the repository's
curated documentation and task tracking aligned with the implementation.

## Files

- Add a curated concept document under `docs/concepts/` through the context
  documentation writer.
- Update `skills/ocode-web/SKILL.md` only where its dialog/keyboard guidance now
  needs the new contract.
- Add the new concept document to `docs/index.md`.
- Record any deliberately deferred list surfaces by exact component name in
  `TODO.md`; do not leave an unbounded “other selectors” note.

## Order of work

1. Capture the final behavior after implementation and tests pass.
2. Ask the context agent to write the concept document and update the curated
   index through the knowledge-bundle documentation tools.
3. Update the web skill with the shared helper, scope boundaries, and regression
   test pointers.
4. Add named deferred surfaces to TODO.md only if they remain out of scope.
5. Review generated documentation diffs without staging unrelated concurrent
   hunks.

## Required content

Document ArrowUp/ArrowDown, Home/End, Enter, explicit multi-select Space,
search-input entry/return, no-wrap/clamping, real focus, nested actions,
SessionDialog load-more, and the distinction between cmdk/Radix surfaces and
custom dialogs. State that ModelDialog remains single-select and that no global
keyboard listener was added.

## Validation

Check documentation paths and links, run `git diff --check`, and verify the
final working tree contains no accidental generated or unrelated changes.
