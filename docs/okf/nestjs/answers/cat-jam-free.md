# cat-jam-free — closed-book answers for nestjs (opencode-go, 2026-10-14); answerer saw only _prompts/nestjs.md
# Delivery: one `ocode run -m opencode-go/cat-jam-free -effort med` session, sheet attached via -f, empty scratch cwd;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: nestjs-modules-01
  answer: |
    @Module() declares a Nest module's scope: imports brings in other modules' exported providers, controllers registers the module's HTTP controllers, providers registers injectable services/factories for DI within the module, and exports exposes selected providers for importing modules. A module is an organizational/DI boundary that groups related functionality and encapsulates its providers by default.

- id: nestjs-modules-02
  answer: |
    AuthService must be exported by AuthModule, and UsersModule must import AuthModule. If either is missing, Nest cannot resolve the dependency and fails to bootstrap with an unknown-dependency error.

- id: nestjs-modules-03
  answer: |
    A dynamic module is a module whose metadata is generated at runtime by a static method (returning a DynamicModule). By convention, forRoot() configures and registers global/singleton setup once at the application root, while forFeature() registers feature-scoped resources (such as entities or handlers) in a feature module without repeating global configuration.

- id: nestjs-modules-04
  answer: |
    @Global() makes the module's exported providers available application-wide without needing imports. It should be used sparingly because it hides dependencies, increases coupling, and can pollute the DI container namespace.

- id: nestjs-di-01
  answer: |
    @Injectable() marks a class as a Nest-managed provider. The class is registered in a module's providers array, then Nest's container reads constructor parameter metadata, recursively resolves dependencies, and creates/injects instances. By default, providers are singletons shared across consumers.

- id: nestjs-di-02
  answer: |
    Register a custom provider with a token, for example { provide: 'CONFIG', useValue: config } or useFactory. Inject it with @Inject('CONFIG') in the constructor. The token can be a string, symbol, or class, and the provider form determines how the injected value is created.

- id: nestjs-di-03
  answer: |
    Nest's scopes are DEFAULT (singleton, one instance per application), REQUEST (new instance per request), and TRANSIENT (a new instance per consumer/injection). DEFAULT is the default and recommended because it minimizes instance creation and overhead.

- id: nestjs-di-04
  answer: |
    A REQUEST-scoped dependency forces its dependents to also become request-scoped, because Nest must create request-specific instances through the dependency chain. This adds per-request instantiation and DI overhead. TRANSIENT does not force consumers to become transient; it provides a fresh instance to each consumer without propagating request scope.

- id: nestjs-routing-01
  answer: |
    @Controller('cats') sets the base route path. Method decorators such as @Get() and @Post() register handlers for HTTP methods and optional subpaths. @Param(), @Query(), and @Body() extract route parameters, query-string values, and request-body data into handler arguments.

- id: nestjs-routing-02
  answer: |
    Declare parameters with colon syntax, such as @Get(':id') or @Get(':catId/toys/:toyId'). In NestJS 11, wildcard routes changed with newer HTTP adapter support; bare '*' wildcards are no longer accepted, and a named wildcard pattern such as '*path' (syntax depends on the adapter) should be used.

- id: nestjs-routing-03
  answer: |
    DTOs are classes because decorators and runtime type metadata are preserved at runtime; TypeScript interfaces are erased during compilation. Pipes such as ValidationPipe and class-transformer need classes to validate and transform payloads.

- id: nestjs-routing-04
  answer: |
    The default status is 200, except POST which defaults to 201. Override it with @HttpCode(). If a handler returns a Promise, Nest awaits it; if it returns an Observable, Nest subscribes and sends the emitted value.

- id: nestjs-lifecycle-01
  answer: |
    OnModuleInit fires during module initialization, after the module's dependencies have been resolved. OnApplicationBootstrap fires after all modules have been initialized, before the application starts listening for incoming connections.

- id: nestjs-lifecycle-02
  answer: |
    Shutdown hooks run as onModuleDestroy, then beforeApplicationShutdown, then onApplicationShutdown. You must enable them with app.enableShutdownHooks() (optionally passing signal names such as SIGTERM/SIGINT) for them to fire on process signals.

- id: nestjs-lifecycle-03
  answer: |
    Destroy hooks run in reverse initialization order: A, then B, then C. To my knowledge, NestJS 11 keeps destruction as reverse topological/initialization order rather than changing it to init order.

- id: nestjs-lifecycle-04
  answer: |
    Initialization hooks are triggered when the application is created/initialized (for example during app.init() or listen()). Yes, Nest awaits async hooks that return a Promise before continuing to the next hook/module phase.

- id: nestjs-validation-01
  answer: |
    ValidationPipe validates incoming request data against DTO decorators and can transform payloads. It relies on class-validator and class-transformer. Apply it globally with app.useGlobalPipes(new ValidationPipe()) or by registering APP_PIPE.

- id: nestjs-validation-02
  answer: |
    whitelist strips properties not declared in the DTO; forbidNonWhitelisted makes the request fail if extra properties are present. They help prevent mass assignment and enforce that payloads contain only expected fields.

- id: nestjs-validation-03
  answer: |
    transform: true converts plain payloads into DTO class instances and transforms values according to decorators/types. For example, a @Param('id') typed as number can be converted from the route string "123" to the number 123.

- id: nestjs-validation-04
  answer: |
    A built-in pipe like ParseIntPipe parses and validates a specific value, converting it to a number and throwing BadRequestException on failure. A global pipe applies automatically to matching route parameters across the app, while a parameter-bound pipe applies only to that one parameter/route.

- id: nestjs-guards-01
  answer: |
    A guard implements the CanActivate interface and its canActivate() method. Returning true allows the request to continue; returning false or throwing results in a forbidden (403 by default) response.

- id: nestjs-guards-02
  answer: |
    The request pipeline runs middleware, then guards, then interceptors (pre-handler), then pipes, then the route handler, then interceptors (post-handler). Exception filters handle errors thrown at any stage. Interceptors wrap around the handler, running logic both before and after it.

- id: nestjs-guards-03
  answer: |
    An interceptor implements NestInterceptor and its intercept(context, next) method, returning an Observable. Interceptors are useful for logging/timing requests and transforming or caching responses.

- id: nestjs-guards-04
  answer: |
    A roles guard reads per-route metadata using the Reflector class, commonly via reflector.getAllAndOverride(Roles, [context.getHandler(), context.getClass()]). The custom @Roles() decorator stores this metadata with SetMetadata.

- id: nestjs-filters-01
  answer: |
    HttpException is Nest's base HTTP exception carrying status code and response details. Built-in exception classes such as BadRequestException and NotFoundException extend it. Throwing one produces an HTTP response with the corresponding status code and a JSON error body.

- id: nestjs-filters-02
  answer: |
    A custom exception filter uses the @Catch() decorator, implements ExceptionFilter, and defines catch(exception, host: ArgumentsHost). It can inspect the exception and use the host to access the request/response and send a custom response.

- id: nestjs-filters-03
  answer: |
    Exception filters resolve from most specific to least specific: route-level, then controller-level, then global. Register a global filter with app.useGlobalFilters() or by providing APP_FILTER.

- id: nestjs-filters-04
  answer: |
    An unhandled plain Error produces a 500 Internal Server Error. Nest's default handling returns a generic 500 JSON response and does not expose the stack trace in production.

- id: nestjs-providers-01
  answer: |
    Create an async provider with useFactory: async (...) => { ... } and optional dependencies via inject. Nest resolves the factory before the application becomes ready, so yes, it waits for the Promise to resolve during bootstrap.

- id: nestjs-providers-02
  answer: |
    useValue registers a fixed value or existing instance; use it for constants, config, or mocks. useClass tells Nest to instantiate a specified class; use it for implementation binding or overriding. useFactory derives a value with custom logic, async work, or dependencies; use it when creation needs computation or injected inputs.

- id: nestjs-providers-03
  answer: |
    ConfigModule exposes ConfigService, whose .get() reads configuration values. isGlobal: true makes ConfigModule's exports available application-wide without importing it into every module.

- id: nestjs-providers-04
  answer: |
    Use Test.createTestingModule({ providers: [...] }) and chain .overrideProvider(RealProvider).useValue(mock) (or useFactory/useClass). Then call compile() and retrieve the service with module.get(Service).
