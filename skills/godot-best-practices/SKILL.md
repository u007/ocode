---
name: godot-best-practices
description: Godot 4 / GDScript best practices and gotcha avoidance. Use when writing, reviewing, or debugging Godot projects (.gd, .tscn, project.godot) to avoid memory leaks, orphan nodes, dangling references, fragile get_node paths, per-frame waste, bad scene/autoload architecture, slow loading, and version-control mistakes. Triggers: "godot", "gdscript", "scene tree", "autoload", "queue_free", "memory leak godot", "review my godot code", ".tscn", ".gd".
---

# Godot Best Practices (GDScript, Godot 4)

Source: Godot "Best practices" docs (`docs/tutorials/best_practices/`, stable branch), plus the
engine-lifecycle rules in section 8, which the docs do not state as a single page. Sections 1-7 map
to the docs; section 8 is marked where it goes beyond them.

Rules are imperative. When a rule conflicts with a user request, the user wins. When a rule
conflicts with project docs, the project docs win and the conflict gets raised first.

## 0. Fast checklist (run before finishing any Godot change)

- [ ] Every node created with `.new()` or `instantiate()` is either added to the tree or freed.
- [ ] Nodes are removed with `queue_free()`, not `free()`, unless no frame is in progress that could still touch them.
- [ ] A node created but discarded before `add_child()` is freed immediately with `free()`.
- [ ] Node references are cached with `@onready var x = $Path` (or `@export`), not looked up each frame.
- [ ] No `get_node()` / `$` in `_process` or `_physics_process` loops.
- [ ] No input polling in `_process`; use `_input` / `_unhandled_input`, or signals.
- [ ] Recurring work that does not need per-frame updates uses a `Timer` timeout, not `_process`.
- [ ] Each scene has no dependencies on its parent's internal structure.
- [ ] Autoloads hold only systems that manage their own data and must be global.
- [ ] No reference cycles between `RefCounted` objects (they never free).
- [ ] Externally assigned references are checked (`is_instance_valid` / `has_method`) before use.
- [ ] `const X = preload(...)` only for stable imports. Replaceable resources are `@export` or `load()` into a variable, set to `null` when done.
- [ ] Properties set before `add_child()` when possible (exception: `global_position`, which needs the node in the tree).
- [ ] `.godot/` is git-ignored. Line endings are LF.

## 1. Scene and node architecture

- **Scenes have no dependencies.** A scene contains everything it needs. Reusing it must not break node paths or signal connections.
- **Use dependency injection** when a scene must talk to its environment. A higher-level parent supplies the dependency. Classes that reach into their surroundings cause bugs that are hard to trace.
- **Expose data through one of these, in this preference order:**
  1. Signals. The child emits, the parent connects. Signal names are past-tense verbs (`item_collected`). Signals announce behavior; they do not start it.
  2. Callable properties. The parent assigns a `Callable`; the child calls it. The child does not need to own the method.
  3. Node or Object references assigned by the parent.
  4. NodePath, resolved with `get_node()`. Last resort.
  5. Method-name strings. Avoid; the child must own that method.
- **Call down, signal up.** A parent calls methods on its children directly (it owns them). A child reports upward by emitting a signal. This matches the docs' signals-first ordering. It prevents dangling references and tight coupling. A child holding a parent reference does not keep the parent alive, since `Node` is not reference-counted; the risk is coupling and invalid access, not a leak.
- **Keep internal access points hidden.** Public API changes should be rare, so callers do not break.
- **Siblings only know their own hierarchy.** An ancestor mediates between siblings. The owner of a set of objects manages the relationships among them.
- **Null-check every `get_node()` result** when you are not sure the node exists. Null return values crash when connected or assigned.
- **Declare requirements in the editor.** Override `_get_configuration_warnings()` in `@tool` scripts to return a `PackedStringArray` of unmet dependencies. This shows a warning icon in the Scene dock instead of relying on separate docs.
- **Single entry point.** A `Main` node with a `main.gd` script controls flow. Typical children: `World` (Node2D/Node3D) and `GUI` (Control).
- **Swap level content, not the whole app.** Change the children of `World` for level transitions, or use `SceneTree.change_scene_to_file()` for simple games.
- **Protect GUI across scene changes.** Make GUI an autoload, a temporary child of the root, or a direct child of the root. Otherwise it is deleted with the scene.
- **Parent-child only for true ownership.** Ask: if the parent is removed, should this node be removed too? If not, make it a sibling or put it elsewhere.
- **Think relationally, not spatially.** A node that cannot exist without its parent is a child of it, usually inside the parent's scene.
- **Nodes are not components.** The node tree expresses aggregation. Do not reshuffle nodes often.
- **Follow a transform without inheriting it.** Insert a plain `Node` between them (declarative), or set `top_level` on a `CanvasItem` / `Node3D` (imperative). Use `RemoteTransform2D` / `RemoteTransform3D` when a separate node must mirror another's transform.
- **Shared entities live outside volatile branches.** A player that moves between rooms should not live inside a room that gets deleted.
- **Give each subsystem its own branch** of the SceneTree.
- **Networked games:** keep authority-only or player-specific systems (for example a PlayerController) separate from the shared world branch.

### Scenes vs scripts

- A scene declares structure (node composition, signal wiring, initial properties). A script declares behavior. Usual pattern: a scene plus a scripted root node.
- Scripts extend an engine class imperatively. Building large node trees in script is slow; each instruction crosses the scripting API. Prefer a `PackedScene` for anything large or complex.
- Scripts without `extends` implicitly extend `RefCounted`. They cannot be attached to a `Node`.
- Named types: `class_name` makes a script a type usable in the editor and at runtime. `EditorPlugin.add_custom_type` is editor-only and does not carry type information at runtime.
- Give a scene a constant name via a `RefCounted`-based script with `const MyScene = preload(...)`, then use `MyScene.instantiate()`. This keeps the script out of the create dialog.

## 2. Lifecycle, notifications, and processing

- **Order:** `_init()` runs first (constructor semantics), then `_enter_tree()`, then `_ready()`. `_ready()` runs children first, then parents.
- **Build a script's own node subtree in `_init()`** (no SceneTree needed yet). Anything that needs the tree (group membership, global position, parent lookups) goes in `_enter_tree()` or `_ready()`.
- **Exported value resolution** on instantiate: declared default, then `_init()` assignments, then the Inspector value (which overrides both). Setters can run more than once with different values; keep setters free of side effects that break under repeated calls.
- **Use `_notification()` with `match` on `NOTIFICATION_*`** for lifecycle events that have no dedicated virtual. Two examples:
  - `NOTIFICATION_PARENTED` / `NOTIFICATION_UNPARENTED`: cache the parent, check `has_user_signal(...)` before connecting, and disconnect on unparent.
  - `NOTIFICATION_PREDELETE` (on `Object`): the destructor hook. Free owned, non-Node objects here.
- **`_process(delta)`** only for work that truly needs per-frame updates and a variable delta.
- **`_physics_process(delta)`** for fixed-step movement and kinematic work.
- **Do not poll input in `_process` / `_physics_process`.** Use `_input()` / `_unhandled_input()`. You may still call `Input.is_action_just_pressed()` inside them.
- **Periodic work** that does not need every frame: a `Timer` child with `timeout` connected to a handler.
- **Set values before `add_child()`.** Some setters do heavy update work. Global position is the exception and must be set after the node is in the tree.

## 3. Node references, duck typing, interfaces

- **Cache references:** `@onready var child: Sprite2D = $Child`. Exported node references are safe across scene-tree moves when assigned in the Inspector.
- **Validate externally assigned references** before use: return early, print an error, or `assert`. Use `is_instance_valid(obj)` for objects that may have been freed.
- **`assert` is stripped from release export templates.** Never rely on it for runtime validation of input that can happen in production; use an explicit check with a return or error.
- **Godot is duck-typed.** It checks whether an object has a member, not its type. Choose the check deliberately:
  - Direct access (`get_parent().visible = false`): halts if unsupported.
  - `call("set_visible", false)`: dynamic call.
  - `has_method("set_visible")` before calling: optional behavior.
  - `obj is CanvasItem`: type check; adds a dependency for user-defined types.
- **Property lookup can be overridden** by setters, getters, `_get`, and `_set`. Do not assume a property is a plain member.
- **Pass `Callable`s to minimize coupling.** The child calls what it was given; the parent wires it up.
- **Names and groups as informal interfaces** only with documentation. Groups are slower than static access.
- **Resources are references.** Loading returns the cached instance. Use `duplicate()` or `new()` when you need an independent copy.
- **PascalCase** for scene and script constants, **snake_case** for variables that hold them.

## 4. Data types and performance

- **Use static typing** (`var hp: int`, `var pos := Vector2.ZERO`, `Array[Enemy]`). The static typing docs say it detects more errors before running, and typed code uses optimized opcodes when operand types are known at compile time. They give no benchmark figures.
- **Project warnings** (Project Settings > Debug > GDScript; enable Advanced Settings to see them, both off by default):
  - `UNTYPED_DECLARATION`: set to Warn or Error to require static types everywhere.
  - `INFERRED_DECLARATION`: opt-in team preference for explicit types. Do not enable by default: it flags `:=`, which the style guide recommends when the type is obvious.
- **Arrays:**
  - Iteration and index get/set are fast. Append and pop at the end are fast.
  - Insert or remove at the front is slow. For many front edits, reverse, edit at the end, reverse back.
  - `find()` is linear. If you search often, use a Dictionary keyed by the value.
- **Dictionaries:** fast key access, insertion-ordered iteration. They keep gaps and grow in powers of two, so memory is traded for speed. Do not search for a key by its value.
- **Objects vs Dictionary/Array:** property lookups on Objects walk the script, ClassDB, and inheritance chain, which is slower. Use an Object when you need a stable API, guaranteed properties, or a custom type. Use Dictionaries or Arrays for hot, bulk data.
- **Lightest type that fits the job:**
  - `Object`: lightest; manual memory. Free it yourself.
  - `RefCounted`: default for custom data classes; freed when unreferenced.
  - `Resource`: data that needs Inspector support or `.tres` save/load.
  - `Node`: only when you need the tree (processing, children, signals from the tree).
- **Enums:** int enums compare in constant time. String enums (`@export_enum` only in GDScript) compare in linear time but print directly; use a name map for ints when printing.
- **Algorithmic cost:** avoid O(n) per-frame work over large data. Constant-time work is cheap regardless of size.
- **Animation:** `AnimatedSprite2D` plus `SpriteFrames` for frame-based 2D; `AnimationPlayer` when other effects must be triggered alongside it; `AnimationTree` for blending.

## 5. Loading, preloading, and memory

- **`preload()`** loads at script load time. Use for stable imports (classes, scenes, that never change).
- **`load()`** loads at the call site. Calling it mid-frame causes hitches. Use it for resources you need later.
- **Constants keep resources alive** for as long as the script is loaded. For big, replaceable resources, use a variable:
  - `@export var office_scene: PackedScene` (leave the default empty).
  - `var warehouse: PackedScene = load("res://warehouse.tscn")`, then `warehouse = null` to drop it.
- **Do not `preload` into an export default** that the scene overrides; the preload is wasted.
- **`const X = load(...)` is an error.** `load` is a runtime call.
- **Large levels:** split into smaller scenes for reuse. Static loading is simple but wastes RAM on big worlds. Dynamic loading (a manager node that creates, loads, unloads at runtime) suits large or procedural worlds, at the cost of complexity. For medium/large projects, put the manager in a plugin or library once it stabilizes.

## 6. Autoloads (singletons)

- **Prefer regular nodes by default.** The node tree and signals avoid most global state.
- **Use an autoload only if all three hold:** it tracks its own data, it must be globally reachable, and it can exist in isolation (for example a quest, dialogue, or save system).
- **Never let an autoload modify another system's data.** Make it its own script or scene.
- **Autoloads are not freed** on `change_scene_to_file()`. They persist, so reset their state on scene changes when needed.
- **Autoloads are not singletons.** Copies can still be instantiated; they just live under the root.
- **Shared behavior without autoloads:**
  - Shared functions: a `Node` with `class_name`.
  - Shared data: a custom `Resource`, or a reference on a common ancestor.
  - Helper libraries: `static func` in a `class_name` script. Static functions cannot use instance members or `self`.
  - Shared variables across instances: `static var` (Godot 4.1+).
- **Access an autoload** by name (for example `Sound`) or `get_node("/root/Sound")`.
- Global audio pools are the classic failure: too small causes dropped sounds, too large wastes memory. Give each scene its own players.

## 7. Project organization and version control

- **snake_case** for files and folders. **PascalCase** for node names. Lowercase generally, consistent throughout. C# scripts are the exception: PascalCase, matching the class.
- **Style guide.** The official GDScript style guide is at `docs.godotengine.org/en/stable/tutorials/scripting/gdscript/gdscript_styleguide.html`. Key rules:
  - snake_case for files, functions, variables, and signals. PascalCase for class and node names and enums (singular). CONSTANT_CASE for constants and enum members.
  - Signal names are past tense. Prefix virtual methods and private members with `_`.
  - Declaration order: tool/class annotations, `class_name`, `extends`, doc comments, signals, enums, constants, static vars, exports, other vars, `@onready` vars, then `_init`/`_ready` and other callbacks, then methods, then inner classes. Public before private within each group.
  - Consistency inside a project matters more than exact compliance.
- **Keep assets near the scenes that use them.** Shared base assets (sprites, meshes, materials, music) in one folder; built levels in another that references them.
- **Third-party code** goes in a top-level `addons/` folder, even when it is not an editor plugin. Exception: a character's own third-party assets stay with that character.
- **`.gdignore`** (empty file, leading dot) stops import and hides a folder. Such a folder cannot be loaded with `load()` / `preload()`. It does not use `.gitignore` patterns.
- **Case matters for export.** Windows and macOS are often case-insensitive; Linux and Godot's PCK filesystem are not. Mismatched casing breaks exports.
- **Git:**
  - Ignore `.godot/` (regenerated on open) and `*.translation` (generated from CSV).
  - Generate `.gitignore` / `.gitattributes` from the project manager (Git option) or **Project > Version Control > Generate Version Control Metadata**.
  - Use LF. Set `git config --global core.autocrlf input` on Windows. The generated `.gitattributes` uses `* text=auto eol=lf`.
  - Large binaries (`*.glb`, images, audio, `.scn`, `.res`, `.material`, `.anim`, `.mesh`, `.lmbake`, and so on): Git LFS. Run `git lfs install` and `git lfs track` before committing those files; anyone cloning needs LFS.

## 8. Memory leaks and lifecycle hazards (engine semantics, beyond the docs' best-practice pages)

These come from Godot 4 object lifecycle behavior, not from the best-practice pages.

**Leak profiling recipe.** Read the counters from the `Performance` singleton. The editor's Debugger Monitors tab is the usual place to view them; the debugger docs did not list the labels, so check them in your version:
- `Performance.OBJECT_COUNT`: all live Objects, including nodes.
- `Performance.OBJECT_NODE_COUNT`: nodes in the scene tree.
- `Performance.OBJECT_RESOURCE_COUNT`: resources in use.
- `Performance.OBJECT_ORPHAN_NODE_COUNT`: nodes not parented to the tree. Debug builds only; returns 0 in release exports.
- Run the project from the editor. Switch scenes back and forth. If counts keep climbing and do not return to baseline, inspect dynamic spawn and removal code.
- Optional: read `Performance.get_monitor(...)` in a test scene and assert on the counts.

- **Orphan nodes.** A node created with `.new()` / `instantiate()` and never added to the tree or freed leaks. Removing a node from the tree with `remove_child()` also leaves it alive. Call `queue_free()` after removing it, unless you are pooling it on purpose.
- **`queue_free()` vs `free()`:**
  - `queue_free()` defers deletion to the end of the frame. Safe in signal handlers and loops. It also detaches the node, so it does not need a separate `remove_child()` first.
  - `free()` deletes immediately. Using a node afterward, or freeing it while it is mid-call, causes errors or crashes.
  - Use `free()` for two cases only: a node you created and discarded before `add_child()`, or an object you own where nothing else touches it this frame.
- **Freed Objects go invalid silently.** A held reference to a freed Object errors when used. Check with `is_instance_valid()` or ensure the owner outlives the reference.
- **Reference cycles leak `RefCounted`.** Two `RefCounted` objects holding each other are never freed. Break one side with `weakref()`, or drop the reference explicitly:
  ```gdscript
  var parent_weak := weakref(parent_instance)
  # Later:
  var parent = parent_weak.get_ref()
  if parent:
      parent.do_something()
  ```
- **Signals and Callables:**
  - Verify in your Godot version whether connections to a freed Object target are removed automatically. Do not rely on it for lambdas or Callables you bound yourself.
  - For connections to objects you do not own, disconnect explicitly in `NOTIFICATION_UNPARENTED` / `_exit_tree()`.
  - A `Tween` created with `create_tween()` on a node is bound to that node. A tween created on a long-lived object outlives the scene; kill it when no longer needed.
- **`await` on a signal from an object that gets freed** may never resume. Guard the continuation with `is_instance_valid()`.
- **Scene changes do not free autoloads.** Autoload-held references to scene nodes become stale after `change_scene_to_file()`. Clear them.
- **Dynamic resources:** a resource held in a property stays in memory until the property is cleared. Set large resources to `null` when a level or feature is unloaded.
- **Pooling:** the best-practices pooling page returned 404 at fetch time; no pooling guidance is captured here. Measure before pooling.

## 9. Review checklist for a Godot change

1. Node lifetime: every node created is added or freed; `queue_free()` used for removal.
2. References: cached, validated, not cycling; autoloads hold only global data.
3. Coupling: children do not reach up; parents inject dependencies; signals or Callables used for upward communication.
4. Per-frame cost: no `get_node`, no input polling, no heavy loops in `_process` / `_physics_process`.
5. Loading: `preload` only for stable constants; big resources released when done.
6. Typing: static types on new variables and exports.
7. Editor: `_get_configuration_warnings()` in `@tool` scripts where a scene has required children.
8. Files: naming, `addons/`, `.gdignore` if needed, no `.godot/` committed, LFS for large binaries.

## Known gaps in the source material

- The data-preferences page is marked work-in-progress and does not cover floats vs ints, Packed arrays, typed arrays, or string handling. Those rules are not captured here.
- The `Object` tree example in the docs uses the `Reference` class name, which predates Godot 4. Use `RefCounted`.
- The version-control page does not cover `.import` sidecar files.
- The pooling page returned 404 at fetch time. The debugger docs page does not list the Monitors counters, so the counter names come from the `Performance` class page.
- The logic-preferences page covers property order, `preload` vs `load`, and static vs dynamic levels, but not `await` or signal-naming rules.
- The GDQuest material in a third-party guide has no URL and was not verified. It is not cited here.
- The "call down, signal up" rule is the skill's own framing of the docs' signals-first guidance, not a quoted doc rule.
