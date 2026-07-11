# Nestjs — Kaizen blind answer sheet (questions only)

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

Total questions: 32

---

### nestjs-modules-01

What does the `@Module()` decorator's metadata (`imports`, `controllers`, `providers`, `exports`) each declare, and what is a Nest module for?

### nestjs-modules-02

Modules are encapsulated. If `AuthModule` provides `AuthService` and `UsersModule` wants to inject it, what must be true, and what happens if you forget?

### nestjs-modules-03

What is a dynamic module, and what is the conventional difference between a `forRoot()` and a `forFeature()` static method?

### nestjs-modules-04

What does the `@Global()` decorator do to a module, and why should it be used sparingly?

### nestjs-di-01

How does Nest's dependency injection resolve a class dependency? Walk through `@Injectable()`, provider registration, and constructor injection.

### nestjs-di-02

You need to inject a value that isn't a class (a config object, a string, a third-party instance). How do you register and inject it with a custom token?

### nestjs-di-03

Name Nest's three injection scopes and what instance lifetime each gives. Which is the default and why is it recommended?

### nestjs-di-04

Why does making one provider `REQUEST`-scoped tend to make others request-scoped too, and what's the performance implication? Does `TRANSIENT` behave the same way?

### nestjs-routing-01

In a controller, what does `@Controller('cats')` set, and how do `@Get()/@Post()`, `@Param()`, `@Query()`, and `@Body()` map an HTTP request to a handler?

### nestjs-routing-02

How do you declare a route with a path parameter and a nested sub-path, and what changed about wildcard routes in NestJS 11?

### nestjs-routing-03

Why are request DTOs defined as classes rather than TypeScript interfaces in NestJS?

### nestjs-routing-04

What is the default HTTP status code for a handler, how do you override it, and how does Nest handle a handler that returns a Promise or observable?

### nestjs-lifecycle-01

What is the difference between `OnModuleInit` and `OnApplicationBootstrap`, and when does each fire?

### nestjs-lifecycle-02

Name the shutdown lifecycle hooks in order, and what must you enable for them to fire on a SIGTERM/SIGINT.

### nestjs-lifecycle-03

If initialization hooks run modules in order C → B → A (dependency order), in what order do the destroy hooks run, and did this change in NestJS 11?

### nestjs-lifecycle-04

What triggers the init lifecycle hooks, and does Nest wait for an async hook (one returning a Promise) before continuing?

### nestjs-validation-01

What does the built-in `ValidationPipe` do, what two libraries does it rely on, and how do you apply it to the whole app?

### nestjs-validation-02

What do the ValidationPipe options `whitelist` and `forbidNonWhitelisted` do, and why use them?

### nestjs-validation-03

What does `transform: true` on the ValidationPipe do, including what happens to a `@Param('id')` typed as `number`?

### nestjs-validation-04

Besides ValidationPipe, what does a built-in pipe like `ParseIntPipe` do, and what is the practical difference between a globally-registered pipe and one bound to a single route parameter?

### nestjs-guards-01

What is a guard, what interface does it implement, and how does its return value control the request?

### nestjs-guards-02

Put the request pipeline in order: middleware, guards, interceptors, pipes, the route handler, and exception filters. Where do interceptors run relative to the handler?

### nestjs-guards-03

What is an interceptor, what interface does it implement, and give two things interceptors are good for.

### nestjs-guards-04

How does a roles guard read per-route metadata set by a custom decorator like `@Roles('admin')`, and what class does it use?

### nestjs-filters-01

What is `HttpException`, how do the built-in exception classes relate to it, and what does throwing one produce?

### nestjs-filters-02

How do you write a custom exception filter? Name the decorator, the interface, and the argument the catch method receives.

### nestjs-filters-03

Guards, interceptors, and pipes resolve global → controller → route. In what order do exception filters resolve, and how do you register a global filter?

### nestjs-filters-04

If your code throws a plain `Error` (not an `HttpException`) and no custom filter handles it, what does the client receive?

### nestjs-providers-01

How do you create an async provider whose value depends on a Promise (e.g. a DB connection), and does Nest wait for it before the app is ready?

### nestjs-providers-02

Contrast the custom-provider forms `useValue`, `useClass`, and `useFactory`. When would you pick each?

### nestjs-providers-03

How does `@nestjs/config`'s `ConfigModule` expose configuration, and what does `isGlobal: true` change?

### nestjs-providers-04

Using `@nestjs/testing`, how do you build a module for a unit test and replace a real provider with a mock?
