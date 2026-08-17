```yaml
- id: ror-activerecord-01
  answer: |
    Yes, since Rails 5 `belongs_to` is required by default (`optional: false`). If the parent is missing, validation fails and the record can't be saved. To make it optional, add `optional: true` to the association declaration.

- id: ror-activerecord-02
  answer: |
    `has_and_belongs_to_many` (HABTM) uses a simple join table with no model — you can't add extra columns or validations to the join. `has_many :through` uses an explicit join model (e.g., `has_many :taggings, has_many :tags, through: :taggings`), giving you a full ActiveRecord model on the join table with its own validations, callbacks, and extra attributes. You should reach for `has_many :through` in almost all cases; HABTM is a legacy shortcut for the simplest situation with no extra data on the join.

- id: ror-activerecord-03
  answer: |
    `dependent: :destroy` instantiates each child and runs its callbacks (including its own `dependent` options) — slow on large collections but safe. `dependent: :delete_all` issues a single `DELETE` SQL statement, skipping all callbacks — fast but can leave orphaned records in grandchildren associations. `dependent: :nullify` sets the foreign key to `NULL` on the children and does not delete them — useful when children should outlive the parent.

- id: ror-activerecord-04
  answer: |
    Application-level uniqueness validation is subject to race conditions: two concurrent processes can both read, both find no existing record, and both insert. You must also add a database-level unique index on the column (or composite index) so the database enforces uniqueness. The migration `add_index :users, :email, unique: true` is the essential complement to the model validation.

- id: ror-activerecord-05
  answer: |
    `normalizes` (Rails 7.1) declares a normalization that runs when the attribute is assigned (or on load). For example, `normalizes :email, with: ->(e) { e.strip.downcase }` transforms the value at assignment time and persists the normalized form. It's better than a `before_save` callback because (a) the value is normalized immediately on assignment so you see the canonical value throughout the request lifecycle, (b) it's defined declaratively on the attribute itself rather than buried in a callback chain, and (c) it runs on initial assignment too, not just on save.

- id: ror-querying-01
  answer: |
    `preload` issues a separate query for each association (e.g., one `SELECT * FROM users WHERE id IN (...)` for authors). `eager_load` uses a single LEFT OUTER JOIN query and loads all columns of the joined table into memory (so the association must not be already loaded). `includes` is a hint that lets Rails choose: it uses `eager_load` if the association is referenced in a `WHERE` or `ORDER` clause, otherwise falls back to `preload`. All three avoid N+1, but they differ in query shape and when you can filter on the associated table.

- id: ror-querying-02
  answer: |
    This is the classic N+1 query problem. Fix it by eager-loading the association: `Post.includes(:author).each { |p| puts p.author.name }`. This batches the author lookup into a single query upfront. You can also use `preload` explicitly: `Post.preload(:author)`.

- id: ror-querying-03
  answer: |
    `find(id)` returns a single record (or raises `ActiveRecord::RecordNotFound` if none matches). `find_by(conditions)` returns a single record or `nil` if nothing matches. `where(conditions)` returns a relation (an `ActiveRecord::Relation`) that can be chained, filtered further, and is lazy — it doesn't hit the database until iterated or explicitly evaluated. `where` never returns a single record directly; you chain `.first`, `.take`, etc. if you want one.

- id: ror-querying-04
  answer: |
    `pluck(:email)` issues a single `SELECT emails FROM users` and returns a plain array of scalars — no ActiveRecord objects are instantiated. `User.all.map(&:email)` loads every row into memory as full ActiveRecord objects (with all columns), then extracts the email attribute. `pluck` is faster, uses less memory, and avoids the overhead of object instantiation.

- id: ror-callbacks-transactions-01
  answer: |
    `after_save` runs inside the database transaction before it's committed. If the transaction rolls back later (e.g., a later callback fails, a constraint violation in another model, or the transaction is explicitly rolled back), the side effect has already happened — the email was sent or the API was called, but the record doesn't exist. `after_commit` runs only after the transaction has been successfully committed to the database, so the record is guaranteed to persist and the side effect is tied to a durable state.

- id: ror-callbacks-transactions-02
  answer: |
    `save` returns `true` on success and `false` on failure (after running validations). `save!` returns the record on success but raises `ActiveRecord::RecordInvalid` (with validation errors) or other exceptions on failure. The same pattern applies to `create` vs `create!`. Use `save!`/`create!` when failure is exceptional and should halt execution; use `save`/`create` when you want to handle the failure gracefully in a conditional.

- id: ror-callbacks-transactions-03
  answer: |
    Raising `ActiveRecord::Rollback` inside a `transaction` block silently rolls back the transaction — the block returns `nil` and no exception propagates. This is special because it is the *only* exception that is swallowed by the transaction block. Any other exception will roll back the transaction *and* propagate up the call stack. You cannot force a rollback by simply raising a standard exception inside the block and expecting it to be caught; you must raise `ActiveRecord::Rollback` specifically to roll back without propagating.

- id: ror-callbacks-transactions-04
  answer: |
    For a new record, the callback order on `save` is: `before_validation`, `after_validation`, `before_save`, `before_create`, `around_create` (before the yield), `after_create`, `after_commit` (or `after_rollback` on failure), `after_save`, `around_save` (after the yield). Heavy callbacks are a design smell because they create implicit, hard-to-trace side effects, make models slow and unpredictable, tightly couple unrelated concerns, and make testing and debugging difficult — the model becomes a god object doing too much.

- id: ror-migrations-schema-01
  answer: |
    You can use `change` when Rails can automatically infer the reverse operation (e.g., `add_column` reverses to `remove_column`, `create_table` reverses to `drop_table`). You need separate `up`/`down` methods when the operation has no automatic inverse — custom SQL, `execute`, `change_column` (which requires data transformation), or anything where `down` needs more logic than simply undoing the statement.

- id: ror-migrations-schema-02
  answer: |
    `schema.rb` is a Ruby DSL representation of your database schema — it's generated by `db:schema:dump` and is the default. `structure.sql` is a plain SQL dump (via `mysqldump`/`pg_dump`). You switch to `structure.sql` when your migrations use database features that `schema.rb` can't represent: raw SQL, triggers, stored procedures, views, partial indexes, or database-specific types. Set `config.active_record.schema_format = :sql` in your config.

- id: ror-migrations-schema-03
  answer: |
    (1) Removing a column: on a large table, this locks the table for the duration of `ALTER TABLE`. Do it in stages — stop reading the column in app code first, deploy a migration that removes the column separately after the code is live, or use `remove_column` which avoids locking on modern databases. (2) Adding a NOT NULL constraint to an existing column with data: this requires a full table scan. Instead, add the column as nullable, backfill the data in batches, then add the constraint. Both are dangerous because they can lock the table for minutes or hours on large tables. Use background migrations, lock-free approaches, or online schema change tools (e.g., `pt-online-schema-change` for MySQL, `pg_repack` for Postgres).

- id: ror-migrations-schema-04
  answer: |
    It generates a `post_id` integer column, a database-level foreign key constraint on `post_id` referencing `posts(id)`, and an index on `post_id`. The foreign key ensures referential integrity at the database level (no orphaned comments pointing to a deleted post). The index speeds up lookups when querying comments by `post_id` — without it, every query filtering/joining on `post_id` would require a full table scan.

- id: ror-controllers-routing-01
  answer: |
    `resources :photos` generates seven RESTful routes mapping to the PhotosController: `GET /photos` → `index`, `GET /photos/new` → `new`, `POST /photos` → `create`, `GET /photos/:id` → `show`, `GET /photos/:id/edit` → `edit`, `PATCH/PUT /photos/:id` → `update`, `DELETE /photos/:id` → `destroy`.

- id: ror-controllers-routing-02
  answer: |
    Strong Parameters prevent mass-assignment vulnerabilities by forcing you to explicitly whitelist which attributes are permitted. `params.require(:user)` extracts the `:user` hash from the params and raises `ActionController::ParameterMissing` if it's absent. `.permit(:name, :email)` whitelists only those specific scalar keys, stripping out anything else an attacker might inject. The result is a clean, safe hash that you pass to `create` or `update`.

- id: ror-controllers-routing-03
  answer: |
    `before_action` registers a method to run before a controller action (or group of actions). If the callback renders or redirects, the action method is never called — Rails short-circuits and skips the action. Returning `false` no longer stops the chain (Rails 5+); you must explicitly `render` or `redirect_to` and `return` to halt. This is commonly used for authentication checks, authorization, setting shared instance variables, or loading records.

- id: ror-controllers-routing-04
  answer: |
    A `member` route generates a URL scoped to a single resource instance (e.g., `/photos/:id/preview`), while a `collection` route generates a URL scoped to the collection (e.g., `/photos/search`) without an `:id`. A successful API `create` should return HTTP `201 Created` (with the created resource in the response body).

- id: ror-controllers-routing-05
  answer: |
    Rails 8's built-in authentication generator (from `rails generate authentication`) gives you a lightweight, session-based authentication system — a `User` model with `has_secure_password`, a `SessionsController` for login/logout, a `Current` model to hold the current user, and a `current_user` helper. It differs from Devise in that it's minimal and transparent: you own every line of the code, there's no Warden middleware, no complex configuration DSL, no modular "devise controllers" — it's just plain Rails code you can modify freely. Devise is more feature-rich out of the box (confirmable, recoverable, lockable, etc.) but adds significant complexity and a large abstraction surface.

- id: ror-views-helpers-01
  answer: |
    Render a partial with locals: `render partial: 'post', locals: { post: @post }`. `render @posts` (a collection shorthand) renders the `_post` partial once for each record in the collection, passing each record as a local variable named after the partial (so each gets a `post` local). It also wraps each in a `div` tag with a unique `id` (e.g., `dom_id(post)`) for targeted updates.

- id: ror-views-helpers-02
  answer: |
    `form_with` unifies `form_for` (model-backed forms) and `form_tag` (generic forms) into a single helper. In modern Rails (5.1+), it defaults to generating a local (standard HTML) form. You can opt into remote/AJAX behavior with `local: false` or by setting `config.action_view.default_enforce_utf8 = false` and `config.action_view.form_with_generates_remote_forms = true` in older versions.

- id: ror-views-helpers-03
  answer: |
    Rails protects against CSRF via the `protect_from_forgery` call in `ApplicationController` (which all controllers inherit). It uses a per-session CSRF token. `form_with` automatically includes a hidden `authenticity_token` field in the form, so the token is submitted with the POST/PATCH/DELETE request. The server verifies the token on state-changing requests, rejecting any request that lacks or mismatches the token.

- id: ror-views-helpers-04
  answer: |
    The fix belongs in the controller — use `includes` or `preload` on the query: `@posts = Post.includes(:author)`. You should not "fix" it in the view by loading the association there because the view's job is presentation, not data loading. Mixing query concerns into views violates separation of concerns, makes the N+1 load order unpredictable, and makes it impossible to optimize at the controller level. The controller is responsible for loading the data the view needs.

- id: ror-concerns-services-01
  answer: |
    `ActiveSupport::Concern` is a module that simplifies writing reusable mixins. Its `included do ... end` block lets you define class methods, associations, validations, and other class-level behavior that are automatically evaluated in the context of the including class when it's included. A plain module mixin requires you to manually call `self.included(base)` and extend the base class, which is more boilerplate and error-prone — `Concern` encapsulates that pattern cleanly.

- id: ror-concerns-services-02
  answer: |
    A service object is a plain old Ruby object (PORO) that encapsulates a single business operation or workflow — e.g., `ChargeCustomer.call(order, payment_method)`. You should extract one when a controller action or model method is doing multiple steps that involve different concerns (e.g., sending emails, calling APIs, updating multiple models), when the logic doesn't belong to any single domain model, or when you need to compose business logic without bloating models or controllers. It's a way to keep models and controllers focused on their primary responsibilities.

- id: ror-concerns-services-03
  answer: |
    "Skinny controller, fat model" means move business logic out of controllers into models, because controllers should only handle request/response plumbing. The failure mode when taken too far is the "fat model" anti-pattern: models become bloated god objects with hundreds of methods covering validation, business rules, third-party integrations, email sending, payment processing, and more. This makes models hard to test, hard to maintain, and tightly couples unrelated concerns. The solution is to extract concerns, service objects, and plain POROs to keep models focused on data representation and persistence.

- id: ror-concerns-services-04
  answer: |
    A concern is mixed into an existing class (model or controller) and becomes part of its inheritance chain — it's available as instance/class methods on the including class. It's tightly coupled to the class it's included in. A service object is a standalone class that you instantiate and call (e.g., `ProcessPayment.new(order).call`) — it's a self-contained unit of work that can be composed, injected, and tested independently. Concerns extend a class; services are composed alongside it.

- id: ror-caching-jobs-01
  answer: |
    Russian-doll caching nests cache fragments so that when a child record changes, only its fragment needs to be invalidated, not the entire parent. You do this by nesting `cache [parent, child]` inside `cache parent`. Using the record itself in the cache key (e.g., `cache @post`) works because Rails generates a cache key that includes the record's `updated_at` timestamp (via `cache_version`). When the record is updated, `updated_at` changes, producing a new key, so the old cached fragment is effectively invalidated without explicitly deleting it — a new miss on the next read.

- id: ror-caching-jobs-02
  answer: |
    ActiveJob provides a unified API for defining, queuing, and running background jobs across different queue backends. The queue adapter is the backend that actually executes jobs — it determines where and how jobs are stored and processed. Common adapters include `Sidekiq`, `Delayed::Job`, `Resque`, and the default `:async` adapter (in-process queue using a thread pool). The `:async` adapter is NOT suitable for production because jobs are lost on server restart, there's no retry mechanism, and it shares the application's process/memory — use Sidekiq or a similar production-grade adapter.

- id: ror-caching-jobs-03
  answer: |
    Jobs must be idempotent because at-least-once delivery means a job can be executed multiple times (due to retries, worker crashes, network issues, or requeuing). If a job isn't idempotent, running it twice could send duplicate emails, charge a customer twice, or corrupt data. To make a job idempotent: use a unique identifier (e.g., a job ID or idempotency key) to track whether it's already been processed, use database-level constraints (unique indexes) to prevent duplicate records, and use conditional checks (e.g., "only create if not already created") with atomic operations.

- id: ror-caching-jobs-04
  answer: |
    `perform_later` enqueues the job onto the queue adapter for background execution and returns immediately. `perform_now` executes the job synchronously in the current process (useful for testing or when you need the result immediately). You pass a record's id (e.g., `MyJob.perform_later(user.id)`) rather than the record itself because the job may execute later in a different process with a fresh database connection — the serialized record object would be stale, deserialized incorrectly, or reference an object that no longer exists. Passing the id allows the job to re-fetch the current state of the record when it runs.
```
