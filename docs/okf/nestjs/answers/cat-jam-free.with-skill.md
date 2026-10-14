# cat-jam-free — WITH-SKILL closed-book answers for nestjs (opencode-go, 2026-10-14)
# Delivery: derived/nestjs.cat-jam-free.SKILL.md body (front matter stripped) prepended to _prompts/nestjs.md; one
# `ocode run -m opencode-go/cat-jam-free -effort med` session, isolated HOME with every tool denied, cwd holding only the sheet;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: nestjs-modules-01
  answer: |
    `imports` lists other modules whose exported providers this module needs; `controllers` registers route controllers; `providers` declares injectable services/factories/guards/etc. available in this module; `exports` declares which providers other importing modules may use. A module is the organizational/composition boundary for a feature: it declares what the feature contains and what it exposes.
- id: nestjs-modules-02
  answer: |
    `AuthService` must be both `provided` by `AuthModule` and `exported` by it, and `UsersModule` must `import: [AuthModule]`. If you forget the export or import, Nest cannot resolve `AuthService` in `UsersModule`’s DI context and throws an unknown-dependency error.
- id: nestjs-modules-03
  answer: |
    A dynamic module is a module whose configuration is computed at runtime, usually via a static method returning a `DynamicModule` (`module`, `imports`, `providers`, `exports`, etc.). Conventionally `forRoot()` configures the module once globally (often async config, connections, root settings), while `forFeature()` registers feature-level resources (entities/repositories/metadata) and is imported by feature modules, often repeatedly.
- id: nestjs-modules-04
  answer: |
    `@Global()` makes a module’s exported providers available application-wide without importing it in every module. Use it sparingly because it hides dependencies, couples features to global state, and makes provider ownership harder to trace; reserve it for cross-cutting infrastructure such as config or common utilities.
- id: nestjs-di-01
  answer: |
    Mark the class with `@Injectable()` so Nest records it as a provider and captures constructor parameter types. Register it in a module’s `providers` array. When Nest instantiates a consumer, it resolves each constructor dependency by type from the module/container scope, recursively instantiating providers, and injects them. The default scope is singleton, so one instance is shared.
- id: nestjs-di-02
  answer: |
    Register a custom provider using a token (string, `Symbol`, or class): `{ provide: 'CONFIG', useValue: configObject }`, or `useFactory`/`useClass` as appropriate, then inject with `@Inject('CONFIG')`. The token is the DI key; `useValue` is typical for already-created values/strings/instances, while `useFactory` handles computed values.
- id: nestjs-di-03
  answer: |
    `DEFAULT` (singleton: one shared instance), `REQUEST` (new instance per request, including its dependency graph), and `TRANSIENT` (new instance for each injection/consumer). The default is `DEFAULT` singleton because it is cheapest and keeps services stateless and reusable.
- id: nestjs-di-04
  answer: |
    `REQUEST` scope bubbles: any provider that depends on a request-scoped provider must also become request-scoped, creating instances per request and adding per-request DI overhead. `TRANSIENT` does not behave the same way; each consumer gets its own instance at its own scope, and a singleton depending on a transient provider does not become request-scoped or per-request.
- id: nestjs-routing-01
  answer: |
    `@Controller('cats')` sets the route prefix (`/cats`) for all handlers in the controller. `@Get()/@Post()` map HTTP method and optional subpath; `@Param()` binds route/path parameters, `@Query()` binds query-string values, and `@Body()` binds the request body. Method parameter decorators receive the corresponding request part, optionally through pipes.
- id: nestjs-routing-02
  answer: |
    Use path params and literal subpaths together, e.g. `@Get(':id/profile')`. In NestJS 11, wildcard routes must use named wildcard parameters (e.g. `*path`) rather than a bare `*`, following Express 5/path-to-regexp v8 changes.
- id: nestjs-routing-03
  answer: |
    DTOs must be classes because validation and transformation happen at runtime; class-validator/class-transformer metadata and decorators are retained on classes. TypeScript interfaces are erased during compilation, so a pipe cannot inspect or validate them at runtime.
- id: nestjs-routing-04
  answer: |
    The default status is generally `200 OK`, except POST which defaults to `201 Created`. Override it with `@HttpCode(...)`. If a handler returns a `Promise`, Nest awaits it; if it returns an `Observable`, Nest subscribes and sends the emitted response value (use `@Sse()` for streaming responses).
- id: nestjs-lifecycle-01
  answer: |
    `OnModuleInit` fires during module initialization, after dependencies are available but before the app is fully bootstrapped. `OnApplicationBootstrap` fires after all modules have initialized, during application bootstrap. Both run in dependency order per module, with bootstrap after init.
- id: nestjs-lifecycle-02
  answer: |
    Shutdown hooks run as `OnModuleDestroy`, then `BeforeApplicationShutdown`, then `OnApplicationShutdown`. They only fire after calling `app.enableShutdownHooks()` (which connects system signals such as SIGTERM/SIGINT to graceful shutdown); otherwise Nest does not listen for process termination.
- id: nestjs-lifecycle-03
  answer: |
    Destroy hooks traditionally run in reverse dependency order, so `A → B → C`. Historically initialization is `C → B → A` and teardown reverses it. I am not fully certain whether NestJS 11 changed this ordering, so treat reverse order as the established default and verify against the v11 release notes if exact ordering matters.
- id: nestjs-lifecycle-04
  answer: |
    Init hooks are triggered by application bootstrap/initialization (e.g. `app.init()`/`listen()`). Yes, Nest awaits a Promise returned by an async hook before continuing to the next hook/module, so async initialization completes before bootstrap proceeds.
- id: nestjs-validation-01
  answer: |
    `ValidationPipe` validates incoming data against DTO classes decorated with `class-validator` rules and uses `class-transformer` for transformation. Apply it globally with `app.useGlobalPipes(new ValidationPipe())` or by providing `APP_PIPE`.
- id: nestjs-validation-02
  answer: |
    `whitelist` strips properties not present in the DTO; `forbidNonWhitelisted` throws a validation error when unknown properties are present. Together they prevent over-posting/mass assignment and enforce the exact expected payload shape.
- id: nestjs-validation-03
  answer: |
    `transform: true` converts plain incoming objects into class instances and performs type coercion based on TypeScript metadata. For example, a route parameter annotated as `number` can be transformed from the incoming string `"123"` to the number `123`.
- id: nestjs-validation-04
  answer: |
    `ParseIntPipe` parses/validates a value as an integer and throws `BadRequestException` if it is invalid. A globally registered pipe applies automatically to eligible handler arguments across the app, while a parameter-bound pipe such as `@Param('id', ParseIntPipe)` explicitly targets one argument and keeps other parameters unaffected.
- id: nestjs-guards-01
  answer: |
    A guard is an authorization/authentication gate implementing `CanActivate` with `canActivate(context: ExecutionContext)`. Returning `true` allows the request to continue; returning `false` makes Nest throw `ForbiddenException` (403). Guards are bound with `@UseGuards()`, `app.useGlobalGuards()`, or an `APP_GUARD` provider.
- id: nestjs-guards-02
  answer: |
    Pipeline order is middleware → guards → interceptors (pre-handler code) → pipes → route handler → interceptors (post-handler/response transformation) → exception filters on errors. Interceptors therefore wrap the handler: code before `next.handle()` runs before it, and operators after `next.handle()` transform the returned Observable after it.
- id: nestjs-guards-03
  answer: |
    An interceptor implements `NestInterceptor` via `intercept(context, next)`, returning an RxJS `Observable`. It is useful for logging/timing, response transformation/mapping, caching, and centralized error handling, using code before `next.handle()` and operators such as `map`, `tap`, `catchError`, and `timeout` after it.
- id: nestjs-guards-04
  answer: |
    A roles guard uses Nest’s `Reflector` class to read metadata set by a custom decorator (often `@SetMetadata('roles', ...)` or a `@Roles()` decorator). It calls something like `reflector.getAllAndOverride('roles', [context.getHandler(), context.getClass()])`, then compares those roles with the authenticated user’s roles.
- id: nestjs-filters-01
  answer: |
    `HttpException` is the base class for HTTP errors in Nest. Built-in exceptions such as `BadRequestException`, `NotFoundException`, and `ForbiddenException` extend it. Throwing one causes Nest’s default exception filter to return an HTTP response with the corresponding status code and error message payload.
- id: nestjs-filters-02
  answer: |
    Use the `@Catch()` decorator (optionally with an exception type), implement `ExceptionFilter`, and define `catch(exception, host: ArgumentsHost)`. From `host.switchToHttp()` you can access the request/response and send a custom error response.
- id: nestjs-filters-03
  answer: |
    Exception filters resolve from most specific to least specific: route-level, then controller-level, then global. This is the opposite direction from guards/interceptors/pipes. Register a global filter with `app.useGlobalFilters(new MyFilter())` or via an `APP_FILTER` provider.
- id: nestjs-filters-04
  answer: |
    If a plain `Error` is thrown and no custom filter handles it, Nest’s default filter returns `500 Internal Server Error` with a generic message such as `"Internal server error"`; the stack trace is not sent to the client (and is logged server-side).
- id: nestjs-providers-01
  answer: |
    Use an async factory provider: `{ provide: TOKEN, useFactory: async (...deps) => await createValue(...deps), inject: [Dep] }`. Nest resolves and awaits the factory during module initialization before the provider is available, so the app is not ready until it completes.
- id: nestjs-providers-02
  answer: |
    `useValue` provides a static/pre-created value or mock; `useClass` lets Nest instantiate a class and resolve its dependencies; `useFactory` dynamically creates a value, supports dependency injection, and can be async/conditional. Choose based on whether you need a constant, class-based implementation, or computed/async creation.
- id: nestjs-providers-03
  answer: |
    `ConfigModule.forRoot()` loads environment/config values and exposes them through `ConfigService.get(key)`. Setting `isGlobal: true` makes the module global, so `ConfigService` is available without importing `ConfigModule` in every module.
- id: nestjs-providers-04
  answer: |
    Use `Test.createTestingModule({ providers: [...] })`. Replace a real provider with a mock through the builder, e.g. `.overrideProvider(RealService).useValue(mockService)`, then `.compile()`. Retrieve the tested class from the compiled module/fixture or use it to create a Nest application for request-level testing.
