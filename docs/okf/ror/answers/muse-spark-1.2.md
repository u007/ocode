# ror knowledge benchmark — answers
# Model: opencode-go/muse-spark-1.2   (provider-stripped id: muse-spark-1.2)
# CLOSED-BOOK via ocode run -dir <empty> (no corpus access). corpus_rev 1.

```yaml
- id: ror-activerecord-01
  answer: |
    Yes, since Rails 5 `belongs_to` is required by default. It adds an implicit `presence` validation - if `belongs_to :author` and `author` is nil / `author_id` doesn't point to a persisted record, validation fails (`Author must exist`) and `save/create` returns false / is invalid.
    Make it optional with `belongs_to :author, optional: true` (the old syntax `required: false` also works). Use when the association is truly nullable (e.g. draft, polymorphic, legacy data).

- id: ror-activerecord-02
  answer: |
    Both model many-to-many.
    `has_and_belongs_to_many` (HABTM): direct join table (e.g. `posts_tags`) with NO join model. You cannot add attributes, validations, callbacks, or extra logic on the join, and you cannot query the join directly.
    `has_many :through`: join via a full ActiveRecord model (e.g. `Post has_many :taggings; has_many :tags, through: :taggings`). The join is a first-class model so you can add columns (e.g. `position`, `added_by`), validations, callbacks, scopes.
    Default to `has_many :through` - it is strictly more flexible and only slightly more code. Reach for HABTM only for a trivial, attribute-free mapping you are sure will never need enrichment.

- id: ror-activerecord-03
  answer: |
    All on `has_many :comments, dependent: ...` control what happens to children when parent is destroyed:
    - `:destroy` - instantiates each child and calls `#destroy` one-by-one. Callbacks (`before_destroy`/`after_destroy`) and `dependent: :destroy` down the chain run. Correct but slow, N queries.
    - `:delete_all` - single `DELETE FROM comments WHERE post_id = ?` SQL without instantiating. No callbacks run. Much faster but skips cleanup logic.
    - `:nullify` - single `UPDATE comments SET post_id = NULL WHERE post_id = ?`. Leaves children orphaned but present. No callbacks, requires column to be nullable.
    Also: `:destroy_async` (background job) and `:restrict_with_error/exception`.

- id: ror-activerecord-04
  answer: |
    `validates :email, uniqueness: true` is only an application-level check with a race condition: two concurrent requests can both pass validation (`SELECT` finds no row) then both `INSERT` duplicates. It also doesn't protect against direct SQL, `update_all`, or bypassing validations.
    You must add a unique database constraint/index: `add_index :users, :email, unique: true` in a migration. For case-insensitive emails also normalize (downcase) and create a lower() unique index or Postgres `citext`. Optionally add `validates :email, uniqueness: { case_sensitive: false }` to match DB semantics.

- id: ror-activerecord-05
  answer: |
    `normalizes :email, with: ->(v) { v.strip.downcase }` (Rails 7.1+) declaratively normalizes an attribute on assignment/before validation.
    Better than `before_save`/`before_validation` because: 1) it runs before validation so validations see the normalized value, 2) it normalizes whenever the attribute is assigned (not just on save), so `user.email = " FOO@BAR "` is immediately `foo@bar`, 3) it's declarative and testable in one place, 4) it also applies to `find_or_create_by` style lookups. `before_save` runs after validation and only on persist, so validation would run on the un-normalized value.

- id: ror-querying-01
  answer: |
    All three solve N+1 but hit DB differently:
    - `preload`: always 2 queries. `SELECT * FROM posts ...` then `SELECT * FROM authors WHERE id IN (...)` and stitches in Ruby. Never uses JOIN. Good when you just want to preload without filtering on the association.
    - `eager_load`: always 1 query with `LEFT OUTER JOIN`. `SELECT posts.*, authors.* FROM posts LEFT OUTER JOIN authors ...`. Needed if you `where`/`order` on the included table in same query but can duplicate rows and is heavier.
    - `includes`: smart hybrid. By default does `preload` (2 queries). If you reference the association in `where`/`order`/`references`, it switches to `eager_load` (1 JOIN). So `Post.includes(:author)` -> 2 queries; `Post.includes(:author).where(authors: {active: true}).references(:authors)` -> 1 JOIN.

- id: ror-querying-02
  answer: |
    Called N+1 query problem: 1 query for `@posts` plus N queries (one per post) for `author` inside the loop.
    Fix by eager loading in the controller/query before iterating: `@posts = Post.includes(:author)` (or `preload`/`eager_load`). Then `p.author.name` hits the already-loaded association with no extra SQL. Detect with `bullet` gem.

- id: ror-querying-03
  answer: |
    - `find(id)` - finds by primary key (can take array of ids). Returns record(s). Raises `ActiveRecord::RecordNotFound` if not found. Always hits DB.
    - `find_by(email: "a@b.com")` - finds first matching record by any column. Returns single record or `nil` if nothing matches. No exception.
    - `where(email: "a@b.com")` - returns an `ActiveRecord::Relation` (lazy, chainable), never nil. Empty relation if nothing matches, array-like collection if matches. Needs `.first`, `.take`, or iteration to materialize. `find_by` is shorthand for `where(...).take`.

- id: ror-querying-04
  answer: |
    `User.pluck(:email)` executes `SELECT email FROM users` and returns a Ruby array of strings directly from the driver. `User.all.map(&:email)` executes `SELECT * FROM users`, instantiates a full `User` ActiveRecord object per row (all columns, type casting, overhead) then maps in Ruby.
    `pluck` is faster, uses far less memory/allocations and less IO, especially for large tables or wide tables. Use `pick` for single value, `pluck` for single/multiple columns.

- id: ror-callbacks-transactions-01
  answer: |
    `after_save` / `after_create` / `after_update` run INSIDE the database transaction before commit. If you enqueue a job/send email/call external API there and the transaction later rolls back (validation failure downstream, exception, `Rollback`), the external side effect has already happened but the DB change never persisted - job will see missing record, email refers to non-existent data, double delivery.
    `after_commit` runs only AFTER the transaction commits successfully (and `after_rollback` on rollback). So use `after_commit :send_email, on: [:create, :update]` or `after_create_commit`. Also use `after_commit` for enqueuing ActiveJob.

- id: ror-callbacks-transactions-02
  answer: |
    `save` / `create` (bang-less) run validations and return `true`/`record` on success or `false` on validation failure (record not persisted, `errors` populated). They do not raise on invalid data.
    `save!` / `create!` run validations and raise `ActiveRecord::RecordInvalid` (and other DB exceptions) on failure instead of returning false. Use bang in seeds, migrations, or when you expect success and want exception control flow; use non-bang in controllers/forms where you branch on `if @user.save`.

- id: ror-callbacks-transactions-03
  answer: |
    Inside `ActiveRecord::Base.transaction do ... end`, any unhandled exception triggers rollback. To force rollback explicitly `raise ActiveRecord::Rollback` or `raise` any error.
    Special: `ActiveRecord::Rollback` is caught by the transaction machinery - it rolls back the transaction but is NOT re-raised outside the block (the block just returns silently). Any other exception rolls back AND bubbles up. So use `raise ActiveRecord::Rollback` when you want to rollback without notifying outer code; use `raise "reason"` or `fail` when caller should know.

- id: ror-callbacks-transactions-04
  answer: |
    For `record = Model.new; record.save` (create path) order is: `before_validation`, `after_validation`, `before_save`, `around_save` (yield), `before_create`, `around_create` (yield), `after_create`, `after_save`, `after_commit/after_rollback` (after DB commit).
    More precisely: `before_validation -> after_validation -> before_save -> before_create -> (around_create/around_save wrapping) -> after_create -> after_save -> after_commit`.
    On update: `before_validation -> after_validation -> before_save -> before_update -> after_update -> after_save -> after_commit`.
    Heavy callbacks are a smell because they hide business logic, make order-dependent side effects hard to reason about/test, violate SRP and lead to callback hell and surprising cascades. Prefer explicit service objects or `after_commit` only for truly internal concerns.

- id: ror-migrations-schema-01
  answer: |
    Use single `def change` when the migration is reversible - Rails can automatically infer `down` from `up`. Creates, `add_column`, `add_index`, `add_reference`, `create_table` etc. are reversible.
    Need explicit `def up` / `def down` (or `def change` with `reversible do |dir|`) when not reversible: `execute "raw sql"`, `change_column`, `change_column_default` with complex logic, data migrations (`update_all`), removing column with data you want to restore, or custom transformations where Rails cannot guess the inverse.

- id: ror-migrations-schema-02
  answer: |
    `schema.rb` is Ruby DSL dump (`ActiveRecord::Schema.define`) generated by `db:schema:dump`. DB-agnostic, readable, but only represents what ActiveRecord understands - loses DB-specific features: triggers, stored procedures, check constraints, enums, extensions (PostGIS, pg extensions), etc.
    `structure.sql` is raw SQL dump via `pg_dump`/`mysqldump`. Preserves everything exactly.
    Switch to `structure.sql` (`config.active_record.schema_format = :sql`) when you use Postgres features like enums, exclusion constraints, custom functions, materialized views, or need exact fidelity for production parity.

- id: ror-migrations-schema-03
  answer: |
    1) Adding a column with default + NOT NULL or adding `NOT NULL` constraint to large table: rewrites whole table, locks for long time. Safe: add column without default/nullable, backfill in batches (`update_in_batches` / background job), then add default and `NOT NULL` in separate migration, or in Postgres 11+ add column with default is faster but still lock careful.
    2) Adding index non-concurrently: `add_index` locks table for writes for duration. Safe: `add_index :users, :email, algorithm: :concurrently` and `disable_ddl_transaction!` (outside transaction) so it doesn't block. Similarly: adding foreign key with `validate: false` then validate concurrently; dropping/changing column type.

- id: ror-migrations-schema-04
  answer: |
    Generates: `post_id` column (type `bigint` by default), an index on `post_id` (`index_comments_on_post_id`), and a foreign key constraint `fk -> posts.id` (if `foreign_key: true`).
    Why both: FK ensures referential integrity at DB level - cannot insert invalid `post_id`, orphan prevention, cascades checked by DB even bypassing Rails. Index speeds up JOINs and lookups (`WHERE post_id = ?`, `includes`), and is required for performant FK checks (otherwise every insert/update on comments/posts would full-scan).

- id: ror-controllers-routing-01
  answer: |
    `resources :photos` creates 7 RESTful routes:
    GET    /photos          -> photos#index
    GET    /photos/new      -> photos#new
    POST   /photos          -> photos#create
    GET    /photos/:id      -> photos#show
    GET    /photos/:id/edit -> photos#edit
    PATCH/PUT /photos/:id   -> photos#update
    DELETE /photos/:id      -> photos#destroy
    plus helpers `photos_path`, `new_photo_path`, `photo_path(id)`, `edit_photo_path(id)`. Variants: `only:`/`except:` to limit, `resource` singular.

- id: ror-controllers-routing-02
  answer: |
    Solves mass-assignment vulnerability - attacker adding `params[:user][:admin]=true` to set protected attributes.
    Strong Parameters require explicit whitelisting. `params.require(:user)` ensures the top-level key `:user` is present (raises `ActionController::ParameterMissing` if absent) and returns the permitted parameters object for that hash. `.permit(:name, :email)` whitelists only those keys (and nested keys you allow) to be passed to `Model.new/update` - unpermitted keys are filtered/logged.

- id: ror-controllers-routing-03
  answer: |
    `before_action :authenticate_user!` (alias `before_action`/`before_filter`) is a controller filter that runs before the action method. Used for auth, loading resources, etc. Can limit with `only:`/`except:`.
    If it renders (`render ...`) or redirects (`redirect_to ...`) the filter chain is halted and the action never runs (in Rails 5+ you must `return` or just render/redirect - the halt is based on performed? ; older needed `render and return`). `prepend_before_action` controls order.

- id: ror-controllers-routing-04
  answer: |
    Inside `resources :photos do ... end`:
    - `member` route acts on a single member, includes `:id`: `member { get :preview }` -> `GET /photos/:id/preview` -> `photos#preview`.
    - `collection` route acts on the whole collection, no `:id`: `collection { get :search }` -> `GET /photos/search` -> `photos#search`.
    Successful API `create` should return `201 Created` (with `Location: /photos/:id` header and body representation), not `200 OK`.

- id: ror-controllers-routing-05
  answer: |
    Rails 8 `rails generate authentication` (built-in, no gem) scaffolds a minimal, auditable auth system using `has_secure_password` + `generates_token_for`: `User` with `password_digest`, `Current` attributes, `Session` model, `Authentication` concern, `Passwords` controller with reset flow, and `Authentication` concern for controllers. You own the code.
    Devise is a full-featured engine/gem providing modules (database_authenticatable, registerable, confirmable, lockable, omniauthable etc.) with zero code ownership but more magic, harder to customize, larger surface. Rails built-in is lighter, simpler, better for most new apps; Devise still wins for complex needs (Omniauth integrations, confirmable, etc.).

- id: ror-views-helpers-01
  answer: |
    `render partial: "posts/card", locals: { post: post, show_author: true }` or shorthand `render "posts/card", post: post, show_author: true`. Partial file `_card.html.erb`.
    `render @posts` is collection rendering sugar: Rails infers partial `posts/_post.html.erb` and renders it once per element, passing local `post` (singular of collection name). Equivalent to `render partial: "posts/post", collection: @posts, as: :post` with `spacer_template` support. It also handles empty collection via `render @posts || "No posts"`.

- id: ror-views-helpers-02
  answer: |
    `form_with` unifies `form_for` (model-bound) and `form_tag` (custom URL) into one helper: `form_with model: @post` or `form_with url: login_path`.
    In current Rails (7+ with Turbo) by default it is NOT `local: true` - it submits remotely via Turbo (`data-turbo="true"`), i.e. a fetch/XHR Turbo Stream request, not a full page reload. Pass `local: true` or `data: { turbo: false }` for a classic synchronous HTML form submission. Historically `form_with` defaulted to `remote: true` (UJS) before Turbo.

- id: ror-views-helpers-03
  answer: |
    Rails protects CSRF via `protect_from_forgery` and per-session authenticity token: server sets `csrf-token` meta tag and session, every non-GET form must submit matching token or `ActionController::InvalidAuthenticityToken` is raised.
    `form_with`/`form_for` automatically inserts a hidden `authenticity_token` input (and `csrf_meta_tags` in layout). For Turbo/fetch, it also reads the meta tag. No manual token handling needed unless you build raw `<form>` or JS fetch (then add `X-CSRF-Token` header).

- id: ror-views-helpers-04
  answer: |
    Fix belongs in the controller (or query object/service that loads `@posts`), not the view: `@posts = Post.includes(:author)`.
    The view's job is presentation; it shouldn't decide how data is fetched. Fixing in view (e.g. caching or separate queries) hides the data-access concern, is not reusable, violates MVC separation, and makes the N+1 easy to reintroduce from other views. Eager loading at the source fixes it for every use of that collection.

- id: ror-concerns-services-01
  answer: |
    `ActiveSupport::Concern` is a module mixin helper that manages dependencies correctly. Its `included do ... end` block is `class_eval`'d in the including class's context, so you can call class macros (`has_many`, `scope`, `validates`, `before_action`) cleanly without `self.included` boilerplate, and it handles `ClassMethods` module auto-extension and dependency ordering (`append_features`). Plain `include MyModule` would require manual `self.included(base)` and `base.extend`.

- id: ror-concerns-services-02
  answer: |
    A service object is a Plain Old Ruby Object (PORO) that encapsulates a single business operation/use case: e.g. `Billing::ChargeCustomer.new(user, amount).call` or `Posts::Publish.call(post)`.
    Extract when: logic spans multiple models, involves external services, is not CRUD, would bloat controller or model, needs isolation for testing, or is an orchestrated workflow (transaction + emails + jobs). Prefer service over adding fat model methods or complex controller actions; keeps SRP.

- id: ror-concerns-services-03
  answer: |
    Guideline: keep controllers thin (only params, auth, calling domain logic, rendering/redirecting) and push business logic into models so it's reusable and testable outside HTTP.
    Taken too far it creates bloated "god models" (e.g. `User` with 2000 lines: auth, billing, notifications, reporting), violating Single Responsibility Principle, making models hard to test and coupling unrelated concerns. Remedy: extract service objects, form objects, query objects, concerns/value objects instead of dumping everything into ActiveRecord models.

- id: ror-concerns-services-04
  answer: |
    Both remove logic from controller.
    Concern is a mixin (inheritance via `include`) - it shares reusable behavior HORIZONTALLY across multiple models/controllers (e.g. `Taggable`, `SoftDeletable`). Mixed in, its methods become instance methods of the host.
    Service object is a standalone object used via composition/delegation - you instantiate/call it explicitly for a single operation (`MyService.call(args)`). Not mixed in, no inheritance, no shared state. Use concerns for cross-cutting, repeatable model/controller behavior; use services for one-off business transactions/workflows.

- id: ror-caching-jobs-01
  answer: |
    Russian-doll (nested fragment) caching: cache a collection/parent fragment that contains cached child fragments. Example: `cache @posts` wrapping `cache post` per post.
    Using the record itself as key (`cache post` or `cache [post, "v1"]`) delegates to `post.cache_key` which is `posts/42-20240101120000` (id + `updated_at`/`cache_version`). When the record changes, `updated_at` bumps so key changes and cache auto-misses - no manual sweep. With `touch: true` on associations, parent key also busts when child changes, enabling doll invalidation.

- id: ror-caching-jobs-02
  answer: |
    `ActiveJob` is a unified abstraction/API over queueing backends (`perform_later`, `perform_now`, `set(wait: 2.hours)`, retries, deserialization via GlobalID). The queue adapter (`config.active_job.queue_adapter`) is the backend that actually executes jobs: `:sidekiq`, `:resque`, `:delayed_job`, `:good_job`, `:solid_queue`, etc.
    Default adapter is `:async` (in-memory thread pool, Rails 7+ `:async`) or `:inline` in test. It is NOT suitable for production: jobs live only in process memory, are lost on restart/deploy, have no persistence, no retries across processes, no monitoring, not shared across dynos.

- id: ror-caching-jobs-03
  answer: |
    Queues guarantee at-least-once delivery: jobs can be retried after failure, worker crash, deploy, or duplicate delivery, so same job may run twice. If not idempotent (e.g. charging, sending email, incrementing counter twice) you get double side effects.
    Make idempotent: guard with state checks (`return if already_sent?`), use idempotency keys/unique constraints (`find_or_create_by`, DB unique index), make operations naturally idempotent (`update status to X` not `increment`), wrap in transaction, use `sidekiq-unique-jobs` / `GoodJob` concurrency controls, and store processed `jid` in Redis/DB.

- id: ror-caching-jobs-04
  answer: |
    `MyJob.perform_later(args)` enqueues asynchronously via the queue adapter to run later in a background worker process. `MyJob.perform_now(args)` executes synchronously inline in the current process/thread, blocking until done (useful in console, tests, or urgent path).
    Pass record `id` (e.g. `MyJob.perform_later(user.id)`) not the record instance, because args are serialized to JSON/Redis. Passing the object serializes via GlobalID (stale snapshot) and can fail if record is unsaved, large, or changed before job runs. Lookup fresh with `User.find(id)` inside `perform` to get current state and avoid stale/deserialization issues.
```
