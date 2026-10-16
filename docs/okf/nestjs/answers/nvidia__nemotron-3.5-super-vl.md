# nvidia/nemotron-3.5-super-vl — closed-book answers for nestjs (openrouter, 2026-10-16); answerer saw only _prompts/nestjs.md; nestjs-providers-01..04 were skipped by the batch run and re-asked alone, closed-book, in a fresh session with a 4-question sheet (same instruction)
- id: nestjs-modules-01
  answer: |
    `@Module()` declares four metadata fields:
    - `imports`: modules whose exported providers this module can inject.
    - `controllers`: controllers belonging to this module (their routes are mounted in this module's context).
    - `providers`: providers Nest instantiates for this module and that can be injected into its controllers/providers.
    - `exports`: providers from `providers` that are made available to any module importing this one.
    A Nest module is a dependency-injection boundary: it groups related controllers/providers, controls visibility outside the module via `exports`, and encapsulates everything else.
- id: nestjs-modules-02
  answer: |
    `AuthModule` must list `AuthService` in its `exports` array, and `UsersModule` must list `AuthModule` in its `imports`. Only then can `UsersModule`'s classes inject `AuthService`. Forget either, and Nest throws at bootstrap (unknown provider / cannot instantiate module) because modules are encapsulated by default.
- id: nestjs-modules-03
  answer: |
    A dynamic module is a module whose static method (e.g. `forRoot()`) returns a `DynamicModule` object (`{ module, providers, exports?, global? }`), letting the importing app configure the module at import time (options, connections, repositories). Convention: `forRoot()` sets up the module once app-wide, while `forFeature()` registers feature-scoped providers (e.g. one entity repository/model) inside a feature module, typically used together with `forRoot()`.
- id: nestjs-modules-04
  answer: |
    `@Global()` makes every provider exported by the module injectable from any other module without importing it. Use sparingly because it breaks encapsulation, creates hidden cross-module dependencies, complicates testing, and risks provider-token collisions.
- id: nestjs-di-01
  answer: |
    `@Injectable()` marks a class so Nest can instantiate and inject it (decorator metadata lets Nest inspect constructor parameter types). Register it in a module's `providers` array. When Nest builds a dependent class, it recursively resolves each constructor parameter, caches a single instance per provider token, and passes the instances into the constructor.
- id: nestjs-di-02
  answer: |
    Register it as a provider object with a custom token, e.g. `{ provide: 'CONFIG', useValue: configObject }` (values; `useClass`/`useFactory` cover aliases and factories). Inject with `@Inject('CONFIG')` on the constructor parameter so Nest resolves by token instead of by class type.
- id: nestjs-di-03
  answer: |
    `Scope.DEFAULT` — singleton, one instance shared app-wide (default). `Scope.REQUEST` — one instance per incoming request. `Scope.TRANSIENT` — a new instance every time it is injected. DEFAULT is recommended: singletons are cheap, suit stateless services, and make shared state explicit instead of incurring per-request allocation.
- id: nestjs-di-04
  answer: |
    A `REQUEST`-scoped provider exists for only one request, so any provider injecting it cannot remain a plain singleton holding it across requests — Nest effectively makes its dependents request-scoped too ("scoping infects upward"). Cost: more instances and GC pressure per request versus a shared singleton. `TRANSIENT` also creates a new instance per injection, but it does not force the whole dependency graph to become per-request; each injector simply gets its own copy.
- id: nestjs-routing-01
  answer: |
    `@Controller('cats')` sets the base path prefix for every route in that controller. `@Get()`/`@Post()` etc. map an HTTP method (plus a path appended to the prefix) to a handler method. `@Param()` reads route parameters, `@Query()` reads query-string parameters, and `@Body()` reads the request body.
- id: nestjs-routing-02
  answer: |
    Path parameter: `@Get(':id')` with `@Param('id') id: string`. Nested sub-path: append segments under the controller prefix, e.g. `@Controller('cats')` + `@Get('breeds/:breedId/cats/:catId')`. NestJS 11 adjusted wildcard route handling (catch-all `*` fallback routes and mid-path wildcard matching semantics); I don't know the exact v11 migration details, so verify those against the v11 changelog.
- id: nestjs-routing-03
  answer: |
    Validation and transformation run at runtime via `class-validator` decorators and `class-transformer` on class instances. TypeScript interfaces are erased at compile time, so Nest cannot inspect or validate them. Classes also let Nest instantiate a real DTO object rather than a plain object.
- id: nestjs-routing-04
  answer: |
    Default status is 200 OK for most methods and 201 Created for POST. Override with `@HttpCode(HttpStatus.CREATED)` (or any status) on the handler. If the handler returns a Promise, Nest awaits it and sends the resolved value; if it returns an observable, Nest subscribes and sends the emitted value(s), waiting for settlement before finishing the response.
- id: nestjs-lifecycle-01
  answer: |
    `OnModuleInit.onModuleInit()` fires once the module and its providers are instantiated, during initialization, before the app listens. `OnApplicationBootstrap.onApplicationBootstrap()` fires after every module's `onModuleInit` has run, just before the application starts accepting connections. Init runs per-module first; bootstrap runs once, after all modules are initialized.
- id: nestjs-lifecycle-02
  answer: |
    Shutdown hooks: `OnModuleDestroy.onModuleDestroy()` and `OnApplicationShutdown.onApplicationShutdown()`. They run `onModuleDestroy` first, then `onApplicationShutdown`. To fire on SIGTERM/SIGINT you must call `app.enableShutdownHooks()` (optionally passing the signals) when starting the app.
- id: nestjs-lifecycle-03
  answer: |
    Destroy hooks run in reverse order: if init ran C → B → A, destroy runs A → B → C. I am not aware of a NestJS 11 change to this ordering; reverse-order teardown has been the standard behavior (verify against v11 notes if you rely on it).
- id: nestjs-lifecycle-04
  answer: |
    Init hooks fire during bootstrap, when `app.init()`/`app.listen()` starts the application, after modules and providers are created. Yes — if an init hook returns a Promise, Nest awaits it before continuing.
- id: nestjs-validation-01
  answer: |
    `ValidationPipe` validates incoming request data against `class-validator` decorators on a DTO class and can transform it into that class. It relies on `class-validator` and `class-transformer`. Apply globally in main.ts: `app.useGlobalPipes(new ValidationPipe())`.
- id: nestjs-validation-02
  answer: |
    `whitelist: true` strips properties that have no validation decorators from the payload. `forbidNonWhitelisted: true` throws a 400 error instead of stripping. Together they keep undeclared or malicious fields from reaching your handler.
- id: nestjs-validation-03
  answer: |
    `transform: true` runs `plainToInstance`, turning the payload into an instance of the DTO class so decorators work and values are coerced to declared types. A `@Param('id') id: number` receives the route string coerced into an actual number.
- id: nestjs-validation-04
  answer: |
    `ParseIntPipe` converts a parameter value to an integer and throws 400 Bad Request if it isn't a valid integer. A globally registered pipe runs for every route/handler; a pipe bound to one parameter or route runs only there.
- id: nestjs-guards-01
  answer: |
    A guard decides whether a request may proceed. It implements `CanActivate` with `canActivate(context: ExecutionContext): boolean | Promise<boolean> | Observable<boolean>`. Returning `true` lets the request continue; `false` (or throwing) rejects it.
- id: nestjs-guards-02
  answer: |
    Order: middleware → guards → interceptors (before the handler) → pipes → route handler → interceptors (after the handler), with exception filters catching exceptions thrown anywhere along the way. Interceptors wrap the handler via `next.handle()`, so they run both before and after it.
- id: nestjs-guards-03
  answer: |
    An interceptor implements `NestInterceptor` with `intercept(context, next): Observable<any>`. Good for: transforming responses (e.g. wrapping every payload in a standard envelope) and logging/timing (also adding headers, caching, error handling).
- id: nestjs-guards-04
  answer: |
    The custom decorator attaches per-route metadata via `SetMetadata('roles', roles)`. The guard reads it with the `Reflector` class, e.g. `this.reflector.get<string[]>('roles', context.getHandler())` (often also checking `context.getClass()`), then compares the roles against the current user's roles.
- id: nestjs-filters-01
  answer: |
    `HttpException` is Nest's base class for HTTP errors, carrying a status code and response object. Built-ins (`BadRequestException`, `NotFoundException`, `ForbiddenException`, `UnauthorizedException`, `InternalServerErrorException`, etc.) extend it. Throwing one produces an error response with that status and a JSON body like `{ message, statusCode, error }`.
- id: nestjs-filters-02
  answer: |
    Mark it with `@Catch(MyException)` (or `@Catch()` for all), implement `ExceptionFilter<T>` with `catch(exception: T, host: ArgumentsHost)`. The catch method receives the exception and an `ArgumentsHost` (use `host.switchToHttp()` to get the request/response).
- id: nestjs-filters-03
  answer: |
    Exception filters resolve closest-to-the-handler first: route-level → controller-level → global. Register a global filter with `app.useGlobalFilter(new MyFilter())` in main.ts (or module-scoped via `app.select(...)`/`ModuleRef`).
- id: nestjs-filters-04
  answer: |
    A plain `Error` falls to Nest's built-in default exception filter, which returns 500 Internal Server Error with a generic body (`{ message: 'Internal server error', statusCode: 500 }`), hiding stack-trace details from the client.
- id: nestjs-providers-01
  answer: |
    Use a custom provider with `useFactory` whose factory is an `async` function. Dependencies can be passed into the factory via `inject`, and the factory can await a Promise (e.g. establishing a DB connection):

    ```ts
    {
      provide: 'DB_CONNECTION',
      useFactory: async (config: ConfigService) => {
        const connection = await createConnection(config.get('db'));
        return connection;
      },
      inject: [ConfigService],
    }
    ```

    Yes. Nest awaits every async provider during module instantiation, so the application does not become ready (and `app.listen()` / request handling does not begin) until those Promises resolve. Lifecycle hooks such as `onModuleInit` run only after their depended-on providers are resolved.
- id: nestjs-providers-02
  answer: |
    `useValue` registers a ready-made value (object, string, instance, etc.) directly — no instantiation and no constructor injection. Pick it for constants, simple configuration objects, or plain-object mocks.

    `useClass` registers a class that Nest instantiates, resolving its own constructor-injected dependencies. Pick it when the provider needs DI itself, or when you want to swap implementations through the provider token (e.g. `{ provide: Storage, useClass: S3Storage }`).

    `useFactory` registers a factory function; `inject` lists dependencies passed as arguments to that function. Pick it for dynamic or conditional selection, values computed from other providers, or async setup (the factory may be `async`, as in a DB connection). A factory can return a value, a class instance, or a Promise.
- id: nestjs-providers-03
  answer: |
    `ConfigModule.forRoot()` loads environment variables (from `process.env` and `.env` files) and exposes configuration through the injectable `ConfigService`. Services read values via `configService.get('KEY', defaultValue)`.

    `isGlobal: true` (e.g. `ConfigModule.forRoot({ isGlobal: true })`) makes `ConfigService` a global provider, so any module can inject it without importing `ConfigModule`. Without `isGlobal: true`, each module must import `ConfigModule` (or use `ConfigModule.forFeature(...)` for a named subset) before injecting `ConfigService`.
- id: nestjs-providers-04
  answer: |
    Build the test module with `Test.createTestingModule({ ... })`, supplying `providers`, `controllers`, and `imports` much like a real `@Module()`, then call `.compile()` to instantiate it:

    ```ts
    const moduleRef = Test.createTestingModule({
      providers: [UsersService],
    }).compile();
    const service = moduleRef.get(UsersService);
    ```

    Replace a real provider with a mock by overriding its token in the `providers` array, using either `useClass` (a mock class with DI) or `useValue` (a plain-object mock):

    ```ts
    const moduleRef = Test.createTestingModule({
      providers: [
        UsersService,
        { provide: UsersRepository, useClass: MockUsersRepository },
      ],
    }).compile();
    ```

    After compilation you can also override with `moduleRef.overrideProvider(UsersRepository).useValue(mock).useClass(...)` followed by `.compile()`. Retrieve instances with `moduleRef.get()`.
