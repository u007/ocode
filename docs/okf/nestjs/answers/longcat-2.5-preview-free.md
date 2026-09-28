- id: nestjs-modules-01
  answer: |
    The @Module() decorator's metadata declares:
    - imports: other modules whose exported providers this module needs.
    - controllers: the controllers registered in this module.
    - providers: the providers (services, repositories, etc.) that Nest instantiates and manages within this module.
    - exports: the subset of providers that are made available to other modules that import this module.
    A Nest module is a class annotated with @Module() that groups related code (controllers, providers, etc.) into a cohesive, encapsulated unit. It is the fundamental organizational building block of a NestJS application.

- id: nestjs-modules-02
  answer: |
    AuthModule must include AuthService in its exports array, and UsersModule must include AuthModule in its imports array. If you forget either step, Nest's dependency injection container will not be able to resolve AuthService when constructing UsersService, and the application will fail to start with an error like "Nest can't resolve dependencies of the UsersService (?, ...). Please make sure that the argument AuthService at index [0] is available in the UsersModule context."

- id: nestjs-modules-03
  answer: |
    A dynamic module is a module whose providers can be configured at import time by calling a static method on the module class, rather than having fixed providers. The conventional difference:
    - forRoot() is called once, typically in the root AppModule, to configure the module globally and return a module that is shared across the application.
    - forFeature() is called in individual feature modules to register module-specific providers (e.g., repository providers, feature-specific services) that are scoped to that feature.

- id: nestjs-modules-04
  answer: |
    The @Global() decorator marks a module as global, meaning its exported providers are available to all other modules without needing to be imported. It should be used sparingly because it creates implicit, hidden dependencies that reduce modularity, make the code harder to reason about, and can lead to tight coupling between unrelated parts of the application.

- id: nestjs-di-01
  answer: |
    Nest's dependency injection resolves a class dependency through these steps:
    1. @Injectable() marks a class as a provider that can be managed by Nest's IoC container.
    2. The provider is registered in a module's providers array (or via a custom provider).
    3. When a class is instantiated, Nest reads the constructor's parameter types via TypeScript's emitted metadata (design:paramtypes).
    4. Nest looks up each parameter type in the container and injects the corresponding instance.
    For example, constructor(private catsService: CatsService) tells Nest to inject the CatsService singleton.

- id: nestjs-di-02
  answer: |
    You register a non-class value using a custom token (a string or Symbol) with one of the custom provider forms:
    - useValue: { provide: TOKEN, useValue: someValue }
    - useFactory: { provide: TOKEN, useFactory: () => someValue }
    - useClass: { provide: TOKEN, useClass: SomeClass }
    Then you inject it using @Inject(TOKEN) in the constructor. For example: constructor(@Inject('CONFIG') private config: ConfigOptions).

- id: nestjs-di-03
  answer: |
    Nest's three injection scopes are:
    - SINGLETON (default): one instance shared across the entire application. Recommended because it is the most memory-efficient and most services are stateless.
    - REQUEST: a new instance is created for each incoming request and garbage-collected after the request completes.
    - TRANSIENT: a new instance is created each time the provider is injected into any consumer.
    SINGLETON is the default and is recommended for most cases because it minimizes memory allocation and is suitable for stateless services.

- id: nestjs-di-04
  answer: |
    When a provider is REQUEST-scoped, any provider that depends on it (directly or transitively) also becomes REQUEST-scoped. This is called "scope bubbling." The performance implication is that more instances are created per request, increasing memory and GC pressure. TRANSIENT does NOT behave the same way — a transient provider does not force its dependencies to become transient; each consumer gets its own instance of the transient provider, but the scope of its dependencies is unaffected.

- id: nestjs-routing-01
  answer: |
    @Controller('cats') sets the base path prefix for all routes in that controller (e.g., /cats). The method decorators map HTTP requests:
    - @Get() handles GET requests to the base path.
    - @Post() handles POST requests to the base path.
    - @Param() extracts route parameters (e.g., @Param('id') gets the id from /cats/:id).
    - @Query() extracts query string parameters (e.g., @Query('name') gets ?name=...).
    - @Body() extracts the parsed request body.

- id: nestjs-routing-02
  answer: |
    A route with a path parameter is declared like @Get(':id'). A nested sub-path is declared like @Get('users/:id/posts'). In NestJS 11, wildcard routes changed due to an update to the path-to-regexp library: wildcards must now be named (e.g., @Get('*splat')) or use a different syntax, and the old unqualified * wildcard behavior is no longer supported in the same way.

- id: nestjs-routing-03
  answer: |
    Request DTOs are defined as classes rather than TypeScript interfaces because interfaces are erased at compile time and do not exist at runtime. NestJS relies on runtime metadata (emitted by TypeScript) to perform validation with class-validator and transformation with class-transformer. Classes preserve this metadata at runtime, enabling decorators like @IsString() and @IsNumber() to work.

- id: nestjs-routing-04
  answer: |
    The default HTTP status code is 200 for GET requests and 201 for POST requests. You override it with the @HttpCode() decorator (e.g., @HttpCode(204)). When a handler returns a Promise, Nest awaits it and sends the resolved value. When a handler returns an Observable, Nest subscribes to it and uses the last emitted value as the response.

- id: nestjs-lifecycle-01
  answer: |
    OnModuleInit fires after the module's providers have been instantiated and the module is initialized, but before the entire application is fully ready. OnApplicationBootstrap fires after all modules have been initialized and the application is fully bootstrapped (after all OnModuleInit hooks have completed across all modules). OnModuleInit is per-module; OnApplicationBootstrap is application-wide.

- id: nestjs-lifecycle-02
  answer: |
    The shutdown lifecycle hooks in order are:
    1. OnModuleDestroy
    2. BeforeApplicationShutdown
    3. OnApplicationShutdown
    You must call app.enableShutdownHooks() on the application instance for these hooks to fire on SIGTERM/SIGINT signals.

- id: nestjs-lifecycle-03
  answer: |
    Destroy hooks run in the reverse order of init hooks. If init runs C → B → A (dependency order), destroy runs A → B → C. This did NOT change in NestJS 11; the reverse-order behavior for destroy hooks has been consistent.

- id: nestjs-lifecycle-04
  answer: |
    The init lifecycle hooks (OnModuleInit and OnApplicationBootstrap) are triggered when the application starts — specifically when app.init() or app.listen() is called. Yes, Nest waits for an async hook (one returning a Promise) to resolve before continuing with the initialization of subsequent modules or the application bootstrap.

- id: nestjs-validation-01
  answer: |
    The built-in ValidationPipe validates incoming request data against DTO classes using the class-validator library for validation rules and the class-transformer library for object transformation. To apply it globally, you register it in the main.ts bootstrap file: app.useGlobalPipes(new ValidationPipe()).

- id: nestjs-validation-02
  answer: |
    - whitelist: when true, ValidationPipe strips any properties from the incoming payload that do not have validation decorators in the DTO, preventing mass-assignment vulnerabilities.
    - forbidNonWhitelisted: when true, ValidationPipe throws an error (BadRequestException) if the payload contains properties not defined in the DTO, rather than silently stripping them.
    Use them together for strict, secure input validation.

- id: nestjs-validation-03
  answer: |
    transform: true instructs ValidationPipe to automatically transform the incoming payload to match the DTO type using class-transformer. For example, @Param('id') typed as number will have its string value from the URL automatically converted to a JavaScript number. Without transform, the value would remain a string.

- id: nestjs-validation-04
  answer: |
    ParseIntPipe is a built-in pipe that parses a string parameter (e.g., a route parameter) into an integer, throwing a BadRequestException if the value cannot be parsed. A globally-registered pipe applies to all routes in the application, while a pipe bound to a single route parameter (e.g., @Param('id', ParseIntPipe)) applies only to that specific parameter.

- id: nestjs-guards-01
  answer: |
    A guard is a class that implements the CanActivate interface. Its canActivate() method receives the execution context and returns a boolean (or a Promise/Observable of boolean). If it returns true, the request proceeds to the next stage. If it returns false, the request is denied and a ForbiddenException (403) is typically thrown.

- id: nestjs-guards-02
  answer: |
    The request pipeline order is:
    1. Middleware
    2. Guards
    3. Interceptors (pre-handler / before)
    4. Pipes
    5. Route handler
    6. Interceptors (post-handler / after)
    7. Exception filters (if an error occurs)
    Interceptors run both before and after the route handler — they wrap the handler execution.

- id: nestjs-guards-03
  answer: |
    An interceptor is a class that implements the NestInterceptor interface with an intercept() method that receives the execution context and a next handler. Two things interceptors are good for:
    1. Logging or measuring request/response timing.
    2. Transforming the response data or adding metadata to responses.
    Other common uses: caching, error mapping, and request/response manipulation.

- id: nestjs-guards-04
  answer: |
    A roles guard reads per-route metadata using the Reflector class. The guard injects Reflector and calls reflector.get<string[]>('roles', context.getHandler()) to retrieve the metadata value. The @Roles('admin') custom decorator uses SetMetadata('roles', roles) to attach the metadata to the handler. The guard then compares the user's roles against the required roles.

- id: nestjs-filters-01
  answer: |
    HttpException is the base class for all HTTP exceptions in NestJS. Built-in exception classes like NotFoundException, BadRequestException, UnauthorizedException, etc., all extend HttpException. Throwing one produces a JSON response body containing statusCode, message, and error fields with the appropriate HTTP status code.

- id: nestjs-filters-02
  answer: |
    To write a custom exception filter:
    - Use the @Catch() decorator (optionally passing the exception class to catch).
    - Implement the ExceptionFilter interface.
    - The catch() method receives two arguments: the exception instance and an ArgumentsHost object (which provides access to the request/response objects).
    Example: catch(exception: HttpException, host: ArgumentsHost) { ... }

- id: nestjs-filters-03
  answer: |
    Exception filters resolve in the reverse order of other components: route-level → controller-level → global. This is the opposite of guards, interceptors, and pipes, which resolve global → controller → route. You register a global filter using app.useGlobalFilters(new MyFilter()) in the bootstrap.

- id: nestjs-filters-04
  answer: |
    If your code throws a plain Error (not an HttpException) and no custom filter handles it, the client receives a 500 Internal Server Error response with a generic JSON body: { "statusCode": 500, "message": "Internal server error" }.

- id: nestjs-providers-01
  answer: |
    You create an async provider using useFactory with an async function:
    { provide: 'DB_CONNECTION', useFactory: async () => { const conn = await createConnection(); return conn; } }
    Yes, Nest waits for the Promise to resolve before considering the application ready. The app will not start listening until all async providers have resolved.

- id: nestjs-providers-02
  answer: |
    - useValue: provides a static, pre-computed value. Use when the value is already available and doesn't need to be constructed (e.g., a config object or a constant).
    - useClass: tells Nest to instantiate a specific class when a token is requested. Use when you want to substitute one implementation for another (e.g., a mock or alternative service).
    - useFactory: calls a function to produce the value. Use when the value requires complex construction logic, depends on other providers, or needs to be computed dynamically.

- id: nestjs-providers-03
  answer: |
    ConfigModule from @nestjs/config loads environment variables from .env files and exposes them through ConfigService, which has get() and getOrThrow() methods to retrieve config values. Setting isGlobal: true makes ConfigModule available throughout the application without needing to import it in every module.

- id: nestjs-providers-04
  answer: |
    Using @nestjs/testing, you build a module for a unit test with:
    const module = await Test.createTestingModule({ providers: [MyService] }).compile();
    To replace a real provider with a mock, use:
    .overrideProvider(MyService).useValue(mockService)
    or
    .overrideProvider(MyService).useClass(MockService)
    before calling .compile(). Then retrieve the provider with module.get(MyService).
