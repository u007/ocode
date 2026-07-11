# .NET Platform Benchmark — Human Render

> **Generated from `questions.yaml` (corpus_rev 1). Do not grade from this file
> — `questions.yaml` is the source of truth.** Hand-synced; if they disagree,
> the YAML wins.

Legend: **W** = weight (1–3), **D** = difficulty. Rubric shows scored points
(`•`) and partial-credit levels (`~`). Platform corpus (runtime/BCL + ASP.NET
Core + EF Core + host/DI); language questions live in the `csharp` corpus.

---

### dotnet-di-lifetimes-01 · hosting-di · W3 · easy
**Q:** Explain the transient, scoped, and singleton DI lifetimes and when each is resolved to a new instance.
**A:** Transient = new instance per resolve. Scoped = one per DI scope (per HTTP request in ASP.NET Core), reused within that request. Singleton = one instance for the whole app lifetime, shared across requests/threads. Registered via AddTransient/AddScoped/AddSingleton.
• transient = new per resolve • scoped = one per scope/request • singleton = one per app, shared ~ names three but conflates scoped with singleton/per-call

### dotnet-di-captive-02 · hosting-di · W3 · hard
**Q:** What is a captive dependency, and why is injecting a scoped service into a singleton a bug?
**A:** A longer-lived service capturing a shorter-lived one past its lifetime. A singleton holding a scoped service freezes the first instance for the whole app — no fresh per-request instance, and a scoped DbContext gets shared across requests/threads (not thread-safe) → corruption. Scope validation throws in Development. Fix: inject IServiceScopeFactory and create a scope.
• longer-lived captures shorter-lived past its lifetime • singleton→scoped shared across requests/threads (corruption) • fix IServiceScopeFactory/CreateScope ~ "lifetime mismatch is bad" without the capture consequence

### dotnet-di-scope-in-singleton-03 · hosting-di · W2 · medium
**Q:** A singleton background service needs a scoped service (DbContext) each iteration — how, correctly?
**A:** Don't constructor-inject the scoped service (captures it). Inject IServiceScopeFactory, and per unit of work `using var scope = scopeFactory.CreateScope();` then resolve the scoped service from `scope.ServiceProvider`, disposing the scope when done.
• inject IServiceScopeFactory not the scoped service • CreateScope per unit of work, resolve from scope, dispose ~ "use a scope" without IServiceScopeFactory / per-iteration create+dispose

### dotnet-di-keyed-04 · hosting-di · W2 · medium
**Q:** Two implementations of one interface resolvable by name — what feature, which .NET version, how consumed?
**A:** Keyed services, added in .NET 8. Register AddKeyedSingleton/Scoped/Transient with a key; consume via [FromKeyedServices("fast")] or GetRequiredKeyedService<T>(key). .NET 9 tightened it: [FromKeyedServices] throws if no matching keyed service is registered.
• keyed services, .NET 8 • AddKeyed{Singleton,Scoped,Transient} + [FromKeyedServices]/GetRequiredKeyedService ~ "register two, inject IEnumerable<T>" (works, not the keyed feature)

### dotnet-config-precedence-01 · configuration-options · W3 · medium
**Q:** Default IConfiguration source precedence — what wins when a key exists in several sources?
**A:** Providers are layered in registration order; last to set a key wins. Default host order: appsettings.json → appsettings.{Environment}.json → User Secrets (Dev only) → environment variables → command-line args. So command line overrides env vars override env-specific JSON override base JSON. Nested keys use `:` (or `__` in env var names); keys are case-insensitive.
• layered, last to set a key wins • order appsettings→appsettings.{Env}→user secrets→env→command line • nested via ':' (or '__') ~ "later overrides earlier" without the actual order

### dotnet-config-options-interfaces-02 · configuration-options · W3 · hard
**Q:** Contrast IOptions/IOptionsSnapshot/IOptionsMonitor — lifetime, reloads, where injectable.
**A:** IOptions = singleton, computed once, no reload, inject anywhere. IOptionsSnapshot = scoped, recomputed per request (sees reloads), named options, but can't inject into a singleton. IOptionsMonitor = singleton, CurrentValue + OnChange, always reflects reloads, injectable anywhere including singletons.
• IOptions singleton one-time no reload • IOptionsSnapshot scoped per-request recompute, named, not into singleton • IOptionsMonitor singleton CurrentValue+OnChange, reloads, injectable anywhere ~ only one pair distinguished

### dotnet-config-secrets-03 · configuration-options · W2 · medium
**Q:** Where do local dev secrets live vs production?
**A:** Dev: Secret Manager / User Secrets (`dotnet user-secrets set`), stored per-user OUTSIDE the project by UserSecretsId, never in source control, Development-only, not encrypted. Prod: env vars or a real secret store (Key Vault etc.). User secrets are not for production; never commit secrets to appsettings.json.
• dev User Secrets out-of-repo, Dev-only • prod env vars / real secret store (not user secrets) ~ "use user secrets" without not-for-prod/out-of-repo

### dotnet-config-options-binding-04 · configuration-options · W2 · medium
**Q:** Bind a config section to a typed options class, and fail fast on invalid options?
**A:** `AddOptions<MyOptions>().Bind(config.GetSection("My")).ValidateDataAnnotations().ValidateOnStart()` (or `Configure<MyOptions>(section)`). Bind maps keys onto the POCO; ValidateOnStart forces validation at host startup so bad config throws immediately. Consume via IOptions/Snapshot/Monitor.
• bind section to POCO (AddOptions().Bind / Configure<T>) • ValidateDataAnnotations + ValidateOnStart fail fast ~ "use Configure<T>" without validation half

### dotnet-pipeline-order-01 · aspnetcore-pipeline · W3 · hard
**Q:** Why is middleware order-sensitive, e.g. UseAuthentication/UseAuthorization vs UseRouting/endpoints?
**A:** Onion model: each middleware runs, awaits next(), then runs on the way back; registration order = execution order, so a component only affects what's after it. UseRouting before UseAuthorization (endpoint + auth metadata selected first), and auth before the endpoint runs so unauthorized requests are rejected before the handler.
• onion: run, await next(), run back • order = execution order, affects only what's after • UseRouting before auth, auth before endpoint ~ "order matters" without next()/two-pass or a concrete ordering

### dotnet-pipeline-use-vs-map-02 · aspnetcore-pipeline · W2 · medium
**Q:** Difference between Use, Run, and Map, and what is short-circuiting?
**A:** Use = middleware that can call next or short-circuit. Run = terminal (never calls next). Map/MapWhen branches the pipeline by path/predicate into a sub-pipeline. Short-circuit = a middleware produces the response and skips the rest of the chain (auth 401, cache hit).
• Use can next or short-circuit; Run terminal • Map/MapWhen branches by path/predicate • short-circuit = respond without next ~ explains only one without the Use/Run/Map distinction

### dotnet-pipeline-minimal-results-03 · aspnetcore-pipeline · W2 · medium
**Q:** How does a minimal API return value become a response, and Results/TypedResults vs a raw object?
**A:** Return value is auto-converted: object → JSON+200, string → text, IResult controls status/headers. Results.Ok/NotFound/BadRequest produce IResult explicitly; TypedResults.Ok(x) returns a concrete typed result (Ok<T>) — strongly typed, better for testing and OpenAPI — vs untyped IResult.
• return auto-converted (object→JSON+200, IResult controls status) • Results/TypedResults explicit; TypedResults strongly typed ~ "return Results.Ok" without auto-serialization/typing

### dotnet-pipeline-filters-vs-middleware-04 · aspnetcore-pipeline · W2 · hard
**Q:** Endpoint/action filter vs middleware — when each, and what can a filter do middleware can't?
**A:** Middleware = app-wide, raw HttpContext, before routing — cross-cutting (logging, exceptions, CORS, auth). A filter = per-endpoint, after model binding, so it sees the bound arguments and endpoint result and can transform/short-circuit them. Filter for endpoint-specific logic needing bound inputs; middleware for app-wide routing-agnostic concerns.
• middleware app-wide/raw/before routing; filter per-endpoint/after binding • filters see bound args + result (middleware can't) ~ "both intercept requests" without the scope distinction

### dotnet-efcore-context-lifetime-01 · efcore · W3 · hard
**Q:** Correct DbContext lifetime, and why isn't an instance safe across threads/concurrent async ops?
**A:** Short-lived — one unit of work; AddDbContext registers scoped (per request) by default. DbContext is NOT thread-safe: no parallel operations on one instance (incl. concurrent async queries) — EF throws "A second operation started..." and undetected concurrency corrupts change-tracker state. For parallel work use separate instances (own scope) / IDbContextFactory; await each op.
• short-lived, scoped by default • not thread-safe, no concurrent ops (throws/corrupts) • separate instances for parallel / await each ~ "use scoped" without the thread-safety reason

### dotnet-efcore-notracking-02 · efcore · W3 · medium
**Q:** What does the change tracker do, and when/why AsNoTracking()?
**A:** By default EF snapshots returned entities so SaveChanges knows what to persist (and identity resolution). That costs memory/CPU. For read-only queries (GET/list) add AsNoTracking() to skip tracking — faster and lighter. Keep tracking when you'll update and SaveChanges.
• tracker snapshots entities so SaveChanges persists changes • AsNoTracking for read-only → skips tracking; keep tracking to update+save ~ "AsNoTracking is faster" without why

### dotnet-efcore-nplus1-03 · efcore · W3 · hard
**Q:** Deferred IQueryable vs ToListAsync(), and how N+1 arises and is fixed?
**A:** IQueryable is deferred — Where/Select build an expression, nothing runs until enumeration (ToListAsync/foreach); ToListAsync forces execution now. N+1 = 1 query for the list then one per row for a related navigation (lazy loading in a loop / unloaded projection) = 1+N round trips. Fix with Include()/ThenInclude() (or a Select projection) to load the relation in one query.
• IQueryable deferred, executes on enumeration; ToListAsync forces it • N+1 = 1 + per-row related query • fix Include/ThenInclude/projection ~ defines N+1 without deferred execution or Include fix

### dotnet-efcore-savechanges-tx-04 · efcore · W2 · medium
**Q:** Is a single SaveChangesAsync() transactional, and what is a migration's role?
**A:** Yes — one SaveChanges wraps all its INSERT/UPDATE/DELETE in a single transaction (all-or-nothing); an explicit BeginTransaction is only needed to span multiple SaveChanges/commands. Migrations = versioned schema evolution: `migrations add` diffs the model and generates code, `database update` applies pending migrations to keep the DB in sync.
• one SaveChanges atomic (single transaction) • explicit tx to span multiple SaveChanges • migrations = versioned schema evolution (add/update) ~ only the tx half or only migrations

### dotnet-gc-generations-loh-01 · memory-gc · W2 · hard
**Q:** Generational GC (gen 0/1/2) and the LOH — why are large short-lived allocations costly?
**A:** Gen 0 is collected frequently and cheaply (most objects die young); survivors promote gen1→gen2, and gen2 is collected rarely because full collections are expensive. Objects ≥ ~85 KB go on the LOH, collected only with gen 2 and not compacted by default — churning large buffers triggers expensive full GCs and fragments the LOH. Pool large buffers (ArrayPool).
• gen0 cheap/frequent, promote to gen1→gen2, gen2 rare/expensive • ≥~85 KB → LOH, collected with gen2, not compacted ~ mentions generations but not LOH threshold / full-GC cost

### dotnet-gc-dispose-finalizer-02 · memory-gc · W3 · hard
**Q:** IDisposable/Dispose vs a finalizer — when each, and what does IAsyncDisposable add?
**A:** Dispose = deterministic cleanup you/`using` call at a known point (release unmanaged + other IDisposables). A finalizer (~Type) is the GC-run, non-deterministic safety net; finalizable objects survive an extra GC so they're costly — only add one for a directly-owned unmanaged handle, implement the full pattern and call GC.SuppressFinalize when disposed (prefer SafeHandle). IAsyncDisposable/DisposeAsync (`await using`) does async cleanup.
• Dispose deterministic/using-invoked; finalizer GC-run safety net • finalizers costly (extra GC) — only unmanaged; SuppressFinalize when disposed • IAsyncDisposable for async cleanup ~ "Dispose frees resources" without finalizer contrast/async

### dotnet-gc-span-arraypool-03 · memory-gc · W2 · hard
**Q:** Span<T>, stackalloc, ArrayPool<T> — what does each avoid?
**A:** Span<T>/ReadOnlySpan<T> = stack-only view/slice over memory (array, stackalloc, slice) — process/slice without allocating a new array or copying. stackalloc = small stack buffer (as Span<T>) freed on return, zero GC pressure. ArrayPool<T>.Shared rents/returns reusable arrays so hot paths reuse instead of allocating large arrays (return what you rent). Together they cut gen-0/LOH churn.
• Span = allocation-free view/slice (no new array/copy) • stackalloc = stack buffer (no GC); ArrayPool = rent/return reusable arrays ~ names them without what allocation each avoids

### dotnet-gc-struct-vs-class-04 · memory-gc · W2 · medium
**Q:** Struct vs class from an allocation standpoint, and what is boxing?
**A:** Class = reference type on the heap, GC-tracked, variables hold references. Struct = value type, copied by value; as a local/field can live on the stack or inline with no separate heap allocation and no GC — good for small short-lived values, but copies on assignment (large structs costly). Boxing = converting a value type to object/interface allocates a heap box and copies the value, defeating the struct win.
• class heap reference type (GC); struct value type, copied, can avoid heap alloc • boxing = value→object/interface heap-allocates ~ struct-vs-class without boxing, or "structs faster" without reason

### dotnet-json-stj-defaults-01 · serialization · W2 · medium
**Q:** System.Text.Json camelCase output, and overriding one property's name?
**A:** JsonSerializerOptions.PropertyNamingPolicy = JsonNamingPolicy.CamelCase (ASP.NET Core already defaults to camelCase). For one property, [JsonPropertyName("custom_name")] overrides the policy for that member. Related knobs: DefaultIgnoreCondition, WriteIndented, converters.
• PropertyNamingPolicy = CamelCase • [JsonPropertyName] overrides a single property ~ names one but not both

### dotnet-json-sourcegen-02 · serialization · W2 · hard
**Q:** STJ source generation — why it matters for performance and trimming/Native AOT?
**A:** The source generator emits serialization metadata/logic at compile time instead of using runtime reflection: declare a `partial class MyContext : JsonSerializerContext` with [JsonSerializable(typeof(T))] and pass the context/TypeInfo to Serialize/Deserialize. Benefits: no runtime reflection (faster startup, less allocation) and trim/Native-AOT safe (types statically referenced, no dynamic reflection).
• compile-time metadata via JsonSerializerContext + [JsonSerializable] (not reflection) • faster/less-alloc AND trim/AOT safe ~ "it's faster" without the AOT/compile-time reason

### dotnet-json-stj-vs-newtonsoft-03 · serialization · W2 · medium
**Q:** System.Text.Json vs Newtonsoft.Json trade-offs?
**A:** STJ: built-in, faster, lower-allocation, Span-based, source-gen/AOT, ASP.NET Core default — but stricter and historically fewer features. Newtonsoft: more permissive and feature-rich (reference-loop/$id-$ref handling, broad converters, dynamic/JObject, populate-existing) — use when you need those or legacy behavior. Default STJ; drop to Newtonsoft for a feature it lacks.
• STJ built-in/faster/AOT/default but stricter • Newtonsoft more permissive & feature-rich — use when needed ~ "STJ is faster" without the feature trade-off

### dotnet-json-options-reuse-04 · serialization · W2 · medium
**Q:** Why reuse a JsonSerializerOptions instance, and how does STJ do polymorphism?
**A:** First use builds and caches per-type metadata on that options instance; a fresh options per call rebuilds the cache each time (slow, allocation-heavy; options become immutable once used) — so reuse one static/shared instance. Polymorphism: STJ serializes by the declared type by default (omits derived-only members); opt in with [JsonDerivedType(typeof(Derived), "disc")] + [JsonPolymorphic] on the base to write a discriminator that round-trips (or serialize as object).
• options cache per-type metadata on first use → reuse one instance • serializes by declared type; opt into polymorphism with [JsonDerivedType]/[JsonPolymorphic] ~ only reuse OR only polymorphism

### dotnet-http-socket-exhaustion-01 · resilience-http · W3 · hard
**Q:** Why does `new HttpClient()` per request exhaust sockets, and how does IHttpClientFactory fix it?
**A:** HttpClient is meant to be reused; a new one per request opens new connections and disposing leaves sockets in TIME_WAIT → port/socket exhaustion under load. A single static HttpClient avoids exhaustion but never picks up DNS changes. IHttpClientFactory pools HttpMessageHandlers with a rotating lifetime — cheap short-lived clients share pooled handlers (no exhaustion) while rotation respects DNS. Register AddHttpClient, inject IHttpClientFactory / a typed client.
• new-per-request + dispose → TIME_WAIT socket exhaustion • static client avoids exhaustion but ignores DNS • factory pools/rotates handlers → no exhaustion + DNS ~ "reuse HttpClient/use the factory" without socket/DNS reasoning

### dotnet-http-typed-clients-02 · resilience-http · W2 · medium
**Q:** Named vs typed clients with IHttpClientFactory, and why prefer typed?
**A:** Named: AddHttpClient("github", ...) resolved via CreateClient("github"). Typed: AddHttpClient<GitHubClient>(...) — inject GitHubClient directly, it gets a configured HttpClient in its constructor, encapsulating base address/headers/calls. Prefer typed: strongly typed, testable, endpoint logic in one place, no magic strings — still factory-managed.
• named = CreateClient("name"); typed = AddHttpClient<T>, inject T • prefer typed: typed/testable/encapsulated, factory-managed ~ one without the distinction or preference reason

### dotnet-http-lifetime-dns-03 · resilience-http · W2 · hard
**Q:** Why can factory clients still serve stale DNS, and the SocketsHttpHandler alternative?
**A:** A client is bound to its handler; the factory rotates handlers on HandlerLifetime (~2 min default) so new clients pick up refreshed DNS — but caching a factory client in a singleton field keeps its original handler and never sees rotation, so request a fresh client near each use. Alternative: configure SocketsHttpHandler.PooledConnectionLifetime to recycle connections for DNS, and disable factory rotation (SetHandlerLifetime(Timeout.InfiniteTimeSpan)).
• client tied to handler; factory rotates on HandlerLifetime — caching in a singleton defeats it • alt: SocketsHttpHandler.PooledConnectionLifetime for DNS ~ "don't cache the client" without handler-lifetime/PooledConnectionLifetime

### dotnet-http-resilience-04 · resilience-http · W2 · medium
**Q:** Add retries/timeouts/circuit breaker to an HttpClient in modern .NET, and where do cancellation tokens fit?
**A:** Microsoft.Extensions.Http.Resilience (Polly v8): chain .AddStandardResilienceHandler() onto AddHttpClient for a default pipeline (retry+backoff, total + per-attempt timeout, circuit breaker), or AddResilienceHandler for a custom one (older code: AddTransientHttpErrorPolicy/AddPolicyHandler). Flow a CancellationToken into SendAsync so the caller / request-aborted token / a timeout CTS can cancel cooperatively; resilience timeouts use linked cancellation.
• Microsoft.Extensions.Http.Resilience AddStandardResilienceHandler (retry/timeout/breaker, .NET 8) • flow CancellationToken for cancellation/timeouts ~ "use Polly" without the resilience handler or cancellation half

### dotnet-log-templates-01 · logging-diagnostics · W3 · medium
**Q:** Why `LogInformation("Order {OrderId} shipped", orderId)` instead of interpolation?
**A:** Structured logging: named placeholders + args are captured as key/value pairs, so providers index/query by OrderId and the constant template groups related logs. Interpolation ($"...{orderId}...") produces a plain formatted string — no structured fields, lost queryability — and builds the string eagerly even when the level is disabled (wasted work/allocation). Pass values as template arguments.
• templates capture named key/value pairs — queryable, grouped by template • interpolation loses structure + formats eagerly even when disabled ~ "templates are structured" without the eager-format/disabled cost

### dotnet-log-levels-02 · logging-diagnostics · W2 · easy
**Q:** ILogger levels in order, and per-category filtering (e.g. quieting EF Core)?
**A:** Trace < Debug < Information < Warning < Error < Critical (None disables). Filtering via Logging config ("Logging:LogLevel" / AddFilter): a Default minimum plus per-category overrides keyed by namespace prefix, most specific wins — e.g. Default=Information but "Microsoft.EntityFrameworkCore"=Warning. Below-minimum logs are dropped cheaply.
• order Trace<Debug<Information<Warning<Error<Critical (None disables) • per-category filtering: Default + namespace-prefix overrides (most specific wins) ~ lists levels without category filtering

### dotnet-log-highperf-03 · logging-diagnostics · W2 · hard
**Q:** What is the [LoggerMessage] source generator, and why prefer it over LogInformation on hot paths?
**A:** [LoggerMessage] (and older LoggerMessage.Define) generates a cached, strongly-typed logging delegate for a template + args. Direct LogInformation parses the template and boxes value-type args into a params array on every call, even when disabled; the generated method is IsEnabled-guarded, allocation-free, boxing-free, and pre-parses the template once — cheaper on high-frequency paths. Declare a partial method with the attribute (id/level/message) and call it.
• [LoggerMessage] source-generates a cached strongly-typed method (pre-parsed) • avoids per-call parse/box/alloc + guards IsEnabled ~ "it's faster logging" without allocation/caching reason

### dotnet-log-scopes-otel-04 · logging-diagnostics · W1 · medium
**Q:** What are logging scopes (BeginScope) for, and how does .NET expose tracing/metrics (OpenTelemetry)?
**A:** BeginScope(state) attaches contextual key/values (e.g. correlation id) to every log inside the `using` scope, sharing context across methods without threading it through (scope-capable providers must be enabled). Tracing/metrics use built-in primitives — System.Diagnostics.Activity/ActivitySource for spans, System.Diagnostics.Metrics.Meter for metrics — which OpenTelemetry collects and exports to backends (correlated traces, metrics, logs).
• BeginScope attaches contextual key/values to logs in the using block • tracing/metrics via Activity/ActivitySource + Meter, collected by OpenTelemetry ~ scopes without tracing/metrics (or vice versa)
