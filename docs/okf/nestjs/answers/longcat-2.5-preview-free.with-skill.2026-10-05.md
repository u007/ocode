- id: nestjs-modules-01
  answer: |
    The @Module() decorator's metadata declares:
    - imports: other modules whose exported providers this module needs
    - controllers: the controllers defined in this module
    - providers: injectable classes (services, repositories, etc.) registered in this module's DI container
    - exports: providers from this module that are made available to other modules that import it

    A Nest module is a class annotated with @Module() that organizes related components (controllers, providers, etc.) into a cohesive, encapsulated unit. It is the fundamental building block for structuring a NestJS application.

- id: nestjs-modules-02
  answer: |
    AuthModule must include AuthService in its exports array, and UsersModule must include AuthModule in its imports array. If you forget to export AuthService from AuthModule, or forget to import AuthModule in UsersModule, Nest's DI container will not be able to resolve the dependency and will throw an error at runtime: "Nest can't resolve dependencies of the UsersService (?). Please make sure that the argument AuthService at index [0] is available in the UsersModule context."

- id: nestjs-modules-03
  answer: |
    A dynamic module is a module whose configuration can be customized at registration time by passing options to a static method. It is created by defining a static method on the module class that returns a DynamicModule (a Module object with a module property pointing back to the class).

    The conventional difference:
    - forRoot(): called once, typically in the root AppModule, to configure a module globally. It often returns a global module with providers that should be shared across the entire application.
    - forFeature(): called in feature modules to register module-specific providers (e.g., repository providers, feature-specific services) that are scoped to that feature module.

- id: nestjs-modules-04
  answer: |
    The @Global() decorator marks a module as global, meaning its exported providers are available to all other modules without needing to explicitly import the module. It should be used sparingly because it reduces modularity, makes dependencies less explicit and harder to trace, and can lead to tight coupling between unrelated parts of the application.

- id: nestjs-di-01
  answer: |
    Nest's dependency injection resolves a class dependency through the following steps:
    1. @Injectable() decorator marks a class as a provider that can be managed by Nest's DI container.
    2. The provider is registered in a module's providers array (or via a custom provider).
    3. When a class is instantiated, Nest inspects its constructor parameters. For each parameter, it looks up the corresponding provider in the DI container by type (or by custom token) and injects the instance.
    4. Nest creates and caches the provider instance (for singleton scope) and recursively resolves its dependencies first.

- id: nestjs-di-02
  answer: |
    You register a non-class value using a custom token with one of the custom provider forms:
    - useValue: { provide: TOKEN, useValue: someValue }
    - useFactory: { provide: TOKEN, useFactory: () => someValue }
    - useClass: { provide: TOKEN, useClass: SomeClass }

    You inject it using the @Inject() decorator on the constructor parameter: constructor(@Inject(TOKEN) private readonly config: ConfigType) {}

- id: nestjs-di-03
  answer: |
    Nest's three injection scopes:
    - SINGLETON (default): one instance shared across the entire application. Created once and reused.
    - REQUEST: a new instance is created for each incoming request and garbage-collected after the request completes.
    - TRANSIENT: a new instance is created each time the provider is injected into any consumer.

    SINGLETON is the default and recommended because it is the most performant (no per-request instantiation) and most services are stateless, making a shared instance safe.

- id: nestjs-di-04
  answer: |
    Making one provider REQUEST-scoped causes its dependencies to also become request-scoped (scope "bubbles up" the dependency chain). This means every dependency in the chain gets a new instance per request, which has a performance cost due to increased object creation and garbage collection.

    TRANSIENT does NOT behave the same way. A transient provider creates a new instance each time it is injected, but it does not force its dependencies to change scope. The dependencies retain their own scope.

- id: nestjs-routing-01
  answer: |
    @Controller('cats') sets the base path prefix for all routes in that controller (e.g., /cats).

    - @Get() / @Post(): map HTTP GET/POST requests to the decorated handler method.
    - @Param(): extracts route parameters (e.g., @Param('id') gets the id from /cats/:id).
    - @Query(): extracts query string parameters (e.g., @Query('name') gets ?name=value).
    - @Body(): extracts the request body (typically JSON payload).

- id: nestjs-routing-02
  answer: |
    A route with a path parameter and nested sub-path is declared like: @Get(':id/details') or @Get('sub/:id'). The path parameter is denoted by a colon prefix.

    In NestJS 11, wildcard routes changed due to an update to the path-to-regexp library. The new version has stricter matching rules and different wildcard syntax behavior, which can affect routes using wildcards (*).

- id: nestjs-routing-03
  answer: |
    Request DTOs are defined as classes rather than TypeScript interfaces because interfaces are erased at compile time and do not exist at runtime. Classes persist at runtime, allowing NestJS to use them for validation with class-validator decorators and for transformation with class-transformer. This enables runtime metadata reflection for validation.

- id: nestjs-routing-04
  answer: |
    The default HTTP status code is 200 for GET requests and 201 for POST requests. You override it with the @HttpCode() decorator (e.g., @HttpCode(204)).

    Nest handles a handler that returns a Promise by awaiting it before sending the response. For Observables, Nest subscribes to them and uses the last emitted value as the response.

- id: nestjs-lifecycle-01
  answer: |
    OnModuleInit fires when the module's dependencies have been resolved, before the application starts listening. It is called once per module during the initialization phase.

    OnApplicationBootstrap fires after all modules have been initialized (all OnModuleInit hooks have completed), just before the application starts listening for incoming requests. It is the final initialization hook.

- id: nestjs-lifecycle-02
  answer: |
    The shutdown lifecycle hooks in order are:
    1. onModuleDestroy()
    2. beforeApplicationShutdown(signal?)
    3. onApplicationShutdown(signal?)

    For these hooks to fire on OS signals (SIGTERM/SIGINT), you must call app.enableShutdownHooks() in your application bootstrap code.

- id: nestjs-lifecycle-03
  answer: |
    In NestJS 11, destroy hooks run in the reverse order of the init hooks. If initialization runs C → B → A (dependency order), then destroy runs A → B → C. This is a change introduced in NestJS 11; in previous versions the order was not guaranteed to be reversed.

- id: nestjs-lifecycle-04
  answer: |
    Init lifecycle hooks are triggered by app.init(). The app.listen() method calls app.init() internally if it has not already been called. They are not triggered by "application start" in general.

    Yes, Nest awaits a hook that returns a Promise (or is async) before moving to the next phase. This ensures asynchronous initialization completes before the application proceeds.

- id: nestjs-validation-01
  answer: |
    The built-in ValidationPipe validates incoming request data against DTO classes using decorators from class-validator. It relies on two libraries: class-validator (for validation decorators) and class-transformer (for transforming plain objects to class instances).

    You apply it to the whole app by registering it globally: app.useGlobalPipes(new ValidationPipe()) in your main.ts bootstrap function.

- id: nestjs-validation-02
  answer: |
    - whitelist: when true, ValidationPipe strips any properties from the payload that do not have validation decorators in the DTO class. This prevents mass-assignment vulnerabilities.
    - forbidNonWhitelisted: when true, ValidationPipe throws an error (BadRequestException) if the payload contains properties not defined in the DTO, rather than silently stripping them.

    Use them for security: whitelist prevents unwanted properties from being accepted, and forbidNonWhitelisted gives you strict control over what data is accepted.

- id: nestjs-validation-03
  answer: |
    transform: true on the ValidationPipe causes it to automatically transform the incoming payload to an instance of the DTO class and perform type conversion based on TypeScript type metadata. For example, a @Param('id') typed as number will be automatically converted from the string "42" to the number 42, without needing a separate ParseIntPipe.

- id: nestjs-validation-04
  answer: |
    A built-in pipe like ParseIntPipe parses a string parameter into an integer. It takes the raw string value, converts it using parseInt(), and passes the number to the handler. If the value cannot be parsed, it throws a BadRequestException.

    A globally-registered pipe applies to all routes and parameters in the application. A pipe bound to a single route parameter (e.g., @Param('id', ParseIntPipe) id: number) applies only to that specific parameter, giving you fine-grained control.

- id: nestjs-guards-01
  answer: |
    A guard is a class that implements the CanActivate interface with a canActivate(context: ExecutionContext) method. It decides whether a request should be allowed to proceed.

    The return value controls the request:
    - true: the request proceeds to the next stage (interceptors, pipes, handler)
    - false: Nest throws a ForbiddenException (HTTP 403)
    - The guard may also throw its own exception (e.g., UnauthorizedException for 401)

- id: nestjs-guards-02
  answer: |
    The request pipeline order is:
    1. Middleware
    2. Guards
    3. Interceptors (pre-handler)
    4. Pipes
    5. Route handler
    6. Interceptors (post-handler)
    7. Exception filters

    Interceptors run both before and after the handler. The pre-handler code runs before next.handle() is called, and post-handler code (via RxJS operators like map, tap, catchError) runs after next.handle() returns the Observable.

- id: nestjs-guards-03
  answer: |
    An interceptor is a class that implements the NestInterceptor interface with an intercept(context: ExecutionContext, next: CallHandler) method. The next.handle() method returns an RxJS Observable of the route handler's result.

    Two things interceptors are good for:
    1. Logging and timing requests (using tap operator)
    2. Transforming the response (using map operator)
    They are also commonly used for error handling (catchError), caching, and adding metadata to responses.

- id: nestjs-guards-04
  answer: |
    A roles guard reads per-route metadata using the Reflector class. The Reflector is injected into the guard, and you call reflector.get(METADATA_KEY, handler) or reflector.get(METADATA_KEY, classRef) to retrieve the metadata value set by a custom decorator like @Roles('admin').

    The Reflector class is provided by @nestjs/core and is the standard way to access metadata set by decorators.

- id: nestjs-filters-01
  answer: |
    HttpException is the base class for all HTTP exceptions in NestJS. Built-in exception classes like NotFoundException, BadRequestException, UnauthorizedException, ForbiddenException, etc. all extend HttpException.

    Throwing an HttpException produces a structured HTTP response with the specified status code and a JSON body containing the error message and status code (e.g., { "statusCode": 404, "message": "Not Found" }).

- id: nestjs-filters-02
  answer: |
    To write a custom exception filter:
    - Decorator: @Catch() (optionally pass exception types, e.g., @Catch(HttpException))
    - Interface: ExceptionFilter
    - Method: catch(exception: T, host: ArgumentsHost)

    The catch method receives the exception instance and an ArgumentsHost which provides access to the request/response objects, allowing you to customize the HTTP response.

- id: nestjs-filters-03
  answer: |
    Exception filters resolve in the same order as guards, interceptors, and pipes: global → controller → route. The most specific (route-level) filter is applied first.

    You register a global filter using app.useGlobalFilters(new MyCustomFilter()) in your bootstrap code.

- id: nestjs-filters-04
  answer: |
    If your code throws a plain Error (not an HttpException) and no custom exception filter handles it, the client receives a 500 Internal Server Error response with a generic message: { "statusCode": 500, "message": "Internal server error" }. The actual error details are not exposed to the client for security reasons.

- id: nestjs-providers-01
  answer: |
    You create an async provider using useFactory that returns a Promise:
    {
      provide: 'DB_CONNECTION',
      useFactory: async (): Promise<Connection> => {
        return await createConnection(options);
      }
    }

    Yes, Nest waits for the Promise to resolve before the application is considered ready. The application will not start listening until all async providers have resolved.

- id: nestjs-providers-02
  answer: |
    - useValue: provides a static, pre-computed value. Use when the value is already available and does not need any computation or dependency injection.
    - useClass: maps a token to a class that Nest will instantiate. Use when you want to swap implementations (e.g., use a different service class for the same token) or when the provider needs its own dependencies injected.
    - useFactory: provides a value from a factory function. Use when the value requires complex computation, dependency injection, or conditional logic. The factory can inject other providers via the inject option.

- id: nestjs-providers-03
  answer: |
    ConfigModule from @nestjs/config exposes configuration through the ConfigService. You inject ConfigService and call configService.get('key') to read configuration values from environment variables and .env files.

    isGlobal: true makes the ConfigModule available to all other modules without needing to import it explicitly. This is commonly used because configuration is needed throughout the application.

- id: nestjs-providers-04
  answer: |
    Using @nestjs/testing, you build a module for a unit test with:
    const module = await Test.createTestingModule({
      providers: [MyService],
    }).compile();

    To replace a real provider with a mock, use overrideProvider:
    const module = await Test.createTestingModule({
      providers: [MyService],
    })
      .overrideProvider(MyService)
      .useValue(mockMyService)
      .compile();

    You can also use .useClass() or .useFactory() for more complex mock scenarios.
