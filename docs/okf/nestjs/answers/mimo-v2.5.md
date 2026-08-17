```yaml
- id: nestjs-modules-01
  answer: |
    A Nest module is an organizational unit that groups related controllers, providers, and imports. The `@Module()` decorator declares:
    - `providers`: services and other injectable classes that belong to this module; they are instantiated by Nest's DI container and can be injected within this module.
    - `controllers`: the controller classes that define route handlers and receive HTTP requests.
    - `imports`: other modules whose exported providers should be available for injection within this module.
    - `exports`: a subset of providers that should be available to other modules that import this one. By default providers are private to the declaring module.

- id: nestjs-modules-02
  answer: |
    `AuthService` must be exported from `AuthModule` (listed in `exports`), and `AuthModule` must be listed in the `imports` array of `UsersModule`. If you forget either step, Nest will throw an "Unknown element" / "Can't resolve dependencies" error because the DI container treats modules as encapsulated — a provider is only visible to the module that declares it unless it is explicitly exported and the consuming module explicitly imports it.

- id: nestjs-modules-03
  answer: |
    A dynamic module is a module whose configuration is supplied at runtime (typically through a static method that returns a `DynamicModule` object containing `module`, `providers`, `imports`, `exports`, and optionally `global`). The conventional difference: `forRoot()` is called once at the application root and provides global/shared configuration (e.g., database connection, Redis config); `forFeature()` is called per-feature module to register providers scoped to that feature (e.g., `TypeOrmModule.forFeature([Entity])` registering specific repositories). `forRoot()` usually creates and manages the shared resource; `forFeature()` registers pieces that depend on it.

- id: nestjs-modules-04
  answer: |
    `@Global()` makes the module's exported providers available for injection anywhere in the application without needing to import the module in each consumer. It should be used sparingly because it breaks explicit dependency declarations — consumers don't know where a provider comes from, making the dependency graph harder to reason about, increasing coupling, and making it easy to accidentally depend on providers that might be removed or changed without warning.

- id: nestjs-di-01
  answer: |
    When a class is marked `@Injectable()`, Nest registers it as a potential provider in the DI container. When the class is used as a dependency (listed in a module's `providers` array), Nest examines its constructor signature via TypeScript metadata reflection (`emitDecoratorMetadata`). For each constructor parameter, Nest looks up the corresponding provider in the current module's (and imported modules') provider map, recursively resolving any transitive dependencies. It then instantiates the provider by calling `new ClassName(resolvedDep1, resolvedDep2, ...)`. The resulting instance is cached in the DI container (singleton scope by default) and injected into any class that requests it.

- id: nestjs-di-02
  answer: |
    Use a custom token via `useValue`, `useClass`, or `useFactory` in the providers array:

    ```ts
    { provide: 'CONFIG', useValue: { apiKey: '...' } }
    ```

    Then inject it with `@Inject('CONFIG')` on the constructor parameter (or `@Inject(CONFIG)` with a const token). You can also use an `InjectionToken` class or an abstract class as the token for type safety, or use `@nestjs/config`'s `ConfigService` for environment-based configuration.

- id: nestjs-di-03
  answer: |
    The three scopes:
    - **Default (Singleton)**: one instance per module, reused for the lifetime of the application. This is the default and recommended because it avoids repeated instantiation overhead.
    - **Request**: a new instance is created for each incoming HTTP request and destroyed when the request completes. Useful when a provider needs request-specific data but adds overhead since every dependent in the chain also becomes request-scoped.
    - **Transient**: a new instance is created every time the provider is injected, even within the same request. Useful for value objects or independent state containers where each consumer needs its own isolated instance.

- id: nestjs-di-04
  answer: |
    When a provider is `REQUEST`-scoped, every provider that injects it (directly or transitively) must also be request-scoped, because a singleton instance cannot hold a request-scoped dependency (the lifecycle would conflict). This cascading effect means most of the dependency graph for that feature becomes request-scoped, with a significant performance cost: every request triggers a new instantiation chain for all those providers, and cleanup runs per-request. `TRANSIENT` does not cascade the same way — each injection point gets its own instance, but the injection sites themselves don't become transient; transient instances are simply created fresh each time without affecting the scope of the injector.

- id: nestjs-routing-01
  answer: |
    `@Controller('cats')` sets the route prefix `cats` for all handlers in that controller, so routes will be `/cats`, `/cats/:id`, etc.
    - `@Get()`, `@Post()` etc. define the HTTP method and optional sub-path for a handler (e.g., `@Get(':id')` maps to `GET /cats/:id`).
    - `@Param('id')` extracts the named route parameter from `req.params.id`.
    - `@Query('page')` extracts query string parameters from `req.query`.
    - `@Body()` extracts the parsed request body from `req.body` (optionally narrowed by a DTO class or property name string).

- id: nestjs-routing-02
  answer: |
    A route with a path parameter and nested sub-path: `@Get(':id/details')` or `@Get(':id/tags/:tagId')`. In NestJS 11, wildcard routes changed: previously you would use `@Get('*')` but the wildcard parameter handling was updated. NestJS 11 introduced support for `@All('*path')` style wildcards with named parameters, and the generic `*` (unnamed wildcard) no longer captures into a named param automatically — you should use a named wildcard like `@Get('*path')` to access the matched segment via `@Param('path')`.

- id: nestjs-routing-03
  answer: |
    Request DTOs are defined as classes (not interfaces) because TypeScript interfaces are erased at compile time and are not available at runtime. NestJS's `ValidationPipe` and `class-transformer` rely on runtime type metadata and decorators (`class-validator`, `class-transformer`) to validate and transform incoming data. Classes preserve this metadata via `emitDecoratorMetadata` and decorators, whereas interfaces exist only for static type checking.

- id: nestjs-routing-04
  answer: |
    The default HTTP status code is `200 OK`. You override it with the `@HttpCode(HttpStatus.CREATED)` decorator (or a numeric value like `@HttpCode(201)`). Nest automatically handles both synchronous and asynchronous return values — if a handler returns a `Promise`, Nest awaits it and sends the resolved value; if it returns an `Observable`, Nest subscribes to it and sends each emitted value (for streams) or the final value. The response is sent when the promise resolves or the observable completes.

- id: nestjs-lifecycle-01
  answer: |
    `OnModuleInit` fires when the module's providers have been created and the module is being initialized — this happens early in the bootstrap sequence, before the HTTP server starts listening. `OnApplicationBootstrap` fires after all modules have been initialized and the application is fully bootstrapped — meaning the HTTP server is ready but hasn't started accepting connections yet (or has just started). The key difference: `OnModuleInit` is per-module and fires as soon as that module's dependencies are resolved; `OnApplicationBootstrap` is global and fires once after everything is wired up.

- id: nestjs-lifecycle-02
  answer: |
    The shutdown hooks in order: `onModuleDestroy`, `onApplicationShutdown`, then `beforeApplicationShutdown`. You must enable `enableShutdownHooks()` on the `NestApplication` (or pass `enableShutdownHooks: true` to the `NestFactory.create` options) for these hooks to fire. Without this, Nest will not listen for `SIGTERM`/`SIGINT` signals and the destroy hooks won't run on process termination.

- id: nestjs-lifecycle-03
  answer: |
    Destroy hooks run in reverse dependency order — if init ran C → B → A (because A depends on B which depends on C), destroy runs A → B → C. Each module's `onModuleDestroy` fires after all modules that depend on it have been destroyed, ensuring dependencies are cleaned up after their consumers. This behavior was consistent in NestJS 11 (no change to the reverse-order destruction sequence), though NestJS 11 did refine some timing aspects of the shutdown lifecycle.

- id: nestjs-lifecycle-04
  answer: |
    Init lifecycle hooks (`OnModuleInit`, `OnApplicationBootstrap`) are triggered during the application bootstrap sequence, after the DI container has wired up all modules and resolved all providers. Yes, Nest waits for async hooks — if the hook method returns a Promise, Nest awaits it before proceeding to the next lifecycle step. This ensures initialization side effects (database connections, cache warmup, etc.) are complete before the server starts accepting requests.

- id: nestjs-validation-01
  answer: |
    The built-in `ValidationPipe` automatically validates incoming request bodies against DTO class decorators. It relies on `class-validator` (for decorator-based validation rules) and `class-transformer` (for transforming raw data into DTO instances). To apply it globally: `app.useGlobalPipes(new ValidationPipe())` after creating the app, or register it in a module with `APP_PIPE` custom provider, or pass it in `NestFactory.create` options via `validationPipe: true` in recent NestJS versions.

- id: nestjs-validation-02
  answer: |
    - `whitelist: true` strips any properties from the incoming object that do not have corresponding decorators on the DTO class — unknown properties are removed before the handler sees them.
    - `forbidNonWhitelisted: true` goes further: if any unknown properties are present after whitelisting, it throws a `BadRequestException` instead of silently stripping them.
    They are used together as a security and correctness measure — to prevent clients from injecting unexpected fields that could cause bugs or security issues.

- id: nestjs-validation-03
  answer: |
    `transform: true` tells the `ValidationPipe` to create an instance of the DTO class from the raw plain object (using `class-transformer`'s `plainToInstance`) and to cast/transform parameter types accordingly. For a `@Param('id')` typed as `number` (e.g., `@Param('id', ParseIntPipe) id: number`), when `transform: true` is enabled, the pipe will attempt to transform the raw string from the URL into the declared type — so `"42"` becomes the number `42`. Without `transform`, the parameter remains the raw string and only the pipe's transformation is applied.

- id: nestjs-validation-04
  answer: |
    A built-in pipe like `ParseIntPipe` transforms and validates a single input — it converts a string parameter to an integer and throws a `BadRequestException` if the value is not a valid integer. The practical difference: a globally-registered pipe applies to every route handler parameter that doesn't have a specific pipe, whereas a pipe bound to a single route parameter (e.g., `@Param('id', ParseIntPipe)`) only applies to that specific parameter in that specific route. Global pipes handle broad concerns like validation; per-parameter pipes handle type coercion for specific arguments.

- id: nestjs-guards-01
  answer: |
    A guard is an interceptor that runs before the route handler to determine whether a request should be processed. It implements the `CanActivate` interface (which has a single `canActivate(context: ExecutionContext): boolean | Promise<boolean> | Observable<boolean>` method). If the method returns `true`, the request proceeds; if `false` (or the guard throws), the request is rejected. Guards can throw `ForbiddenException` to deny access with a 403 status.

- id: nestjs-guards-02
  answer: |
    The order: **middleware → guards → interceptors (before) → pipes → route handler → interceptors (after) → exception filters**. Interceptors wrap the route handler — their `intercept` method runs before the handler, calls `next.handle()` to invoke the handler, and can observe/modify the result after. Exception filters sit outside the entire chain and catch any errors thrown at any stage.

- id: nestjs-guards-03
  answer: |
    An interceptor is a class implementing the `NestInterceptor` interface, which has an `intercept(context: ExecutionContext, next: CallHandler): Observable<any>` method. Interceptors wrap the request-response cycle and can: (1) log/transform responses (e.g., add timestamps, format output), and (2) implement caching (e.g., cache responses and return cached data without invoking the handler). They are also useful for timeout handling, retry logic, and response mapping.

- id: nestjs-guards-04
  answer: |
    A roles guard reads per-route metadata by calling `context.getHandler()` and `context.getClass()` to get the handler function and controller class, then uses the `Reflector` class (`@Inject(Reflector)`) to extract metadata set by the `@Roles()` custom decorator. The `@Roles('admin')` decorator uses `SetMetadata('roles', ['admin'])` to attach metadata. The guard reads it with `reflector.get<string[]>('roles', context.getHandler())` (and optionally `context.getClass()` to also check controller-level metadata) and checks whether the current user's roles include at least one required role.

- id: nestjs-filters-01
  answer: |
    `HttpException` is the base exception class in NestJS for HTTP errors. Built-in exception classes like `NotFoundException`, `ForbiddenException`, `BadRequestException`, `UnauthorizedException`, and `ConflictException` extend `HttpException` and set the appropriate HTTP status code and default message. Throwing one produces a JSON response to the client with `statusCode`, `message`, and `error` fields (e.g., `{ "statusCode": 404, "message": "Not Found" }`). If the exception has a custom `message` property, it replaces the default.

- id: nestjs-filters-02
  answer: |
    Create a class implementing `ExceptionFilter` (from `@nestjs/common`). Use the `@Catch()` decorator to specify which exception(s) to catch (e.g., `@Catch(HttpException)` or `@Catch()` for all). The class must implement a `catch(exception: T, host: ArgumentsHost)` method. The `host` argument provides access to the underlying platform (HTTP, RPC, etc.) — for HTTP, call `host.switchToHttp()` to get `Request` and `Response` objects, then use `response.status().json()` to send a custom error response.

- id: nestjs-filters-03
  answer: |
    Exception filters resolve in order: first the **route-level** filter (bound to the handler), then the **controller-level** filter (bound to the controller class), then **global** filters (registered via `app.useGlobalFilters()` or the `APP_FILTER` custom provider). More specific filters run first. A global filter is registered with `app.useGlobalFilters(new MyFilter())` on the application instance, or via a module using `{ provide: APP_FILTER, useClass: MyFilter }`.

- id: nestjs-filters-04
  answer: |
    If no custom filter catches the plain `Error`, Nest's built-in `ExceptionsHandler` catches it and returns a `500 Internal Server Error` response with the message `"Internal server error"`. The original error message is not exposed to the client for security reasons — only the generic status and message are sent.

- id: nestjs-providers-01
  answer: |
    Use `useFactory` with an async factory function:

    ```ts
    {
      provide: 'DB_CONNECTION',
      useFactory: async () => {
        const connection = await createConnection(config);
        return connection;
      },
    }
    ```

    Or use `inject` to get dependencies:

    ```ts
    {
      provide: 'DB_CONNECTION',
      useFactory: async (configService: ConfigService) => {
        return await createConnection(configService.get('DB_URL'));
      },
      inject: [ConfigService],
    }
    ```

    Yes, Nest waits for all async providers to resolve before the application finishes bootstrapping — the app won't start listening until every async factory/promise has resolved.

- id: nestjs-providers-02
  answer: |
    - `useValue`: provides a static value directly (e.g., a config object, a string, a pre-created instance). Use when the value is already known and doesn't need instantiation — common for constants, environment config, or mock values in tests.
    - `useClass`: provides an instance of the specified class (Nest instantiates it via DI). Use when you want to provide a specific class implementation, often to override an existing provider (e.g., replace a real service with a mock class).
    - `useFactory`: provides a value created by a factory function that can contain arbitrary logic and async operations. Use when creating the value requires logic, conditional behavior, async resolution, or combining multiple dependencies.

- id: nestjs-providers-03
  answer: |
    `ConfigModule.forRoot()` (typically imported in the root module) loads `.env` files and makes `ConfigService` available for injection throughout the app. It reads environment variables and parses them into a typed configuration. `isGlobal: true` makes the `ConfigModule` and its `ConfigService` available in every module without needing to import `ConfigModule` in each one — equivalent to calling `@Global()` on the module. Without `isGlobal: true`, each module that needs `ConfigService` must import `ConfigModule` in its own `imports` array.

- id: nestjs-providers-04
  answer: |
    ```ts
    const module = await Test.createTestingModule({
      providers: [
        MyService,
        { provide: ExternalService, useValue: mockExternalService },
      ],
    }).compile();

    const myService = module.get(MyService);
    ```

    You use `Test.createTestingModule()` to build a module specifically for testing. To replace a real provider with a mock, add a provider entry with `useValue` (for a simple mock object), `useClass` (for a mock class), or `useFactory` (for a dynamic mock). The mock must satisfy the same token/type as the real provider. This allows unit-testing a service in isolation without hitting external dependencies like databases or APIs.
```
