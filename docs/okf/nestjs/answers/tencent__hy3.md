# nestjs knowledge benchmark — answers
# Model: novita-ai/tencent/hy3   (provider-stripped id: tencent/hy3)
# CLOSED-BOOK via ocode run -dir <empty> (no corpus access). corpus_rev 1.

```yaml
- id: nestjs-modules-01
  answer: |
    A Nest module (decorated with @Module()) is a class that groups related
    providers, controllers, and other modules into a cohesive, encapsulated unit
    — it is the fundamental organizing structure of a Nest application and the
    unit of code splitting / lazy loading.

    The @Module() metadata object declares:
      - imports: other modules whose exported providers this module wants to use.
      - controllers: the controllers instantiated by (and scoped to) this module.
      - providers: the providers (services, repositories, etc.) instantiated and
        injected within this module.
      - exports: a subset of this module's providers (or imported modules) that
        are made visible to other modules that import this one.

- id: nestjs-modules-02
  answer: |
    Modules are encapsulated: a provider created in AuthModule is only visible
    inside AuthModule unless it is explicitly exported. For UsersModule to inject
    AuthService, two things must be true: (1) AuthModule must list AuthService in
    its exports array, and (2) UsersModule must list AuthModule in its imports
    array.

    If you forget either step, Nest cannot resolve the dependency and throws a
    "Nest can't resolve dependencies" error at bootstrap (or when the provider is
    first constructed), citing a potential circular dependency or a missing
    provider/export. The provider must be exported by its owning module and that
    module must be imported where it is consumed.

- id: nestjs-modules-03
  answer: |
    A dynamic module is a module that returns its configuration at runtime via a
    static method that calls Module.forRoot()/Module.forFeature() and returns a
    DynamicModule object (an object with { module, imports, providers, exports,
    global? }). This lets the module accept options, register providers
    conditionally, and return different metadata per call, instead of having fixed
    @Module() metadata.

    Convention:
      - forRoot() (or forRootAsync()) is typically called once in the root/App
        module to register global, application-wide configuration and providers
        (often using @Global so consumers don't re-import it).
      - forFeature() is called in feature modules to register providers scoped to
        that feature (e.g., a repository for a specific entity/model), without
        re-initializing global config.

- id: nestjs-modules-04
  answer: |
    @Global() marks a module as global, so its exported providers are available
    everywhere in the application without needing to be listed in each consuming
    module's imports array.

    It should be used sparingly because it defeats module encapsulation and makes
    dependency wiring implicit/harder to trace, increases coupling, and can mask
    missing imports. It is typically reserved for truly app-wide singletons such
    as ConfigModule or a database connection module registered once via forRoot().

- id: nestjs-di-01
  answer: |
    Nest's DI is built on TypeScript's emitted design-time type metadata
    (reflect-metadata).

    1. @Injectable() marks a class as a provider eligible for DI and adds metadata
       so Nest can read the types declared in its constructor parameters.
    2. Provider registration: the class is listed in the `providers` array of a
       module (e.g., providers: [CatsService]). Under the hood this is shorthand
       for { provide: CatsService, useClass: CatsService }, creating a token
       (the class itself) bound to a factory that instantiates the class.
    3. Constructor injection: a consumer declares the dependency in its
       constructor (constructor(private cats: CatsService) {}). At runtime Nest
       resolves the token CatsService from the provider registry, constructs the
       instance (singleton by default), and passes it in. If the provider is not
       registered/visible, Nest throws a resolution error.

- id: nestjs-di-02
  answer: |
    For non-class values you register a provider using a custom injection token
    (a string or a Symbol / InjectionToken) with useValue (or useFactory), e.g.:

      providers: [{ provide: 'CONFIG', useValue: { apiUrl: '...' } }]

    Then inject it with @Inject('CONFIG') on the constructor parameter:

      constructor(@Inject('CONFIG') private config: Config) {}

    Nest also supports the typed InjectionToken class
    (new InjectionToken('CONFIG')) for better type safety. The @Inject decorator
    tells the injector which token to look up, since the type is not a class.

- id: nestjs-di-03
  answer: |
    Nest has three provider scopes (set via the `scope` option on a provider /
    @Injectable({ scope: Scope.X })):
      - DEFAULT (singleton): one instance per the lifetime of the application,
        shared across all consumers.
      - REQUEST: a new instance is created for each incoming request, and the
        instance is destroyed when the request completes.
      - TRANSIENT: a new instance is created each time the provider is injected /
        resolved.

    DEFAULT/singleton is the default and is recommended because it is the most
    memory- and performance-efficient (no per-request allocation/GC overhead) and
    is the simplest mental model. Request/transient scopes should be used only
    when a provider genuinely needs per-request or per-consumer state.

- id: nestjs-di-04
  answer: |
    Because of the resolution rule, a provider can only be injected into a
    consumer that lives at least as long as it does. If a singleton (DEFAULT)
    provider depends on a REQUEST-scoped provider, the singleton would hold a
    reference to an instance that is destroyed at the end of each request, which
    is invalid. To keep things consistent, Nest forces the consuming singleton to
    also become REQUEST-scoped. This cascades: any singleton that injects it also
    becomes request-scoped, so a single request-scoped dependency can "infect" an
    otherwise-singleton graph, multiplying per-request object creation.

    Performance implication: you lose the singleton benefit and allocate/instantiate
    many more objects per request, plus request-scoped providers can't use some
    optimizations and may be harder to test.

    TRANSIENT does NOT behave the same way: a singleton can safely inject a
    TRANSIENT provider — each injection point gets its own transient instance, but
    the singleton consumer remains a singleton. Transient injection does not force
    the consumer to change scope.

- id: nestjs-routing-01
  answer: |
    @Controller('cats') sets the path prefix for every route handler in that
    controller, so a handler decorated @Get() inside it maps to GET /cats, @Get('id')
    maps to GET /cats/id, etc.

    - @Get()/@Post() (and @Put, @Patch, @Delete, @All) bind a handler to an HTTP
      method and an optional sub-path, producing the full route
      (prefix + sub-path).
    - @Param() extracts route path parameters into a named argument
      (@Param('id') id: string).
    - @Query() extracts the URL query string parameters (?foo=bar).
    - @Body() extracts and parses the request body (typically JSON) into the
      argument.

    Together they map an incoming HTTP request (method + URL + params + query +
    body) onto a typed handler function.

- id: nestjs-routing-02
  answer: |
    Path parameters are declared with a colon prefix inside the path, e.g.
    @Get(':id') or @Get('cats/:catId/owners/:ownerId') for a nested sub-path. The
    values are then read via @Param('catId'), @Param('ownerId').

    Wildcard routes changed in NestJS 11: the legacy standalone `*` wildcard
    segment (e.g. @Get('*') or @Get('ab*')) was removed/deprecated when Nest moved
    to the newer path matching. You must now express catch-alls with a named
    wildcard parameter, e.g. @Get('*splat') / a parameter suffixed with `*`, or a
    regex-style catch-all such as '(.*)'. In short: bare `*` in a route path is no
    longer supported in v11 and must be replaced with a named wildcard parameter.

- id: nestjs-routing-03
  answer: |
    Request DTOs are defined as classes (not interfaces) because TypeScript
    interfaces are erased at runtime — they produce no JS output, so there is no
    runtime type/constructor to reflect on. Nest relies on runtime type metadata
    (reflect-metadata) and class-transformer / class-validator, which operate on
    actual class constructors and instances. A class survives compilation, so
    ValidationPipe and PipeTransform can instantiate it, read its property
    decorators (e.g. @IsString()), validate, and transform the payload into a real
    object. Interfaces cannot be used for this runtime validation/transformation
    step.

- id: nestjs-routing-04
  answer: |
    - Default status code: 200 OK for successful GET/POST/etc. (201 Created is
      returned automatically for successful POST only when the handler has no
      explicit response and the route uses the default; more precisely the default
      is 200 for all, and 201 for POST is applied by the framework by default as
      well — historically POST defaults to 201). The safe statement: the default
      is 200 OK (and 201 for POST by default).
    - Override: set the status on the response object
      (@Res() / response.status(204).send()) or use the @HttpCode(204) decorator
      on the handler.
    - Promises/observables: Nest awaits a returned Promise and serializes the
      resolved value as the response body; it also subscribes to a returned
      RxJS Observable, takes its emitted value (the first emitted value, then
      completes) and sends that as the body. You do not need to manually await in
      the handler.

- id: nestjs-lifecycle-01
  answer: |
    Both are lifecycle interfaces you implement on a provider/module.
      - OnModuleInit: onModuleInit() is called once the host module's
        dependencies have been resolved and its providers are ready, but BEFORE
        the application has fully started listening. It runs during the init
        phase, module by module, in dependency order.
      - OnApplicationBootstrap: onApplicationBootstrap() is called after all
        modules have initialized and the application has fully started (listening
        for connections). It is the last hook before the app is "ready."

    So OnModuleInit fires earlier (per-module, as that module comes up) and
    OnApplicationBootstrap fires once, after everything is bootstrapped.

- id: nestjs-lifecycle-02
  answer: |
    Shutdown hooks, in order:
      1. onModuleDestroy()  (OnModuleDestroy interface)
      2. beforeApplicationShutdown()  (BeforeApplicationShutdown)
      3. onApplicationShutdown()  (OnApplicationShutdown)

    These fire on SIGTERM / SIGINT only if you enable shutdown hooks, which is
    done by calling app.enableShutdownHooks() on the Nest application instance
    (before listen). Without enableShutdownHooks(), the process terminates
    immediately and these hooks do NOT run.

- id: nestjs-lifecycle-03
  answer: |
    Initialization hooks run in topological/dependency order (dependencies before
    the things that depend on them): so C (a dependency) → B → A (top consumer).
    Destroy hooks run in the REVERSE order, i.e. A → B → C, so the top-level
    dependents are torn down before the dependencies they rely on.

    This did change in NestJS 11: prior versions ran the shutdown/destroy hooks in
    the same order as initialization, which was effectively a bug; NestJS 11 fixed
    the ordering so destroy/onModuleDestroy/beforeApplicationShutdown run in
    reverse (topological teardown) order — i.e., the reversal was corrected in v11.

- id: nestjs-lifecycle-04
  answer: |
    The init lifecycle hooks are triggered automatically by Nest during the
    application bootstrap, when Nest calls app.init() (and internally as each
    module's providers are wired up). They are not triggered by a request; they
    are part of startup (and, for destroy hooks, shutdown).

    Yes — Nest awaits async hooks. If onModuleInit() / onApplicationBootstrap()
    (or the destroy hooks) return a Promise, Nest awaits it before moving on to
    the next module/hook, so the next step does not begin until the async work
    completes. This is why you can safely do async setup (e.g. opening a
    connection) in these hooks.

- id: nestjs-validation-01
  answer: |
    The built-in ValidationPipe validates and (optionally) transforms incoming
    request payloads against class-based DTOs using decorators. It relies on two
    libraries: class-validator (the decorators like @IsString(), @IsEmail() that
    define the rules) and class-transformer (to convert plain objects into
    class instances and to perform type transformation when transform:true).

    Apply it app-wide by registering it as a global pipe in main.ts:
      app.useGlobalPipes(new ValidationPipe())
    (or via APP_PIPE provider for DI-aware global registration). You can also
    bind it at the controller or route-param level.

- id: nestjs-validation-02
  answer: |
    - whitelist: true — strips any properties on the incoming object that are NOT
      decorated with a validation decorator in the DTO class (only whitelisted
      properties are kept). This prevents unknown/extra fields from reaching your
      handler.
    - forbidNonWhitelisted: true — instead of silently stripping unknown
      properties, it throws a validation error if any non-whitelisted property is
      present.

    Use them for security and robustness: to enforce a strict input contract,
    reject or drop unexpected fields (defense against mass-assignment / over-
    posting), and keep handlers working only with the intended shape.

- id: nestjs-validation-03
  answer: |
    transform: true makes ValidationPipe use class-transformer to convert the
    incoming plain object into an instance of the DTO class and to coerce values
    to the declared property types. So a DTO field typed as `number` will have
    its string value converted to a real number, and the handler receives a DTO
    instance (with methods, if any) rather than a plain object.

    For a route parameter like @Param('id') typed as number, with transform:true
    the ValidationPipe (or the param-level pipe) coerces the string path segment
    (URL params are always strings) into a JavaScript number before it reaches
    the handler. (For explicit integer coercion people usually also use
    ParseIntPipe, but transform:true handles the type coercion via class-
    transformer when the DTO/param type is number.)

- id: nestjs-validation-04
  answer: |
    A built-in pipe like ParseIntPipe parses and validates a single value,
    throwing a BadRequestException if the value cannot be converted to an integer
    (e.g. 'abc'). Other built-ins include ParseUUIDPipe, ParseBoolPipe,
    ParseFloatPipe, ParseArrayPipe, ParseEnumPipe. They are focused, single-value
    transforms/validators rather than whole-DTO validators.

    Practical difference between a globally-registered pipe and one bound to a
    single route parameter: a global pipe (useGlobalPipes) runs for every route
    and every argument unless opted out, applying the same options everywhere; a
    route-parameter-bound pipe (e.g. @Param('id', ParseIntPipe)) runs only for
    that specific parameter on that specific handler, letting you apply targeted
    parsing/validation exactly where needed without affecting the rest of the app.
    Global pipes also don't participate in DI unless registered via the APP_PIPE
    provider, whereas inline pipes are instantiated per use.

- id: nestjs-guards-01
  answer: |
    A guard is a class that determines whether a given request is allowed to
    proceed to the route handler — it implements the CanActivate interface and
    provides canActivate(context): boolean | Promise<boolean> | Observable<boolean>.

    The return value controls the request: returning true allows the request to
    pass to the handler; returning false (or throwing) denies it, and Nest
    short-circuits with a 403 Forbidden (or whatever the guard throws). Guards
    typically inspect the request/user via the ExecutionContext (e.g. reading the
    JWT from the request) to make the authorization decision.

- id: nestjs-guards-02
  answer: |
    The request processing pipeline order is:
      middleware → guards → interceptors (pre) → pipes → route handler →
      interceptors (post, after handler returns) → exception filters (only if an
      error/exception was thrown).

    Interceptors wrap the route handler: they run BOTH before the handler
    executes (the "pre" side, around the call) and after it returns (the "post"
    side, where they can transform the result or handle the returned observable).
    So interceptors sit around the handler, executing logic on the way in and on
    the way out, whereas guards run strictly before the handler and pipes run just
    before the handler receives its validated arguments.

- id: nestjs-guards-03
  answer: |
    An interceptor is a class implementing the NestInterceptor interface, which
    provides intercept(context, next): Observable<any> and calls
    next.handle() to delegate to the downstream handler (and receive its
    observable).

    Two common uses:
      - Transforming the response: mapping/modifying the value returned by the
        handler (e.g. wrapping it in { data: ... }, stripping fields).
      - Cross-cutting concerns around the call: logging/timing the request,
        caching responses, binding extra context, error handling via the
        observable, or adding headers/metadata. (Other good answers: request
        mutation, timeout/retry, exception translation.)

- id: nestjs-guards-04
  answer: |
    The custom decorator @Roles('admin') is implemented with SetMetadata, e.g.
    SetMetadata('roles', roles), which attaches the value to the route handler's
    metadata. The roles guard reads that metadata at runtime using the Reflector
    class (a built-in Nest provider): it calls reflector.get<string[]>('roles',
    context.getHandler()) (optionally also checking the controller via
    context.getClass()) to obtain the required roles, then compares them against
    the current user's roles from the request. The guard uses Reflector's get/
    getAllAndOverride to retrieve the metadata set by the decorator.

- id: nestjs-filters-01
  answer: |
    HttpException is the base class for all HTTP-exception types thrown in Nest.
    The built-in exception classes (BadRequestException, UnauthorizedException,
    ForbiddenException, NotFoundException, ConflictException, GoneException,
    PayloadTooLargeException, UnsupportedMediaTypeException,
    InternalServerException, etc. — the full HTTP status series) all extend
    HttpException and simply preset a specific HTTP status code and default
    message.

    Throwing one (e.g. throw new NotFoundException()) immediately terminates the
    handler and is caught by Nest's exception layer, which sends an HTTP response
    with the corresponding status code and a JSON error body of the form
    { "statusCode": ..., "message": ..., "error": ... }.

- id: nestjs-filters-02
  answer: |
    A custom exception filter is written by implementing the ExceptionFilter
    interface, decorated with @Catch(...), and providing a catch(exception, host)
    method.

      - Decorator: @Catch(ExceptionType) (or @Catch() to catch everything).
      - Interface: ExceptionFilter.
      - catch receives two arguments: the exception that was thrown, and an
        ArgumentsHost (host) which gives access to the underlying request/response
        objects (via host.switchToHttp().getResponse() / getRequest()) so you can
        formulate a custom response.

- id: nestjs-filters-03
  answer: |
    Exception filters resolve in the SAME layered order as guards/interceptors/
    pipes: global → controller → route. The most specific (route-level) filter
    takes precedence over controller-level, which takes precedence over the global
    one. (So the resolution order is global → controller → route, with route
    winning.)

    Register a global filter with app.useGlobalFilters(new MyFilter()) in main.ts
    (or, for DI-aware registration, provide it via the APP_FILTER provider token
    in a module's providers).

- id: nestjs-filters-04
  answer: |
    If your code throws a plain Error (not an HttpException) and no custom filter
    handles it, Nest's built-in default exception filter catches it and responds
    with HTTP 500 Internal Server Error. In development mode the response body
    includes the error message and stack; in production it returns a generic
    message (typically { "statusCode": 500, "message": "Internal server error" })
    without leaking internal details.

- id: nestjs-providers-01
  answer: |
    Create an async provider with the useFactory form, where the factory function
    is async and returns the value (e.g. a DB connection):

      {
        provide: 'DB_CONNECTION',
        useFactory: async () => { return await createConnection(...) },
      }

    (Optionally inject other providers into the factory via the `inject` array.)
    Nest DOES wait for it: during module initialization Nest awaits providers that
    return promises (the async factory), so the module — and the application as a
    whole — is not considered ready / onModuleInit does not run until the promise
    resolves. If it rejects, the app fails to bootstrap.

- id: nestjs-providers-02
  answer: |
    - useValue: binds a token to a fixed, already-created value/instance
      (e.g. a config object, a constant, or a mock). Use it for static values and
      constant tokens.
    - useClass: binds a token to a class that Nest will instantiate as the
      implementation (often used for substituting an implementation, e.g.
      provide: IStorage, useClass: S3Storage). Use it for class-based providers
      where you want DI/construction and possibly a different concrete class than
      the token.
    - useFactory: binds a token to a value produced by a factory function, which
      can be async, can take injected dependencies (via `inject`), and can compute
      the value conditionally. Use it when construction needs logic, config, async
      work, or depends on other providers (e.g. building a client/connection).

- id: nestjs-providers-03
  answer: |
    @nestjs/config's ConfigModule loads environment variables (via dotenv) and
    exposes them through the ConfigService provider. You inject ConfigService and
    call get<T>('KEY') (or getOrThrow) to read configuration values. You typically
    register it with ConfigModule.forRoot() in the root module, optionally passing
    options like envFilePath, ignoreEnvFile, validationSchema, isGlobal, etc.

    isGlobal: true makes ConfigModule global, so ConfigService is available in
    every module without re-importing ConfigModule. Without isGlobal, each module
    that needs ConfigService must import ConfigModule (or the module that exports
    it) explicitly.

- id: nestjs-providers-04
  answer: |
    Using @nestjs/testing's Test class, build a testing module and override the
    real provider:

      const moduleRef = await Test.createTestingModule({
        controllers: [CatsController],
        providers: [CatsService],
      })
        .overrideProvider(CatsService)
        .useValue(mockCatsService)   // or .useClass(...), .useFactory(...)
        .compile();

      const controller = moduleRef.get(CatsController);
      const service = moduleRef.get(CatsService); // the mock

    You can also call .overrideGuard(), .overrideInterceptor(), .overridePipe(),
    and .overrideFilter() on the testing module builder. After compile(), use
    moduleRef.get(Token) to retrieve instances. This lets you replace a real
    provider with a mock object (e.g. with jest.fn() methods) so unit tests don't
    touch real dependencies.
```
