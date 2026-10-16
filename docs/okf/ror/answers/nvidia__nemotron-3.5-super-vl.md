# nvidia/nemotron-3.5-super-vl — closed-book answers for ror (openrouter, 2026-10-16); answerer saw only _prompts/ror.md
- id: ror-activerecord-01
  answer: |
    Yes. Since Rails 5, belongs_to is required by default (optional: false). If the parent is missing or its foreign key points to a nonexistent row, the record is invalid ("must exist") and will not save. To allow a missing parent, declare belongs_to :post, optional: true.
- id: ror-activerecord-02
  answer: |
    has_many :through uses a join model backed by its own table, so the association can carry its own data, validations, and callbacks. has_and_belongs_to_many uses a bare join table with no model class. Reach for has_many :through in most cases; use HABTM only for simple, legacy-style join tables with no extra attributes.
- id: ror-activerecord-03
  answer: |
    dependent: :destroy destroys each associated record individually, so callbacks run and dependent objects are cleaned up. dependent: :delete_all issues a single DELETE SQL statement and skips all callbacks. dependent: :nullify sets the foreign key to NULL on the associated records without destroying them.
- id: ror-activerecord-04
  answer: |
    It is not enough because of a race condition: two requests can both pass the uniqueness validation and then both insert, since validation happens in the application, not the database. You must add a unique database index (add_index :table, :email, unique: true) so the database itself enforces uniqueness.
- id: ror-activerecord-05
  answer: |
    normalizes (Rails 7.1) declares how an attribute value should be normalized (e.g., downcasing/stripping an email) before validation and before it is written to the database. It is better than a before_save callback because it applies consistently on assignment and validation, is idempotent, keeps the callback stack clean, and makes the normalization explicit and testable instead of hidden in a save hook.
- id: ror-querying-01
  answer: |
    preload always loads the association in a separate query and cannot be used with conditions on the associated table. eager_load loads association and parent in a single LEFT OUTER JOIN, so you can filter/order on the association, but joining multiple collections can produce a cartesian product. includes is the smart default: it uses preload unless you reference the associated table in conditions, in which case it uses eager_load. All three avoid the N+1 problem.
- id: ror-querying-02
  answer: |
    It is called the N+1 query problem: one query for the posts plus one author query per post. Fix it by eager loading the association before iterating, e.g., Post.includes(:author).each { ... }, so the authors are loaded in one additional query.
- id: ror-querying-03
  answer: |
    find(id) returns a record and raises ActiveRecord::RecordNotFound when nothing matches. find_by(conditions) returns the first matching record or nil. where(conditions) returns an ActiveRecord::Relation (chainable and lazy) and returns an empty relation when nothing matches; it never raises.
- id: ror-querying-04
  answer: |
    User.pluck(:email) issues SELECT email FROM users and returns an array of raw values without instantiating ActiveRecord objects. User.all.map(&:email) instantiates every full User model just to throw almost all of it away, using far more memory and time. pluck is faster and lighter when you only need specific columns.
- id: ror-callbacks-transactions-01
  answer: |
    after_commit runs only after the database transaction has committed, so the record is durably saved; that makes it safe to enqueue emails or call external APIs knowing the work will not need to be undone. after_save runs inside the transaction, so if the transaction later rolls back the side effect (already-sent email, already-made API call) has already happened and cannot be rolled back.
- id: ror-callbacks-transactions-02
  answer: |
    save returns false on validation failure and does not raise; save! raises ActiveRecord::RecordInvalid on failure. Likewise create returns a possibly-invalid record, while create! raises. Use the bang versions when you want failures to surface as exceptions.
- id: ror-callbacks-transactions-03
  answer: |
    Force a rollback by raising an exception inside the transaction block. Raising ActiveRecord::Rollback is special: it triggers a rollback but is swallowed by the transaction and not propagated to the caller, so it rolls back without surfacing as an unhandled exception. Any other exception still rolls back and is re-raised.
- id: ror-callbacks-transactions-04
  answer: |
    For a new record: before_validation, validation, before_save, before_create, after_create, after_save, then after_commit/after_rollback once the transaction commits (with around_ variants wrapping the corresponding stages). Heavy callbacks are a design smell because they hide side effects, blur transaction boundaries, slow tests, and couple unrelated concerns into the model.
- id: ror-migrations-schema-01
  answer: |
    A single change method works for reversible operations Rails knows how to invert (create_table, add_column, add_index, add_reference, rename_column, etc.). You need separate up/down methods (or change with reversible) for irreversible operations such as executing arbitrary SQL, changing a column type without a known reverse, or data migrations.
- id: ror-migrations-schema-02
  answer: |
    schema.rb is a Ruby-format dump of the schema: fast, database-agnostic, and diff-friendly, but it loses database-specific features. structure.sql is a native SQL dump (e.g., pg_dump) that preserves everything, including foreign keys, triggers, functions, and enum types. Switch to structure.sql (config.active_record.schema_format = :sql) when you use database-specific features schema.rb cannot represent.
- id: ror-migrations-schema-03
  answer: |
    Adding a column with a default value and adding a regular index are dangerous on large production tables because they lock/rewrite the table. Do them safely: add the column without a default and backfill in batches, then set the default; add the index concurrently (algorithm: :concurrently with disable_ddl_transaction! on PostgreSQL). Similarly, use tools like gh-ost or pt-online-schema-change for large table changes.
- id: ror-migrations-schema-04
  answer: |
    It generates a post_id column on comments, a foreign key constraint referencing posts(id), and an index on post_id. The foreign key enforces referential integrity (no orphan comments); the index speeds up lookups such as "all comments for this post" and helps avoid locking issues when checking the foreign key.
- id: ror-controllers-routing-01
  answer: |
    resources :photos creates the seven RESTful routes: GET /photos (index), GET /photos/new (new), POST /photos (create), GET /photos/:id (show), GET /photos/:id/edit (edit), PATCH/PUT /photos/:id (update), DELETE /photos/:id (destroy). They map to PhotosController#index, #new, #create, #show, #edit, #update, and #destroy respectively.
- id: ror-controllers-routing-02
  answer: |
    Strong Parameters prevent mass-assignment attacks by requiring an explicit allow-list before attributes reach the model. require(:user) raises ActionController::ParameterMissing if the :user key is absent. permit(:name, :email) returns a new object containing only those permitted scalar keys, stripping everything else.
- id: ror-controllers-routing-03
  answer: |
    before_action registers a method to run before the controller action. If the before_action renders or redirects (and returns/halts the chain) or throws :abort, the action is not executed. Note that render/redirect_to alone do not halt execution, so you typically need an explicit return after them.
- id: ror-controllers-routing-04
  answer: |
    member routes act on a single record and include an id (/photos/:id/preview). collection routes act on the whole collection with no id (/photos/export). A successful API create should return 201 Created.
- id: ror-controllers-routing-05
  answer: |
    Rails 8's built-in authentication generator (bin/rails generate authentication) scaffolds session-based authentication: a user model using has_secure_password, sessions controller for sign in/out, current-session/current-user helpers (Current.session), and a password-reset mailer flow, with no external gems. Devise is a full-featured engine gem with many optional modules (confirmable, trackable, lockable, OmniAuth, etc.); it is more powerful and battle-tested but heavier and more magical, whereas Rails 8's generator is minimal and you bring your own extensions for advanced features.
- id: ror-views-helpers-01
  answer: |
    render "posts/post", locals: { post: @post } (or render partial: "posts/post", locals: { post: @post }). render @posts renders a collection partial: Rails guesses the partial name from the model class (e.g., _post.html.erb), renders it once per element with a local variable named after the model, and gracefully handles empty collections.
- id: ror-views-helpers-02
  answer: |
    form_with unifies form_for (model-backed) and form_tag (URL-backed) into a single helper. In current Rails it generates a local (standard, non-AJAX) form by default (local: true); use local: false to make it remote.
- id: ror-views-helpers-03
  answer: |
    Rails protects against CSRF by issuing a per-session authenticity_token and verifying it on state-changing requests (POST/PUT/PATCH/DELETE). form_with automatically inserts a hidden authenticity_token field so the posted form passes verification.
- id: ror-views-helpers-04
  answer: |
    The fix belongs in the controller or query layer: eager load the association before rendering, e.g., @posts = Post.includes(:author).all. Fixing it in the view is wrong because views should not trigger database queries; data-access logic in views is hard to test, scatters concerns across partials and layouts, and is easy to miss, so the N+1 keeps reappearing.
- id: ror-concerns-services-01
  answer: |
    ActiveSupport::Concern is a mixin helper that makes modules behave more like classes. Its included do ... end block runs in the context of the including class, so you can define associations, validations, callbacks, and class-level DSL inside it as if written directly in the class. It also automatically handles the ClassMethods module/extend boilerplate that a plain mixin requires you to wire up manually with self.included.
- id: ror-concerns-services-02
  answer: |
    A service object is a plain Ruby object (PORO) that encapsulates one business action or transaction, typically with a single public method such as call or perform. Extract one when logic spans multiple models, calls external services, or is too complex for the controller or model — for example "place an order" involving inventory, payment, and email — instead of bloating a model or controller.
- id: ror-concerns-services-03
  answer: |
    "Skinny controller, fat model" means controllers should only handle HTTP concerns (params, session, rendering, redirection) while business logic lives in models. Taken too far, the failure mode is bloated "god models" full of unrelated logic, callbacks everywhere, hard-to-test classes, and models becoming dumping grounds — fat models are just as bad as fat controllers.
- id: ror-concerns-services-04
  answer: |
    A concern is a module mixed into an object (model, controller, mailer) to extend that object's own behavior and share code across classes. A service object is instantiated and invoked to perform a discrete unit of work. Concerns modify how an object behaves; services are explicit, procedural units of work you call.
- id: ror-caching-jobs-01
  answer: |
    Russian-doll caching nests fragment caches inside one another (e.g., cache a post fragment, and inside it cache each comment fragment). Using the record itself in the cache key makes the key include the record's id and updated_at (via cache_key/touch), so when a record changes its key changes, the nested fragment expires, and the parent fragment's content changes too — invalidation propagates automatically outward.
- id: ror-caching-jobs-02
  answer: |
    ActiveJob is a framework for declaring and enqueuing background jobs (perform_later, retry logic, GlobalID serialization). The queue adapter is the backend bridge that actually runs the jobs (Sidekiq, GoodJob, Resque, Delayed Job, etc.). The default async adapter is an in-process thread pool fine for development, but it is not suitable for production because jobs are not persisted and are lost when the process restarts.
- id: ror-caching-jobs-03
  answer: |
    Jobs can be delivered at least once (retries after transient failure, duplicate enqueues), so the same job may run more than once with the same arguments. Jobs must be idempotent so running twice causes no harm. Make them idempotent by checking state before acting (status flags, "already processed" records), using unique constraints or idempotency keys, and designing each step (create-or-update, find_or_create_by) to be safely repeatable.
- id: ror-caching-jobs-04
  answer: |
    MyJob.perform_later enqueues the job to run asynchronously through the queue adapter; MyJob.perform_now runs the job immediately inline. Pass the record's id rather than the record object so the job fetches a fresh record at execution time instead of acting on stale data serialized at enqueue time, and to keep the job payload small and serializable.
