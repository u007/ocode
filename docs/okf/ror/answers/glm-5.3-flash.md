```yaml
- id: ror-activerecord-01
  answer: |
    Yes. Since Rails 5, new apps set `belongs_to_required_by_default = true`, so a
    `belongs_to` association is required by default. If the parent is missing (nil),
    validation fails with an error like "must exist" and the record won't save. Make
    it optional with `belongs_to :post, optional: true`. Note that by default the
    validation checks that the associated record actually exists (not just that the
    foreign key column is present); if you only care about the FK being present, add
    `validates :post_id, presence: true` alongside `optional: true`.

- id: ror-activerecord-02
  answer: |
    `has_many :through` uses an explicit join model (a real class backed by a table
    that can hold extra columns, validations, callbacks, and be queried directly).
    `has_and_belongs_to_many` uses an implicit join table containing only the two
    foreign keys — no model, no way to store extra data, no direct access to the
    association rows. Reach for `has_many :through` in almost every case: you can
    start simple and later add attributes/behavior to the join model, and the
    association is introspectable. HABTM is only worth it for a truly pure, permanent
    link table that will never carry extra data.

- id: ror-activerecord-03
  answer: |
    `dependent: :destroy` — when the parent is destroyed, Rails loads each child and
    calls `destroy` on it individually, so child callbacks/validations run. Safe but
    can be slow (N queries). `dependent: :delete_all` — issues one SQL DELETE
    (or one per association) against the children; no child callbacks or validations
    run. Fast but dangerous if children have important destroy logic.
    `dependent: :nullify` — sets the children's foreign key to NULL instead of
    deleting them; the child rows survive, orphaned, with no parent.

- id: ror-activerecord-04
  answer: |
    The uniqueness validation only runs a SELECT before saving — it can't guard
    against a race condition where two concurrent requests both pass validation and
    then both INSERT. You must add a unique index (or unique constraint) at the
    database level: `add_index :users, :email, unique: true` in a migration. Then
    handle the resulting `ActiveRecord::RecordNotUnique` (and/or rescue the
    validation race) in code. Also consider case-insensitivity (e.g. a functional
    unique index on LOWER(email) plus normalizing the email).

- id: ror-querying-01
  answer: |
    All three eager-load associations to avoid N+1. `preload` runs one query per
    association (e.g. one SELECT for posts, then one for comments) and stitches them
    together in memory — no join. `eager_load` forces a single query using a LEFT
    OUTER JOIN. `includes` is the smart one: it delegates to `preload` by default,
    but switches to `eager_load` (a JOIN) when you reference the association in a
    WHERE/ORDER clause (with a `references` call or string conditions), because you
    can't filter on a separately-queried association.

- id: ror-querying-02
  answer: |
    It's the N+1 query problem: 1 query for posts plus one query per post to fetch
    its author. Fix it with eager loading in the query that produces @posts:
    `Post.includes(:author)` (or `preload`/`eager_load`). Then each post already has
    its author loaded and iterating fires no extra queries.

- id: ror-querying-03
  answer: |
    `find(id)` returns a single record and raises `ActiveRecord::RecordNotFound` when
    nothing matches. `find_by(hash)` returns the first matching record or `nil` when
    nothing matches (no exception). `where` never returns a record — it returns a
    lazy, chainable `ActiveRecord::Relation` (an empty relation if nothing matches);
    you get records by iterating or calling `first`/`take`, and it never raises for
    "no results".

- id: ror-querying-04
  answer: |
    `User.pluck(:email)` issues `SELECT email FROM users` and returns an array of
    plain strings — it never instantiates ActiveRecord objects. `User.all.map(&:email)`
    selects every column (`SELECT users.*`), instantiates a full model object per row
    (allocating lots of memory and running attribute initialization), and then
    extracts the attribute. pluck is faster and dramatically lighter on memory when
    you only need one or a few columns.

- id: ror-callbacks-transactions-01
  answer: |
    `save` (and its callbacks, including `after_save`) runs inside the enclosing
    database transaction, which commits only when the whole transaction succeeds.
    Side effects in `after_save` fire *before* commit: if the transaction later rolls
    back, you've already sent the email / called the API for data that was never
    saved; and an enqueued background job can even run before the data is visible to
    its own DB connection. `after_commit` runs only after the transaction has
    actually committed, so external side effects happen only for durably-saved data.

- id: ror-callbacks-transactions-02
  answer: |
    `save` returns `false` (and halts the callback chain) if the record is invalid or
    unsavable; `save!` raises `ActiveRecord::RecordInvalid` (or
    `RecordNotSaved`) instead. `create` instantiates and saves, returning the
    unsaved record with `errors` populated on failure; `create!` raises
    `ActiveRecord::RecordInvalid` on failure. The bang versions are useful when
    failure is exceptional and you want it to blow up (e.g. in seeds, tests, or
    service code where falsey returns get silently ignored).

- id: ror-callbacks-transactions-03
  answer: |
    You force a rollback by raising an exception inside the block — or, to roll back
    quietly, `raise ActiveRecord::Rollback`. What's special about it: the transaction
    machinery catches `ActiveRecord::Rollback` and swallows it — the transaction is
    rolled back but the exception is NOT re-raised, so the code after the transaction
    block runs normally and no `rescue` is needed. Any other exception rolls back the
    transaction and propagates. With nested transactions, `ActiveRecord::Rollback`
    also only affects the (logical) inner block rather than bubbling a normal
    exception up through the outer transaction.

- id: ror-callbacks-transactions-04
  answer: |
    For `record.save` on a new record, the order is: `before_validation` →
    `after_validation` → `before_save` → `around_save` (before yield) →
    `before_create` → `around_create` (before yield) → the INSERT SQL →
    `after_create` → `around_create` (after yield) → `after_save` → and, when the
    surrounding transaction commits, `after_commit` (with its `create` variant).
    Heavy callbacks are a smell because they run implicitly on *every* save from
    *every* call site, coupling unrelated side effects to persistence, hiding
    behavior in hard-to-see execution order, slowing down bulk operations, and making
    tests brittle. Better to move multi-step or external work into explicitly-called
    service objects or jobs.

- id: ror-migrations-schema-01
  answer: |
    A single `change` method works when every operation in it is automatically
    reversible by Rails: `create_table`, `add_column`, `rename_column`, `add_index`,
    `add_reference`, `create_join_table`, `remove_index` (with enough info), etc.
    You need `up`/`down` (or a `reversible` block inside `change`) when Rails cannot
    infer the inverse: raw `execute` of SQL, `change_column` (the old type isn't
    recorded), `drop_table` without a block describing the schema, or operations
    whose reverse needs explicit instructions. `remove_column` is reversible in
    `change` only if you pass the column type.

- id: ror-migrations-schema-02
  answer: |
    `schema.rb` is a Ruby DSL dump of the current schema (generated by
    `db:schema:dump`) that Rails replays to set up dev/test databases; it's
    database-agnostic and readable, but it can't represent database-specific
    features. `structure.sql` is a raw SQL dump of the schema
    (`config.active_record.schema_format = :sql`). Switch to `structure.sql` when
    you use features `schema.rb` can't round-trip: triggers, stored
    procedures/functions, database views, custom types, and other engine-specific
    constructs (e.g. in PostgreSQL) — otherwise they silently vanish from schema.rb.

- id: ror-migrations-schema-03
  answer: |
    Examples: adding an index (a full scan that can lock the table for a long time),
    and changing a column type / adding a column with a volatile default or NOT NULL
    + backfill (which rewrites the table or runs huge updates in one transaction).
    Safe approaches: on PostgreSQL add indexes with `algorithm: :concurrently`
    (requires `disable_ddl_transaction!`); add columns as nullable without backfill
    in the migration, then backfill in batched background/rake jobs (e.g. with
    `in_batches`), and only then enforce NOT NULL; use the expand/contract (multi-
    release) pattern so schema and code deploy separately; and run strong_migrations
    in dev to catch dangerous operations before they ship.

- id: ror-migrations-schema-04
  answer: |
    It generates: an integer (bigint) `post_id` column on `comments`, an index on
    `comments.post_id`, and a `post_id` foreign-key constraint
    (`add_foreign_key :comments, :posts`) referencing `posts.id`. The foreign key
    gives referential integrity at the database level — you can't insert a comment
    pointing at a nonexistent post, and deleting a parent with children raises (or
    cascades, if configured). The index is still needed because most databases (e.g.
    PostgreSQL) do NOT automatically index the child side of a foreign key, and
    lookups/joins/dependent-destroy queries on `post_id` would otherwise full-scan.

- id: ror-controllers-routing-01
  answer: |
    It creates the seven RESTful routes: GET /photos → index, GET /photos/new → new,
    POST /photos → create, GET /photos/:id → show, GET /photos/:id/edit → edit,
    PATCH/PUT /photos/:id → update, DELETE /photos/:id → destroy; plus named route
    helpers (photos_path, photo_path(record), new_photo_path, edit_photo_path).
    You can trim it with `only:`/`except:`, add `member`/`collection` blocks, and
    use `shallow: true` to nest some routes flat.

- id: ror-controllers-routing-02
  answer: |
    Strong Parameters prevent mass-assignment attacks: a malicious user can POST
    extra keys (e.g. `user[admin]=1`) that would otherwise be bulk-assigned to the
    model. `params.require(:user)` asserts the `:user` key exists and returns its
    sub-hash (raising `ActionController::ParameterMissing` if absent — usually a
    400). `.permit(:name, :email)` whitelists exactly those keys, silently dropping
    everything else (nested hashes need nested permit lists). Anything unpermitted
    simply never reaches the model.

- id: ror-controllers-routing-03
  answer: |
    `before_action` registers a method (or lambda) to run before the controller
    action — class-level filters like `before_action :authenticate_user!`. If a
    before_action renders or redirects, Rails halts processing: the action itself
    and any remaining callbacks never run. That's the standard guard pattern, e.g.
    `redirect_to login_path unless logged_in?` short-circuits the request.

- id: ror-controllers-routing-04
  answer: |
    A `member` route applies to a single resource and includes the id in the URL —
    inside `resources :photos do member { get :preview } end` you get
    GET /photos/:id/preview and preview_photo_path(photo). A `collection` route
    applies to the whole set with no id — GET /photos/search and search_photos_path.
    A successful API create should return 201 Created (ideally with a Location
    header pointing at the new resource), not plain 200.

- id: ror-controllers-routing-05
  answer: |
    Rails 8 ships a built-in auth generator (`rails generate authentication`) that
    scaffolds a minimal stack: a User model with bcrypt-hashed password_digest, a
    Session model, session-based sign in/out via a SessionsController, a `Current`
    session object, and a password-resume/reset flow (PasswordsController + token
    mailer). It gives you the core of authentication with almost no magic and full
    control over views/flows. Devise is a third-party gem with many optional modules
    (rememberable, trackable, lockable, timeoutable, omniauthable, registration
    controllers, prebuilt views/mailers). Choose the built-in generator for simple,
    hackable session auth you own; reach for Devise when you want batteries-included
    features like OAuth/omniauth and account locking without writing them yourself.

- id: ror-views-helpers-01
  answer: |
    Render a partial with locals via `render partial: "posts/card", locals: { post: post }`
    (or the shorthand `render "posts/card", post: post`). `render @posts` on a
    collection iterates the records and renders the conventionally-named partial
    once per record — for `@posts` it looks up `posts/_post.html.erb` and passes each
    record as the local named after the model (singular, `post`). You can override
    with `partial:` and `locals:`.

- id: ror-views-helpers-02
  answer: |
    `form_with` unifies the old `form_for` (model-backed) and `form_tag`
    (URL-backed) helpers into one: with a model it infers the URL and HTTP verb
    (and field values from the record), with just a url: it builds a plain form.
    In current Rails (6.1+) it generates a LOCAL (standard) form by default —
    `form_with_generates_remote_forms` defaults to false, so no UJS-driven
    remote/AJAX form unless you explicitly pass `remote: true` (or use Turbo, which
    intercepts standard form submissions instead).

- id: ror-views-helpers-03
  answer: |
    Rails uses an authenticity token: a random token stored in the session, embedded
    in every non-GET form as a hidden `authenticity_token` field, and verified by
    `protect_from_forgery` (default `with: :exception` in generated apps) on
    non-GET requests. An attacker's cross-site form can't know the token, so the
    forged request fails. `form_with` automatically inserts that hidden token input
    for you — you never add it manually (for non-form AJAX you must send
    X-CSRF-Token yourself, e.g. via the rails-ujs/meta tag).

- id: ror-views-helpers-04
  answer: |
    The fix belongs in the controller (the data-access layer): eager-load there, e.g.
    `@posts = Post.includes(:author)`. The view's job is presentation; it should
    consume fully-prepared data and never perform or optimize queries. "Fixing" it in
    the view (adding includes there) leaks query responsibility into presentation,
    makes the view dependent on persistence concerns, and hides a data-access
    decision where nobody maintaining queries will look. The N+1 originates from how
    the data was loaded, so it's fixed where it's loaded.

- id: ror-concerns-services-01
  answer: |
    An `ActiveSupport::Concern` is a module wrapper that makes mixins ergonomic: it
    lets you define a `class_methods` block that is auto-extended onto the including
    class, resolves dependencies between concerns, and provides the `included do
    ... end` hook. That `included` block runs in the context of the including
    (base) class when the concern is included, so you can cleanly invoke class-level
    macros on the host — `has_many`, `scope`, `validates`, `before_action`, etc. —
    which a plain module can only do via a manual, clunky
    `def self.included(base); base.extend ...; base.class_eval ...; end`.

- id: ror-concerns-services-02
  answer: |
    A service object is a plain Ruby object (PORO) that encapsulates one business
    operation — typically a single public method (`call`/`perform`) taking explicit
    arguments, often returning a result object. Extract one when the operation
    doesn't belong to a single model's lifecycle (spans several models, wraps a
    transaction with side effects like payments/notifications), when it's reused
    from multiple entry points (controller, job, rake task), or when the controller/
    model has grown past its natural responsibility.

- id: ror-concerns-services-03
  answer: |
    The guideline: keep controllers skinny (just parse params, delegate, respond with
    HTTP) by pushing business logic down into models, which are easy to unit test
    without HTTP. Taken too far it creates "fat model" / god-model failure mode: a
    thousand-line model mixing persistence, domain logic, validation, formatting,
    notification, and reporting — unmaintainable, with tangled concerns and slow,
    sprawling test suites. The modern antidote is extracting concerns, service
    objects, query objects, and form objects rather than making one class do
    everything.

- id: ror-concerns-services-04
  answer: |
    A concern is a mixin: its code is injected into the classes that include it and
    becomes part of those objects' own behavior — it's about *shared behavior across
    classes*, used implicitly wherever the host class is used. A service object is a
    separate collaborator you instantiate and call explicitly at a chosen point in
    the flow — it *operates on* models rather than living inside them, and it's about
    *a discrete operation*, with its own lifecycle and test. Rule of thumb: use a
    concern when several models genuinely share the same behavior; use a service
    when one operation is its own thing.

- id: ror-caching-jobs-01
  answer: |
    Russian-doll caching is nested fragment caching: each cached fragment may itself
    contain other cached fragments, so re-rendering a parent can reuse unchanged
    child fragments. Each cache key includes the record itself
    (`cache post` uses `post.cache_key_with_version`, e.g. posts/42-20260901...),
    which embeds `updated_at`. Any save updates `updated_at`, so the key changes —
    the old fragment simply stops being hit (and is evicted later) and the new one is
    written. Invalidation is therefore automatic: no manual expiry lists, a changed
    record just gets a new key. For nesting to stay correct, children must touch
    their parent on change (`belongs_to :parent, touch: true`) so the parent's key
    changes too.

- id: ror-caching-jobs-02
  answer: |
    ActiveJob is a framework-level abstraction for background jobs: a common job
    class API (perform, callbacks, serialization of arguments including records via
    GlobalID, retries, enqueue helpers) so application code is written once and is
    independent of the queueing backend. The queue adapter is the pluggable backend
    that actually stores and executes the jobs (Sidekiq, SolidQueue, GoodJob,
    Delayed::Job, etc.). The default adapter is `:async`, which runs jobs on
    in-process threads without persistence — jobs are lost on restart and it offers
    no durability or distribution — so it is NOT suitable for production; configure a
    real adapter.

- id: ror-caching-jobs-03
  answer: |
    Most queue systems guarantee at-least-once delivery: a worker crash, timeout, or
    retry policy can execute the same job twice, so a non-idempotent job causes
    duplicate side effects (double charge, double email, double inventory
    decrement). An idempotent job produces the same outcome whether run once or many
    times. Make jobs idempotent by: checking whether the work is already done before
    acting (and returning early), using unique/idempotency keys (deduped enqueues or
    unique-job features), writing state instead of incrementing it ("set status to
    sent" rather than "mark one more sent"), using conditional updates
    (`WHERE state = 'pending'`) or upserts, and using idempotency keys with external
    payment/email APIs.

- id: ror-caching-jobs-04
  answer: |
    `MyJob.perform_later(args)` serializes the arguments and enqueues the job on the
    queue adapter to be run asynchronously by a worker later. `MyJob.perform_now`
    executes the job synchronously, inline in the current process (useful for tests,
    or "urgent" work you consciously want on the request path). Pass the record's id
    (or let GlobalID serialize the record, which still refetches it) rather than
    relying on a full serialized object, because: the record may change or be deleted
    between enqueue and execution, so the job should reload fresh state from the
    database at execution time; it avoids serializing stale in-memory state or large
    object graphs; and it keeps the job's arguments small. Handle a missing record
    (record deleted before the job runs) inside the job.

# ror-activerecord-05 was omitted from the batch reply; re-asked in a fresh closed-book context.
- id: ror-activerecord-05
  answer: >-
    `normalizes` is a declarative macro introduced in Rails 7.1 (work by Jonathan Hefner) that registers an
    attribute-level normalization on an ActiveRecord model, e.g.:

    `class User < ApplicationRecord; normalizes :email, with: ->(value) { value.strip.downcase }; end`

    Two things happen once declared:

    1. WRITE SIDE — the `with:` lambda is applied at *assignment time*, not at save time. Any path that assigns
    through the attribute writer (`new`, `assign_attributes`, `update`, `email=`) normalizes immediately, so the
    in-memory object is always in canonical form. By default `nil` values are left untouched; `apply_to_nil: true`
    opts into normalizing nils as well. The normalization is applied before validations run, so validations and
    error messages see normalized data.

    2. READ/QUERY SIDE — values passed to `find_by` and `where` conditions for normalized attributes are also
    normalized before the query is built. So `User.find_by(email: "  DAVID@EXAMPLE.COM ")` matches a record stored
    as `david@example.com`. The declared normalizer can also be invoked standalone via
    `User.normalize_value_for(:email, value)`.

    Why this beats a `before_save` callback doing the same stripping/downcasing:

    - TIMING: `before_save` runs after validations and only during the save cycle. Validations, uniqueness checks,
    and any pre-save business logic observe the *raw* value, and what you validated can differ from what gets
    persisted. `normalizes` normalizes at assignment, so the object is consistent from the moment the value enters
    the model — no gap between in-memory state and persisted state.
    - NO ORDERING HAZARDS: callback ordering (before_validation vs before_save, interactions with autosaves and
    other callbacks) is a classic source of subtle bugs. The declarative macro removes the save lifecycle from the
    equation entirely.
    - READ-SIDE CONSISTENCY: a `before_save` callback only fixes writes; it cannot make lookups work, so
    `find_by`/`where` with unnormalized input would silently miss records. `normalizes` normalizes query arguments
    too, giving uniform semantics for reads and writes.
    - DECLARATIVE INTENT: normalization is co-located with the attribute declaration and self-documenting ("this
    attribute is always normalized"), instead of imperative logic buried in callbacks that must be hunted down
    across the model or included concerns.
    - TESTABLE AND REUSABLE: the normalizer can be exercised directly with `normalize_value_for` without
    instantiating or saving a record, and reused for form-input cleanup; callback logic is entangled with the save
    cycle.

    Fair caveats it shares with callbacks: `normalizes` does not normalize values loaded from the database, so
    existing rows need a data migration; and bulk write paths that skip model instantiation (`insert_all`,
    `update_all`, raw SQL) bypass it just as they bypass callbacks. It is not magic — it is the same write-time
    coverage with better semantics, plus the read/lookup integration and freedom from callback-ordering bugs.
    (A `before_validation` callback would be a somewhat better callback variant, but it still lacks query-side
    normalization and assignment-time consistency. Answered from model knowledge, no files or web consulted.)
```
