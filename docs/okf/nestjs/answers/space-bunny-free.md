- id: nestjs-modules-01
  answer: |
    A Nest module is a container and encapsulation boundary that groups related controllers, providers, and their dependencies.

    `@Module()` metadata declares:
    - `imports`: other module classes whose exported providers are made available to this module.
    - `controllers`: HTTP route handlers exposed by the module.
    - `providers`: injectable services, repositories, factories, values, and other dependencies.
    - `exports`: providers, or sometimes imported modules, that this module exposes to importing modules.

    Modules organize the application, define dependency-injection boundaries, and participate in the application lifecycle.

- id: nestjs-modules-02
  answer: |
    `AuthModule` must list `AuthService` in its `exports`, and `UsersModule` must import `AuthModule`.

    If either part is missing, the injection token is not visible in `UsersModule` and Nest cannot resolve the dependency, normally causing application bootstrap to fail with a missing-provider error.

- id: nestjs-modules-03
  answer: |
    A dynamic module is created by returning a `DynamicModule` object, usually from a static method on a module class. It can add providers, exports, imports, or configuration that are determined at runtime.

    By convention, `forRoot()` configures and registers a module once at the application root, while `forFeature()` creates feature-specific configuration, often allowing each feature to receive a distinct configuration instance. These are conventions rather than special Nest language constructs.

- id: nestjs-modules-04
  answer: |
    `@Global()` makes a module's exported providers available throughout the application without every consumer module having to import it.

    It should be used sparingly because it creates hidden coupling, makes dependencies harder to trace, and makes providers harder to replace, isolate, reuse, or remove. A global module can also introduce token collisions and complicate isolated testing.

- id: nestjs-di-01
  answer: |
    `@Injectable()` marks a class as a provider that Nest may instantiate and whose constructor dependencies Nest should resolve.

    The class is registered in a module's `providers` array. When Nest instantiates it, decorator metadata emitted by TypeScript supplies the constructor parameter types. Nest uses that metadata, together with `@Inject()` tokens where needed, to recursively resolve each dependency from the current module or one of its imports. It then constructs the provider and injects the resulting instances into the constructor. Under the default scope, the provider instance is reused rather than recreated for every consumer.

- id: nestjs-di-02
  answer: |
    Register the value under a custom string, symbol, or class token with `useValue`:

    ```ts
    providers: [
      { provide: 'DATABASE_CONFIG', useValue: configObject },
    ]
    ```

    Inject it using `@Inject()`:

    ```ts
    constructor(
      @Inject('DATABASE_CONFIG')
      private readonly config: DatabaseConfig,
    ) {}
    ```

    The token identifies the dependency to Nest, while `useValue` supplies the actual object, string, or third-party instance. The provider must still be visible to the consumer module through its imports and exports.

- id: nestjs-di-03
  answer: |
    - `DEFAULT`: one shared instance within its module/injector, reused by consumers.
    - `REQUEST`: a new instance for each incoming request, shared by consumers within that request's injection context.
    - `TRANSIENT`: a new instance for each consumer that injects it; it is never shared as a singleton.

    `DEFAULT` is the default scope and is normally recommended because it avoids repeated allocation and dependency-graph work. Nest warns that making a low-level provider request-scoped can propagate request scope through much of the dependency graph.

- id: nestjs-di-04
  answer: |
    Scope bubbles upward through the dependency graph. If a `REQUEST`-scoped provider depends on another provider, consumers of that provider can also need request-scoped instances, and the request scope can continue propagating toward higher-level providers.

    The cost is additional object construction and a larger per-request dependency graph, so broad request scoping reduces performance.

    `TRANSIENT` also propagates upward, but its lifetime is per consumer injection rather than per request. It can therefore create many more instances, but it does not create one separate instance for every request.

- id: nestjs-routing-01
  answer: |
    `@Controller('cats')` gives the controller a route prefix of `/cats`. The final path is the controller prefix combined with the handler's method path and any application-level global prefix.

    - `@Get()` and `@Post()` declare HTTP methods and optional handler paths.
    - `@Param('id')` extracts a named route parameter such as `id` from `:id`.
    - `@Query('active')` extracts a named query-string value.
    - `@Body()` supplies the parsed request body.

    Route parameters, query values, and body properties are matched by their decorator names, while pipes can transform or validate them before the handler runs.

- id: nestjs-routing-02
  answer: |
    Use `:name` for a path parameter, for example:

    ```ts
    @Get(':id')
    getOne() {}

    @Get(':id/children')
    getChildren() {}
    ```

    Multiple path parameters can appear in one route, such as `@Get('parents/:parentId/children/:childId')`.

    NestJS 11 uses Express 5 and path-to-regexp 8 conventions. Bare unnamed wildcards such as `*` are no longer accepted; a wildcard must be named, for example `@Get('*splat')`.

- id: nestjs-routing-03
  answer: |
    A class exists at runtime, whereas a TypeScript interface is erased during compilation. Nest relies on runtime metadata such as `design:type` to identify DTOs and perform transformation and validation.

    A DTO class gives pipes and validators a runtime value to work with, allowing objects to be transformed into class instances and decorated with `class-validator` rules. An interface cannot provide that runtime metadata.

- id: nestjs-routing-04
  answer: |
    Nest normally returns `201 Created` for `POST` handlers and `200 OK` for other methods. Override the response status with `@HttpCode()`, for example `@HttpCode(200)` or `@HttpCode(204)`.

    If a handler returns a Promise, Nest awaits it and uses the resolved value as the response. If it returns an RxJS Observable, Nest subscribes to it and uses the emitted result according to the normal response transformation behavior.

- id: nestjs-lifecycle-01
  answer: |
    `OnModuleInit` runs during application initialization, after that module's dependencies have been initialized but before the entire application has finished initializing.

    `OnApplicationBootstrap` runs after all modules have completed their `onModuleInit` processing, immediately before the application starts listening for incoming requests.

- id: nestjs-lifecycle-02
  answer: |
    The shutdown hook phases run in this order:

    1. `OnModuleDestroy`
    2. `BeforeApplicationShutdown`
    3. `OnApplicationShutdown`

    For process signals such as `SIGTERM` and `SIGINT`, register Nest's process listeners by calling `app.enableShutdownHooks()`. Calling `app.close()` also executes the shutdown sequence.

- id: nestjs-lifecycle-03
  answer: |
    Assuming C depends on B and B depends on A, initialization runs `C → B → A`, while destruction runs in reverse dependency order: `A → B → C`. Dependents are torn down before the dependencies they use.

    NestJS 11 made this reverse, dependency-aware module teardown order the behavior to rely on; older versions did not provide the same consistent module-order guarantee.

- id: nestjs-lifecycle-04
  answer: |
    Initialization hooks are triggered by `app.init()` or by an operation that performs initialization, such as `app.listen()`. Merely creating the application with `NestFactory.create()` does not necessarily call `app.init()`.

    Nest waits for each asynchronous initialization hook. If a hook returns a Promise, Nest awaits it before moving to the next hook or completing application initialization.

- id: nestjs-validation-01
  answer: |
    `ValidationPipe` validates controller inputs such as DTO bodies, query values, and route parameters. With transformation enabled, it can also convert plain request data into DTO instances.

    It uses `class-validator` for declarative validation rules and `class-transformer` for object transformation.

    Apply it application-wide with:

    ```ts
    app.useGlobalPipes(new ValidationPipe({
      whitelist: true,
      transform: true,
    }));
    ```

    Alternatively, it can be registered as an `APP_PIPE` provider.

- id: nestjs-validation-02
  answer: |
    `whitelist: true` removes properties from incoming objects that do not have validation or transformation decorators.

    `forbidNonWhitelisted: true` rejects the request with a `400 Bad Request` when such non-whitelisted properties are present. It depends on `whitelist: true` to be effective.

    These options prevent clients from silently supplying unexpected fields and reduce accidental mass-assignment or unvalidated-data problems.

- id: nestjs-validation-03
  answer: |
    With `transform: true`, the pipe converts an incoming plain body or query object into an instance of the DTO class before validation and handler execution.

    For primitive route parameters, the pipe can also use reflected TypeScript parameter metadata. Assuming decorator metadata is enabled, a parameter declared as `@Param('id') id: number` receives a numeric value; for example, `"42"` becomes `42` rather than remaining a string. For a guaranteed integer with an explicit bad-request failure, `@Param('id', ParseIntPipe)` is often clearer.

- id: nestjs-validation-04
  answer: |
    A built-in pipe such as `ParseIntPipe` performs a narrow input-conversion and validation operation. `ParseIntPipe` converts a numeric route value to an integer and throws a `BadRequestException` when the value is not a valid integer.

    A globally registered pipe can run across matching arguments throughout the application. A route-bound pipe runs only where it is explicitly placed:

    ```ts
    findOne(@Param('id', ParseIntPipe) id: number) {}
    ```

    Local binding avoids unintended conversions elsewhere and is especially useful for requiring integer semantics for a particular parameter.

- id: nestjs-guards-01
  answer: |
    A guard is a provider that determines whether a request is allowed to proceed to a controller handler. It implements Nest's `CanActivate` interface and exposes a `canActivate()` method.

    The method returns a boolean, a Promise of a boolean, or an Observable of a boolean. `true` permits execution; `false` rejects it, normally with `403 Forbidden`. A guard may also throw an exception to produce a more specific error response.

- id: nestjs-guards-02
  answer: |
    The request pipeline is:

    1. Middleware
    2. Guards
    3. Interceptor code before `next.handle()`
    4. Pipes
    5. Controller route handler
    6. Interceptor code after `next.handle()`
    7. Exception filters for handled exceptions
    8. Response

    Interceptors surround the route execution: their pre-handler logic runs before pipes and the handler, while their post-handler logic runs after the handler produces a result.

- id: nestjs-guards-03
  answer: |
    An interceptor surrounds execution of a handler and can observe, modify, delay, replace, or handle its request and result. It implements `NestInterceptor` and provides an `intercept()` method that receives the `ExecutionContext` and a `CallHandler`; `next.handle()` starts the downstream pipeline.

    Interceptors are useful for:
    - Logging, tracing, metrics, and request timing.
    - Caching, response mapping, timeout handling, and transforming handler results.

- id: nestjs-guards-04
  answer: |
    The custom decorator uses Nest's `SetMetadata()` to attach role metadata to the handler. A guard reads it through the `Reflector` service, typically with `getAllAndOverride()` using `[context.getHandler(), context.getClass()]`. This checks route-level metadata first and then controller-level metadata.

    The guard can then compare the returned roles with the current user's roles and return `true` or `false`.

- id: nestjs-filters-01
  answer: |
    `HttpException` is Nest's base class for exceptions that represent HTTP error responses. It carries an HTTP status and a response body, exposed through `getStatus()` and `getResponse()`.

    Built-in exceptions such as `BadRequestException`, `UnauthorizedException`, `NotFoundException`, and `ForbiddenException` extend `HttpException` and provide convenient predefined statuses and bodies. Throwing one causes the applicable exception filter to produce the corresponding HTTP error response; without a custom filter, the built-in handler does so.

- id: nestjs-filters-02
  answer: |
    Define a class with the `@Catch()` decorator and implement the `ExceptionFilter` interface:

    ```ts
    @Catch(UserNotFoundException)
    export class UserExceptionFilter implements ExceptionFilter {
      catch(exception: UserNotFoundException, host: ArgumentsHost) {
        const response = host.switchToHttp().getResponse<Response>();
        response.status(404).json({ message: 'User not found' });
      }
    }
    ```

    `@Catch()` selects the handled exception type or types, and `catch()` receives the exception plus an `ArgumentsHost` used to access the current execution context.

- id: nestjs-filters-03
  answer: |
    Exception filters also resolve from broadest to narrowest scope: global, then controller, then route handler.

    Register one globally with `app.useGlobalFilters(new MyExceptionFilter())`, or register a filter through an `APP_FILTER` provider when it needs dependency injection. Nest selects the relevant filter at the narrowest scope rather than combining all matching filters.

- id: nestjs-filters-04
  answer: |
    The client receives a `500 Internal Server Error` response. By default, Nest's built-in exception handling returns a generic response such as:

    ```json
    {
      "statusCode": 500,
      "message": "Internal server error"
    }
    ```

    The original error's message and stack are not exposed by default. A custom filter could deliberately produce a different response.

- id: nestjs-providers-01
  answer: |
    Create a provider whose factory returns the Promise produced by the connection operation:

    ```ts
    {
      provide: 'DB_CONNECTION',
      inject: [ConfigService],
      useFactory: async (config: ConfigService) =>
        createDatabaseConnection(config.getOrThrow('DATABASE_URL')),
    }
    ```

    Nest awaits the factory's Promise and injects the resolved value, not an unresolved Promise. Initialization waits for asynchronous providers to resolve before dependent providers are initialized and before application initialization or listening completes.

- id: nestjs-providers-02
  answer: |
    - `useValue`: supplies an already-created, fixed value, such as a configuration object, string, mock, or third-party instance.
    - `useClass`: supplies a provider class, allowing Nest to instantiate it and resolve its own constructor dependencies.
    - `useFactory`: calls a function to construct the value, optionally with injected dependencies, and can compute configuration or perform asynchronous initialization.

    Use `useValue` for an existing value, `useClass` when the provider should be a normal injectable class, and `useFactory` when the value must be created or configured dynamically.

- id: nestjs-providers-03
  answer: |
    Register `ConfigModule` once with `ConfigModule.forRoot(...)`. It loads environment variables and optional configuration factories, and consumers inject the resulting `ConfigService` to read typed or named configuration values.

    Setting `isGlobal: true` makes the configuration module global, allowing `ConfigService` to be injected throughout the application without importing `ConfigModule` into every feature module.

- id: nestjs-providers-04
  answer: |
    Build a focused testing module and replace the real provider with a mock value:

    ```ts
    const moduleRef = await Test.createTestingModule({
      controllers: [UsersController],
      providers: [
        UsersService,
        { provide: UserRepository, useValue: mockUserRepository },
      ],
    }).compile();

    const controller = moduleRef.get(UsersController);
    ```

    For an existing imported application module, list the service under test in the testing module's `providers` and call `overrideProvider(Token).useValue(mock)` before `compile()`. Call `moduleRef.init()` when lifecycle hooks must run, and close the module after the test.
