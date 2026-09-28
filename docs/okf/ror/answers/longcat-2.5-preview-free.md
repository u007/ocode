- id: ror-activerecord-01
  answer: |
    In Rails 5+, belongs_to associations are required by default. If the parent record is missing, the model will fail validation with an error on the foreign key. To make it optional, pass `optional: true` to the association declaration.

- id: ror-activerecord-02
  answer: |
    has_many :through uses an explicit join model (an ActiveRecord model with its own table, validations, and callbacks), allowing you to add attributes and logic to the join. has_and_belongs_to_many (HABTM) uses a simple join table with no model, no validations, and no callbacks. You should reach for has_many :through in almost all cases because it is more flexible and explicit; HABTM is only suitable for very simple join tables with no additional data.

- id: ror-activerecord-03
  answer: |
    dependent: :destroy loads each associated record and calls destroy on it, running all callbacks. dependent: :delete_all issues a single SQL DELETE against the table without instantiating records or running callbacks. dependent: :nullify sets the foreign key column on the associated records to NULL, leaving the records in the database but disassociating them.

- id: ror-activerecord-04
  answer: |
    A model-level uniqueness validation is subject to a race condition: two concurrent requests can both pass validation and then both insert, resulting in duplicate rows. You must add a unique index at the database level (e.g., add_index :users, :email, unique: true) to enforce uniqueness atomically.

- id: ror-activerecord-05
  answer: |
    The normalizes macro (introduced in Rails 7.1) declares normalization of attribute values before validation runs (e.g., normalizes :email, with: ->(email) { email.strip.downcase }). It is better than doing the same work in a before_save callback because it runs before validation, so validations and database writes see the normalized value, and it is more declarative and composable.

- id: ror-querying-01
  answer: |
    includes chooses between preload and eager_load depending on the query: it uses preload (separate queries) by default, but switches to eager_load (a single LEFT OUTER JOIN) if you reference the included association in a where or order clause. preload always issues separate queries (one per association) and cannot filter on the joined table. eager_load always uses a single LEFT OUTER JOIN query, allowing you to filter and order on the joined table.

- id: ror-querying-02
  answer: |
    This is called the N+1 query problem. You fix it by eager loading the association using includes, preload, or eager_load (e.g., Post.includes(:author).each { ... }), which loads all associated records in one or two queries instead of one query per record.

- id: ror-querying-03
  answer: |
    find returns a single record and raises ActiveRecord::RecordNotFound if no record matches. find_by returns a single record (the first match) or nil if nothing matches. where returns an ActiveRecord::Relation (a chainable query object) that may be empty; it does not raise when nothing matches.

- id: ror-querying-04
  answer: |
    User.pluck(:email) selects only the email column directly from the database and returns an array of values, without instantiating full ActiveRecord objects. User.all.map(&:email) loads every column of every row into full ActiveRecord objects and then extracts the email in Ruby, which is far more memory- and CPU-intensive.

- id: ror-callbacks-transactions-01
  answer: |
    after_commit runs only after the database transaction has successfully committed. If you enqueue a job or call an external API in after_save, the transaction may still roll back (e.g., due to a later callback raising), causing the job to fire for a record that was never persisted. after_commit ensures the side effect only happens when the data is durably saved.

- id: ror-callbacks-transactions-02
  answer: |
    save returns true on success and false on failure (validation errors), without raising. save! raises ActiveRecord::RecordInvalid on failure. Similarly, create returns the (unsaved) record on failure, while create! raises an exception.

- id: ror-callbacks-transactions-03
  answer: |
    You force a rollback by raising an exception inside the transaction block. Raising ActiveRecord::Rollback is special because it is caught by the transaction block and not re-raised to the caller — the transaction rolls back silently and execution continues after the block. Any other exception rolls back and propagates to the caller.

- id: ror-callbacks-transactions-04
  answer: |
    The create callback order is: before_validation → after_validation → before_save → before_create → (INSERT) → after_create → after_save → after_commit. Heavy callbacks are a design smell because they make models difficult to test in isolation, can trigger unexpected side effects (sending emails, API calls) during tests or unrelated operations, and obscure the true cost of saving a record.

- id: ror-migrations-schema-01
  answer: |
    A migration can use a single change method when every operation is reversible (Rails knows how to invert it automatically — e.g., add_column, add_index, create_table). You need separate up/down methods when the operation is irreversible or cannot be automatically inverted, such as remove_column without specifying the type, execute of raw SQL, or data migrations that cannot be undone.

- id: ror-migrations-schema-02
  answer: |
    schema.rb is a Ruby DSL representation of your database schema, generated by Rails. structure.sql is a raw SQL dump of the database structure (via the database's own tools like pg_dump). You would switch to structure.sql when you use database features that schema.rb cannot represent (e.g., advanced PostgreSQL types, triggers, stored procedures, or database-specific extensions).

- id: ror-migrations-schema-03
  answer: |
    Two dangerous operations on large tables: (1) add_column with a default value — in older MySQL and some PostgreSQL versions this rewrites the entire table and locks it; safe approach is to add the column without a default, then backfill in batches, then set the default. (2) add_index — in PostgreSQL it locks writes; safe approach is to use algorithm: :concurrently (PostgreSQL) or perform the index addition in a separate migration outside a transaction.

- id: ror-migrations-schema-04
  answer: |
    It generates a post_id column (integer/bigint) on the comments table, an index on that column, and a foreign key constraint referencing the posts table. You add a foreign key to enforce referential integrity at the database level (preventing orphaned records). You add an index because foreign key columns are frequently used in lookups and joins, and without an index those queries require full table scans.

- id: ror-controllers-routing-01
  answer: |
    resources :photos creates seven RESTful routes mapping to the PhotosController actions: GET /photos → index, GET /photos/:id → show, GET /photos/new → new, POST /photos → create, GET /photos/:id/edit → edit, PATCH/PUT /photos/:id → update, DELETE /photos/:id → destroy.

- id: ror-controllers-routing-02
  answer: |
    Strong Parameters solve the mass-assignment vulnerability, where an attacker can submit arbitrary attributes (e.g., admin: true) via form params. require(:user) ensures the params hash contains a :user key (raises if missing). permit(:name, :email) whitelists only the :name and :email attributes, stripping all others from the params before they reach the model.

- id: ror-controllers-routing-03
  answer: |
    before_action is a controller callback that runs before the specified action(s). If a before_action renders or redirects, the action itself is halted and will not execute — this is commonly used for authentication and authorization checks.

- id: ror-controllers-routing-04
  answer: |
    A member route operates on a specific, identified record and includes the record's id in the URL (e.g., POST /photos/:id/publish). A collection route operates on the collection as a whole and does not include an id (e.g., GET /photos/search). A successful API create should return HTTP 201 Created.

- id: ror-controllers-routing-05
  answer: |
    Rails 8's built-in authentication generator (bin/rails generate authentication) creates a User model with password hashing (has_secure_password), a SessionsController for login/logout, and the necessary views and routes. It differs from Devise in being simpler, more transparent, fully customizable (you own the code), and having no external dependency — whereas Devise is a full-featured gem with many modules but less visibility into its internals.

- id: ror-views-helpers-01
  answer: |
    You render a partial with local variables using render partial: 'post', locals: { post: my_post }. When you call render @posts (a collection), Rails infers the partial name from the model (e.g., _post.html.erb), renders it once for each element, and makes each element available as a local variable named after the partial (e.g., post).

- id: ror-views-helpers-02
  answer: |
    form_with unifies form_for (model-backed forms) and form_tag (generic forms) into a single helper. In current Rails (6.1+), form_with generates a local (standard, non-remote) form by default — it does not use AJAX/remote submission unless you explicitly pass local: false.

- id: ror-views-helpers-03
  answer: |
    Rails protects against CSRF by requiring a unique authenticity token for every non-GET request. The token is embedded in the form and verified by the server on submission. form_with automatically includes this hidden authenticity token field in every form it generates, so you do not need to add it manually.

- id: ror-views-helpers-04
  answer: |
    The fix belongs in the controller (or query layer), where you should eager load the association (e.g., @posts = Post.includes(:author)). You should not fix it in the view because views should not be responsible for query optimization — putting includes in the view scatters query logic, makes it hard to reuse, and violates separation of concerns.

- id: ror-concerns-services-01
  answer: |
    ActiveSupport::Concern is a module that, when extended, provides a clean way to package both instance methods and class methods along with setup code. Its included do ... end block lets you run code in the context of the including class (e.g., calling class macros like belongs_to, validates, or scope) when the module is included, which a plain module mixin cannot do cleanly because it has no hook into the class body at include time.

- id: ror-concerns-services-02
  answer: |
    A service object is a Plain Old Ruby Object (PORO) that encapsulates a single business operation or transaction, typically with a simple interface like .call or .run. You should extract one when the logic involves multiple models, is too complex for a single model method, needs to be reused across controllers, or when you want to isolate and test business logic independently of the web layer.

- id: ror-concerns-services-03
  answer: |
    The "skinny controller, fat model" guideline says controllers should be thin (just parsing params, calling model logic, and rendering) while models hold business logic. Taken too far, it leads to "god models" — massive models with hundreds of methods, mixed responsibilities, and tight coupling to many other models, making them difficult to test, maintain, and understand.

- id: ror-concerns-services-04
  answer: |
    A concern is mixed into a class (via include) and adds behavior directly to that class — it is shared, reusable functionality that becomes part of the class's own methods. A service object is a separate object that you instantiate and call — it encapsulates a specific operation and is invoked explicitly, keeping its state and logic separate from any model or controller.

- id: ror-caching-jobs-01
  answer: |
    Russian-doll caching is a pattern where cached fragments are nested inside other cached fragments. When an inner fragment changes, its cache key changes, which busts the outer fragment's cache too, cascading invalidation up the nesting hierarchy. Using the record itself in the cache key (e.g., cache post) makes invalidation automatic because Rails generates a cache key from the record's updated_at timestamp — when the record is updated, the timestamp changes, the cache key changes, and the old cached fragment is never served again.

- id: ror-caching-jobs-02
  answer: |
    ActiveJob provides a unified interface for defining and enqueueing background jobs, abstracting away the specifics of the queueing backend. The queue adapter determines which backend actually processes the jobs (e.g., Sidekiq, Resque, GoodJob). The default adapter is :async, which runs jobs in an in-memory thread pool — it is not suitable for production because jobs are lost on restart and it does not scale across multiple processes or servers.

- id: ror-caching-jobs-03
  answer: |
    Jobs must be idempotent because background job delivery is at-least-once: a job may be retried after a timeout, a worker crash, or a network error, causing the same job to execute multiple times. To make a job idempotent, check whether the work has already been done before performing it, use database unique constraints to prevent duplicate records, and design the job so that running it multiple times has the same effect as running it once.

- id: ror-caching-jobs-04
  answer: |
    MyJob.perform_later enqueues the job to be processed asynchronously by a queue worker. MyJob.perform_now executes the job immediately in the current process, bypassing the queue. You pass a record's id rather than the record itself because the record may not be serializable (e.g., to JSON), the record may change between enqueue and execution (so you want to load the fresh version), and passing a small id is more efficient than serializing an entire object graph.
