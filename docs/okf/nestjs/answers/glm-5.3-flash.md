```yaml
- id: nestjs-modules-01
  answer: |
    @Module() metadata: `imports` lists other modules whose exported providers this module may use; `controllers` lists the controller classes the module instantiates and wires routes for; `providers` lists services/etc. created by Nest's injector and usable within the module; `exports` is the subset of providers made available to modules that import this one. A module is the organizational unit of a Nest app: it groups related components, defines the boundaries of the dependency graph, and is the structure the IoC container uses to assemble the application (everything hangs off a root AppModule).
- id: nestjs-modules-02
  answer: |
    AuthModule must `export` AuthService (in `exports`), and UsersModule must `import` AuthModule (in `imports`). If you forget either side, Nest throws a dependency-resolution error at bootstrap — typically "Nest can't resolve dependencies of the AuthService(...). Please make sure that the service ... is available in the current context" — i.e., the app fails to start rather than failing at request time.
- id: nestjs-modules-03
  answer: |
    A dynamic module is created by a static method on a module class that returns a DynamicModule ({ module: X, providers/imports/exports/... }), letting you configure the module at import time, e.g. ConfigModule.forRoot({ isGlobal: true }). Convention: `forRoot()` is called once (usually in the root module) to configure the module overall and register the shared/global providers; `forFeature()` is called in each feature module to register local, per-feature providers derived from that root configuration (e.g. TypeOrmModule.forFeature([Entity]) registering repositories).
- id: nestjs-modules-04
  answer: |
    @Global() makes the module's exported providers available everywhere in the app without each consuming module importing it. Use sparingly because it makes dependencies implicit — the dependency graph becomes harder to trace, test, and reason about, and it invites coupling. It's normally reserved for truly cross-cutting modules (ConfigModule, Prisma/db modules) and registered once in the root module.
- id: nestjs-di-01
  answer: |
    @Injectable() marks a class as something Nest's IoC container can instantiate and manage. You register it in a module's `providers` array, which associates the class (used as its injection token) with the container. When another injectable (controller or service) declares it in its constructor (`constructor(private users: UsersService)`), Nest resolves the token, recursively instantiates the provider's own constructor dependencies, caches the instance (singleton by default), and injects it — all during bootstrap. A token missing from the container causes a startup-time "Nest can't resolve dependencies" error.
- id: nestjs-di-02
  answer: |
    Register with a custom token (string, symbol, or class) and inject with @Inject(). E.g. `{ provide: 'CONFIG', useValue: { port: 3000 } }` then `constructor(@Inject('CONFIG') private config)`. For a third-party instance you typically use `useValue` with the pre-built instance, or `useFactory` to construct it (optionally async). Injection tokens don't have to be classes — any value usable as a key works.
- id: nestjs-di-03
  answer: |
    DEFAULT (singleton): one instance per application, shared by all consumers — the default. REQUEST: one instance per incoming request, shared within that request's graph. TRANSIENT: a new instance for every consumer, never shared. Singleton is the default and recommended because it's cheapest — instances are created once and reused — while REQUEST/TRANSIENT create new instances per request/per injection with real allocation and GC overhead.
- id: nestjs-di-04
  answer: |
    Request scope propagates up the dependency chain: a consumer that injects a REQUEST-scoped provider itself needs a fresh instance per request (it can't hold a stale reference), so it too becomes request-scoped — and so on up to controllers/gateways. Implication: per-request instantiation of a whole chain adds allocation/GC overhead and latency, and request-scoped classes can't be used in places instantiated once at startup (e.g. passport strategies). TRANSIENT does not propagate the same way: it just gives each consumer its own instance (created per consumer); it doesn't force singletons up the chain to become request-scoped.
- id: nestjs-routing-01
  answer: |
    @Controller('cats') sets the path prefix for every route in that class. Method decorators @Get()/@Post() etc. bind HTTP methods (and optional sub-paths) to handler methods, forming the routing table. Parameter decorators extract request data: @Param() reads route path parameters, @Query() reads the query string, @Body() reads the request body. Nest matches an incoming request against the combined controller-prefix + method-path and dispatches it to the handler with the extracted values injected.
- id: nestjs-routing-02
  answer: |
    Path parameters use colon syntax, e.g. @Get(':id/likes') with @Param('id'). NestJS 11 changed wildcards because it moved to Express 5 / path-to-regexp v8: a bare `*` no longer acts as a catch-all; wildcards must be named, e.g. @Get('*splat') (or `/{*splat}` patterns), and optional path segments changed from `?` to brace syntax (e.g. ':id/{userId}'). I'm confident about the named-wildcard (`*splat`) requirement in v11; I'm less certain of the exact optional-param syntax details.
- id: nestjs-routing-03
  answer: |
    Because TypeScript interfaces are erased at compile time — at runtime there is nothing left for the ValidationPipe to work with. Classes persist at runtime, carry decorator metadata that class-validator reads, and give class-transformer a constructor to instantiate/transform the plain payload into, enabling validation and type coercion. With an interface, the pipeline has no runtime representation to validate against.
- id: nestjs-routing-04
  answer: |
    Default is 200, except POST handlers which default to 201. Override with the @HttpCode(...) decorator (or by using @Res() and setting the status manually). If a handler returns a Promise, Nest awaits it and sends the resolved value; if it returns an Observable, Nest subscribes and sends the last value emitted before completion (the response is sent once the stream completes).
- id: nestjs-lifecycle-01
  answer: |
    onModuleInit fires once each module's dependencies are resolved, per module, during initialization. onApplicationBootstrap fires once, after ALL modules have been initialized, immediately before the application starts accepting connections/being ready. So module-init happens per module first, and application-bootstrap is the app-wide signal that everything is wired and startup work (e.g. seeding, prewarming) can run.
- id: nestjs-lifecycle-02
  answer: |
    In order: onModuleDestroy (per module) → beforeApplicationShutdown (app-level, receives the signal) → onApplicationShutdown (app-level, receives the signal). These only fire if you enable them with app.enableShutdownHooks() — by default Nest ignores SIGTERM/SIGINT for shutdown hooks (deployment platforms like Kubernetes send SIGTERM).
- id: nestjs-lifecycle-03
  answer: |
    They run in reverse (LIFO) order of initialization: if init ran C → B → A, destroy hooks run A → B → C — so dependents are destroyed before the dependencies they rely on. Yes, I believe this changed in NestJS 11 (teardown order was made the reverse of the initialization order; pre-v11 teardown followed the initialization order). Moderate confidence on exactly what pre-11 did; confident that v11 made teardown reverse-order.
- id: nestjs-lifecycle-04
  answer: |
    Init hooks (onModuleInit, onApplicationBootstrap) are triggered automatically by Nest during application startup, after the dependency graph is resolved. Yes — Nest awaits hooks that return Promises (async methods) before continuing to the next stage, so the app isn't considered initialized/ready until all async init hooks resolve.
- id: nestjs-validation-01
  answer: |
    The built-in ValidationPipe validates incoming payloads (body, params, query) against DTO classes — reading class-validator decorator rules and using class-transformer to turn plain objects into DTO instances — and throws a 400 BadRequestException with the validation errors if the payload fails. It relies on the `class-validator` and `class-transformer` libraries. Apply app-wide with `app.useGlobalPipes(new ValidationPipe())` in main.ts, or register it via the APP_PIPE DI token.
- id: nestjs-validation-02
  answer: |
    whitelist: true strips (removes) any properties from the payload that are not decorated/declared in the DTO. forbidNonWhitelisted: true instead makes the request fail validation (400) when extra properties are present. You use them to prevent over-posting/mass-assignment — clients silently injecting fields (e.g. isAdmin) — either by cleaning the payload or by rejecting it explicitly.
- id: nestjs-validation-03
  answer: |
    transform: true makes the pipe transform payloads into instances of the DTO class (plain-to-class via class-transformer) and perform basic type coercion based on the TypeScript type metadata. So a @Param('id') typed as number: the raw value is the string "5", and with transform: true it is converted to the number 5 (auto-conversion for primitive types based on the declared type); without transform you'd receive the string.
- id: nestjs-validation-04
  answer: |
    ParseIntPipe transforms and validates a route parameter to an integer — it converts "5" to 5 and throws a 400 BadRequestException if the value isn't a valid integer. A globally-registered pipe (useGlobalPipes / APP_PIPE) runs for every route handler in the app, which is convenient for something like ValidationPipe; a pipe bound to a single parameter (@Param('id', ParseIntPipe)) applies only to that one parameter/handler. Global is blanket policy, parameter-bound is fine-grained and explicit per use.
- id: nestjs-guards-01
  answer: |
    A guard is a class implementing the CanActivate interface with canActivate(context: ExecutionContext): boolean | Promise<boolean> | Observable<boolean>. It runs after middleware and decides whether the request is allowed: returning true lets the request proceed to the handler; returning false (or a falsy value) causes Nest to throw a ForbiddenException (403) and the handler is never executed. Typical use: authentication/authorization (roles, permissions).
- id: nestjs-guards-02
  answer: |
    Order: middleware → guards → interceptors (pre-handler part) → pipes → route handler → interceptors (post-handler part: response mapping) → exception filters (catch anything thrown along the way). Interceptors wrap the handler — they run both before it (can mutate the request) and after it (they wrap the handler's response stream via next.handle() and RxJS operators).
- id: nestjs-guards-03
  answer: |
    An interceptor implements the NestInterceptor interface: intercept(context: ExecutionContext, next: CallHandler): Observable<any>, working with RxJS streams around the handler. Good for: (1) response mapping/transformation (map the handler's result before it's sent), (2) cross-cutting concerns like logging, caching, or timeouts by wrapping the response stream with operators (tap, map, timeout), i.e. extra logic before and/or after handler execution.
- id: nestjs-guards-04
  answer: |
    The decorator uses SetMetadata('roles', roles) to attach metadata to the route handler (or class). The guard injects Nest's Reflector and reads it back, e.g. this.reflector.get<string[]>('roles', context.getHandler()) — or reflector.getAllAndOverride('roles', [handler, class]) to also consider class-level metadata — then compares the required roles against the authenticated user's roles and returns true/false from canActivate.
- id: nestjs-filters-01
  answer: |
    HttpException is the base exception class Nest recognizes; when thrown, Nest catches it and serializes it into an HTTP response (status code + JSON body with statusCode, message, and optional error). Built-in exceptions — BadRequestException, UnauthorizedException, NotFoundException, ForbiddenException, ConflictException, RequestTimeoutException, InternalServerErrorException, NotImplementedException, BadGatewayException, ServiceUnavailableException, GatewayTimeoutException, etc. — all extend HttpException with their respective status codes (e.g. NotFoundException → 404).
- id: nestjs-filters-02
  answer: |
    Create a class decorated with @Catch() (optionally @Catch(SomeException) to narrow the type), implementing the ExceptionFilter interface with a catch(exception: T, host: ArgumentsHost) method. From host you call host.switchToHttp() to get the Request and Response objects, then build and send the response yourself (e.g. response.status(...).json(...)). Bind it with @UseFilters(MyFilter) on a handler/controller, or register globally via app.useGlobalFilters() or the APP_FILTER token.
- id: nestjs-filters-03
  answer: |
    Exception filters resolve in the opposite order: route-level first → controller-level → global. The filter bound closest to the handler gets the first chance to catch the exception. Register a global filter with app.useGlobalFilters(new AllExceptionsFilter()) in main.ts, or as a DI provider using the APP_FILTER token (which allows dependency injection into the filter).
- id: nestjs-filters-04
  answer: |
    The client receives a 500 Internal Server Error with a JSON body like { "statusCode": 500, "message": "Internal server error" } — the original error's message is not leaked to the client (Nest logs it server-side instead).
- id: nestjs-providers-01
  answer: |
    Use a factory provider whose factory is async: { provide: 'DB_CONNECTION', useFactory: async () => { const conn = await createConnection(opts); return conn; } }, optionally with inject: [...] for the factory's own dependencies. Consume it with @Inject('DB_CONNECTION'). Yes — Nest awaits the promise during module initialization; the application isn't considered ready until the async provider resolves.
- id: nestjs-providers-02
  answer: |
    useValue: supply a fixed, already-created value (constants, config objects, existing third-party instances, or test mocks). useClass: map a token to a class Nest instantiates — used to alias tokens or swap implementations (e.g. a fake in tests or a different driver). useFactory: run a function (with injectable deps, may be async) to compute the value — used when construction needs logic, other providers, or async setup. Pick useValue for statics/mocks, useClass for swappable implementations, useFactory for dynamic/async/dependent construction.
- id: nestjs-providers-03
  answer: |
    ConfigModule.forRoot() loads .env files (dotenv), and exposes ConfigService, which you inject and query with configService.get('KEY') (typed getters, namespaced config via register()). By default you must import ConfigModule in every module that needs ConfigService; with isGlobal: true the module is registered globally so ConfigService is injectable anywhere without importing ConfigModule in each module.
- id: nestjs-providers-04
  answer: |
    Use Test.createTestingModule({ controllers: [...], providers: [...] }).compile() to build the module. Replace a real provider with a mock by declaring it with the same token and useValue: providers: [{ provide: CatsService, useValue: { findAll: jest.fn().mockResolvedValue([...]) } }] (or via .overrideProvider(CatsService).useValue(mock)). Then retrieve instances with moduleRef.get(CatsController) / .get(CatsService), or moduleRef.createTestingApplication()/init() to also run lifecycle hooks.
```
