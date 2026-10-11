- id: ror-activerecord-01
  answer: |
    Yes. Since Rails 5, `belongs_to` is required by default (driven by
    `config.active_record.belongs_to_required_by_default`, set by `load_defaults 5.0+`).
    If the parent is missing (the foreign key is nil), an implicit presence validation
    fails, so `save`/`create` returns false (or raises with `save!`/`create!`) and the
    record is invalid. Make it optional per-association with `belongs_to :parent, optional: true`,
    or globally by setting `belongs_to_required_by_default = false`.
- id: ror-activerecord-02
  answer: |
    `has_many :through` goes through a real join *model* (e.g. `has_many :appointments`
    plus `has_many :patients, through: :appointments`). That join model can have its own
    columns, validations, callbacks, scopes and associations. `has_and_belongs_to_many`
    (HABTM) uses a hidden join table with no model, so you cannot attach data or behaviour
    to the relationship. Reach for `has_many :through` in almost all cases — it is the
    documented recommendation and is far more flexible; use HABTM only for a pure, simple
    many-to-many with no extra attributes and no need for a join model.
- id: ror-activerecord-03
  answer: |
    `dependent: :destroy` instantiates each associated record and calls `destroy` on it, so
    its callbacks run (and its own dependents cascade) — correct but slow.
    `dependent: :delete_all` issues a single SQL `DELETE` for the associated rows without
    instantiating them or running callbacks (fast, but skips callbacks/validations and can
    leave orphans further down the chain).
    `dependent: :nullify` sets the children's foreign key to NULL via a single SQL `UPDATE`
    instead of deleting them (children survive, detached). It also does not run callbacks.
- id: ror-activerecord-04
  answer: |
    The uniqueness validation is only a check-then-insert at the application layer; two
    concurrent requests (or two processes) can both read "no existing row" and both insert,
    producing a duplicate. It is also vulnerable to races between `valid?` and `save`.
    The real guarantee must come from the database: add a unique index
    (`add_index :users, :email, unique: true`), and optionally rescue
    `ActiveRecord::RecordNotUnique` (or use `create_or_find_by`) to handle the race
    gracefully. Note the validation's case-sensitivity depends on the column's collation.
- id: ror-activerecord-05
  answer: |
    `normalizes` (Rails 7.1) declares a normalization applied consistently to an attribute,
    e.g. `normalizes :email, with: ->(e) { e.strip.downcase }`. It runs when the attribute
    is assigned and also normalizes values used in query methods like `where`/`find_by`
    (so lookups match), and it runs before validation. Compared with a `before_save`
    callback it is better because: it normalizes on assignment (not only at persist time),
    it normalizes query inputs too, it runs before validations so uniqueness/presence see
    the normalized value, it avoids callback ordering and callback side effects, and it is
    declarative and easier to test.
- id: ror-querying-01
  answer: |
    `preload` always loads the association with a separate query per association
    (`SELECT * FROM authors WHERE id IN (...)`) and cannot be used in `where`/`order`.
    `eager_load` always loads with a single `LEFT OUTER JOIN`, so you can filter/order on
    the association's columns. `includes` is the smart default: it uses `preload`
    (separate queries) normally, but switches to `eager_load` (join) when it detects the
    association is referenced in `where`/`order`/`references` — so `includes` gives you the
    best of both unless you force the strategy with `references`.
- id: ror-querying-02
  answer: |
    This is the N+1 query problem: one query to load the posts, then one extra query per
    post to load its author. Fix it by eager-loading the association before iterating:
    `@posts = Post.includes(:author)` (or `preload`/`eager_load`) in the controller, so it
    becomes two queries total. A counter cache (`counter_cache: true`) is the alternative
    when you only need a count.
- id: ror-querying-03
  answer: |
    `find(id)` takes a primary key, returns the single record, and raises
    `ActiveRecord::RecordNotFound` if none exists. `find_by(attrs)` returns the first
    matching record or `nil` (no exception). `where(attrs)` returns an
    `ActiveRecord::Relation` (always, even if it matches nothing), which is lazy and
    chainable; you must call `.first`/`.each` to get records. So `find` → record or raise,
    `find_by` → record or nil, `where` → relation.
- id: ror-querying-04
  answer: |
    `pluck(:email)` issues `SELECT email FROM users` and returns an array of raw values,
    without instantiating `User` objects. `User.all.map(&:email)` runs `SELECT * FROM users`,
    builds a full ActiveRecord object per row (allocating and typecasting every column), then
    calls the method on each. `pluck` is therefore much faster and uses far less memory —
    it only selects and materializes the column you need. Use `pluck` when you want data,
    not models.
- id: ror-callbacks-transactions-01
  answer: |
    `after_save` fires *inside* the surrounding transaction, before it commits. So an email
    or external API call can go out even though the transaction later rolls back (e.g. a
    later callback or a nested save raises), and the record may not be visible to other
    connections. `after_commit` fires only after the transaction has successfully committed,
    so the data is durable and visible, and side effects only happen for a real, persisted
    change. For enqueuing mail/jobs or calling external APIs, use `after_create_commit` /
    `after_update_commit` / `after_commit`. (Even then, enqueue with the record's id and
    make the job idempotent.)
- id: ror-callbacks-transactions-02
  answer: |
    `save` runs validations and returns `true` or `false`; on failure the record is left
    with errors and nothing is persisted. `save!` runs the same validations but raises
    `ActiveRecord::RecordInvalid` (or `RecordNotSaved`) on failure instead of returning
    false. Likewise `create` = `new` + `save` and returns the object whether or not it
    persisted (check `persisted?`/`errors`), while `create!` raises on failure. Bang methods
    are for cases where failure is exceptional.
- id: ror-callbacks-transactions-03
  answer: |
    Force a rollback by raising `ActiveRecord::Rollback` inside the block. What is special
    about it is that the `transaction` method rescues it internally: the transaction is
    rolled back but the exception does *not* propagate out of the `transaction` call, and
    the call returns `nil`. Any other exception also rolls back, but it propagates to the
    caller (and can trigger `after_rollback` callbacks). `throw :abort` is used inside
    callbacks, not the transaction block, for the same silent-rollback effect.
- id: ror-callbacks-transactions-04
  answer: |
    For `save` on a new record the order is:
    `before_validation` → `after_validation` → `before_save` → `before_create`
    → `after_create` → `after_save` → `after_commit` (or `after_rollback`).
    (There are also `around_save`/`around_create` wrappers around the save/create phases,
    and `after_save` still runs before commit.) Heavy callbacks are a design smell because
    they make saves slow and unpredictable, couple persistence to unrelated business logic
    and external side effects, create ordering dependencies that are hard to reason about,
    and make models difficult to test and reuse. Side effects belong in service objects or
    `after_commit` enqueued jobs.
- id: ror-migrations-schema-01
  answer: |
    Use a single reversible `change` method when every operation in the migration has a
    known inverse that Rails can infer — `create_table`, `add_column`, `remove_column`
    (with a type), `add_index`, `add_reference`, `rename_column`, etc. You need explicit
    `up`/`down` when the migration is not automatically reversible: raw `execute` SQL,
    `change_column`, data migrations, conditional logic, dropping a column whose type/name
    Rails can't reconstruct, or anything where the reverse is ambiguous. You can also wrap
    specific steps in `reversible do |dir| dir.up { ... } dir.down { ... } end` inside a
    `change` method.
- id: ror-migrations-schema-02
  answer: |
    `schema.rb` is a Ruby DSL dump of the schema — database-agnostic, compact, fast to load,
    and easy to diff. It cannot represent database-specific objects such as triggers, views,
    stored functions/procedures, some constraints, or certain extensions.
    `structure.sql` is a raw SQL dump (e.g. `pg_dump`) and captures *everything* exactly as
    the database holds it. Switch to `structure.sql` (via
    `config.active_record.schema_format = :sql`) when you rely on DB features that
    `schema.rb` cannot express (triggers, views, materialized views, functions, check
    constraints, extensions, partial/complex indexes), or when you need byte-exact parity
    with production.
- id: ror-migrations-schema-03
  answer: |
    Two dangerous operations on a large table:
    1. Adding an index without `CONCURRENTLY` — it locks writes/reads for the duration.
       Safe form: `add_index ..., algorithm: :concurrently` (run with
       `disable_ddl_transaction!` in PostgreSQL).
    2. Changing a column's type (or adding a column with a default on older DBs) — rewrites
       and locks the whole table. Safe form: add a new column, backfill in batches, swap, or
       add the column without a default then backfill. Other examples: adding a `NOT NULL`
       constraint directly (add it as `NOT VALID` then `validate_constraint`), or adding a
       foreign key (use `validate: false` then validate later). Tools like the
       `strong_migrations` gem flag these.
- id: ror-migrations-schema-04
  answer: |
    `add_reference :comments, :post, foreign_key: true` adds a `post_id` column (bigint by
    default), an index on `post_id`, and a database-level foreign key constraint from
    `comments.post_id` to `posts.id`. You want both because they do different jobs: the
    foreign key enforces referential integrity (the DB refuses to insert a comment with a
    nonexistent post, and can restrict/cascade deletes), while the index makes lookups,
    joins and FK checks fast — without it, queries filtering by `post_id` and parent
    deletes can be slow or take locks. `add_reference` adds both automatically.
- id: ror-controllers-routing-01
  answer: |
    `resources :photos` creates the seven RESTful routes and maps them to `PhotosController`:
    `GET /photos` → `index`, `GET /photos/new` → `new`, `POST /photos` → `create`,
    `GET /photos/:id` → `show`, `GET /photos/:id/edit` → `edit`,
    `PATCH/PUT /photos/:id` → `update`, `DELETE /photos/:id` → `destroy`. It also generates
    the named route helpers (`photos_path`, `photo_path`, `new_photo_path`, `edit_photo_path`).
- id: ror-controllers-routing-02
  answer: |
    Strong Parameters prevent mass-assignment vulnerabilities: they force you to explicitly
    whitelist which request parameters may be assigned to a model, so a malicious client
    cannot set fields like `admin` or `role`. In
    `params.require(:user).permit(:name, :email)`, `require(:user)` fetches the nested
    `user` hash and raises `ActionController::ParameterMissing` if it is absent;
    `.permit(:name, :email)` returns a new `ActionController::Parameters` containing only
    those allowed keys (scalar, array or nested as specified), filtering everything else
    out. Passing an unpermitted hash to `create`/`update` raises `ForbiddenAttributesError`.
- id: ror-controllers-routing-03
  answer: |
    `before_action` registers a callback that runs before the specified controller actions
    (optionally limited with `only:`/`except:`), commonly used for authentication,
    authorization, or loading a record. If the callback renders or redirects (i.e. produces
    a response), the action itself is halted and never runs — the request ends with the
    callback's response. In Rails 5+ simply returning `false` does *not* halt the chain;
    halting happens because a response was performed (render/redirect_to). So a redirect in
    a `before_action` short-circuits the action.
- id: ror-controllers-routing-04
  answer: |
    A `member` route acts on a single resource and includes an `:id` in the URL, e.g.
    `member { get :preview }` → `/photos/:id/preview` → `photos#preview`. A `collection`
    route acts on the whole collection and has no `:id`, e.g. `collection { get :search }`
    → `/photos/search` → `photos#search`. A successful API `create` should return
    **201 Created** (typically with a `Location` header pointing at the new resource);
    some APIs return 200, but 201 is the correct status.
- id: ror-controllers-routing-05
  answer: |
    Rails 8 ships a built-in authentication generator (`bin/rails generate authentication`)
    that scaffolds a minimal, session-cookie-based password authentication: a `User` model
    with `has_secure_password`, a `Session` model/record, an `Authentication` controller
    concern, `Current` for the current user, sessions and passwords controllers, password
    reset mailer, and the migrations/views needed — all code you own with no external
    dependency. It differs from Devise, which is a full-featured third-party gem with many
    optional modules (confirmable, lockable, recoverable, trackable, rememberable, OmniAuth
    integrations, etc.) and heavy configuration. Reach for the generator for simple
    first-party email/password auth you can read and modify; reach for Devise when you need
    its richer feature set, social login, or a long-established ecosystem.
- id: ror-views-helpers-01
  answer: |
    Render a partial with locals via
    `render partial: "post", locals: { post: @post }`, or the shorthand
    `render "post", post: @post` (the leading underscore and extension are optional).
    `render @posts` (passing a collection) renders the `_post` partial once for each element,
    exposing each as the local named after the partial (`post`), and concatenates the output;
    it also enables collection caching if the partial is wrapped in a `cache` block.
- id: ror-views-helpers-02
  answer: |
    `form_with` unifies the old `form_for` (model-backed forms) and `form_tag` (plain forms)
    into one helper; it takes a model or a URL and builds the correct action, method, and
    field-name scoping. In current Rails (since Rails 6.1) it generates a **local** (standard,
    full-page submit) form by default; you opt into AJAX with `local: false` (or `remote: true`
    in older versions, where the default was the opposite).
- id: ror-views-helpers-03
  answer: |
    Rails protects against CSRF with a per-session authenticity token: the server embeds a
    token tied to the user's session in forms (and in a `<meta>` tag), and on every non-GET
    (state-changing) request `protect_from_forgery` compares the submitted token with the
    session's, raising `ActionController::InvalidAuthenticityToken` on mismatch. `form_with`
    automatically inserts the hidden `authenticity_token` field for you, so you get CSRF
    protection on generated forms without doing anything; for AJAX you must include the token
    (e.g. via the `X-CSRF-Token` header).
- id: ror-views-helpers-04
  answer: |
    The fix belongs in the controller (or a model scope/association), not the view: eager-load
    the association with `includes(:author)` (or `preload`) before the view iterates, turning
    N+1 queries into a constant number. You should not "fix" it in the view because by the
    time the view runs the collection is already loaded and the queries have already fired;
    loading strategy is a data-access concern that belongs with the query, and putting it in
    the view couples presentation to the database and hides the real cost from the controller.
- id: ror-concerns-services-01
  answer: |
    `ActiveSupport::Concern` is a module with a defined interface for mixins. Its
    `included do ... end` block is evaluated in the context of the class that includes the
    concern, so you can call class-level macros (`has_many`, `validates`, `scope`,
    `before_save`, etc.) as if written in the model. It also provides `class_methods do ... end`
    to define class methods cleanly, and it automatically resolves/include the concern's own
    dependencies (other included concerns). A plain module mixin can't do this cleanly: you'd
    have to hand-write `self.included(base)` and `base.extend ClassMethods`, and nested module
    dependencies wouldn't be included automatically.
- id: ror-concerns-services-02
  answer: |
    A service object is a plain Ruby object (PORO) that encapsulates a single business
    operation or use case, usually exposing a `call` (or `perform`) method, taking its
    collaborators/inputs via the initializer and returning a result. Extract one when the
    logic spans multiple models, is a multi-step process or transaction, coordinates external
    services, is reused by controllers/jobs/console, or doesn't naturally belong to any one
    model's data. Prefer it over adding another model method when the behaviour isn't really
    about that model's own data/lifecycle, and over a controller method when it is reusable
    or too complex to belong in the request layer.
- id: ror-concerns-services-03
  answer: |
    "Skinny controller, fat model" says controllers should only orchestrate — parse/whitelist
    params, invoke the domain, and render/redirect — while business logic lives in the models,
    which own their data and rules. The failure mode of taking it too far is the god object:
    models grow to thousands of lines mixing persistence, business rules, external API calls,
    reporting, and even presentation, becoming hard to test, understand, and change, with
    unrelated responsibilities tangled together. The remedy is to split that fat model into
    focused concerns and service objects rather than blindly piling everything onto the model.
- id: ror-concerns-services-04
  answer: |
    A concern is a mixin: it is `include`d into a class, so its methods become part of that
    class's instances and it extends the object's behaviour in place (e.g. sharing a
    `soft_deletable` or `publishable` behaviour across models). A service object is a separate
    collaborator you instantiate and call to perform a specific task, passing inputs and
    getting a result; it is not mixed into the model and does not become part of its public
    surface. In short: a concern adds reusable behaviour *to* an object; a service performs
    an operation *on behalf of* the caller.
- id: ror-caching-jobs-01
  answer: |
    Russian-doll caching nests fragment caches: an outer fragment's cache key incorporates
    the keys of the inner fragments/collections, so when any inner piece changes the outer
    key changes too and the whole stale fragment is discarded. Using the record itself as the
    cache key (`cache post do ... end`) builds the key from `post.cache_key_with_version`,
    i.e. the record's id plus its `updated_at`. Any update bumps `updated_at`, which changes
    the cache key, so the next render misses the old entry and writes a fresh one — the cache
    invalidates itself automatically without you having to expire it manually. For a
    collection, `cache_key` is based on the max `updated_at` among the records.
- id: ror-caching-jobs-02
  answer: |
    ActiveJob provides a backend-agnostic interface for background jobs: a common API
    (`perform_later`, `perform_now`), argument serialization, queue names/priorities, and
    features like `retry_on`/`discard_on`, `after_perform`, and scheduling. The queue adapter
    is the actual backend that stores and executes the jobs — Sidekiq, Resque, Delayed Job,
    GoodJob, Solid Queue, etc. The default adapter (`:async`) runs jobs in an in-process
    thread pool; it is **not** suitable for production because jobs are held in memory and are
    lost on process restart/crash, there is no persistence or cross-process retry, and it
    competes with request threads. Production needs a durable adapter (Rails 8 defaults to
    Solid Queue).
- id: ror-caching-jobs-03
  answer: |
    Jobs can run more than once because queueing systems generally offer at-least-once
    delivery: a worker may crash after doing the work but before acknowledging it, a job may
    be retried after a timeout, or a duplicate may be enqueued. If the job isn't idempotent,
    a re-run can double-charge a card, send a duplicate email, or double-increment a counter.
    An idempotent job has the same effect whether run once or many times. Make one idempotent
    by: using unique constraints and `find_or_create_by`/upsert instead of blind inserts,
    checking a "processed" flag or idempotency key before acting, using database
    transactions/locks, updating to an absolute value rather than incrementing, deduplicating
    with a unique job key, and making the operation naturally repeat-safe.
- id: ror-caching-jobs-04
  answer: |
    `MyJob.perform_later` enqueues the job on the configured queue adapter to run
    asynchronously later (returning a job instance), while `MyJob.perform_now` executes the
    job immediately, inline, in the current process. You should pass a record's **id**
    rather than the record because ActiveJob must serialize the arguments; passing a record
    relies on GlobalID and captures the object as it was at enqueue time, so by the time the
    job runs the record may have changed or been deleted, and serialization of arbitrary
    objects can be fragile. Passing the id (a simple, serializable value) lets the job
    re-fetch the latest state in its own transaction, avoids stale data, and is cheaper and
    more reliable to serialize. (Jobs should also handle the record having been deleted.)
