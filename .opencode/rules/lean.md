# Lean changes

<!-- Adapted from Ponytail v5.3.3 (https://github.com/DietrichGebert/ponytail), MIT License, Copyright Dietrich Gebert. Trial rule: remove this file to switch it off. -->

Solve the whole problem with the least new code. The best code is the code never written.

## Before you write

Read the task and the code it touches. List every place your change must reach: callers, tests, fixtures, config, exports. Check what your change could break for users: data it would destroy or expose, callers that stop working. That is scope. Extra features are not.

## The smallest complete change

Take the first option that fully works:

1. Does it need to exist? Skip features, options and flexibility nobody asked for, and name them in one line.
2. Already in this codebase (a helper, component, service, pattern)? Use it the way the surrounding code does.
3. Does the project already use a library for this job? Keep using it, even where the standard library could do the same. Consistency beats a second way of doing one thing.
4. Standard library or a platform feature? Use it.
5. Another installed dependency? Use it. Never add a dependency for a few lines.
6. Otherwise: the minimum code that works. One line when a reader gets it at a glance.

- Be lazy about the solution, never about the change itself: finish every part the task needs, including the callers, tests and fixtures your change breaks.
- No abstraction, wrapper, type conversion, option, config, boilerplate or "for later" code nobody asked for. Deletion beats addition. Keep the structure the codebase already has: its layers, interfaces and conventions.
- The invariants in `CLAUDE.md` are scope, not extra code. A shorter diff that skips one is wrong.
- The shortest working diff wins, once you know everything it must touch. A one-liner that needs decoding is not short.
- Comment only the why the code cannot show, in one line.
- Bug fix: before you edit, grep every caller of the function you touch, then fix the root cause once in the shared code.
- Code you move or merge keeps its error handling and validation.
- Between options of equal size, take the one that is correct on edge cases.
- New non-trivial logic (a branch, a loop, a parser, money or security) leaves a test that fails when it breaks.
- A shortcut with a known limit gets a code comment in this form: `shortcut: <the limit>, <when to upgrade>`.

Never cut: validation at trust boundaries, error handling that prevents data loss, security, accessibility, anything the user asked for.

## With process skills

These rules decide how much code to write. A loaded process skill (test-driven development, brainstorming, planning, verification) decides how the work is done, and it wins where the two disagree: if it asks for a test or a design step, do it, then keep the design and the code this small.
