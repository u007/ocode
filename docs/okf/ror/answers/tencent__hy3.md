# ror knowledge benchmark — answers
# Model: novita-ai/tencent/hy3   (provider-stripped id: tencent/hy3)
# CLOSED-BOOK via ocode run -dir <empty> (no corpus access). corpus_rev 1.

- id: ror-activerecord-01
  answer: |
    Since Rails 5, a `belongs_to` association is required by default. If you save a record whose `belongs_to` parent is `nil`/missing, the validity check fails, the save is rejected, and the parent record gets an error added (e.g. `author` must exist). To make it optional, declare it with `optional: true`: `belongs_to :author, optional: true`.

- id: ror-activerecord-02
  answer: |
    `has_many :through` uses an explicit join model (e.g. `Membership`) that can carry its own attributes, timestamps, validations, and scopes; the relationship is mediated by that model. `has_and_belongs_to_many` uses a bare join table with no model — only the two foreign keys exist and there's nowhere to hang extra data. Reach for `has_many :through` in almost all cases because it's far more flexible; only use HABTM for a trivial many-to-many with zero join-table data and no need to query the join directly.

- id: ror-activerecord-03
  answer: |
    All three control what happens to child records when the parent is destroyed. `dependent: :destroy` loads each child and calls `destroy` on it, running callbacks, validations, and any nested dependents (slow but safe). `dependent: :delete_all` issues a single `DELETE` SQL statement for all children without instantiating them or running their callbacks (fast, but skips callback logic). `dependent: :nullify` updates the children's foreign-key column to `NULL` (a single `UPDATE`) so the children are kept but dissociated from the parent.

- id: ror-activerecord-04
  answer: |
    The uniqueness validation is checked in the application and is subject to a race condition: two concurrent requests can both read "not present," both pass validation, and both insert duplicate rows. To guarantee uniqueness you must add a database-level unique constraint/index via a migration, e.g. `add_index :users, :email, unique: true`. The DB enforces it atomically; the model validation only provides a friendly error message.

- id: ror-activerecord-05
  answer: |
    `normalizes` (Rails 7.1+) declaratively normalizes an attribute's value — the normalization is applied on assignment (on write) and before validation, so the stored/canonical form is used consistently. It's better than a `before_save` callback because: it runs as soon as the attribute is set (so comparisons and `uniqueness` checks see the normalized value), it's declared in a single declarative place, it works with bulk inserts/updates, and you avoid the "only normalized at save time" gap and the duplicated callback boilerplate that `before_save` encourages.

- id: ror-querying-01
  answer: |
    `preload` runs separate queries — one for the parents and one (or more) for the association using an `IN (parent_ids)` clause — and then merges in Ruby. `eager_load` forces a single query using a `LEFT OUTER JOIN`, loading parents and children together. `includes` is the smart/default wrapper: it uses `preload` (separate queries) unless you reference the association in a condition/order that requires a JOIN, in which case it switches to `eager_load`. So: `includes` auto-picks, `preload` = N queries no JOIN, `eager_load` = 1 JOIN query.

- id: ror-querying-02
  answer: |
    This is the classic "N+1 query problem" — one query to load `@posts` plus one additional query per post to load its author (1 + N queries). Fix it with eager loading at the query layer: `@posts = Post.includes(:author)` (or `preload`/`eager_load`). That loads all authors in one extra query (or a JOIN) so the loop makes no additional DB calls.

- id: ror-querying-03
  answer: |
    `find(id)` returns a single record by primary key and raises `ActiveRecord::RecordNotFound` if it doesn't exist. `find_by(conditions)` returns the first matching record or `nil` when nothing matches (never raises). `where(conditions)` returns an `ActiveRecord::Relation` (a lazy, chainable query object), not records — if nothing matches it returns an empty relation, not `nil` and not an error. So: `find`/`find_by` return records (or nil/raise), `where` returns a relation you must further evaluate.

- id: ror-querying-04
  answer: |
    `pluck(:email)` generates SQL that selects only the `email` column and returns plain Ruby strings/arrays directly from the database, avoiding instantiating full `User` model objects and fetching every column. `User.all.map(&:email)` first loads every full User object (all columns) into memory and then extracts the email, which wastes memory and is slower. `pluck` also works on relations for filtered, single-query results.

- id: ror-callbacks-transactions-01
  answer: |
    `after_save` fires *inside* the surrounding database transaction. If that transaction is later rolled back (because a later validation fails, another callback raises, or the outer transaction aborts), the email/API call would already have been enqueued/sent even though the record was never persisted — causing inconsistency. `after_commit` (or `after_create_commit`) fires only *after* the transaction has successfully committed, so you know the record truly exists in the DB before triggering side effects.

- id: ror-callbacks-transactions-02
  answer: |
    `save` returns `false` when the record is invalid/fails to persist and does not raise; you check the return value / `errors`. `save!` raises `ActiveRecord::RecordInvalid` (or `RecordNotSaved`) on failure. Likewise `create` returns the (possibly invalid) unsaved object and `create!` raises on failure. Use the bang variants in scripts/console/background jobs where you want exceptions to surface, and the non-bang variants in request flows where you handle failures gracefully with error messages.

- id: ror-callbacks-transactions-03
  answer: |
    Raising any exception inside the `transaction do ... end` block triggers a rollback. `ActiveRecord::Rollback` is special: raising it rolls back the transaction but is caught by the transaction machinery, so it does *not* propagate up as an error to the caller (unlike other exceptions, which both roll back and re-raise). It's the intended, non-erroring way to abort a transaction deliberately.

- id: ror-callbacks-transactions-04
  answer: |
    For a new record, `save` fires: `before_validation`, `after_validation`, `before_save`, then `before_create`, then the INSERT, then `after_create`, then `after_save` (and `after_commit` after the transaction commits). Heavy callbacks are a design smell because they couple business logic/side effects to persistence, bloat transactions (longer locks, slower writes), make the model hard to test and reuse outside web requests, and hide behavior that belongs in a service object.

- id: ror-migrations-schema-01
  answer: |
    The single `change` method works for any migration Rails can automatically reverse (e.g. `create_table`, `add_column`, `add_index`, `add_reference`, `remove_column` with a type given). You need explicit `up`/`down` (or a `reversible` block) when an operation is not auto-reversible — such as `execute` of raw SQL, `remove_column` without specifying a type, `change_column` in ways Rails can't invert, dropping tables, or anything where the down step isn't a clean inverse.

- id: ror-migrations-schema-02
  answer: |
    `schema.rb` is a Ruby DSL dump of the database schema that ActiveRecord can load on any supported DB; it's portable but only captures concepts ActiveRecord understands (tables, columns, indexes, basic constraints) — not DB-specific features. `structure.sql` is a native SQL dump (e.g. `pg_dump`) of the full schema, preserving everything including triggers, stored procedures, custom types, partial/expression indexes, and extensions. Switch to `structure.sql` (via `config.active_record.schema_format = :sql`) when your database uses features `schema.rb` cannot represent.

- id: ror-migrations-schema-03
  answer: |
    Two dangerous operations on large production tables: (1) adding a column with a default — historically rewrote the whole table/locked it (mitigate by adding the column without a default first, then backfilling in batches, or rely on PG 11+ which adds the default without rewrite); (2) adding/removing an index or altering a column — these can lock writes. Safe approaches: for indexes use `add_index ... algorithm: :concurrently` inside `disable_ddl_transaction!` (Postgres), perform changes in off-peak windows or with online schema-change tools (pg_online, gh-ost), and remove columns by first ignoring them in the app before dropping.

- id: ror-migrations-schema-04
  answer: |
    `add_reference :comments, :post, foreign_key: true` generates a `post_id` column (integer/UUID) on `comments`, adds a database foreign-key constraint referencing `posts`, and (by default) adds an index on `post_id`. You add the foreign key for referential integrity — the DB prevents orphaned rows and bad inserts. You add the index because FK checks, joins, and the `belongs_to` lookups on `post_id` would otherwise do full table scans; the index makes both fast.

- id: ror-controllers-routing-01
  answer: |
    `resources :photos` generates the seven RESTful routes: GET `/photos` → `index`, GET `/photos/new` → `new`, POST `/photos` → `create`, GET `/photos/:id` → `show`, GET `/photos/:id/edit` → `edit`, PATCH/PUT `/photos/:id` → `update`, DELETE `/photos/:id` → `destroy`. It maps each route to the correspondingly-named controller action.

- id: ror-controllers-routing-02
  answer: |
    Strong Parameters prevent mass assignment of unintended attributes (the modern replacement for `attr_accessible`). In `params.require(:user).permit(:name, :email)`: `require(:user)` extracts the nested `user` hash and raises `ActionController::ParameterMissing` if the key is absent; `permit(:name, :email)` whitelists only those scalar keys and returns a permitted params hash, silently filtering out any other submitted attributes.

- id: ror-controllers-routing-03
  answer: |
    `before_action` registers a filter that runs before the controller action. If a `before_action` renders, redirects, or otherwise sets a response, the requested action does **not** run — the response is already determined. (Returning `false` from a filter halts the filter chain and prevents the action from executing; in modern Rails you typically redirect/head to short-circuit.) It's commonly used for authentication/authorization.

- id: ror-controllers-routing-04
  answer: |
    A `member` route acts on a single resource and includes the `:id` segment (e.g. `get 'preview'` → `/photos/:id/preview`). A `collection` route acts on the whole collection and has no `:id` (e.g. `get 'search'` → `/photos/search`). A successful API `create` should return HTTP **201 Created**, conventionally with a `Location` header pointing to the new resource.

- id: ror-controllers-routing-05
  answer: |
    Rails 8's built-in authentication generator (`bin/rails generate authentication`) scaffolds a minimal, in-app solution: a `Current.user`, a `SessionsController`, password digests via `bcrypt`, and sign-up/login/logout flows — zero third-party dependency and fully visible/editable in your codebase. Devise is a heavyweight third-party gem offering many ready-made features (confirmable, recoverable, lockable, omniauth, views, mailers, routes). The built-in generator is lighter and easier to maintain/customize for simple needs; Devise is "batteries-included" but more magic and upgrade overhead.

- id: ror-views-helpers-01
  answer: |
    Render a partial with locals using `render partial: "posts/post", locals: { post: @post }` (the local is then available as `post` inside the partial). `render @posts` (passing a collection) auto-detects the partial from the model's name (`posts/post` for `Post`), renders it once per element passing each item as the `post` local, and supports automatic collection caching — efficient for lists.

- id: ror-views-helpers-02
  answer: |
    `form_with` unifies the old `form_for` (model-backed) and `form_tag` (generic) helpers into one API. Regarding remote vs local: historically `form_with` defaulted to a remote/UJS (AJAX) form via `data-remote`, but in modern Rails (7+, with Turbo and no jQuery UJS) it generates a standard HTML form that submits natively (Turbo Drive intercepts it for SPA-like navigation) — i.e. a normal form submission rather than hand-rolled AJAX. You can force behavior with `local: true/false`.

- id: ror-views-helpers-03
  answer: |
    Rails protects against CSRF by embedding a per-session authenticity token and verifying it for every non-GET request (`protect_from_forgery` is on by default in `ApplicationController`); a missing or mismatched token raises an `InvalidAuthenticityToken` error. `form_with` automatically includes the hidden `authenticity_token` field in the generated form, so you don't have to add it manually.

- id: ror-views-helpers-04
  answer: |
    The fix belongs in the data-fetching layer — eager load the association in the controller/query (`Post.includes(:author)`) so the data is present before the view iterates. Don't "fix" it in the view because by the time you're iterating the view, each `post.author` access already triggers its own query (the queries fire on access), and the view shouldn't be responsible for DB loading strategy; the proper, performant place is to load associations up front.

- id: ror-concerns-services-01
  answer: |
    `ActiveSupport::Concern` is a module mixin with class-level DSL sugar (`extend ActiveSupport::Concern`). Its `included do ... end` block runs in the context of the *including class*, letting you declare associations, validations, scopes, callbacks, and class/instance methods all together. A plain Ruby module mixin can't cleanly invoke class macros like `has_many`/`validates` at include time — the `included` block gives you that class-context hook for vertical reuse.

- id: ror-concerns-services-02
  answer: |
    A service object is a Plain Old Ruby Object (PORO) class that encapsulates a single business operation (often with a `call`/`perform` method), coordinating multiple models and/or external services. Extract one when logic spans several models, involves external API calls, multi-step workflows, or orchestration that doesn't naturally belong to any single model — rather than piling more methods onto a controller ("fat controller") or onto one model.

- id: ror-concerns-services-03
  answer: |
    "Skinny controller, fat model" says controllers should only handle request/response plumbing and business logic should live in models. Taken too far it produces "god objects" — one model class with dozens of unrelated responsibilities and hundreds of methods, which becomes hard to test, understand, and maintain. The mature evolution is to push logic into smaller units (service objects, concerns, form objects) rather than letting any single model balloon.

- id: ror-concerns-services-04
  answer: |
    A concern is *mixed into* a class to share/deduplicate behavior and becomes part of that class's interface (vertical reuse across models). A service object is *instantiated and called* to perform a discrete unit of work; it is not mixed into anything (horizontal, one-shot orchestration). In short: a concern is a trait you include; a service object is an operation you invoke.

- id: ror-caching-jobs-01
  answer: |
    Russian-doll caching is nested fragment caching where an outer fragment (e.g. a list/page) contains inner fragments (e.g. each item), so updating one inner item only expires that item's cache, not the whole tree. Using the record itself as part of the cache key (via `cache_key`/`cache_key_with_version`, which encodes `id` + `updated_at` + optional version) makes invalidation automatic: whenever the record changes, its key changes, so the old fragment is naturally busted without manual expiration logic.

- id: ror-caching-jobs-02
  answer: |
    ActiveJob is Rails' abstraction layer for background jobs, giving a single API (`MyJob.perform_later`) that works across many backends (Sidekiq, Resque, Delayed Job, etc.). The queue adapter is the configuration that wires ActiveJob to a concrete backend (e.g. `:sidekiq`, `:async`, `:inline`). The default adapter (typically `:async` in dev or `:inline`) is **not** suitable for production — `:async` runs jobs in an in-process thread that dies with the process, and `:inline` runs them synchronously with no real queuing. Production needs a real backend like Sidekiq.

- id: ror-caching-jobs-03
  answer: |
    Background queues generally guarantee *at-least-once* delivery, plus retries on failure, so a job can run more than once. If the job isn't idempotent, duplicate execution causes duplicate side effects (e.g. sending the same email twice, double-charging). Make it idempotent by checking prior state before acting ("return early if already processed"), using DB unique constraints, recording a processed marker, or designing the operation so repeated application is harmless.

- id: ror-caching-jobs-04
  answer: |
    `perform_later` enqueues the job onto the configured queue and returns (running asynchronously in a worker); `perform_now` executes the job immediately/synchronously in the current process. You should pass the record's **id** rather than the record object because the job may execute in a different process/worker where the object isn't available, ActiveRecord objects don't serialize cleanly across processes, and passing the id avoids shipping stale or large object state — the job reloads the record by id at runtime.
