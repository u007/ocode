# NestJS Benchmark — Human Render

> **Generated from `questions.yaml` (corpus_rev 1). Do not grade from this file
> — `questions.yaml` is the source of truth.** Hand-synced; if they disagree,
> the YAML wins.

Legend: **W** = weight (1–3), **D** = difficulty. Rubric shows scored points
(`•`) and partial-credit levels (`~`).

---

### nestjs-modules-01 · modules · W3 · easy
**Q:** What does `@Module()` metadata (`imports`, `controllers`, `providers`, `exports`) declare, and what is a module for?
**A:** A module groups a slice of the app. `providers` = injectables Nest instantiates/serves inside the module; `controllers` = its HTTP controllers; `imports` = other modules whose exports become available; `exports` = the providers made visible to importers. Nest builds the DI container from the module graph.
• providers = injectables in the module • controllers + imports (pull in modules) • exports = providers visible to importers ~ names fields but not imports/exports wiring

### nestjs-modules-02 · modules, di · W3 · medium
**Q:** `AuthModule` provides `AuthService`; `UsersModule` wants to inject it — what must be true, and what if you forget?
**A:** Providers are private by default. `AuthModule` must `exports: [AuthService]` and `UsersModule` must `imports: [AuthModule]`. Declaring it in providers alone isn't enough. Forgetting either → "can't resolve dependencies" error at bootstrap.
• AuthService in AuthModule exports • UsersModule imports AuthModule • else unresolved-dependency error (encapsulation) ~ "import the module" but omits export

### nestjs-modules-03 · modules, providers-async · W2 · medium
**Q:** What is a dynamic module, and the conventional `forRoot()` vs `forFeature()` difference?
**A:** A module whose metadata is built at runtime by a static method returning `DynamicModule`, so callers pass config. `forRoot()` configures once at the root with global options (e.g. a connection); `forFeature()` registers feature-scoped pieces (e.g. entities/repos) per consuming module.
• dynamic module returns DynamicModule built at runtime • forRoot = configure once, global • forFeature = feature-scoped per module ~ describes dynamic module but not forRoot/forFeature split

### nestjs-modules-04 · modules · W1 · easy
**Q:** What does `@Global()` do, and why use it sparingly?
**A:** Registers the module's exports app-wide so consumers needn't import it (still imported once, usually at root). Overusing breaks encapsulation and hides dependencies — reserve for cross-cutting things (config, logging).
• @Global exports available everywhere without re-import • use sparingly — breaks encapsulation ~ "makes it global" no caveat

### nestjs-di-01 · di · W3 · easy
**Q:** How does Nest DI resolve a class dependency (`@Injectable`, registration, constructor injection)?
**A:** `@Injectable()` marks a manageable provider; register it in a module's `providers`. Nest reads the consumer's constructor param types (metadata) and injects the matching provider by token, caching one instance by default. A class is its own token.
• @Injectable + registered in providers • reads constructor types, injects by token • class is token; singleton cached ~ "injects automatically" no token/registration

### nestjs-di-02 · di, providers-async · W2 · medium
**Q:** How to register/inject a non-class value (config object, string) with a custom token?
**A:** Custom provider with a string/symbol token: `{ provide: 'CONFIG_OPTIONS', useValue: {...} }`. Since the type can't be inferred, inject with `@Inject('CONFIG_OPTIONS')`. Symbols avoid collisions.
• custom provider with string/symbol token (useValue/useFactory) • inject via @Inject(token) ~ mentions custom provider but omits @Inject

### nestjs-di-03 · di · W3 · medium
**Q:** Name the three injection scopes and their lifetimes; which is default and why?
**A:** `DEFAULT` (singleton) = one shared cached instance, created at bootstrap (the default, recommended). `REQUEST` = new instance per request. `TRANSIENT` = new instance per consumer (not shared). Set via `@Injectable({ scope })`.
• DEFAULT = singleton, default • REQUEST = per request • TRANSIENT = per consumer ~ names scopes but mixes REQUEST vs TRANSIENT

### nestjs-di-04 · di · W3 · hard
**Q:** Why does one `REQUEST`-scoped provider make dependents request-scoped, the perf cost, and does `TRANSIENT` do the same?
**A:** REQUEST bubbles UP: any dependent (incl. controllers) becomes request-scoped, so the chain is instantiated per request (~up to 5% latency) — keep it small. TRANSIENT does NOT bubble: a singleton injecting a transient gets a fresh instance but stays singleton.
• REQUEST propagates up (dependents become request-scoped) • per-request cost — keep minimal • TRANSIENT does not bubble ~ notes bubbling but claims TRANSIENT bubbles

### nestjs-routing-01 · controllers-routing · W3 · easy
**Q:** What does `@Controller('cats')` set, and how do `@Get`/`@Post`, `@Param`, `@Query`, `@Body` map a request?
**A:** `@Controller('cats')` sets the `/cats` prefix. Method decorators bind a verb (+ optional sub-path). `@Param('id')` reads a route param, `@Query('q')` a query field, `@Body()` the parsed body.
• @Controller = route prefix • method decorators bind verb + sub-path • @Param/@Query/@Body map param/query/body ~ muddles the param decorators

### nestjs-routing-02 · controllers-routing · W2 · medium
**Q:** Declare a route with a path param + nested sub-path; what changed about wildcards in NestJS 11?
**A:** `@Get(':id/profile')` with the prefix → `/cats/:id/profile`, read via `@Param('id')`. NestJS 11 adopts Express v5: the old bare `*` wildcard is discouraged (named `*splat` is correct); v11 auto-converts legacy syntax but don't rely on it.
• ':id' param + sub-path appends to prefix • NestJS 11/Express 5 changed wildcard syntax ~ shows param route but misses v11 wildcard change

### nestjs-routing-03 · controllers-routing, pipes-validation · W2 · medium
**Q:** Why are DTOs classes, not interfaces?
**A:** Interfaces are erased at compile time — nothing at runtime for Nest to use. Classes persist as runtime metatypes, and class-validator/class-transformer decorators attach metadata to the class and operate on instances.
• interfaces erased at compile time • classes persist → decorators/metadata + validation work ~ "use classes" no erasure reasoning

### nestjs-routing-04 · controllers-routing · W1 · easy
**Q:** Default status code, how to override it, and how are Promise/observable returns handled?
**A:** Default 200 (201 for `@Post`); override with `@HttpCode()`. Returned value = response body; a Promise (async) or RxJS observable is awaited/subscribed and the resolved/emitted value sent.
• default 200 (201 POST); @HttpCode overrides • returned value = body; Promise/observable awaited ~ one half only

### nestjs-lifecycle-01 · lifecycle · W2 · medium
**Q:** `OnModuleInit` vs `OnApplicationBootstrap` — when does each fire?
**A:** `onModuleInit()` runs once a module's own deps are resolved (per module). `onApplicationBootstrap()` runs after ALL modules are initialized, before listening. Use the latter when every module must be ready.
• onModuleInit = after that module's deps resolved • onApplicationBootstrap = after all modules, before listening ~ "both at startup" no distinction

### nestjs-lifecycle-02 · lifecycle · W2 · medium
**Q:** Shutdown hooks in order, and what to enable for OS signals?
**A:** `onModuleDestroy()` → `beforeApplicationShutdown()` (gets the signal) → `onApplicationShutdown()` (after connections close). Fire on `app.close()`; to react to SIGTERM/SIGINT call `app.enableShutdownHooks()`.
• order: onModuleDestroy → beforeApplicationShutdown → onApplicationShutdown • enableShutdownHooks() for OS signals ~ lists hooks but omits enableShutdownHooks

### nestjs-lifecycle-03 · lifecycle · W2 · hard
**Q:** If init runs C→B→A, what order do destroy hooks run, and did this change in NestJS 11?
**A:** NestJS 11 runs termination hooks in REVERSE of init: init C→B→A means destroy A→B→C. Global modules are a dependency of all — initialized first, destroyed last. This reverse-order guarantee is a v11 change.
• destroy runs reverse of init (A→B→C) • reverse-order behaviour is a NestJS 11 change ~ "reverse order" but not attributed to v11

### nestjs-lifecycle-04 · lifecycle · W1 · easy
**Q:** What triggers init hooks, and does Nest await async hooks?
**A:** Init hooks fire on `app.init()` (called by `app.listen()`). Nest awaits a Promise-returning hook before moving to the next phase, so async setup is safe.
• init hooks fire on app.init()/listen() • Nest awaits a Promise-returning hook ~ one half only

### nestjs-validation-01 · pipes-validation · W3 · easy
**Q:** What does `ValidationPipe` do, which two libraries, and how to apply globally?
**A:** Validates/transforms payloads against a DTO. Relies on `class-validator` (rules) + `class-transformer` (object↔instance), both installed. Apply globally via `app.useGlobalPipes(new ValidationPipe())` (or per-route/param).
• validates/transforms against DTO • uses class-validator + class-transformer • register via useGlobalPipes ~ "validates DTOs" without both libraries

### nestjs-validation-02 · pipes-validation · W2 · medium
**Q:** What do `whitelist` and `forbidNonWhitelisted` do?
**A:** `whitelist: true` strips properties with no validation decorator (silently removes unknown fields). `forbidNonWhitelisted: true` turns strip into reject — a 400 when non-whitelisted props are present.
• whitelist strips undecorated properties • forbidNonWhitelisted rejects instead of stripping ~ conflates the two

### nestjs-validation-03 · pipes-validation · W2 · medium
**Q:** What does `transform: true` do, including `@Param('id'): number`?
**A:** Returns real DTO class instances (class-transformer) and does implicit primitive conversion: a string param is coerced to the declared type, so `@Param('id') id: number` is a real `number`.
• returns DTO class instances • implicit primitive conversion (string→number) ~ "transforms payload" without conversion detail

### nestjs-validation-04 · pipes-validation · W2 · medium
**Q:** What does `ParseIntPipe` do, and global pipe vs route-param pipe?
**A:** `ParseIntPipe` parses one value to an int and throws 400 on invalid (`@Param('id', ParseIntPipe)`). A global pipe runs on every handler app-wide; a route/param pipe is scoped to that handler/param only.
• ParseIntPipe parses to int, throws on invalid • global = app-wide, route/param = scoped ~ describes ParseIntPipe but not global-vs-scoped

### nestjs-guards-01 · guards-interceptors · W3 · easy
**Q:** What is a guard, what interface, and how does its return value control the request?
**A:** Implements `CanActivate` / `canActivate(context)`, returns a boolean (or Promise/observable): `true` proceeds, `false` denies (403). Gets `ExecutionContext`; used for authz/authn; attached with `@UseGuards`.
• implements CanActivate • boolean: true proceeds, false denies (403) • @UseGuards; authz/authn ~ "protects routes" no mechanism

### nestjs-guards-02 · guards-interceptors, pipes-validation · W3 · medium
**Q:** Order the pipeline (middleware, guards, interceptors, pipes, handler, filters); where do interceptors run vs the handler?
**A:** middleware → guards → interceptor(pre) → pipes → handler → interceptor(post) → exception filters → response. Interceptors WRAP the handler (before and after, RxJS). Guards run before any interceptor/pipe; filters catch anything thrown.
• middleware → guards → interceptor(pre) → pipes → handler • interceptors wrap (post-handler too), before filters • guards before interceptors/pipes ~ misses interceptor post-phase (wrap)

### nestjs-guards-03 · guards-interceptors · W2 · medium
**Q:** What is an interceptor, what interface, and two good uses?
**A:** Implements `NestInterceptor` / `intercept(context, next)`. `next.handle()` returns an RxJS `Observable` of the result — run before, transform after (`map`/`tap`/`catchError`). Good for response shaping, logging/timing, caching, error mapping.
• implements NestInterceptor / intercept(context, next) • next.handle() Observable — before + transform after • two valid uses ~ "intercepts requests" no next.handle detail

### nestjs-guards-04 · guards-interceptors, di · W2 · medium
**Q:** How does a roles guard read `@Roles('admin')` metadata, and what class?
**A:** `@Roles` sets metadata via `SetMetadata`/a Reflector decorator. The guard injects `Reflector` and reads `getAllAndOverride(KEY, [getHandler(), getClass()])` (method overrides class), then compares to the user from the request.
• metadata via SetMetadata/custom decorator • guard uses Reflector (getAllAndOverride) on handler+class ~ "checks roles" no Reflector mechanism

### nestjs-filters-01 · exception-filters · W2 · easy
**Q:** What is `HttpException`, how do built-ins relate, and what does throwing produce?
**A:** Base HTTP error (message + status). Built-ins (`NotFoundException`, `BadRequestException`, …) extend it. Throwing is caught by Nest's exception layer and turned into a proper JSON HTTP response with that status — no manual formatting.
• HttpException = base (message + status) • built-ins extend it; throwing yields right HTTP response ~ names HttpException but not subclasses/auto response

### nestjs-filters-02 · exception-filters · W2 · medium
**Q:** How to write a custom exception filter — decorator, interface, catch argument?
**A:** `@Catch(SomeException)` (or bare `@Catch()`) + implement `ExceptionFilter` / `catch(exception, host: ArgumentsHost)`. `host.switchToHttp().getResponse()` builds a custom response. Bind via `@UseFilters()` or globally.
• @Catch selects what it handles • ExceptionFilter / catch(exception, host) • ArgumentsHost → response; @UseFilters ~ mentions @Catch but not ExceptionFilter/ArgumentsHost

### nestjs-filters-03 · exception-filters · W2 · medium
**Q:** Guards/interceptors/pipes go global→controller→route; what order do filters resolve, and how to register a global filter?
**A:** Filters resolve the OPPOSITE way: route → controller → global (most specific wins; lower-level catch isn't re-caught higher). Register globally with `app.useGlobalFilters(...)`, or an `APP_FILTER` provider when DI is needed.
• filters resolve route → controller → global (reverse) • register via useGlobalFilters (or APP_FILTER for DI) ~ knows registration but wrong order

### nestjs-filters-04 · exception-filters · W1 · easy
**Q:** A plain `Error` (not HttpException) with no custom filter — what does the client get?
**A:** Nest's built-in global filter catches it; without an inferable status it returns a generic `500 Internal Server Error` with a standard body, logging the real error rather than leaking it. Throw an `HttpException` for a meaningful status.
• unrecognized error → default 500 • built-in global filter; details not leaked / throw HttpException for real status ~ "it errors" without 500

### nestjs-providers-01 · providers-async · W2 · medium
**Q:** How to create an async provider depending on a Promise, and does Nest wait?
**A:** `useFactory` that's `async`/returns a Promise: `{ provide: 'DB', useFactory: async () => await createConnection(), inject: [...] }`. Nest awaits it during bootstrap and won't instantiate dependents until it resolves.
• async useFactory returning a Promise (inject deps) • Nest awaits during bootstrap before dependents ~ mentions useFactory but not the await

### nestjs-providers-02 · providers-async · W2 · easy
**Q:** Contrast `useValue`, `useClass`, `useFactory`.
**A:** `useValue` = ready-made constant/instance/mock. `useClass` = Nest instantiates a class (swap impl per env behind one token). `useFactory` = computed function, can `inject` deps and be async.
• useValue = constant/existing instance/mock • useClass = Nest instantiates (swappable) • useFactory = computed, inject/async ~ conflates the others

### nestjs-providers-03 · providers-async, modules · W2 · medium
**Q:** How does `ConfigModule` expose config, and what does `isGlobal: true` change?
**A:** `ConfigModule.forRoot()` loads env/config and registers `ConfigService`, read via `configService.get('KEY')`. `forRoot({ isGlobal: true })` makes it global so every module can inject `ConfigService` without importing the module.
• forRoot loads config; inject ConfigService and .get('KEY') • isGlobal → global, no per-module import ~ mentions ConfigService but not forRoot/isGlobal

### nestjs-providers-04 · providers-async, di · W2 · medium
**Q:** With `@nestjs/testing`, how to build a test module and replace a provider with a mock?
**A:** `Test.createTestingModule({...})`, chain `.overrideProvider(Real).useValue(mock)`, then `await ...compile()`. Retrieve instances with `moduleRef.get(Token)` (`resolve()` for scoped) and assert.
• createTestingModule({...}).compile() • overrideProvider(X).useValue(mock) • retrieve via moduleRef.get() (resolve() for scoped) ~ mentions createTestingModule but not overrideProvider
