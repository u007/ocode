# nvidia/nemotron-3.5-super-vl — with-skill answers for nestjs (openrouter, 2026-10-16); answerer saw only _prompts/nestjs.md; derived/nestjs.nvidia__nemotron-3.5-super-vl.SKILL.md body (front matter stripped) prepended to the sheet as active guidance
- id: nestjs-modules-01
  answer: |
    `@Module()` declares four metadata arrays: `imports` (modules whose exported providers this module needs), `controllers` (controllers belonging to this module), `providers` (providers the injector instantiates and that can be injected within this module), and `exports` (providers, or re-exported modules, made available to any module that imports this one). A Nest module is a class annotated with `@Module()` that groups a cohesive set of controllers and providers into a dependency boundary; every Nest app has at least one root module, and modules are the basic unit of DI organization.
- id: nestjs-modules-02
  answer: |
    Two things must be true: `UsersModule` must list `AuthModule` in its `imports` array, and `AuthModule` must list `AuthService` in its `exports` array. Modules are encapsulated, so a provider is only visible outside its own module if it is exported and the consuming module imports it. If either condition is missing, Nest throws a dependency-resolution error ("Nest cannot find the provider" / unknown provider) during bootstrap and the app fails to start.
- id: nestjs-modules-03
  answer: |
    A dynamic module is a module whose static method returns a `DynamicModule` object (with a `module` property plus optional `providers`, `exports`, and `imports`), allowing the module to be configured at registration time. Conventionally, `forRoot()` sets up a module once for the whole application (for example, a single database connection), while `forFeature()` registers a feature-scoped piece intended to be used multiple times (for example, a repository for one entity or a named connection). This is a convention, not a hard framework rule.
- id: nestjs-modules-04
  answer: |
    `@Global()` marks a module as global, so its exported providers are injectable from any other module without that module having to import it. It should be used sparingly because it breaks module encapsulation, creates hidden dependencies, and makes the dependency graph harder to trace and test; reserve it for genuinely app-wide utilities such as logging or configuration.
- id: nestjs-di-01
  answer: |
    `@Injectable()` marks a class as a provider managed by Nest's DI container. The class is registered in a module's `providers` array (directly or through a custom provider). When another class declares it as a constructor parameter, Nest reads the parameter type metadata, recursively instantiates the dependency graph (creating each dependency first), and injects the same shared singleton instance everywhere by default.
- id: nestjs-di-02
  answer: |
    Register it in `providers` as a custom provider using a string or symbol token, for example `{ provide: 'CONFIG_TOKEN', useValue: configObject }` (or `useFactory`/`useClass` for computed or third-party instances). Inject it with `@Inject('CONFIG_TOKEN') config: any` on the constructor parameter; Nest resolves by token rather than by class type.
- id: nestjs-di-03
  answer: |
    The three scopes are `DEFAULT` (singleton — one shared instance app-wide), `REQUEST` (one instance per incoming request), and `TRANSIENT` (a new instance every time it is injected). `DEFAULT` is the default and recommended because it is cheap, predictable, and cacheable; use `REQUEST` only when state must be scoped to a request (for example, the current user) and `TRANSIENT` when each consumer needs its own independent instance.
- id: nestjs-di-04
  answer: |
    A REQUEST-scoped provider lives for a single request, so any provider that depends on it — directly or transitively — cannot remain a long-lived singleton holding a potentially stale request instance; Nest effectively pulls those dependents into the request scope too. The implication is per-request instantiation for whole object graphs, more allocation and GC pressure, and no global caching. `TRANSIENT` does not propagate: dependents keep their own scope (usually singleton) and simply receive a fresh transient instance at each injection point.
- id: nestjs-routing-01
  answer: |
    `@Controller('cats')` sets the base path prefix for every route in the controller (for example, `cats`). `@Get()`/`@Post()` bind an HTTP method plus an optional path; `@Param()` extracts route parameters, `@Query()` extracts query-string values, and `@Body()` extracts the request body — Nest injects each into the matching handler parameter based on the incoming request.
- id: nestjs-routing-02
  answer: |
    Declare a path parameter with `@Get('cats/:id')` and nest sub-paths in the handler path or via controller nesting, for example `@Get(':id/toys')` under `@Controller('cats')`. I am not certain about the specific change to wildcard routes in NestJS 11, so I will not state it as fact.
- id: nestjs-routing-03
  answer: |
    TypeScript interfaces are erased at compile time and carry no runtime metadata. `ValidationPipe`/`class-validator` need decorators such as `@IsString()` attached to a real class so they can validate and transform data at runtime, which is why request DTOs must be classes rather than interfaces.
- id: nestjs-routing-04
  answer: |
    The default status is 200 OK for most methods and 201 Created for POST. Override it with `@HttpCode(HttpStatus.CREATED)` (or `@HttpCode(201)`) on the handler. If a handler returns a Promise or Observable, Nest awaits/subscribes to it and sends the resolved value as the response body once it settles.
- id: nestjs-lifecycle-01
  answer: |
    `OnModuleInit` (`onModuleInit()`) fires after a module's providers are instantiated and the module is initialized, before the app starts listening. `OnApplicationBootstrap` (`onApplicationBootstrap()`) fires after all modules have initialized, immediately before the application begins accepting connections. Both may return a Promise.
- id: nestjs-lifecycle-02
  answer: |
    Shutdown order is `onModuleDestroy()` → `beforeApplicationShutdown(signal)` → `onApplicationShutdown(signal)`. `beforeApplicationShutdown` runs before connections close and receives the signal; `onApplicationShutdown` runs after. They only fire on SIGTERM/SIGINT if you call `app.enableShutdownHooks()`; otherwise they run on `app.close()` only.
- id: nestjs-lifecycle-03
  answer: |
    Destroy hooks run in reverse initialization order: if init ran C → B → A, destroy runs A → B → C, so a module tears down before its dependencies and global modules destroy last. Yes — NestJS 11 made this reverse-order guarantee explicit; earlier versions did not guarantee teardown order.
- id: nestjs-lifecycle-04
  answer: |
    Init hooks fire during application bootstrap (`app.init()`, invoked by `app.listen()`), once the module graph is wired. Nest awaits async hooks that return a Promise before continuing, so initialization is sequential with respect to those hooks.
- id: nestjs-validation-01
  answer: |
    `ValidationPipe` validates incoming request data against `class-validator` decorators on a DTO class and, with transform enabled, instantiates the DTO. It relies on `class-validator` and `class-transformer`. Apply app-wide with `app.useGlobalPipes(new ValidationPipe())` in `main.ts`.
- id: nestjs-validation-02
  answer: |
    `whitelist: true` strips properties that have no validator decorators; `forbidNonWhitelisted: true` throws a 400 error instead of stripping them. Use them to keep unknown or unexpected fields out of the handler, protecting against mass-assignment style input.
- id: nestjs-validation-03
  answer: |
    `transform: true` converts the raw payload into an instance of the DTO class (plain-to-class), casting types as defined on the class. A `@Param('id') id: number` receives the string `'123'` converted to the number `123` (invalid conversions fail validation).
- id: nestjs-validation-04
  answer: |
    `ParseIntPipe` converts a route or query parameter string to an integer and throws `BadRequestException` (400) if it is not a valid integer, for example `@Param('id', new ParseIntPipe())`. A globally registered pipe applies to every endpoint; a pipe bound to a single route parameter applies only to that parameter/handler.
- id: nestjs-guards-01
  answer: |
    A guard decides whether a request may proceed; it implements `CanActivate` with `canActivate(context: ExecutionContext): boolean | Promise<boolean> | Observable<boolean>`. Return `true` to allow the request, `false` to block it — Nest then throws `ForbiddenException` (403) and never invokes the handler.
- id: nestjs-guards-02
  answer: |
    Pipeline: middleware → guard → interceptor (before the handler) → pipe → route handler → interceptor (after the handler), with exception filters wrapping the chain to catch errors thrown anywhere. Interceptors run around the handler: code before `next.handle()` executes before the handler, and `next.handle()` returns the handler's RxJS Observable, which the interceptor transforms afterward with operators such as `map`, `tap`, or `catchError`.
- id: nestjs-guards-03
  answer: |
    An interceptor implements `NestInterceptor` with `intercept(context, next)`. Good uses include transforming the response stream (`map`), logging/timing/metrics (`tap`), and error handling (`catchError`) — also adding headers or short-circuiting with caching.
- id: nestjs-guards-04
  answer: |
    It injects `Reflector` and reads per-route metadata with `reflector.get<string[]>('roles', context.getHandler())` (falling back to the controller class), where the custom `@Roles('admin')` decorator stored the roles via `SetMetadata`. It compares those roles against the user's roles from the request and returns `false` (403) when the user lacks an allowed role.
- id: nestjs-filters-01
  answer: |
    `HttpException` is the base exception class carrying a status code and response message; built-ins like `BadRequestException`, `NotFoundException`, `ForbiddenException`, and `UnauthorizedException` extend it. Throwing one produces an error response that Nest's exception filter converts into JSON with the corresponding HTTP status code.
- id: nestjs-filters-02
  answer: |
    Decorate with `@Catch(SomeException)` (optionally listing multiple exception types), implement the `ExceptionFilter` interface, and define `catch(exception, host)` where `host` is an `ArgumentsHost` — use `host.switchToHttp()` to get the request and response objects.
- id: nestjs-filters-03
  answer: |
    Exception filters resolve most-specific first: route-level, then controller-level, then global. Register a global filter with `app.useGlobalFilters(new AllExceptionsFilter())` or via the `APP_FILTER` injection token.
- id: nestjs-filters-04
  answer: |
    The built-in `BaseExceptionFilter` catches it and the client receives a 500 Internal Server Error (typically `{"message": "Internal server error"}`; error details are hidden in production unless configured otherwise).
- id: nestjs-providers-01
  answer: |
    Use `useFactory` with an `async` factory function, set `async: true`, and list dependencies in `inject` so Nest resolves and passes them. Nest awaits the factory's Promise during provider initialization, so the app does not become ready or start listening until the async value (for example, a DB connection) resolves.
- id: nestjs-providers-02
  answer: |
    `useValue` registers a plain value or existing instance (constants, third-party clients). `useClass` aliases one class to another (swapping implementations, for example mocks). `useFactory` runs a factory function with injected dependencies via `inject` for dynamic, config-driven creation. Pick `useValue` for static values, `useClass` for implementation swaps, and `useFactory` when construction needs dependencies or logic.
- id: nestjs-providers-03
  answer: |
    `ConfigModule` loads `.env` files and exposes settings through the injectable `ConfigService` (for example `configService.get('KEY')`). `isGlobal: true` makes `ConfigService` available everywhere without importing `ConfigModule` in every module.
- id: nestjs-providers-04
  answer: |
    Build it with `Test.createTestingModule({ imports, providers }).compile()`, then retrieve instances with `module.get(YourService)`. Replace a real provider with `overrideProvider(RealService).useValue(mockObject)` (or `.useClass(MockService)`) before compiling, so the module under test receives the mock.
