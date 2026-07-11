# Ruby on Rails Benchmark — Human Render

> **Generated from `questions.yaml` (corpus_rev 1). Do not grade from this file
> — `questions.yaml` is the source of truth.** Hand-synced; if they disagree,
> the YAML wins. Framework only — the Ruby language is the separate `ruby` corpus.

Legend: **W** = weight (1–3), **D** = difficulty. Rubric shows scored points
(`•`) and partial-credit levels (`~`).

---

### ror-activerecord-01 · activerecord · W3 · easy
**Q:** Is a `belongs_to` required by default in modern Rails? What happens if the parent is missing, and how do you make it optional?
**A:** Yes — since Rails 5 `belongs_to_required_by_default` is true, so it adds a presence validation and saving without the parent fails. `belongs_to :author, optional: true` allows a nil FK.
• required by default (presence validation) • optional: true allows a nil parent ~ says belongs_to is optional by default (pre-5)

### ror-activerecord-02 · activerecord · W2 · medium
**Q:** `has_many :through` vs `has_and_belongs_to_many` — difference, and which to reach for?
**A:** HABTM uses a bare join table with no model (no columns/validations/callbacks). `:through` goes through a real join model you can validate and add attributes to. Prefer `:through` except for a pure attributeless join.
• HABTM = join table, no model • :through = real join model with attributes • prefer :through ~ names both without the join-model difference

### ror-activerecord-03 · activerecord · W2 · medium
**Q:** `dependent: :destroy` vs `:delete_all` vs `:nullify` on a has_many?
**A:** `:destroy` instantiates each child and runs its destroy callbacks (per-record). `:delete_all` is one SQL DELETE, skips callbacks. `:nullify` sets the child FK to NULL instead of deleting.
• :destroy runs per-child callbacks • :delete_all single SQL DELETE, no callbacks • :nullify nulls the FK ~ destroy vs delete without callbacks distinction

### ror-activerecord-04 · activerecord · W3 · hard
**Q:** Why is `validates :email, uniqueness: true` not enough, and what must you add?
**A:** The validator does SELECT-then-INSERT, so two concurrent requests can both pass and both insert (race). Add a DB unique index as the real guarantee; the DB rejects the dup (RecordNotUnique).
• SELECT-then-INSERT race → both concurrent inserts pass • add a DB unique index ~ notices dups slip through, no unique-index fix

### ror-activerecord-05 · activerecord · W1 · medium
**Q:** What does `normalizes` (Rails 7.1) do, and why is it better than a `before_save`?
**A:** `normalizes :email, with: ->(e){e.strip.downcase}` (7.1+) transforms an attribute on assignment/save AND on finder queries (`find_by`), which a `before_save` doesn't. Centralizes the rule.
• normalizes (7.1) transforms attr on assignment/save • also applies to finder queries, unlike before_save ~ "it downcases the field" without finder/version detail

### ror-querying-01 · querying · W3 · hard
**Q:** `includes` vs `preload` vs `eager_load` — how does each hit the DB?
**A:** `preload` = separate query per association (can't filter on it). `eager_load` = single LEFT OUTER JOIN (can filter/order on it). `includes` preloads by default but promotes to eager_load when you reference the association in where/order (references / SQL string).
• preload = separate query • eager_load = single LEFT JOIN (filterable) • includes preloads, promotes to eager_load when referenced ~ "all fix N+1" without separate-query-vs-JOIN

### ror-querying-02 · querying · W3 · medium
**Q:** One query per post for `p.author.name` — what's it called and the fix?
**A:** The N+1 query problem (1 + N lazy loads). Fix by eager loading up front — `Post.includes(:author)`. Bullet / strict-loading help catch it.
• identifies N+1 (1 + N) • fix with includes/preload/eager_load ~ "add includes" without naming N+1

### ror-querying-03 · querying · W2 · medium
**Q:** `find` vs `find_by` vs `where` — record or relation, and behavior on no match?
**A:** `find(id)` raises RecordNotFound when missing. `find_by` returns nil. `where` returns a lazy Relation — doesn't hit the DB until enumerated, and returns an empty relation (not nil) on no match.
• find raises, find_by returns nil • where = lazy Relation, empty (not nil) on no match ~ find vs find_by only, misses lazy relation

### ror-querying-04 · querying · W2 · medium
**Q:** Why is `User.pluck(:email)` better than `User.all.map(&:email)`?
**A:** `pluck` selects only the column and returns raw values — no model objects instantiated. `map(&:email)` loads all columns and builds a full object per row, then discards it. Wasteful.
• pluck selects only the column, no objects instantiated • map loads all columns + builds an object per row ~ "pluck is faster" with no reason

### ror-callbacks-transactions-01 · callbacks-transactions · W3 · hard
**Q:** Enqueue an email after save — why `after_commit`, not `after_save`?
**A:** `after_save` runs inside the open transaction; a later rollback leaves the side effect fired but the row gone (and jobs may run before commit). `after_commit` runs only after commit, so data is durable/visible first.
• after_save inside txn → rollback fires side effect but loses the row • after_commit runs post-commit (durable/visible) ~ "use after_commit for external stuff" without the txn reason

### ror-callbacks-transactions-02 · callbacks-transactions · W2 · easy
**Q:** `save` vs `save!` (and create/create!)?
**A:** `save` returns true/false — you must check it (errors in `record.errors`). `save!` raises RecordInvalid on failure. Use `save!` when failure should abort (in a transaction/service).
• save returns true/false; save! raises RecordInvalid • use save! when failure should abort ~ "! raises" without the check-return contrast

### ror-callbacks-transactions-03 · callbacks-transactions · W3 · hard
**Q:** Inside a `transaction` block, how to force rollback, and what's special about `ActiveRecord::Rollback`?
**A:** Any escaping exception rolls back and re-propagates. `ActiveRecord::Rollback` also rolls back but is swallowed at the boundary (block returns nil, not re-raised) — abort silently. `save!` inside converts failures to rollbacks.
• escaping exception rolls back + re-propagates • ActiveRecord::Rollback rolls back but is swallowed ~ "raise to roll back" without the Rollback-swallowed point

### ror-callbacks-transactions-04 · callbacks-transactions · W1 · medium
**Q:** Order of create callbacks on `save`, and why heavy callbacks are a smell?
**A:** before_validation → after_validation → before_save → before_create → (INSERT) → after_create → after_save → after_commit (around_* wrap). Heavy/side-effecting callbacks fire implicitly everywhere (tests/seeds/imports) and are hard to skip — prefer a service / after_commit.
• order: before_validation → after_validation → before_save → before_create → after_create → after_save → after_commit • heavy callbacks a smell → service/after_commit ~ roughly right but misplaces validation/commit

### ror-migrations-schema-01 · migrations-schema · W2 · medium
**Q:** When can a migration use `change`, and when do you need `up`/`down`?
**A:** `change` for auto-reversible ops (create_table, add_column, add_index…) — Rails infers the down. Use `up`/`down` (or `reversible`) for irreversible ops: raw `execute`, data backfills, un-invertible removes. Otherwise rollback raises IrreversibleMigration.
• change for auto-reversible ops (Rails infers down) • up/down for irreversible: raw SQL/data/un-invertible removes ~ "change is simpler" without when it can't reverse

### ror-migrations-schema-02 · migrations-schema · W2 · medium
**Q:** `schema.rb` vs `structure.sql`, and when to switch?
**A:** Both are the canonical schema dump loaded to build a fresh/test DB (not replaying migrations). `schema.rb` is DB-agnostic Ruby limited to the DSL. Switch to `structure.sql` (`schema_format = :sql`) for DB-specific features (constraints, triggers, functions, custom types). Commit both.
• both are the dump loaded to build fresh/test DB • switch to structure.sql for DB features the DSL can't express ~ "one is Ruby one is SQL" without the DSL limitation

### ror-migrations-schema-03 · migrations-schema · W3 · hard
**Q:** Two operations dangerous on a large table in production, done safely?
**A:** (1) `add_column default:/null: false` can lock-rewrite every row → add column, backfill in batches, add the constraint separately. (2) `add_index` locks writes → `algorithm: :concurrently` with `disable_ddl_transaction!` on Postgres. Never backfill in the DDL transaction. `strong_migrations` flags these.
• column default/NOT NULL locks large table → batch backfill + separate constraint • index build locks writes → add_index algorithm: :concurrently ~ names a risk but no safe approach

### ror-migrations-schema-04 · migrations-schema · W2 · easy
**Q:** What does `add_reference :comments, :post, foreign_key: true` generate, and why FK + index?
**A:** A `post_id` column, an index on it, and a DB foreign-key constraint. Index → fast lookups/joins; FK → referential integrity at the DB level (can't point at a missing post). Add `null: false` if required.
• adds post_id + index + DB FK constraint • index for lookups, FK for referential integrity ~ "adds the column" without index/FK purpose

### ror-controllers-routing-01 · controllers-routing · W3 · easy
**Q:** What does `resources :photos` create and which actions do routes map to?
**A:** Seven RESTful routes: index, new, create, show, edit, update, destroy — mapping HTTP verb + path to each action, plus named path helpers. `resource` (singular) omits index and :id.
• seven actions index/new/create/show/edit/update/destroy • maps verb+path to each (+ path helpers) ~ "makes REST routes" without listing them

### ror-controllers-routing-02 · controllers-routing · W3 · medium
**Q:** What do Strong Parameters solve, and how do `require`/`permit` work?
**A:** Prevent mass-assignment: only whitelisted attributes reach the model. `require(:user)` asserts/extracts the key (raises ParameterMissing if absent); `permit(...)` whitelists the allowed attributes; arrays/nested permitted explicitly (`tag_ids: []`).
• prevent mass-assignment (only whitelisted attrs reach model) • require asserts key; permit whitelists attributes ~ "they filter params" without the security reason

### ror-controllers-routing-03 · controllers-routing · W2 · easy
**Q:** What is `before_action`, and what does rendering/redirecting inside one do?
**A:** Runs a filter before actions (scope with only:/except:) for setup (`set_post`) or gatekeeping (`require_login`). If it renders/redirects/throws :abort, Rails halts the chain — the action never runs. Filters run in declaration order.
• runs before actions for setup/auth (only:/except:) • render/redirect halts the chain → action doesn't run ~ "runs before the action" without halt behavior

### ror-controllers-routing-04 · controllers-routing · W2 · medium
**Q:** `member` vs `collection` route, and the status code for a successful API create?
**A:** `member` = per-record route with `:id` (`PATCH /photos/:id/publish`); `collection` = whole-set route without id (`GET /photos/search`). A successful create returns `201 Created`; update/read `200`, no-body `204`, validation fail `422`, missing `404`.
• member = per-record with :id; collection = whole-set no id • create returns 201 Created ~ gets member/collection but wrong/absent status (or vice versa)

### ror-controllers-routing-05 · controllers-routing · W1 · medium
**Q:** What does Rails 8's authentication generator give you, vs Devise?
**A:** Rails 8.0 `bin/rails generate authentication` scaffolds session-based auth (User with has_secure_password, Session, sign-in/out, Current, password reset) as owned app code, no gem. Minimal vs Devise's larger configurable engine (confirmable, OmniAuth, lockable).
• Rails 8 generator scaffolds session auth (has_secure_password/sessions/reset) as owned code • minimal/no-gem vs Devise's larger engine ~ "Rails 8 has auth now" without what it generates / Devise contrast

### ror-views-helpers-01 · views-helpers · W2 · easy
**Q:** Render a partial with locals, and what does `render @posts` do?
**A:** `render partial: "post", locals: {post: @post}` (or `render "post", post: @post`) renders `_post` with `post` local. `render @posts` renders `_post` once per element with each as the inferred `post` local, in order. Prefer explicit locals.
• render partial with locals: {} passes locals to _partial • render @posts renders collection (once per element, item as local) ~ knows partials but not collection/locals

### ror-views-helpers-02 · views-helpers · W2 · medium
**Q:** What does `form_with` unify, and remote or local form by default now?
**A:** `form_with` (5.1+) unified `form_for` (model) + `form_tag` (URL); `form_with model:` infers URL/method and namespaces fields. Since Rails 6.1 it generates a LOCAL (standard) form by default (was remote/AJAX in 5.1).
• form_with unifies form_for + form_tag • defaults to local since 6.1 (was remote in 5.1) ~ explains form_with but says it defaults to remote/AJAX now

### ror-views-helpers-03 · views-helpers · W3 · medium
**Q:** How does Rails protect against CSRF, and what does `form_with` do automatically?
**A:** A per-session authenticity token verified on non-GET requests (`protect_from_forgery`, on by default; mismatch → InvalidAuthenticityToken). `form_with`/`form_tag` auto-inject the hidden `authenticity_token`; JS reads the `<meta name="csrf-token">`.
• per-session token verified on non-GET (protect_from_forgery, on by default) • form_with auto-includes the hidden authenticity_token ~ "Rails has CSRF" without token / form_with auto-inclusion

### ror-views-helpers-04 · views-helpers · W2 · medium
**Q:** N+1 from `post.author.name` in a view — where does the fix belong, and why not the view?
**A:** In the controller/query: `@posts = Post.includes(:author)`. Views stay declarative; patching it in the template hides data access and still fires per-row queries. A collection partial alone doesn't fix N+1 — the eager load does.
• eager-load in controller/query, not the view • views render already-loaded data; queries stay out of templates ~ "add includes" without noting it belongs in the controller

### ror-concerns-services-01 · concerns-services · W2 · medium
**Q:** What is an `ActiveSupport::Concern`, and what does `included do…end` enable?
**A:** A module (in concerns/) extended with `ActiveSupport::Concern` packaging reusable behavior. `included do…end` runs in the host CLASS, so it can call class macros (validates/has_many/before_action/scopes); also handles deps and gives `class_methods do`, avoiding `self.included` boilerplate.
• module (ASC) packaging reusable model/controller behavior • included do runs in host class → class macros; avoids self.included boilerplate ~ "a shared module" without included-block capability

### ror-concerns-services-02 · concerns-services · W2 · medium
**Q:** What is a service object (PORO), and when to extract one?
**A:** A plain Ruby object encapsulating one business operation (`RegisterUser.call`), coordinating models/transactions/external calls behind one entry point. Extract when logic spans several models, is multi-step, or is too fat for a controller yet not one model's core job. Keeps controllers thin, easy to test.
• PORO encapsulating one operation (single call entry) • extract when logic spans models / multi-step / doesn't fit one model ~ "a class for business logic" without when/why

### ror-concerns-services-03 · concerns-services · W2 · easy
**Q:** "Skinny controller, fat model" — the guideline and its failure mode?
**A:** Controllers handle only HTTP (params/auth/response) and delegate logic down; "fat model" pushes logic into ActiveRecord. Overdone, models become God objects (thousands of lines, tangled callbacks). Modern refinement: skinny controller + focused models, splitting into concerns/services/POROs.
• controllers stay thin, delegate logic out • over-fat models → God objects; split into concerns/services ~ states the slogan without the God-object failure mode

### ror-concerns-services-04 · concerns-services · W1 · medium
**Q:** Concern vs service object — key difference in use?
**A:** A concern is a mixin: its methods become methods ON the including model/controller, sharing its state — for behavior reused across models (Taggable). A service is a separate object you call for one operation; it doesn't extend the model and holds its own collaborators.
• concern = mixin adding methods to host (shared behavior) • service = standalone object called for one operation ~ "both hold logic" without mixin-vs-standalone

### ror-caching-jobs-01 · caching-jobs · W2 · hard
**Q:** Russian-doll caching, and why does keying on the record auto-invalidate?
**A:** Nested fragment caches (`<% cache post do %>` with comments cached inside). Passing an object builds the key from `cache_key_with_version` (id + updated_at), so updating/touching the post changes its key and regenerates that fragment; unchanged inner ones stay. `touch: true` bumps parents.
• nested fragments; key from record (id + updated_at) • record update changes key → auto-invalidate; touch: true propagates to parents ~ "caches view fragments" without record-keyed invalidation

### ror-caching-jobs-02 · caching-jobs · W2 · medium
**Q:** What does ActiveJob provide, the adapter's role, and is the default production-ready?
**A:** ActiveJob is a backend-agnostic interface for enqueuing background jobs. The queue adapter is the real backend (Sidekiq, Solid Queue, Resque…) set via `queue_adapter`. The default `:async` runs on an in-process thread pool and drops pending jobs on restart — dev only, not production.
• ActiveJob = backend-agnostic enqueue interface • adapter = real backend; default :async is in-memory/loses jobs → not production ~ describes ActiveJob but thinks default adapter is production-ready

### ror-caching-jobs-03 · caching-jobs · W2 · hard
**Q:** Why must jobs be idempotent, and how do you make one idempotent?
**A:** Workers can crash before acking or retry on error, so a job may run more than once (at-least-once) — non-idempotent work double-charges/double-sends. Guard on state, use an idempotency key / DB unique constraint, or upsert so re-runs are no-ops. Pass IDs and reload.
• runs >1 time (retries/at-least-once) → non-idempotent double-acts • guard/idempotency key/unique constraint/upsert → no-op re-runs ~ "should be idempotent" without why/how

### ror-caching-jobs-04 · caching-jobs · W1 · easy
**Q:** `perform_later` vs `perform_now`, and why pass an id not the record?
**A:** `perform_later` enqueues for async execution on a worker; `perform_now` runs `perform` synchronously inline (tests/inline needs). Pass an id and reload inside the job — ActiveJob serializes args, and re-fetching avoids stale data / serialization issues so the job sees current state.
• perform_later enqueues async; perform_now runs synchronously inline • pass id + reload → avoids stale data/serialization ~ gets later-vs-now but no reason to pass id
