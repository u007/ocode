# cat-jam-free — closed-book answers for ror (opencode-go, 2026-10-14); answerer saw only _prompts/ror.md
# Delivery: one `ocode run -m opencode-go/cat-jam-free -effort med` session, sheet attached via -f, empty scratch cwd;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: ror-activerecord-01
  answer: |
    In modern Rails (5+), `belongs_to` is required by default: the parent must be present, otherwise validation fails with a blank-associated-record error. Make it optional with `belongs_to :parent, optional: true` (or adjust the global `belongs_to_required_by_default` config).
- id: ror-activerecord-02
  answer: |
    `has_and_belongs_to_many` uses a direct implicit join table with no join model, so you cannot store extra attributes or validations on the association. `has_many :through` uses an explicit join model, which allows validations, callbacks, timestamps, and extra columns. Prefer `has_many :through` for anything beyond a trivial many-to-many.
- id: ror-activerecord-03
  answer: |
    `dependent: :destroy` loads and destroys each associated record, running callbacks. `dependent: :delete_all` issues a bulk DELETE without instantiating records or callbacks. `dependent: :nullify` sets the foreign keys to NULL without destroying associated records.
- id: ror-activerecord-04
  answer: |
    The uniqueness validator is not atomic: two concurrent requests can pass it before either inserts. Add a unique database index (and appropriate constraint), preferably unique on the column or scoped columns, to enforce correctness under races.
- id: ror-activerecord-05
  answer: |
    `normalizes` applies a normalization block when assigning and when loading persisted values, keeping the attribute normalized in memory and before validations. It is better than `before_save` because normalization is consistent everywhere (not only at save time), avoids stale unnormalized in-memory values, and can support normalized lookups.
- id: ror-querying-01
  answer: |
    `preload` always uses separate queries (one for the main records, one per association). `eager_load` always uses a single LEFT OUTER JOIN. `includes` chooses between the two, typically preload unless you reference the association in conditions/joins. Use `references` or query conditions to force eager_load behavior with includes.
- id: ror-querying-02
  answer: |
    This is the N+1 query problem. Fix it by eager-loading the association, e.g. `Post.includes(:author)` and then iterate posts.
- id: ror-querying-03
  answer: |
    `find` looks up by primary key and raises `ActiveRecord::RecordNotFound` when missing. `find_by` returns the first matching record or `nil`. `where` returns an `ActiveRecord::Relation` (chainable/lazy), empty when nothing matches and does not raise.
- id: ror-querying-04
  answer: |
    `pluck` performs a SQL SELECT for only the requested column and returns an array without instantiating ActiveRecord objects, so it uses far less memory and less work than `all.map(&:email)`.
- id: ror-callbacks-transactions-01
  answer: |
    `after_save` can run even if the surrounding transaction later rolls back, causing external side effects for a record that was not actually persisted. `after_commit` runs only after the transaction commits, keeping side effects consistent with the database.
- id: ror-callbacks-transactions-02
  answer: |
    `save`/`create` return `false` and populate errors on validation failure. `save!`/`create!` raise `ActiveRecord::RecordInvalid` (or related) instead. Use bang versions when failure should be exceptional.
- id: ror-callbacks-transactions-03
  answer: |
    Raise `ActiveRecord::Rollback` inside the block. It is special because Rails catches it and rolls the transaction back without re-raising it, so execution continues after the transaction block.
- id: ror-callbacks-transactions-04
  answer: |
    Typical order includes `before_validation`, `after_validation`, `before_save`, `before_create`, the INSERT, `after_create`, `after_save` (and `after_commit` after transaction commit), with around callbacks wrapped around the block. Heavy callbacks are a design smell because they couple domain actions to persistence, slow tests, and can cause transactional or ordering problems.
- id: ror-migrations-schema-01
  answer: |
    Use a single reversible `change` method when Rails can infer the reverse operation. Use separate `up`/`down` when the migration uses irreversible operations, raw/custom SQL, data manipulation, or complex changes where Rails cannot determine the safe reverse.
- id: ror-migrations-schema-02
  answer: |
    `schema.rb` is a Ruby representation of the schema, usually DB-agnostic but unable to express all database features. `structure.sql` is a raw SQL dump that preserves DB-specific features. Switch to `structure.sql` when using views, triggers, stored procedures, custom types, extensions, or other features `schema.rb` cannot represent.
- id: ror-migrations-schema-03
  answer: |
    Two dangerous examples: adding an index that locks a large table, and adding/enforcing a NOT NULL constraint or changing a column type that forces a full table scan/rewrite. Safer approaches include creating indexes with `algorithm: :concurrently` where supported, adding nullable columns, backfilling in batches, and adding constraints as `NOT VALID` then validating later.
- id: ror-migrations-schema-04
  answer: |
    It generates a `post_id` column, an index on that column, and a foreign key constraint referencing `posts`. The index supports queries/joins and parent updates/deletes; the foreign key enforces referential integrity and prevents orphaned comments.
- id: ror-controllers-routing-01
  answer: |
    `resources :photos` generates the seven standard RESTful routes mapped to `index`, `show`, `new`, `edit`, `create`, `update`, and `destroy` actions, plus path helpers.
- id: ror-controllers-routing-02
  answer: |
    Strong Parameters prevent mass-assignment vulnerabilities by requiring explicit whitelisting. `require(:user)` ensures the params contain a `:user` hash and raises if absent; `permit(:name, :email)` whitelists allowed keys and filters anything else.
- id: ror-controllers-routing-03
  answer: |
    `before_action` runs code before the action. Rendering or redirecting inside it halts the callback chain and prevents the controller action from running; returning early without render/redirect does not by itself stop the action in modern Rails.
- id: ror-controllers-routing-04
  answer: |
    A `member` route operates on a single existing resource and includes an `:id` (e.g. `/photos/:id/archive`). A `collection` route operates on the whole collection and has no `:id` (e.g. `/photos/search`). A successful API `create` should return `201 Created`.
- id: ror-controllers-routing-05
  answer: |
    Rails 8's authentication generator scaffolds a conventional email/password authentication system (user model, sessions, current user, controllers/views, password-reset flow, migrations) that you fully own. It avoids adding a large external auth gem and gives standard, customizable code rather than Devise's feature-heavy plugin model.
- id: ror-views-helpers-01
  answer: |
    Render a partial with locals via `render 'comment', comment: @comment` or `render partial: 'comment', locals: { comment: @comment }`. `render @posts` infers `_post.html.erb`, renders it once per post, and passes each post as the local `post`.
- id: ror-views-helpers-02
  answer: |
    `form_with` unifies `form_for`/`form_tag` into one helper for model- and non-model-backed forms. In current Rails it defaults to a local standard (non-AJAX) form submission; earlier Rails versions defaulted to remote/AJAX.
- id: ror-views-helpers-03
  answer: |
    Rails protects against CSRF with `protect_from_forgery`, using an authenticity token verified on non-GET requests. `form_with` automatically includes the hidden authenticity-token field. Non-HTML requests can also use the token from meta tags or headers.
- id: ror-views-helpers-04
  answer: |
    The fix belongs in the controller/model query layer: eager-load `:author` when loading `@posts`. Views should render data, not trigger database queries or own loading strategy.
- id: ror-concerns-services-01
  answer: |
    An `ActiveSupport::Concern` is a module mixin pattern. Its `included do ... end` block evaluates in the including class context, so associations, validations, callbacks, and scopes can be declared cleanly for each including class.
- id: ror-concerns-services-02
  answer: |
    A service object is a PORO encapsulating one business operation/use case. Extract one when logic is complex, spans multiple models, calls external APIs, orchestrates side effects, or has no natural single-model owner instead of bloating a model or controller.
- id: ror-concerns-services-03
  answer: |
    The guideline says controllers handle HTTP and models hold domain logic/business rules. Taken too far, it produces bloated "god" models, hidden callbacks, hard-to-test code, and mixed responsibilities.
- id: ror-concerns-services-04
  answer: |
    A concern is mixed into an existing class to share behavior, while a service object is instantiated/called to perform a specific operation. Concerns extend objects' behavior; services orchestrate use cases.
- id: ror-caching-jobs-01
  answer: |
    Russian-doll caching nests cached fragments. Using the record itself (or its updatedAt/version) in the cache key makes invalidation automatic because updating the record changes the key, causing cache misses and rebuilding dependent fragments.
- id: ror-caching-jobs-02
  answer: |
    ActiveJob provides a unified API for declaring and enqueuing background jobs across queue backends. The adapter determines queuing/execution behavior (e.g. Sidekiq, Delayed Job, async). The default inline/async-style adapter is not suitable for production; use a persistent adapter.
- id: ror-caching-jobs-03
  answer: |
    Background systems may retry or deliver a job more than once, so repeated execution must not duplicate side effects. Make jobs idempotent by recording processed IDs, checking current state before mutating, using unique constraints, and wrapping state changes in transactions.
- id: ror-caching-jobs-04
  answer: |
    `perform_later` enqueues the job for background execution; `perform_now` runs it synchronously in the current process. Pass a record's id rather than the record because job arguments must be serializable and records can become stale; the job should reload the record from the database.
