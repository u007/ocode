# cat-jam-free — WITH-SKILL closed-book answers for ror (opencode-go, 2026-10-14)
# Delivery: derived/ror.cat-jam-free.SKILL.md body (front matter stripped) prepended to _prompts/ror.md; one
# `ocode run -m opencode-go/cat-jam-free -effort med` session, isolated HOME with every tool denied, cwd holding only the sheet;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: ror-activerecord-01
  answer: |
    Yes. Since Rails 5, `belongs_to` is required by default and validates the
    parent's presence; saving without a parent fails validation. Make it optional
    with `belongs_to :parent, optional: true` (or `required: false`).

- id: ror-activerecord-02
  answer: |
    `has_and_belongs_to_many` uses a join table with no join model and stores no
    extra attributes on the relationship. `has_many :through` uses an explicit join
    model, which can carry attributes, validations, and callbacks. Prefer
    `has_many :through` unless you only need a simple, attributeless many-to-many.

- id: ror-activerecord-03
  answer: |
    `dependent: :destroy` loads and destroys each child, running callbacks and
    `:dependent` rules recursively. `dependent: :delete_all` deletes children
    directly with SQL, without instantiating them or running callbacks. `dependent:
    :nullify` sets each child's foreign key to `NULL` instead of deleting it.

- id: ror-activerecord-04
  answer: |
    The validation runs in the application process, so two concurrent requests
    can both pass it and insert duplicate values. Add a unique database index (and
    often a unique constraint) on the column; optionally rescue
    `ActiveRecord::RecordNotUnique` at the application boundary.

- id: ror-activerecord-05
  answer: |
    `normalizes` declares a transformation applied when an attribute is assigned
    (and loaded from the database), so validations, dirty tracking, and queries see
    the normalized value. It is better than `before_save` because the value is
    normalized before validation and query construction, rather than only
    immediately before persistence, where it can bypass validations or produce
    inconsistent comparisons.

- id: ror-querying-01
  answer: |
    `preload` always issues separate queries, one for the parent scope and one
    per association. `eager_load` uses a single `LEFT OUTER JOIN` query. `includes`
    chooses between them: it uses `preload` by default and switches to
    `eager_load` when the query references the associated table. `preload` avoids
    row duplication/Jointable issues; `eager_load` can order or filter by the
    joined table.

- id: ror-querying-02
  answer: |
    It is the N+1 query problem. Fix it by eager-loading the association, e.g.
    `Post.includes(:author)` or `Post.preload(:author)`, before iterating.

- id: ror-querying-03
  answer: |
    `find(id)` returns a record and raises `ActiveRecord::RecordNotFound` if no
    match. `find_by(conditions)` returns the first matching record or `nil`.
    `where(conditions)` returns an `ActiveRecord::Relation` (chainable scope) and
    does not immediately load records.

- id: ror-querying-04
  answer: |
    `pluck(:email)` issues `SELECT email FROM users` and returns an array of raw
    values without instantiating `User` objects or loading other columns. `User.all.map
    (&:email)` loads and instantiates every record, using far more memory and CPU.

- id: ror-callbacks-transactions-01
  answer: |
    `after_save` runs inside the transaction but before commit, so side effects can
    leak out when the transaction later rolls back. `after_commit` runs only after
    the transaction is committed, so external work happens only when the database
    change is durable.

- id: ror-callbacks-transactions-02
  answer: |
    `save`/`create` return a boolean or record and put validation errors on the
    record without raising. `save!`/`create!` raise `ActiveRecord::RecordInvalid`
    on validation failure. Use bang methods when failure must abort the surrounding
    flow; otherwise handle the false/invalid result explicitly.

- id: ror-callbacks-transactions-03
  answer: |
    Raise any real exception to roll back and re-raise it. Raising
    `ActiveRecord::Rollback` also rolls back but is swallowed by the transaction
    block, which returns `nil` without re-raising, so the caller sees no error.

- id: ror-callbacks-transactions-04
  answer: |
    On create: `before_validation`, `after_validation`, `before_save`,
    `before_create`, the `around_create` yield/insert, `after_create`, `after_save`,
    then `after_commit` after commit (or `after_rollback` on rollback). Heavy
    callbacks are a smell because they hide side effects, make testing and
    importing difficult, and can hold database transactions open while doing slow
    work.

- id: ror-migrations-schema-01
  answer: |
    Use `change` when Rails can automatically invert the operation, such as adding
    or removing tables, columns, indexes, or foreign keys. Use separate `up` and
    `down` methods, or a `reversible` block, for irreversible or ambiguous
    operations such as `execute`, `change_column`, or data migrations.

- id: ror-migrations-schema-02
  answer: |
    `schema.rb` is a Ruby-format schema dump that is database-agnostic but cannot
    represent database-specific objects. `structure.sql` is a SQL-format dump that
    preserves full database-specific behavior. Switch to `structure.sql` when using
    features such as PostgreSQL views, functions, triggers, or complex constraints
    that Ruby schema cannot capture.

- id: ror-migrations-schema-03
  answer: |
    Dangerous examples: adding an index with plain `add_index` locks writes on a
    large table, and adding a non-null column with a default can rewrite the table.
    Do them safely by adding indexes with `algorithm: :concurrently` and
    `disable_ddl_transaction!`, and by adding columns as nullable first,
    backfilling in batches, then enforcing `NOT NULL` separately.

- id: ror-migrations-schema-04
  answer: |
    It adds a `post_id` column and an index on it, plus a foreign-key constraint
    because `foreign_key: true`. The index speeds lookups, joins, and foreign-key
    checks; the foreign key enforces referential integrity in the database.

- id: ror-controllers-routing-01
  answer: |
    `resources :photos` declares seven RESTful routes: `index`, `show`, `new`,
    `create`, `edit`, `update`, and `destroy`, mapped to the corresponding actions
    in `PhotosController`.

- id: ror-controllers-routing-02
  answer: |
    Strong Parameters prevent mass assignment by whitelisting permitted fields.
    `params.require(:user)` ensures the `:user` key is present and returns that
    nested hash; `permit(:name, :email)` whitelists only those attributes for mass
    assignment.

- id: ror-controllers-routing-03
  answer: |
    `before_action` runs a callback before the controller action. If it renders or
    redirects, that response is returned and the action itself is not executed; the
    callback chain halts unless explicitly continued.

- id: ror-controllers-routing-04
  answer: |
    A `member` route acts on one identified record and includes `:id` in its path,
    while a `collection` route acts on the whole collection without `:id`. A
    successful API `create` should return HTTP 201 Created.

- id: ror-controllers-routing-05
  answer: |
    Rails 8's `rails generate authentication` scaffolds a lightweight
    authentication stack, including a `User` with `has_secure_password`, a
    `Session` model, controllers, and routes. It is smaller and self-owned than
    Devise, which is a fuller feature gem with confirmations, lockouts, password
    resets, and many configurable modules.

- id: ror-views-helpers-01
  answer: |
    Render with locals using `render "post", post: @post` or
    `render partial: "post", locals: { post: @post }`. `render @posts` renders the
    collection by inferring partial `_post` for each item and passing each record
    as the local `post`.

- id: ror-views-helpers-02
  answer: |
    `form_with` unifies `form_for` and `form_tag` into one helper for model-backed
    and URL-backed forms. In current Rails it generates a standard local form by
    default; use `local: false` for remote/AJAX submission if needed.

- id: ror-views-helpers-03
  answer: |
    Rails uses a per-session CSRF authenticity token and verifies it on non-GET
    requests via `protect_from_forgery`. `form_with` automatically inserts the
    hidden authenticity-token field into the form.

- id: ror-views-helpers-04
  answer: |
    Fix it in the controller/query construction by eager-loading the association,
    e.g. `@posts = Post.includes(:author)`. The view should only display data; it
    should not trigger or control database queries.

- id: ror-concerns-services-01
  answer: |
    `ActiveSupport::Concern` packages reusable mixin behavior. Its `included do ...
    end` block runs in the including class/module context, so it can define
    associations, validations, scopes, and other class-level declarations cleanly
    without manual `self.included(base)` plumbing.

- id: ror-concerns-services-02
  answer: |
    A service object is a PORO encapsulating a business operation. Extract one
    when logic spans multiple models, involves external systems, performs a
    multi-step workflow, or would otherwise bloat a model or controller.

- id: ror-concerns-services-03
  answer: |
    "Skinny controller, fat model" means HTTP concerns stay in controllers while
    domain behavior lives in models. Taken too far, it creates bloated god models
    with unrelated responsibilities, heavy callbacks, and poor testability.

- id: ror-concerns-services-04
  answer: |
    A concern is mixed into a model or class to share reusable behavior in that
    object. A service object is invoked independently to perform an operation and
    is not mixed into the domain objects it coordinates.

- id: ror-caching-jobs-01
  answer: |
    Russian-doll caching nests fragment caches so unchanged inner fragments can be
    reused inside an outer cached fragment. Using the record in the cache key
    includes its `updated_at`, so any change produces a new key and automatically
    invalidates the old cached fragment.

- id: ror-caching-jobs-02
  answer: |
    ActiveJob provides a unified API for enqueuing and running background jobs
    across queue backends. The queue adapter chooses the backend. The default
    `:async` adapter is not suitable for production because jobs are held only in
    process memory and are lost on restart.

- id: ror-caching-jobs-03
  answer: |
    At-least-once delivery and retries mean jobs can run multiple times. Jobs
    must be idempotent: repeat execution should not duplicate side effects. Use
    state checks, atomic updates, idempotency keys, and database constraints to
    make operations safe to repeat.

- id: ror-caching-jobs-04
  answer: |
    `perform_later` enqueues the job for asynchronous background processing;
    `perform_now` runs it synchronously in the current process. Pass a record's id
    rather than the record so the job serializes cheaply and reloads a fresh,
    current database state when it runs.
