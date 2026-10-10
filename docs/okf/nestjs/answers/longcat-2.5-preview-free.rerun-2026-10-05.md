- id: nestjs-modules-01
  answer: |
    The `@Module()` decorator's metadata declares:
    - `imports`: other modules whose exported providers this module needs.
    - `controllers`: the controllers instantiated in this module.
    - `providers`: classes (or factory results) registered in this module's DI container.
    - `exports`: providers from this module that are made available to other modules that import it.

    A Nest module is a class annotated with `@Module()` that groups related controllers, providers, and other modules into a cohesive, encapsulated unit of the application.

- id: nestjs-modules-02
  answer: |
    `AuthModule` must list `AuthService` in its `exports` array, and `UsersModule` must list `AuthModule` in its `imports` array. If you forget either, Nest's DI container cannot resolve the dependency and throws a "Nest can't resolve dependencies" error at startup.

- id: nestjs-modules-03
  answer: |
    A dynamic module is a module created by a static method that accepts configuration options and returns a `DynamicModule` object (a module with extra provider configuration). The conventional difference: `forRoot()` is called once at the application root to set up global, singleton configuration; `forFeature()` is called within feature modules to register module-scoped providers (e.g., repository or service bindings specific to that feature).

- id: nestjs-modules-04
  answer: |
    `@Global()` marks a module so that its exported providers are available to every module in the application without needing to import the module. It should be used sparingly because it creates implicit, hidden dependencies, reduces modularity, and makes the dependency graph harder to reason about.

- id: nestjs-di-01
  answer: |
    1. `@Injectable()` marks a class as a provider that can be managed by Nest's DI container.
    2. The provider is registered by listing it in a module's `providers` array (or via a custom provider).
    3. When a class declares a constructor parameter with a type annotation, Nest reads the TypeScript-emitted design:paramtypes metadata, looks up a provider matching that type in the container, and injects the instance automatically.

- id: nestjs-di-02
  answer: |
    Register it using a custom token (a string or Symbol) with `useValue`, `useClass`, or `useFactory` in a module's providers. Then inject it using `@Inject(TOKEN)` in the constructor. For example: `{ provide: 'CONFIG', useValue: configObj }` and constructor(@Inject('CONFIG') config: Config).

- id: nestjs-di-03
  answer: |
    - `SINGLETON` (default): one instance shared across the entire application.
    - `REQUEST`: a new instance created for each incoming request.
    - `TRANSIENT`: a new instance created each time the provider is injected into a consumer.

    SINGLETON is the default and recommended because it minimizes object creation overhead and allows stateful services to share data safely.

- id: nestjs-di-04
  answer: |
    When a provider is REQUEST-scoped, any provider that depends on it is also forced to be REQUEST-scoped (scope "bubbles" up the dependency graph). The performance implication is that more instances are created per request, increasing memory and GC pressure. TRANSIENT does not bubble in the same way — each consumer gets its own instance, but it does not force its dependencies to become transient.

- id: nestjs-routing-01
  answer: |
    `@Controller('cats')` sets the base route prefix for all handlers in that controller. `@Get()` and `@Post()` map HTTP GET and POST requests to the decorated handler method. `@Param()` extracts route parameters (e.g., `:id`), `@Query()` extracts query string parameters, and `@Body()` extracts the parsed request body.

- id: nestjs-routing-02
  answer: |
    Declare a path parameter with `@Get(':id')` and a nested sub-path with `@Get(':id/sub')` or `@Get(':id/sub/:subId')`. In NestJS 11, wildcard route handling changed due to an update to the underlying `path-to-regexp` library — the syntax and behavior of wildcards (e.g., `*`) were adjusted to match the new library version.

- id: nestjs-routing-03
  answer: |
    DTOs are defined as classes because TypeScript interfaces are erased at runtime and cannot be introspected. Classes persist at runtime, enabling `class-validator` decorators to be read by the ValidationPipe for runtime validation.

- id: nestjs-routing-04
  answer: |
    The default HTTP status code is 200 for GET and 201 for POST. Override it with the `@HttpCode()` decorator. If a handler returns a Promise, Nest awaits it; if it returns an Observable, Nest subscribes to it and uses the emitted value(s).

- id: nestjs-lifecycle-01
  answer: |
    `OnModuleInit` fires after the module's dependencies have been resolved, allowing the module to perform initialization. `OnApplicationBootstrap` fires after all modules have been initialized and the application is fully ready, making it suitable for final setup that depends on the entire application being bootstrapped.

- id: nestjs-lifecycle-02
  answer: |
    The shutdown hooks in order are: `beforeApplicationShutdown` (fires before the app closes) and `onApplicationShutdown` (fires during shutdown). You must call `app.enableShutdownHooks()` for them to fire on SIGTERM/SIGINT signals.

- id: nestjs-lifecycle-03
  answer: |
    Destroy hooks run in the reverse order of initialization (A → B → C if init ran C → B → A). This behavior did not change in NestJS 11.

- id: nestjs-lifecycle-04
  answer: |
    Init lifecycle hooks are triggered automatically when the application starts, after all modules are resolved. Yes, Nest waits for an async hook (one returning a Promise) to resolve before proceeding with the next hook or completing the bootstrap process.

- id: nestjs-validation-01
  answer: |
    The built-in `ValidationPipe` validates incoming request payloads against DTO classes decorated with `class-validator` decorators. It relies on two libraries: `class-validator` (for validation decorators) and `class-transformer` (for transforming plain objects to class instances). Apply it globally with `app.useGlobalPipes(new ValidationPipe())`.

- id: nestjs-validation-02
  answer: |
    `whitelist: true` strips any properties from the payload that are not defined in the DTO, preventing mass-assignment attacks. `forbidNonWhitelisted: true` goes further by throwing a BadRequestException if non-whitelisted properties are present. Both are used for security — to ensure only expected, validated data reaches the handler.

- id: nestjs-validation-03
  answer: |
    `transform: true` causes the ValidationPipe to transform the incoming payload into an instance of the DTO class and perform type coercion. For example, a `@Param('id')` typed as `number` will be automatically converted from the string `"42"` to the number `42`.

- id: nestjs-validation-04
  answer: |
    `ParseIntPipe` parses a string parameter into an integer, throwing a BadRequestException if parsing fails. A globally-registered pipe applies to all routes and parameters in the application, while a parameter-scoped pipe (e.g., `@Param('id', ParseIntPipe)`) applies only to that specific parameter.

- id: nestjs-guards-01
  answer: |
    A guard is a class that implements the `CanActivate` interface. Its `canActivate()` method returns a boolean (or a Promise/Observable of boolean): `true` allows the request to proceed to the handler, `false` denies it (typically resulting in a 403 Forbidden response).

- id: nestjs-guards-02
  answer: |
    The request pipeline order is: middleware → guards → interceptors (pre-handler phase) → pipes → route handler → interceptors (post-handler phase) → exception filters. Interceptors run both before and after the route handler, wrapping it.

- id: nestjs-guards-03
  answer: |
    An interceptor is a class that implements the `NestInterceptor` interface. Two things interceptors are good for: (1) logging or measuring request/response timing, and (2) transforming the response data or adding metadata to it. They are also commonly used for caching and error mapping.

- id: nestjs-guards-04
  answer: |
    A roles guard uses the `Reflector` class (injected via the constructor) to read metadata set by a custom `@Roles()` decorator. It calls `reflector.get<string[]>('roles', context.getHandler())` to retrieve the roles required for the route, then compares them against the user's roles.

- id: nestjs-filters-01
  answer: |
    `HttpException` is the base class for HTTP exceptions in NestJS. Built-in exception classes (e.g., `BadRequestException`, `NotFoundException`, `UnauthorizedException`) extend it. Throwing one produces a structured JSON response with the appropriate HTTP status code, a message, and an error description.

- id: nestjs-filters-02
  answer: |
    Write a custom exception filter by decorating a class with `@Catch()` (optionally passing exception types) and implementing the `ExceptionFilter` interface. The `catch()` method receives two arguments: the exception that was thrown and an `ArgumentsHost` (which provides access to the request/response objects).

- id: nestjs-filters-03
  answer: |
    Exception filters resolve in the reverse order of other components: route-level → controller-level → global. Register a global filter using `app.useGlobalFilters(new MyFilter())`.

- id: nestjs-filters-04
  answer: |
    If a plain `Error` is thrown and no custom exception filter handles it, the client receives a 500 Internal Server Error response with a generic `{ "statusCode": 500, "message": "Internal server error" }` JSON body.

- id: nestjs-providers-01
  answer: |
    Create an async provider using `useFactory` that returns a Promise (e.g., a database connection). Nest waits for the Promise to resolve before considering the application ready, ensuring the connection is established before the app starts accepting requests.

- id: nestjs-providers-02
  answer: |
    - `useValue`: for static, pre-computed values (e.g., configuration objects, constants).
    - `useClass`: to map one class to another token, useful for swapping implementations (e.g., interface → concrete class).
    - `useFactory`: for complex creation logic that depends on other providers (receives injected dependencies as arguments).

- id: nestjs-providers-03
  answer: |
    `ConfigModule` exposes configuration through `ConfigService`, which can be injected and used to read environment variables and config files. `isGlobal: true` makes the module available throughout the application without requiring other modules to import it.

- id: nestjs-providers-04
  answer: |
    Use `Test.createTestingModule({ controllers: [...], providers: [...] })` from `@nestjs/testing` to build a module. Replace a real provider with a mock using `.overrideProvider(MyService).useValue(mockService)` before calling `.compile()`.
