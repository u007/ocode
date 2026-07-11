# Ror — Kaizen blind answer sheet (questions only)

> **CLOSED-BOOK.** Answer every question from your own knowledge alone. You MUST
> NOT open, search, or otherwise access the Kaizen corpus — `questions.yaml`,
> `questions.md`, `scores/`, `derived/`, `meta.yaml`, or any file in this repo —
> nor look the answers up online. Doing so invalidates the evaluation.
>
> Answer each question **independently** (treat every item as a fresh context —
> no memory of earlier answers). If you are unsure, say so; do not guess to look
> complete. This measures what you actually know, not what you can retrieve.
>
> **Return format** — one YAML record per question so the grader can map answers
> back by id:
>
> ```yaml
> - id: <question-id>
>   answer: |
>     <your answer>
> ```

Total questions: 34

---

### ror-activerecord-01

In modern Rails, is a `belongs_to` association required by default? What happens if the parent is missing, and how do you make it optional?

### ror-activerecord-02

What is the difference between `has_many :through` and `has_and_belongs_to_many`, and which should you reach for?

### ror-activerecord-03

On a `has_many`, what is the difference between `dependent: :destroy`, `dependent: :delete_all`, and `dependent: :nullify`?

### ror-activerecord-04

`validates :email, uniqueness: true` is on your model. Why is it not enough to guarantee uniqueness, and what must you add?

### ror-activerecord-05

What does the `normalizes` macro (Rails 7.1) do, and why is it better than doing the same work in a `before_save` callback?

### ror-querying-01

Explain the difference between `includes`, `preload`, and `eager_load` for loading an association, and how each hits the database.

### ror-querying-02

`@posts.each { |p| puts p.author.name }` fires one query per post for the author. What is this called and how do you fix it?

### ror-querying-03

Contrast `find`, `find_by`, and `where`. Which return a record vs a relation, and what does each do when nothing matches?

### ror-querying-04

You only need the email column of every user. Why is `User.pluck(:email)` better than `User.all.map(&:email)`?

### ror-callbacks-transactions-01

You need to enqueue an email / call an external API after a record is saved. Why is `after_commit` the right hook and `after_save` the wrong one?

### ror-callbacks-transactions-02

What is the difference between `save` and `save!` (and `create` vs `create!`)?

### ror-callbacks-transactions-03

Inside `ActiveRecord::Base.transaction do ... end`, how do you force a rollback, and what is special about raising `ActiveRecord::Rollback`?

### ror-callbacks-transactions-04

State the order in which the create callbacks fire when you call `record.save` on a new record, and why heavy callbacks are a design smell.

### ror-migrations-schema-01

When can a migration use a single `change` method, and when do you need separate `up`/`down` methods?

### ror-migrations-schema-02

What is the difference between `schema.rb` and `structure.sql`, and when would you switch to `structure.sql`?

### ror-migrations-schema-03

Name two migration operations that are dangerous on a large table in production and how to do them safely.

### ror-migrations-schema-04

What does `add_reference :comments, :post, foreign_key: true` generate, and why add a foreign key AND an index?

### ror-controllers-routing-01

What does `resources :photos` create, and which controller actions do the routes map to?

### ror-controllers-routing-02

What problem do Strong Parameters solve, and how do `require` and `permit` work in a `params.require(:user).permit(:name, :email)` call?

### ror-controllers-routing-03

What is `before_action`, and what does returning early / rendering or redirecting inside one do to the action?

### ror-controllers-routing-04

In a routes block, what is the difference between a `member` route and a `collection` route, and what HTTP status should a successful API `create` return?

### ror-controllers-routing-05

What does Rails 8's built-in authentication generator give you, and how does it differ from reaching for Devise?

### ror-views-helpers-01

How do you render a partial with local variables, and what does `render @posts` (a collection) do?

### ror-views-helpers-02

What does `form_with` unify, and does it generate a remote (AJAX) or a local (standard) form by default in current Rails?

### ror-views-helpers-03

How does Rails protect against CSRF, and what does `form_with` do for you automatically?

### ror-views-helpers-04

A view iterates `@posts` and prints `post.author.name`, causing N+1 queries. Where does the fix belong, and why not "fix" it in the view?

### ror-concerns-services-01

What is an `ActiveSupport::Concern`, and what does its `included do ... end` block let you do that a plain module mixin doesn't cleanly?

### ror-concerns-services-02

What is a service object (a PORO service), and when should you extract one instead of adding another model or controller method?

### ror-concerns-services-03

Explain the "skinny controller, fat model" guideline and the failure mode it can lead to if taken too far.

### ror-concerns-services-04

A concern and a service object both hold logic out of the controller. What is the key difference in how they're used?

### ror-caching-jobs-01

What is Russian-doll caching in Rails views, and why does using the record itself in the cache key make invalidation automatic?

### ror-caching-jobs-02

What does ActiveJob provide, and what is the role of the queue adapter? Is the default adapter suitable for production?

### ror-caching-jobs-03

Background jobs can run more than once (retries, at-least-once delivery). Why must jobs be idempotent, and how do you make one idempotent?

### ror-caching-jobs-04

What is the difference between `MyJob.perform_later` and `MyJob.perform_now`, and why pass a record's id rather than the record?
