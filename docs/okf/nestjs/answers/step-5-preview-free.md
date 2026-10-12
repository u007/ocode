# step-5-preview-free — closed-book answers for nestjs (opencode-go, 2026-10-12); answerer saw only _prompts/nestjs.md
Closed-book run: answering purely from knowledge (no web, no repo files, no answer key). Assumptions: I treat "current Nest" as my knowledge of NestJS 10/11-era behavior; where I'm version-uncertain I hedge explicitly rather than guess.

- id: nestjs-modules-01
  answer: |
    `imports` lists other modules whose **exported** providers this module needs injected.
    `controllers` lists the controllers Nest will instantiate and route for this module.
    `providers` lists the providers (services, repositories, factories, values) that Nest
    will instantiate inside this module and may inject here — they are encapsulated, so
    nothing outside the module can see them unless exported.
    `exports` lists the subset of this module's providers that becomes visible to any
    module that imports this one (you can only export providers defined here).
    A module is the organizational and encapsulation unit of a Nest application: a class
    annotated with `@Module({...})` that groups a cohesive set of related capabilities
    (typically a feature), defines the dependency-injection boundary for its providers,
    and controls what is shared with the rest of the graph. The root module is usually
    `AppModule`, registered by `NestFactory.create(AppModule)`.
- id: nestjs-modules-02
  answer: |
    Two things must be true: `AuthService` must appear in `AuthModule`'s `exports` array,
    and `AuthModule` must appear in `UsersModule`'s `imports` array. If you forget, provider
    encapsulation means nothing outside the module registry can inject it, and Nest throws
    at bootstrap (DI resolution time), typically:
    "Nest can't resolve dependencies of the UsersService/UsersController (?). Please make
    sure that the argument AuthService at index [0] is available in the UsersModule
    context." The application fails to start — it is not a silent no-op.
- id: nestjs-modules-03
  answer: |
    A dynamic module is one whose providers/structure are computed at runtime instead of
    statically by decorator metadata. Conventionally a module class exposes a static
    method that returns a `DynamicModule` object — `{ module: SomeModule, providers,
    exports, imports, ... }` — rather than just decorating a class with `@Module`.
    `forRoot()` is called once, typically in the root/app module, to configure the module
    once for the whole app (e.g. connection options, a global instance). `forFeature()` is
    called later, per feature module, to add feature-specific providers that build on what
    `forRoot` produced (e.g. making repositories/entities injectable in each domain
    module). Rule of thumb: forRoot = one-time global setup; forFeature = re-usable
    per-feature registration that depends on forRoot's setup.
- id: nestjs-modules-04
  answer: |
    `@Global()` marks the module as global, so all of its exported providers become
    injectable in every other module without each one having to import it. It should be
    used sparingly because it breaks encapsulation and makes the dependency graph
    implicit: modules no longer declare what they consume, making the app harder to read
    and reason about, encouraging accidental coupling, and risking token collisions.
    The docs suggest reserving it for a small number of infrastructure-ish modules
    (config, logging), not feature modules.
- id: nestjs-di-01
  answer: |
    1) Annotate the dependency class with `@Injectable()` so metadata about its
    constructor parameter types gets emitted (via `emitDecoratorMetadata` / reflect-metadata).
    2) Register the class in a module's `providers` array — the class itself acts as the
    injection token.
    3) In a consumer (another provider, controller, etc.), declare it as a constructor
    parameter typed with the class.
    During bootstrap, Nest reads the reflected `design:paramtypes` metadata to know what
    to inject, instantiates the provider (a singleton by default), and passes the instance
    into the consumer's constructor. If the class was never registered in any module that
    can see it, resolution fails at runtime with a "Nest can't resolve dependencies" error.
- id: nestjs-di-02
  answer: |
    Register a custom provider with a non-class token: a string, Symbol, or any object
    used as a token, e.g.
    `{ provide: 'MY_CONFIG', useValue: { key: 'value' } }` (or `useFactory` for async),
    then inject with `@Inject('MY_CONFIG')` on the constructor parameter:
    `constructor(@Inject('MY_CONFIG') private config: MyConfigType) {}`.
    `useValue` injects an existing value/instance directly; `@nestjs/config` uses this same
    token mechanism (e.g. `ConfigService` registered via `useClass`/provider token).
- id: nestjs-di-03
  answer: |
    Three scopes (set via `@Injectable({ scope: Scope.X })`, enum from `@nestjs/common`):
    `Scope.DEFAULT` — singleton, one instance shared by the whole application;
    `Scope.REQUEST` — a new instance per incoming HTTP request, shared among everything
    within that request (Nest uses a request-scoped context);
    `Scope.TRANSIENT` — a new instance for every consumer that injects it, never shared.
    The default is DEFAULT (singleton), and it is recommended because a singleton is
    created once and reused: minimal memory and CPU. The other two multiply instance
    creation with request/consumer volume and (in the REQUEST case) also bubble up through
    the dependency graph.
- id: nestjs-di-04
  answer: |
    A REQUEST-scoped provider holds per-request state, so every provider or controller that
    depends on it — directly or transitively — is forced to become REQUEST-scoped too
    ("scope bubbling"); otherwise a longer-lived (singleton) consumer would capture and
    leak one request's instance into subsequent requests. The performance implication is
    that instances are now created per request rather than once at startup, so memory,
    allocation/GC pressure, and response latency grow with request traffic, and a small
    number of request-scoped providers at the bottom of a graph can cascade widely.
    TRANSIENT does not behave the same way: it does not bubble up. Each consumer simply
    receives its own fresh instance at injection time, so a singleton consumer may inject
    a transient provider without itself becoming request-scoped.
- id: nestjs-routing-01
  answer: |
    `@Controller('cats')` sets the path prefix for every handler in that controller (all
    routes are registered under `/cats`; it can also take options like a `host`).
    The HTTP-method decorators (`@Get()`, `@Post()`, `@Put()`, `@Patch()`, `@Delete()`,
    `@All()`, ...) bind a class method to a verb — optionally with a sub-path such as
    `@Get(':id')` — and Nest composes them into routes like `GET /cats/:id`.
    The parameter decorators pull pieces of the incoming request into the handler's
    arguments: `@Param('id')` → a path parameter, `@Query('name')` → a query-string value,
    `@Body()` → the parsed request body (JSON/form). You can also pass the whole object
    (`@Param()`, `@Query()`), or custom decorators built on top of them.
- id: nestjs-routing-02
  answer: |
    Path parameter: `@Get(':id')` with the handler reading `@Param('id') id: string`.
    Nested sub-path: just concatenate segments in the method decorator, e.g.
    `@Get(':id/comments')` or deeper, e.g. `@Get('profile/settings')` — the prefix from
    `@Controller()` composes with the sub-path.
    Wildcards changed in NestJS 11 (the router moved onto path-to-regexp v8, aligned with
    the Express 5 line): the old bare `*` wildcard syntax is no longer valid. Wildcards
    must now be named, e.g. `@Get('*splat')` / `@Get('docs/*splat')`, and the matched
    remainder is read via `@Param('splat')`. An old-style `@Get('*')` now errors during
    route registration/boot. (I'm quite confident about the named-wildcard change; less
    certain about the exact error message wording.)
- id: nestjs-routing-03
  answer: |
    TypeScript interfaces exist only at compile time — they are erased from the emitted
    JavaScript, so there is no runtime object to attach decorators to or to reflect over.
    Validation and transformation need runtime metadata: `class-validator` decorators must
    be applied to a real class, and `class-transformer`/Nest's reflection needs the class
    constructor to build and check instances. Classes survive at runtime and carry their
    decorator metadata, so DTOs must be declared as classes even though they look like
    interfaces with added rules.
- id: nestjs-routing-04
  answer: |
    The default status code is 201 for POST handlers and 200 for everything else
    (GET, PUT, PATCH, DELETE, ALL). You override it with the `@HttpCode(status)` decorator
    on a handler (or on the whole controller to apply to all its routes).
    If the handler returns a Promise, Nest awaits it and uses the resolved value as the
    response body, with the same status-code defaults. If it returns an Observable, Nest
    subscribes to it and emits the **last** value received as the response body — it does
    not stream every emission to the client.
- id: nestjs-lifecycle-01
  answer: |
    `OnModuleInit` (`onModuleInit()`) fires when the enclosing module's providers are
    instantiated during module initialization — after that module's imported dependencies
    have been initialized, so dependency modules' init hooks run first. `OnApplicationBootstrap`
    (`onApplicationBootstrap()`) fires once, after ALL modules are initialized (i.e., after
    every `onModuleInit`), just before the application starts listening for requests.
    Use `onModuleInit` for per-module setup that can rely on its own dependencies;
    use `onApplicationBootstrap` when the work requires the entire app to be up but must
    still run before traffic (e.g. cache warming, readiness tasks).
- id: nestjs-lifecycle-02
  answer: |
    The shutdown hooks are `onModuleDestroy`, `beforeApplicationShutdown`, and
    `onApplicationShutdown`. On a signal Nest fires `beforeApplicationShutdown(signal)`
    first, then calls each module's `onModuleDestroy()` (as of Nest 11, in reverse of
    initialization order), then `onApplicationShutdown(signal)`; both application-level
    hooks receive the signal string (e.g. `'SIGTERM'` as their argument).
    For any of these to run on SIGTERM/SIGINT you must call `app.enableShutdownHooks()`
    after creating the app (`NestFactory.create(AppModule)` then `app.enableShutdownHooks()`),
    which subscribes to process signals — otherwise Nest does not listen for them by
    default and the hooks only fire if you call `app.close()` yourself.
    (I'm confident about the method names and the enableShutdownHooks() requirement;
    moderately confident about beforeApplicationShutdown firing before onModuleDestroy.)
- id: nestjs-lifecycle-03
  answer: |
    Destroy hooks run in reverse of initialization, i.e. A → B → C — dependents are
    destroyed before their dependencies. Yes, this changed in NestJS 11: before v11 the
    `onModuleDestroy` hooks ran in the same order in which modules were initialized
    (dependency order, C → B → A), which is backwards since a module could be torn down
    while a module depending on it still needed it; v11 made destroy ordering reverse of
    init ordering.
- id: nestjs-lifecycle-04
  answer: |
    The init hooks are triggered during application bootstrap: `NestFactory.create()`
    builds the instance graph and initialization is run as part of `app.init()`, which
    `app.listen()` calls for you. Each module's `onModuleInit` is called once that module's
    providers are instantiated (dependencies first), and afterward every provider's
    `onApplicationBootstrap` is called before listening begins.
    Yes — Nest waits for an async hook: if `onModuleInit` or `onApplicationBootstrap`
    returns a Promise, Nest awaits it before proceeding to the next module / to starting
    the server.
- id: nestjs-validation-01
  answer: |
    `ValidationPipe` validates incoming payloads (body, query, params) against a DTO class
    decorated with validation decorators (e.g. `@IsString()`), and it can also transform
    the plain incoming object into a typed DTO instance. It relies on two libraries:
    **class-validator** (the validation rules) and **class-transformer** (turning the
    plain object into a class instance). On validation failure it throws
    `BadRequestException`, producing a 400 with a list of the errors.
    Apply it to the whole app in `main.ts` with `app.useGlobalPipes(new ValidationPipe())`;
    alternatives are the `@UsePipes(ValidationPipe)` decorator at controller/handler scope
    or registering it via the `APP_PIPE` provider token (`{ provide: APP_PIPE, useClass:
    ValidationPipe }`), the latter being DI-aware.
- id: nestjs-validation-02
  answer: |
    `whitelist: true` — the validated output keeps only properties that carry at least one
    validation decorator on the DTO; everything else is stripped from the object passed to
    the handler. `forbidNonWhitelisted: true` — instead of silently stripping, the pipe
    rejects the request with a 400 `BadRequestException` if any non-whitelisted property is
    present (and it requires `whitelist` to be enabled to have any effect).
    Use them so clients can't smuggle unexpected/unauthorized extra fields into your
    handlers (mass-assignment / over-posting protection), and to enforce a strict payload
    contract that fails loudly rather than silently dropping data.
- id: nestjs-validation-03
  answer: |
    With `transform: true` (which is the default behavior of the pipe) the ValidationPipe
    runs the incoming plain payload through `class-transformer`, so the handler receives an
    actual instance of the DTO class rather than a plain JSON object, and validation errors
    reference the DTO shape. For a route parameter typed `@Param('id') id: number`, the
    pipe converts the raw string `'5'` into the number `5`, using the TypeScript
    `design:paramtypes` metadata emitted for the parameter. Caveat/nuance: in more recent
    Nest versions this implicit primitive conversion is controlled by the
    `enableImplicitConversion` option — if you find params still arriving as strings, set
    that option to true or annotate with `@Type(() => Number)`.
- id: nestjs-validation-04
  answer: |
    `ParseIntPipe` is a built-in parameter pipe: it takes the value of a single parameter
    (`@Param('id', ParseIntPipe)` — also useful for query params), runs `parseInt` on it,
    and if the result is NaN it throws `BadRequestException` ("Validation failed: numeric
    string is expected"), which Nest turns into a 400. It can be configured, e.g.
    `new ParseIntPipe(new HttpException('Custom msg', HttpStatus.NOT_FOUND))` or `PipeTransformOptions`.
    The practical difference in registration scope: a param-bound pipe only applies to that
    one value on that one route and can be individually configured (error message, status,
    optional validation); a globally registered pipe (`app.useGlobalPipes`) applies to
    every value in every route — all params, query, and body — so it's uniform and
    maintenance-free but has app-wide reach, cannot be customized per route, and (when
    registered outside module context) cannot participate in dependency injection.
- id: nestjs-guards-01
  answer: |
    A guard decides whether a given request is allowed to be handled — its job is
    authorization (with authentication-aware data like the request user already available,
    e.g. via passport middleware or a cookie). It is an `@Injectable()` class implementing
    the `CanActivate` interface, whose single method `canActivate(context: ExecutionContext)`
    returns `boolean`, `Promise<boolean>`, or `Observable<boolean>`. Returning `true`
    lets the request continue to interceptors/pipes/the handler; returning `false` makes
    Nest throw `ForbiddenException` and the request is rejected with 403. The
    `ExecutionContext` (extends `ArgumentsHost`) gives access to the request, response,
    the handler method and the controller class.
- id: nestjs-guards-02
  answer: |
    Order: Middleware → Guards → Interceptors → Pipes → Route handler (controller method) →
    post-handler Interceptors → response, with Exception Filters wrapping the whole
    pipeline last. So interceptors run on BOTH sides of the handler: their pre-handler
    logic runs before it (after guards, before pipes), and their post-handler logic
    (transformed result/error) runs after the handler completes.
    Note middleware is separate from Nest's DI pipeline — it runs before guards and only
    has access to raw req/res (it cannot see the DI context).
- id: nestjs-guards-03
  answer: |
    An interceptor binds extra logic around the execution of a controller handler using
    aspect-oriented programming: it sits in front of the handler, can transform the request
    or stream, and can react to the response stream. It is an `@Injectable()` class
    implementing the `NestInterceptor` interface, whose single method
    `intercept(context: ExecutionContext, next: CallHandler)` returns an Observable —
    it must call `next.handle()` to get the handler's result stream (it can `pipe`/`map`/
    `tap` it).
    Two classic uses: (1) response transformation/mapping (e.g. wrapping handler results in
    `{ data, statusCode: 200 }`) and (2) caching — check the cache first and short-circuit
    with `of(cachedValue)` instead of calling `next.handle()`. Other common uses:
    logging/timing, timeout and retry, error interception/reshaping.
- id: nestjs-guards-04
  answer: |
    `@Roles('admin')` is a small custom decorator conventionally built with
    `SetMetadata('roles', roles)` (from `@nestjs/common`), which attaches the role list as
    metadata on the route handler method (and applies to the class if used there). In the
    guard, you inject the `Reflector` class from `@nestjs/core` into the constructor and
    read the metadata, either `this.reflector.get<string[]>('roles', context.getHandler())`
    (falling back to `context.getClass()`), or one call with
    `getAllAndOverride('roles', [context.getHandler(), context.getClass()])` so a
    route-level value overrides the controller-level one. You then compare those roles
    against the user (e.g. `request.user`) and return true/false.
- id: nestjs-filters-01
  answer: |
    `HttpException` is Nest's base class for expected, HTTP-aware errors — it holds a
    status code and an optional response body/description, e.g.
    `new HttpException('Forbidden', HttpStatus.FORBIDDEN)`. The built-in exception classes
    (BadRequestException, UnauthorizedException, ForbiddenException, NotFoundException,
    MethodNotAllowedException, NotAcceptableException, RequestTimeoutException,
    ConflictException, GoneException, PayloadTooLargeException,
    UnsupportedMediaTypeException, UnprocessableEntityException, InternalServerErrorException,
    NotImplementedException, BadGatewayException, ServiceUnavailableException,
    GatewayTimeoutException, ...) all extend `HttpException` and pre-fill the status code
    (and a sensible default message/error name) so you only supply a message.
    Throwing one causes the exception layer to catch it and produce the HTTP response with
    the exception's status code and a JSON body shaped like
    `{ "statusCode": 400, "message": "...", "error": "Bad Request" }` (or the custom
    object you passed as the response body).
- id: nestjs-filters-02
  answer: |
    You implement the `ExceptionFilter<T>` interface (`catch(exception: T, host: ArgumentsHost)`)
    and decorate the class with `@Catch(...)` — `@Catch(HttpException)` catches a specific
    type, `@Catch(NotFoundException)` a class, or `@Catch()` for everything. Apply it via
    `@UseFilters(new MyFilter())` at controller/method scope (or globally, see the next
    question). In `catch(exception, host)` the second argument is an `ArgumentsHost`: use
    `host.switchToHttp()` to get the platform-specific request/response objects
    (`getRequest()`, `getResponse()`) and write the client response yourself, e.g.
    `response.status(500).json({ statusCode: 500, message: 'error' })`.
- id: nestjs-filters-03
  answer: |
    Exception filters use the same precedence hierarchy: global → controller → route —
    the framework checks the outermost (global) first and the most specific/closest filter
    that matches the exception type is the one that handles it, so a route-level filter
    overrides a controller-level one which overrides a global one. Register a global
    filter in `main.ts` with `app.useGlobalFilters(new MyExceptionFilter())`, or — which is
    what you need if the filter itself requires injected dependencies — register it as a
    provider using the `APP_FILTER` token (`{ provide: APP_FILTER, useClass: MyFilter }`).
- id: nestjs-filters-04
  answer: |
    Nest's built-in exception layer catches anything that isn't an `HttpException`, so an
    unexpected plain `Error` results in an HTTP 500 Internal Server Error with a generic
    JSON body: `{ "statusCode": 500, "message": "Internal server error" }`. The original
    error message/stack is not sent to the client — it's treated as an unknown error and
    only the generic 500 response is returned (you'd add a custom `@Catch()` filter if you
    wanted to alter that behavior).
- id: nestjs-providers-01
  answer: |
    Use the `useFactory` provider form, where the factory returns a Promise:
    `{ provide: 'DATABASE_CONNECTION', inject: [ConfigService], useFactory: async (config) =>
    { return await createConnection(...); } }` and list it in the module's `exports` so
    anything injecting `'DATABASE_CONNECTION'` receives the resolved value. Yes — Nest
    awaits the factory's Promise during application initialization: dependent providers
    are not instantiated and the application does not finish starting (it is not
    request-ready) until the promise resolves, and if it rejects, application boot fails.
    (`inject` lets the factory depend on other providers.)
- id: nestjs-providers-02
  answer: |
    `useValue` — inject an already-existing value/object/instance directly; pick it for
    constants, plain config objects, pre-constructed third-party clients, or test mocks.
    `useClass` — provide a class that Nest will instantiate (resolving its own
    dependencies) for the token; pick it to swap an implementation behind a stable token
    (e.g. a concrete service vs a stub) without touching consumers' imports.
    `useFactory` — provide a function that computes/returns the value at bootstrap; pick
    it when the value needs computation, async I/O, runtime decisions, or its own
    dependencies (declared via `inject`).
    Roughly: existing value → useValue; substitute an implementation → useClass;
    dynamic/async/DI-dependent value → useFactory.
- id: nestjs-providers-03
  answer: |
    `ConfigModule.forRoot()` registers the `ConfigService` provider, which loads
    configuration (by default `process.env` plus a `.env` file via dotenv; configurable
    via `envFilePath`, `load`, `cache`, `ignoreEnvFile`, and schema validation options).
    You inject `ConfigService` and read values with `configService.get('KEY')`, including
    dotted paths into custom config namespaces (`configService.get('database.host')`) and
    a typed form `configService.get<number>('PORT')`.
    `isGlobal: true` registers the module's exported providers as global providers, so
    `ConfigService` is injectable in every module without importing `ConfigModule` into
    each one — you set it up once, by convention in the root module.
- id: nestjs-providers-04
  answer: |
    ```ts
    const moduleRef = await Test.createTestingModule({
      controllers: [UsersController],
      providers: [UsersService],
    })
      .overrideProvider(UsersService)      // token of the real provider to replace
      .useValue(mockUsersService)          // or .useClass(MockUsersService)
      .compile();
    ```
    `Test.createTestingModule({...})` accepts the same metadata shape as `@Module()`
    (imports/controllers/providers), `.overrideProvider(token)` declares which real
    provider is being replaced (`.useValue()` injects a mock value, `.useClass()` a mock
    class), and `.compile()` instantiates the DI graph so it behaves like the real
    application context. Then `moduleRef.get(UsersController)` retrieves a fully-wired
    instance for the test. Related tools: `.overrideGuard()/.overrideInterceptor()/
    .overridePipe()` for the other pipeline pieces, and `.init()` if you need lifecycle
    hooks to run.
```

Validation performed against the task's checklist:
- **All 32 ids answered** — modules 01–04, di 01–04, routing 01–04, lifecycle 01–04, validation 01–04, guards 01–04, filters 01–04, providers 01–04; count matches the sheet's stated total (32), and ids match verbatim.
- **No external lookup** — no web search, no repository file opened, no answer key consulted (the question sheet content was supplied inline in the prompt, so no file read was needed). Every answer is from internal knowledge.
- **Independence** — each item answered on its own; no copying between answers.
- **Honesty over guessing** — hedges flagged where version-dependent: routing-02 named-wildcard syntax detail, lifecycle-02 hook firing order, validation-03 `enableImplicitConversion` nuance. Nothing was fabricated to fill gaps.
- **Format** — YAML list of `{ id, answer }` with block scalars, ready to map by id.

Remaining gaps: the three hedged items above are the only places where I could be wrong on version specifics; everything else is high-confidence core NestJS semantics.
