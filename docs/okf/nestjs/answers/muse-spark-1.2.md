# nestjs knowledge benchmark — answers
# Model: opencode-go/muse-spark-1.2   (provider-stripped id: muse-spark-1.2)
# CLOSED-BOOK via ocode run -dir <empty> (no corpus access). corpus_rev 1.

```yaml
- id: nestjs-modules-01
  answer: |
    A Nest module (@Module()) is the organizational unit of a Nest application — a class annotated to group related controllers, providers and configuration and to define encapsulation boundaries.
    Its metadata:
    - `imports`: Other modules whose exported providers are made available inside this module.
    - `controllers`: Controllers instantiated in this module; they define the HTTP/WebSocket/microservice handlers for this scope.
    - `providers`: Providers (services, repositories, factories, values) instantiated by the Nest IoC container and available for DI inside this module.
    - `exports`: Subset of this module's providers (or re-exports of imported modules) that become visible to any module that `imports` this module. Without exporting, a provider is private to the module.
- id: nestjs-modules-02
  answer: |
    Modules are encapsulated: providers are private by default.
    For `UsersModule` to inject `AuthService` from `AuthModule`:
    1. `AuthModule` must add `AuthService` to both `providers` and `exports`.
    2. `UsersModule` must add `AuthModule` to its `imports`.

    If you forget either, Nest fails at startup with a dependency-resolution error: `Nest can't resolve dependencies of the UsersService (?). Please make sure that the argument AuthService at index [0] is available in the UsersModule context.` The application will not bootstrap.
- id: nestjs-modules-03
  answer: |
    A dynamic module is a module returned at runtime by a static method instead of a static `import`. The method returns a `DynamicModule` object `{ module: Class, providers, imports, exports, global? }` so the module can be configured with parameters.

    Conventional split:
    - `forRoot()`: Configure and provide globally/shared state once for the whole application (e.g. DB connection, global config). Typically called once in AppModule.
    - `forFeature()`: Configure a feature-scoped slice that can be called many times, often per feature module, and usually depends on the `forRoot()` having been called first. Example: `TypeOrmModule.forRoot({url})` vs `TypeOrmModule.forFeature([User, Photo])` or `JwtModule.forRoot()` vs `forFeature()`.
- id: nestjs-modules-04
  answer: |
    `@Global()` makes a module global-scoped: its `exports` are available in every module without needing to `imports: [GlobalModule]`. You only import the global module once (usually in AppModule) and it is injected everywhere.

    Use sparingly because it breaks encapsulation/explicit dependencies, makes the dependency graph implicit and hard to trace, hampers testability and tree-shaking, and hides where a provider comes from. Most modules should be explicitly imported.
- id: nestjs-di-01
  answer: |
    1. Mark a class with `@Injectable()` — this emits metadata (via `reflect-metadata` and `emitDecoratorMetadata`) and registers it as a Nest provider.
    2. Register it in a module's `providers: [CatsService]` (or as a custom provider object).
    3. Request it via constructor injection: `constructor(private catsService: CatsService) {}`

    At bootstrap Nest builds the IoC container, reads constructor param types from `design:paramtypes` metadata, looks up the provider token (the class reference by default) in the container, recursively resolves its own dependencies, instantiates it (by default as a singleton), and injects the instance. For non-class tokens you add `@Inject(TOKEN)`.
- id: nestjs-di-02
  answer: |
    Register with a custom token (string, Symbol, or const) using a custom provider:

    ```ts
    export const CONFIG_TOKEN = Symbol('CONFIG');
    // or export const API_URL = 'API_URL';

    @Module({
      providers: [
        { provide: CONFIG_TOKEN, useValue: { port: 3000 } },
        // or useFactory: () => ({...}), or useFactory with inject
        // { provide: CONFIG_TOKEN, useFactory: (cfg) => ..., inject: [OtherToken] }
      ],
      exports: [CONFIG_TOKEN]
    })
    ```

    Inject with `@Inject()`:

    ```ts
    @Injectable()
    class AppService {
      constructor(@Inject(CONFIG_TOKEN) private config: typeof configObj) {}
    }
    ```
- id: nestjs-di-03
  answer: |
    Three scopes:
    - `Scope.DEFAULT` (singleton): One instance shared for the lifetime of the application. Default.
    - `Scope.REQUEST`: New instance per incoming request, garbage-collected after the request completes. Request and closely tied to it.
    - `Scope.TRANSIENT`: New instance for every injection — each consumer gets its own instance (not tied to request).

    Default is `DEFAULT`/singleton. It is recommended because it is most performant (no per-request allocation), avoids scope propagation, allows caching, is predictable and works with all optimizations.
- id: nestjs-di-04
  answer: |
    Nest cannot inject a short-lived (REQUEST-scoped) provider into a long-lived singleton — the singleton would hold a stale instance. So any provider that directly or transitively injects a REQUEST-scoped provider is automatically bubbled up to REQUEST scope as well. This propagation is transitive through the whole chain.

    Performance implication: you lose singleton benefits — many more instances created per request, higher memory/GC pressure, and request-scoped tree cannot be optimized/cached.

    `TRANSIENT` does NOT behave the same. A singleton can inject a TRANSIENT provider and remain singleton; it just gets a dedicated instance at creation time (one per consumer). Transient does not propagate scope nor tie lifetime to the request.
- id: nestjs-routing-01
  answer: |
    `@Controller('cats')` sets the base path prefix for all route handlers in that controller — e.g. to ` /cats`. The string is prepended; `@Controller()` with no arg is `/`.

    Method decorators `@Get()`, `@Post('create')`, `@Put(':id')`, etc. define HTTP method and optional route sub-path (appended to controller prefix).
    Parameter decorators map parts of the request to handler arguments:
    - `@Param('id')` → route path parameter `/:id`
    - `@Query('page')` → query string `?page=`
    - `@Body()` / `@Body('name')` → deserialized request body (JSON by default)
    Nest binds the HTTP request to the handler method with those extracted values.
- id: nestjs-routing-02
  answer: |
    Path param + nested sub-path:

    ```ts
    @Controller('cats')
    class CatsController {
      @Get(':id/kittens/:kittenId')
      findKitten(@Param('id') id: string, @Param('kittenId') kittenId: string) {}
    }
    // matches GET /cats/123/kittens/456
    ```

    Wildcard change in Nest 11: due to moving to `path-to-regexp` v8 / Express 5, the standalone `*` / `**` wildcard no longer works. You must use named wildcards with the `*` modifier, e.g. `@Get('/{*splat}')` or `@Get('/*splat')` / `@Get('{/*path}')` depending on adapter. The old `@Get('*')` or `@Get('ab*cd')` syntax is invalid and will throw at startup.
- id: nestjs-routing-03
  answer: |
    TypeScript interfaces are erased at runtime, so they cannot be inspected by Nest at request time. DTOs are defined as classes so that:
    - `class-validator` decorators (`@IsString()`, `@IsInt()`, etc.) can be attached to properties,
    - `class-transformer` can instantiate the DTO (`plainToInstance`) and `ValidationPipe` can validate the instance,
    - metadata is available via `reflect-metadata` for Swagger/OpenAPI, etc.
    An interface could only provide compile-time checking; a class provides runtime validation/transformation.
- id: nestjs-routing-04
  answer: |
    Default status is `200 OK` for most handlers, `201 Created` for `@Post()` handlers. Override with `@HttpCode(204)` or `@HttpStatus` decorator, or by using `@Res()` and sending manually.

    If handler returns a `Promise`, Nest `await`s it and sends the resolved value. If it returns an `Observable`, Nest subscribes and sends the last emitted value on completion (waiting for the stream to complete). Both are handled automatically by the router.
- id: nestjs-lifecycle-01
  answer: |
    Both are initialization hooks, but they fire at different phases:

    - `OnModuleInit` (`onModuleInit()`): Called after Nest has resolved the module's dependencies and the module's providers/controllers are instantiated. Runs per-module as each module is initialized.
    - `OnApplicationBootstrap` (`onApplicationBootstrap()`): Called after all modules' `OnModuleInit` hooks have completed and the application is fully bootstrapped. All modules are up.

    Order: all `OnModuleInit` complete first, then `OnApplicationBootstrap` for all modules.
- id: nestjs-lifecycle-02
  answer: |
    Shutdown/destroy hooks in order:
    1. `OnModuleDestroy` — `onModuleDestroy()`
    2. `BeforeApplicationShutdown` — `beforeApplicationShutdown(signal?: string)`
    3. `OnApplicationShutdown` — `onApplicationShutdown(signal?: string)`

    They only fire if shutdown hooks are enabled: call `app.enableShutdownHooks()` (or `app.enableShutdownHooks()` before `await app.listen()`) so Nest listens for `SIGTERM` / `SIGINT` / `SIGQUIT` and triggers the lifecycle.
- id: nestjs-lifecycle-03
  answer: |
    Destroy hooks run in reverse order of init. If init runs C → B → A (dependencies first, dependents last), destroy runs A → B → C (dependents first, dependencies last) — LIFO.

    Yes this changed in NestJS 11: prior to v11, destroy hooks ran in the same order as init (not reversed). Since v11 they are correctly reversed to allow dependents to clean up before dependencies are torn down.
- id: nestjs-lifecycle-04
  answer: |
    Init hooks are triggered by calling `app.init()` or `app.listen()` (which internally calls `init()`), or `NestApplicationContext` initialization.

    Yes, Nest waits: if a hook returns a `Promise` (i.e. is `async`), Nest `await`s it before continuing to the next module/phase. Initialization is sequential and async-aware, so async setup (DB connections, etc.) completes before the app is marked ready.
- id: nestjs-validation-01
  answer: |
    `ValidationPipe` automatically validates (and optionally transforms) incoming request payloads against DTO classes decorated with `class-validator` decorators.

    It relies on `class-validator` (validation) and `class-transformer` (plain-to-class transformation).

    Apply globally with `app.useGlobalPipes(new ValidationPipe({ ... }))` in `main.ts`, or via provider `{ provide: APP_PIPE, useClass: ValidationPipe }` or `app.useGlobalPipes()`.
- id: nestjs-validation-02
  answer: |
    - `whitelist: true` — strips properties that have no validation decorator in the DTO. Prevents clients from sending extra fields (mass-assignment protection).
    - `forbidNonWhitelisted: true` — instead of stripping, throws a `BadRequestException` (400) if any non-whitelisted property is present.

    Use them to enforce strict DTO shapes, reject or remove unknown fields for security and data integrity. `forbidNonWhitelisted` implies `whitelist: true`.
- id: nestjs-validation-03
  answer: |
    `transform: true` tells `ValidationPipe` to use `class-transformer` (`plainToInstance`) to transform plain JSON into an instance of the DTO class, applying `@Type()` / `@Transform()` decorators.

    For a route param `@Param('id') id: number`, the raw value from the URL is a string. With `transform: true` and `transformOptions: { enableImplicitConversion: true }` (or with `@Type(() => Number)` on a DTO/query), `ValidationPipe` will coerce `"42"` → `42`. Without implicit conversion, `@Param` alone is not transformed by class-transformer unless you also use an explicit pipe (`ParseIntPipe`) or `@Type`.
- id: nestjs-validation-04
  answer: |
    A built-in pipe like `ParseIntPipe` transforms and validates a single value — it parses a string to an integer and throws `BadRequestException` if parsing fails.

    Difference of scope:
    - Globally registered pipe (`app.useGlobalPipes(new ParseIntPipe())` or `ValidationPipe`) runs on every handler argument (body, query, param) for all routes.
    - Param-bound pipe (`@Param('id', ParseIntPipe) id: number`) runs only on that specific parameter. It is more selective, useful when only one param needs parsing without affecting the whole app's validation, and its error is scoped to that route.
- id: nestjs-guards-01
  answer: |
    A guard is a class that determines whether a request should be handled. It implements `CanActivate` with method `canActivate(context: ExecutionContext): boolean | Promise<boolean> | Observable<boolean>`.

    If it returns `true` (or resolves to true) the request proceeds; `false` denies it (Nest throws `ForbiddenException`). It can also throw an exception (`UnauthorizedException`, etc.) to customize the denial response.
- id: nestjs-guards-02
  answer: |
    Order for an incoming request:
    1. Middleware
    2. Guards
    3. Interceptors (pre-handler / before `next.handle()`)
    4. Pipes
    5. Route handler
    6. Interceptors (post-handler / after `next.handle()` — RxJS operator chain)
    7. Exception filters (only if an exception was thrown at any stage above)

    Interceptors run wrapped around the handler: logic before `next.handle()` runs before the handler, logic piped after `next.handle()` (via RxJS `map`/`tap`/`catchError`) runs after the handler and can transform the response or errors.
- id: nestjs-guards-03
  answer: |
    An interceptor is a class wrapping the request/response stream, implementing `NestInterceptor` with method `intercept(context: ExecutionContext, next: CallHandler): Observable<any>` where `next.handle()` invokes the handler.

    Good for: 1) Logging / timing / tracing, 2) Response mapping/transforming (e.g. wrapping `{data}`), 3) Caching, 4) Timeout handling, 5) Error mapping. (Any two suffice.)
- id: nestjs-guards-04
  answer: |
    The custom decorator uses `SetMetadata` (e.g. `SetMetadata('roles', ['admin'])` or `@Roles('admin')`). The guard injects the `Reflector` class from `@nestjs/core` and reads the metadata in `canActivate`:

    ```ts
    @Injectable()
    class RolesGuard implements CanActivate {
      constructor(private reflector: Reflector) {}
      canActivate(ctx: ExecutionContext) {
        const roles = this.reflector.getAllAndOverride<string[]>('roles', [ctx.getHandler(), ctx.getClass()]);
        // ...
      }
    }
    ```

    It uses `reflector.get()` / `getAllAndOverride()` / `getAllAndMerge()` to read per-route and per-controller metadata.
- id: nestjs-filters-01
  answer: |
    `HttpException` is the base class for HTTP errors in Nest. It takes a response body/message and an HTTP status code. All built-in exceptions (`BadRequestException`, `NotFoundException`, `UnauthorizedException`, `ForbiddenException`, etc.) extend it with preset status codes.

    Throwing one causes Nest's exception layer to produce an HTTP response with the corresponding status code and a JSON body like `{ statusCode: 404, message: 'Not Found', error: 'Not Found' }` (or a custom message if provided).
- id: nestjs-filters-02
  answer: |
    Use the `@Catch()` decorator (with an exception class or empty for catch-all) and implement `ExceptionFilter`:

    ```ts
    @Catch(HttpException) // or @Catch() for all
    class MyFilter implements ExceptionFilter {
      catch(exception: HttpException, host: ArgumentsHost) {
        const ctx = host.switchToHttp();
        const res = ctx.getResponse();
        const req = ctx.getRequest();
        // build and send response
      }
    }
    ```

    The `catch` method receives the thrown exception and an `ArgumentsHost` (to switch to HTTP/RPC/WS context and access request/response).
- id: nestjs-filters-03
  answer: |
    Exception filters resolve opposite to guards/pipes/interceptors: most specific first. Order: route/method-bound filter → controller-bound filter → global filter. The first filter that matches the thrown exception handles it and stops the chain.

    Register a global filter with `app.useGlobalFilters(new MyFilter())` in bootstrap, or via provider `{ provide: APP_FILTER, useClass: MyFilter }` (which allows DI).
- id: nestjs-filters-04
  answer: |
    If no custom filter handles a plain `Error`, Nest's default built-in exception filter catches it and maps it to a `500 Internal Server Error` response. The client receives `{ statusCode: 500, message: 'Internal server error' }` (generic, without leaking stack trace in production). The original error is logged server-side.
- id: nestjs-providers-01
  answer: |
    Declare the provider with `useFactory` as an `async` function (or returning a Promise):

    ```ts
    {
      provide: 'DATABASE_CONNECTION',
      useFactory: async (config: ConfigService) => {
        return await createConnection(config.get('dbUrl'));
      },
      inject: [ConfigService],
    }
    ```

    Yes, Nest waits: the container `await`s async factories before resolving dependents and before the application is considered initialized (`app.init()`/`app.listen()` won't complete until all async providers resolve). If the Promise rejects, the app fails to start.
- id: nestjs-providers-02
  answer: |
    - `useValue`: Provide a static value/instance as-is (e.g. config object, mock, constant). No instantiation; same reference shared.
    - `useClass`: Provide a class to be instantiated as the implementation for a token. Nest instantiates it (with DI). Used to swap implementations: `{ provide: AbstractService, useClass: ConcreteService }`.
    - `useFactory`: Provide a factory function (sync or async) whose return value becomes the provider value. Can `inject` other providers, run logic, create third-party instances. Most flexible; used for dynamic/async creation.

    Pick `useValue` for constants/mocks, `useClass` for alternative class implementations, `useFactory` for computed/async/dependent values.
- id: nestjs-providers-03
  answer: |
    `ConfigModule` from `@nestjs/config` loads env files (dotenv), parses them, and exposes values via `ConfigService` (injectable service with `get('KEY')`, `getOrThrow()`) and optionally via `registerAs` namespaces. Import it with `ConfigModule.forRoot({ ... })`.

    `isGlobal: true` makes the module global (like `@Global()`), so `ConfigService` is available everywhere without re-importing `ConfigModule` in each feature module.
- id: nestjs-providers-04
  answer: |
    Use `Test.createTestingModule()` from `@nestjs/testing`:

    ```ts
    const moduleRef = await Test.createTestingModule({
      controllers: [CatsController],
      providers: [CatsService], // real or omitted if mocked
    })
    .overrideProvider(CatsService).useValue(mockCatsService)
    // or .useFactory, .useClass
    // or overrideGuard, overridePipe, etc.
    .compile();

    const service = moduleRef.get(CatsService);
    ```

    Alternatively, directly provide the mock: `providers: [{ provide: CatsService, useValue: mock }]` without `overrideProvider`. Then get the instance with `moduleRef.get()` and test against the mock.
```
