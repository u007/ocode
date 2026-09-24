- id: ror-activerecord-01
  answer: |
    In a modern Rails application, `belongs_to` is required by default. If the parent is missing, validation fails: `save` returns `false`, while `save!` raises `ActiveRecord::RecordInvalid`. Make it optional with `belongs_to :parent, optional: true`. A database foreign key or `NOT NULL` constraint may still reject nil even when the model validation is disabled.

- id: ror-activerecord-02
  answer: |
    `has_many :through` represents an association through another association or join model and lets you work with join attributes, validations, callbacks, and additional associations. `has_and_belongs_to_many` is a simpler direct many-to-many association without an explicit join model. Prefer `has_many :through` when the relationship has data or behavior; use HABTM only for simple, symmetric relationships.

- id: ror-activerecord-03
  answer: |
    `dependent: :destroy` destroys each associated record, running validations and callbacks, but can be slow.
    `dependent: :delete_all` issues a direct SQL delete, normally without callbacks or validations, and does not nullify foreign keys.
    `dependent: :nullify` keeps associated records but sets their foreign keys to `NULL`, typically with a direct update that skips callbacks.

- id: ror-activerecord-04
  answer: |
    The validation performs a query and then a later insert, so two concurrent requests can both observe that the email is available and then insert it. Add a database unique constraint or unique index, such as `add_index :users, :email, unique: true`, and handle `ActiveRecord::RecordNotUnique`. The model validation is still useful for immediate user feedback.

- id: ror-activerecord-05
  answer: |
    `normalizes` declares an attribute transformation as part of Active Record’s attribute handling. For example, `normalizes :email, with: ->(email) { email.strip.downcase }` applies canonicalization before validation, so validation, persistence, and supported comparisons use the normalized value. A `before_save` callback runs after validation, can be bypassed by methods such as `update_column`, and hides data normalization as a side effect rather than defining the attribute’s normal form.

- id: ror-querying-01
  answer: |
    `includes(:author)` declares that the association should be loaded; it normally uses a separate query, but can use a join when query conditions or references require it. `preload(:author)` explicitly forces separate queries, avoiding a join. `eager_load(:author)` explicitly forces a `LEFT OUTER JOIN` and loads the records in one query. In general, use `includes` or `preload` for independent associations and `eager_load` when joined columns or join conditions are needed.

- id: ror-querying-02
  answer: |
    This is the N+1 query problem: one query loads the posts, followed by one author query for each post. Preload the association before iterating, for example `Post.includes(:author).each` or `Post.preload(:author).each`, so authors are loaded in a separate batched query.

- id: ror-querying-03
  answer: |
    `find` looks up records by primary key and returns a record (or an array when given multiple keys); it raises `ActiveRecord::RecordNotFound` when no match exists. `find_by` returns the first matching record or `nil`. `where` returns an `ActiveRecord::Relation`, including an empty relation when there are no matches, so it can be chained. `find_by!` is the raising variant of `find_by`.

- id: ror-querying-04
  answer: |
    `pluck(:email)` selects only the email column and returns scalar values without instantiating full Active Record objects. `User.all.map(&:email)` loads every column, creates an object for every user, and then maps in Ruby, using more memory and database bandwidth.

- id: ror-callbacks-transactions-01
  answer: |
    `after_save` runs inside the database transaction, before the transaction commits. An external email or API call could therefore succeed even if the transaction later rolls back. `after_commit` runs only after a successful commit, so the side effect corresponds to durable data. Because an exception in `after_commit` cannot undo the committed database change, production systems often enqueue an idempotent job or use an outbox instead of calling a fragile external service directly.

- id: ror-callbacks-transactions-02
  answer: |
    `save` returns `true` or `false`; a validation failure normally returns `false`. `save!` performs the same operation but raises `ActiveRecord::RecordInvalid` on validation failure, and other persistence errors also propagate. `create` returns the model instance, which is unsaved if creation fails, whereas `create!` raises instead of returning an unsaved instance.

- id: ror-callbacks-transactions-03
  answer: |
    Use `raise ActiveRecord::Rollback` inside the transaction block. The transaction manager catches this exception, rolls back the current transaction or savepoint, and does not let it propagate outside the block. Other exceptions normally roll back the transaction but are re-raised. A bare `return` does not perform a rollback.

- id: ror-callbacks-transactions-04
  answer: |
    For a newly created record, the relevant order is `before_validation`, `after_validation`, `before_save`, `before_create`, the SQL `INSERT`, `after_create`, `after_save`, and then `after_commit` after the transaction commits. `after_rollback` runs instead if the transaction rolls back. Heavy callbacks are a design smell because they hide side effects, couple model persistence to slow work or external systems, lengthen transactions, make retries and tests difficult, and can leave database and external state inconsistent.

- id: ror-migrations-schema-01
  answer: |
    A single `change` method is suitable when every operation can be automatically reversed by Rails, such as many `create_table`, `add_column`, `add_reference`, and index operations. Use separate `up` and `down` methods when an operation is not reliably reversible, especially arbitrary SQL or data transformations. A direction-sensitive operation can also be made explicit with `reversible`.

- id: ror-migrations-schema-02
  answer: |
    `schema.rb` is a Ruby DSL representation of the schema and is generally portable and convenient. `structure.sql` is a SQL dump that can preserve database-specific objects and behavior that the Ruby format cannot represent, such as extensions, views, functions, triggers, custom types, and collations. Switch to `structure.sql` when using database features unsupported by `schema.rb`, accepting that dumps and loading can be slower and more database-specific.

- id: ror-migrations-schema-03
  answer: |
    Adding a non-null column with a default can lock or rewrite a large table, depending on the database and version. Use an expand-and-contract approach: add a compatible nullable column, backfill it in small batches, validate the data, add the constraint, switch application code, and remove the old structure later. Adding an index, especially a unique index, can block writes; create it online or concurrently with an adapter-specific option such as `algorithm: :concurrently`, outside a transaction, and clean up duplicate data first.

- id: ror-migrations-schema-04
  answer: |
    `add_reference :comments, :post, foreign_key: true` conceptually adds a `post_id` column whose type matches the primary key of `posts`, normally adds an index on `post_id`, and adds a foreign-key constraint referencing `posts`. The foreign key protects referential integrity, while the index makes joins and foreign-key checks efficient; databases such as PostgreSQL do not automatically create an index on the referencing side of every foreign key.

- id: ror-controllers-routing-01
  answer: |
    `resources :photos` creates seven conventional RESTful routes: `GET /photos` to `index`, `GET /photos/new` to `new`, `POST /photos` to `create`, `GET /photos/:id` to `show`, `GET /photos/:id/edit` to `edit`, `PATCH` or `PUT /photos/:id` to `update`, and `DELETE /photos/:id` to `destroy`. It also creates helpers such as `photos_path` and `photo_path`.

- id: ror-controllers-routing-02
  answer: |
    Strong parameters prevent mass assignment by allowing only explicitly approved attributes from request data. `require(:user)` retrieves the nested `user` parameter and raises `ActionController::ParameterMissing` if it is absent. `permit(:name, :email)` returns only those whitelisted keys; nested attributes need their own nested permit rules, such as `permit(tags: [], address_attributes: [:city])`.

- id: ror-controllers-routing-03
  answer: |
    `before_action` registers a callback that runs before the controller action, optionally restricted with `only` or `except`. If it renders or redirects, the callback chain is halted and the action is not run. A bare `return` from the callback only exits that callback and does not halt the action; use an explicit response or an appropriate callback halt such as `throw :abort`.

- id: ror-controllers-routing-04
  answer: |
    A `member` route acts on one identified resource and includes `:id`, for example `member { patch :publish }` producing `/photos/:id/publish`. A `collection` route acts on the collection and has no `:id`, for example `collection { get :search }` producing `/photos/search`. A successful API create should normally return `201 Created`, commonly with a representation and a `Location` header.

- id: ror-controllers-routing-05
  answer: |
    `bin/rails generate authentication` scaffolds a minimal, Rails-owned authentication baseline, typically including a user/password model using `has_secure_password`, session handling, authentication concerns, routes, views, and tests. It is intended as a secure starting point that the application customizes. Devise is a third-party, batteries-included framework with conventions and optional modules such as confirmable, recoverable, lockable, trackable, and OmniAuth integrations, so it offers more out of the box but also more dependency and complexity.

- id: ror-views-helpers-01
  answer: |
    Render a partial with locals using `render "posts/post", post: @post` or `render partial: "posts/post", locals: { post: @post }`. The partial can use the local `post`. `render @posts` treats the collection as objects to render, normally using each object’s `to_partial_path`—for example `_post`—and supplies an inferred local such as `post`.

- id: ror-views-helpers-02
  answer: |
    `form_with` provides one API for form generation, combining conveniences formerly spread across `form_for` and `form_tag`, including model or URL inference, scopes, nested attributes, method overrides, and authenticity-token handling. By default it generates a local, standard HTML form, not a remote AJAX form. `local: false` requests the older remote behavior when the relevant JavaScript integration is configured; Turbo may still intercept ordinary forms.

- id: ror-views-helpers-03
  answer: |
    Rails protects against CSRF by issuing a session-bound authenticity token, embedding it in forms, and verifying it on state-changing requests, normally through `protect_from_forgery`. An invalid or missing token causes the request to be rejected. `form_with` automatically includes the hidden authenticity-token field and the appropriate form method behavior, so developers should not manually add the token.

- id: ror-views-helpers-04
  answer: |
    Fix the query at the data-access boundary, usually in the controller, model scope, or service: for example, load `@posts = Post.includes(:author)`. The view should render already-loaded data rather than decide how to optimize database access. Keeping the fix out of the view preserves separation of concerns, avoids embedding query strategy in presentation code, and makes the same association behavior available consistently to JSON responses and other consumers.

- id: ror-concerns-services-01
  answer: |
    `ActiveSupport::Concern` is a standard-library module abstraction for behavior shared by multiple classes. Its `included do ... end` block is evaluated in the context of the including class, allowing clean class-level DSL such as `has_many`, `validates`, callbacks, and associations. It also provides convenient support for class methods and dependencies. A plain module can do this with an `included` callback, but the concern API makes the intent and behavior clearer.

- id: ror-concerns-services-02
  answer: |
    A service object is a plain Ruby object that represents a business operation or use case and coordinates models, repositories, queues, and external services. It is useful when an operation spans multiple records or systems, has meaningful transaction boundaries, contains branching business rules, or needs independent testing and reuse. Keep simple CRUD and narrowly scoped behavior in the model or controller rather than extracting a service for every method.

- id: ror-concerns-services-03
  answer: |
    The guideline keeps controllers focused on HTTP concerns—authorization, parameter handling, orchestration, and responses—while models contain domain rules, invariants, and reusable business behavior. Taken too far as “put all logic in models,” it creates bloated god models that mix persistence, workflows, external integrations, and unrelated responsibilities. Services can provide a better boundary for multi-system or use-case orchestration.

- id: ror-concerns-services-04
  answer: |
    A concern is mixed into a class and supplies reusable behavior or configuration; it is passive until included. A service object is instantiated for a particular operation, accepts inputs, coordinates collaborators, and produces a result. Concerns answer “what capabilities should this class have?” while services answer “what business operation should be performed?”

- id: ror-caching-jobs-01
  answer: |
    Russian-doll caching nests fragment caches, allowing an inner, frequently reused fragment to be cached independently and reused inside other cached fragments. Passing the record itself, such as `cache @post do`, uses its `cache_key_with_version`, typically combining class and id with `cache_version` or an updated timestamp. Updating the record therefore produces a new key automatically; using only `@post.id` would not. If independently changing nested records are embedded in an outer fragment, their versions must also be included or the outer fragment must be invalidated or touched.

- id: ror-caching-jobs-02
  answer: |
    Active Job provides a common Ruby interface for background work, including serialization, queueing, retries, scheduling, priority, logging, and callbacks. A queue adapter is the pluggable backend that stores and executes those jobs, such as Solid Queue, Sidekiq, Resque, or Delayed Job. The framework’s out-of-the-box `async` adapter is in-process and not durable, so it is generally unsuitable for production; configure a persistent, scalable adapter appropriate to the deployment.

- id: ror-caching-jobs-03
  answer: |
    Retries, worker crashes, visibility timeouts, and duplicate enqueueing can cause the same job to run more than once. An idempotent job produces the same final business result when repeated, rather than duplicating side effects. Give the operation a unique idempotency key, record it with a unique database constraint, use atomic conditional updates or upserts, and use provider-side idempotency keys where available.

- id: ror-caching-jobs-04
  answer: |
    `perform_later` creates the job and hands it to the configured queue adapter for asynchronous execution; `perform_now` executes it immediately in the current process and is useful in tests or for deliberately small synchronous work. IDs are simple serializable arguments and make it explicit that the job should load the current record, avoiding stale in-memory objects and unnecessary coupling to a serialized object. Rails does support passing Active Record records: it normally serializes them through Global ID, so using an ID is a deliberate choice rather than a requirement.
