# nvidia/nemotron-3.5-super-vl — with-skill answers for dotnet (openrouter, 2026-10-16); answerer saw only _prompts/dotnet.md; derived/dotnet.nvidia__nemotron-3.5-super-vl.SKILL.md body (front matter stripped) prepended to the sheet as active guidance
- id: dotnet-di-lifetimes-01
  answer: |
    Transient: a new instance is created every time the service is resolved. Each injection (and each nested resolution) gets its own instance, even within the same scope. Best for lightweight, stateless services.
    Scoped: one instance per scope (in ASP.NET Core, one scope per HTTP request). Every resolution inside that scope gets the same instance; a new scope creates a new instance. Best for per-request units of work such as a DbContext.
    Singleton: one instance for the lifetime of the container/application. It is created on first resolution and reused everywhere afterward. Best for stateless, thread-safe, app-wide services and caches. A singleton must not hold onto scoped or transient services (captive dependency).
- id: dotnet-di-captive-02
  answer: |
    A captive dependency is a shorter-lived service (scoped or transient) captured by a longer-lived consumer — classically a singleton that takes a scoped dependency in its constructor and keeps it for the life of the app. Injecting a scoped service into a singleton is a bug because the scoped service (e.g., a DbContext) is never disposed when its scope ends, its state leaks across requests, it is not thread-safe for concurrent use, and it can go stale. The container cannot dispose it because the singleton holds a reference beyond the scope's disposal.
- id: dotnet-di-scope-in-singleton-03
  answer: |
    Do not inject the scoped service directly into the singleton. Inject IServiceScopeFactory instead, and on each iteration create a new scope with scopeFactory.CreateScope(), resolve the scoped service (e.g., DbContext) from scope.ServiceProvider, use it, and dispose the scope (using var scope = ...) so the DbContext and its tracked state/connection are released. Never resolve or cache the scoped service from the singleton's constructor or a singleton field.
- id: dotnet-di-keyed-04
  answer: |
    Keyed services, introduced in .NET 8. Register by key: services.AddKeyedTransient<IService, ImplA>("A") (also AddKeyedScoped/AddKeyedSingleton). Consume by key via [FromKeyedServices("A")] on a constructor/action/minimal-API handler parameter, or programmatically with sp.GetRequiredKeyedService<IService>("A"). Each key resolves its own registered implementation with its own lifetime.
- id: dotnet-config-precedence-01
  answer: |
    Default host precedence is "later sources win": appsettings.json, then appsettings.{Environment}.json, then user secrets (Development), then environment variables, then command-line arguments last/highest. For the same key, command-line arguments beat environment variables, which beat user secrets, which beat environment-specific JSON, which beats base appsettings.json. The last-added configuration provider's value is what IConfiguration returns.
- id: dotnet-config-options-interfaces-02
  answer: |
    IOptions<T>: singleton; value computed once and cached; does not see configuration reloads; safe to inject anywhere including singletons.
    IOptionsSnapshot<T>: scoped; recomputed per scope and sees reloads within the scope's lifetime; cannot be injected into a singleton (captive dependency risk).
    IOptionsMonitor<T>: singleton; reloads on change via change tokens; exposes CurrentValue, Get(name) for named options, and OnChange(callback); safe for singletons and sees reloads.
- id: dotnet-config-secrets-03
  answer: |
    Local development: Secret Manager (dotnet user-secrets), stored outside the project tree — Windows %APPDATA%\Microsoft\UserSecrets\<user_secrets_id>\secrets.json, macOS/Linux ~/.microsoft/usersecrets/<user_secrets_id>\secrets.json — keyed by UserSecretsId in the .csproj. Never commit it to source control; it is not encrypted and is development-only.
    Production: environment variables or a secret store (Azure Key Vault, AWS Secrets Manager, GCP Secret Manager, etc.) via configuration providers, injected at deployment/runtime. User secrets must not be used in production.
- id: dotnet-config-options-binding-04
  answer: |
    Bind a section with services.Configure<MyOptions>(configuration.GetSection("SectionName")) (or AddOptions<MyOptions>().Bind(section)), then inject IOptions<MyOptions> and read .Value. Fail fast with options validation: add [Required]/[Range] attributes and call .ValidateDataAnnotations().ValidateOnStart() (or a custom IValidateOptions<T>/Validate predicate) so invalid configuration throws at startup instead of on first use.
- id: dotnet-pipeline-order-01
  answer: |
    Middleware forms a chain executed in registration order: each component receives HttpContext and a next delegate, may run code before and after await next(), and the response travels back up the chain in reverse. Order matters because earlier middleware shapes what later middleware sees, and not calling next short-circuits the rest of the chain. UseRouting must come first (it matches the request to an endpoint); UseAuthentication/UseAuthorization come after routing (they read endpoint metadata such as [Authorize]) but before the endpoints run, so unauthenticated requests are rejected before reaching the handler.
- id: dotnet-pipeline-use-vs-map-02
  answer: |
    Use: middleware that receives context and next; it can inspect/modify the request and response and decides whether to call next (short-circuiting by not calling it); the pipeline continues after it.
    Run: terminal middleware that never calls next; it produces the response and ends the chain.
    Map (MapGet/MapPost/MapWhen, etc.): branches the pipeline when a path or condition matches; the matching branch runs (and can short-circuit itself) while the main chain continues for non-matching requests. Short-circuit = a middleware declines to invoke next, so nothing downstream runs.
- id: dotnet-pipeline-minimal-results-03
  answer: |
    A handler's return value is converted by the result pipeline: if it is an IResult it is executed directly (ExecuteAsync writes the response); otherwise it is serialized (JSON by default) with status 200. Results/TypedResults (Results.Ok, Results.Json, Results.Created, Results.NotFound, Results.Problem, TypedResults.Ok<T>) return strongly typed IResult implementations, giving control over status code, content type, headers, content negotiation, and OpenAPI metadata instead of returning a raw object.
- id: dotnet-pipeline-filters-vs-middleware-04
  answer: |
    Middleware runs in the pipeline on every request and works only with HttpContext — use it for broad cross-cutting concerns (auth, logging, exceptions, static files) before or after routing. Filters (MVC action filters, endpoint filters) run inside the action/endpoint context: they see model binding results, action arguments, and the return value, can modify arguments or the result, and can short-circuit by not calling next(). Use filters when you need action/endpoint context or per-endpoint behavior; a filter can change action parameters and transform the executed result, which middleware cannot do directly.
- id: dotnet-efcore-context-lifetime-01
  answer: |
    Scoped — one DbContext per request/unit of work, disposed at the end of the scope. A DbContext holds the change tracker, identity map, and a database connection, and is not thread-safe: sharing it across threads or concurrent async operations causes concurrent-modification/invalid-operation exceptions and lost or mixed-up tracked state, since two operations can mutate the same tracker and connection simultaneously.
- id: dotnet-efcore-notracking-02
  answer: |
    The change tracker records loaded entities, their state (Unchanged/Modified/Added/Deleted), the identity map, and pending changes to persist on SaveChanges (including relationship fix-up). AsNoTracking() runs the query without tracking: no snapshot/comparison overhead, lower memory and CPU, entities are not cached in the context, but changes will not be persisted automatically and there is no identity resolution within that query. Use it for read-only data (reports, lookups, selections) you will not update and save.
- id: dotnet-efcore-nplus1-03
  answer: |
    An IQueryable defers execution: it builds an expression tree and the SQL is only sent to the database when enumerated or materialized (ToListAsync, FirstAsync, foreach, etc.). N+1 arises when you load N parents and then access each parent's navigation property, triggering one extra query per parent (1 + N queries total). Fix with eager loading (.Include / ThenInclude, or EF Core split queries) or by projecting the data you need, so related data is fetched in one or few round trips instead of per-row queries.
- id: dotnet-efcore-savechanges-tx-04
  answer: |
    Yes — a single SaveChangesAsync() is atomic: EF Core wraps all pending inserts, updates, and deletes in one transaction (beginning one if none is active), so all changes commit or none do. To span multiple SaveChanges calls or additional commands in one transaction, use context.Database.BeginTransactionAsync(). A migration is a code-based description of a model change (dotnet ef migrations add) with Up/Down methods plus a ModelSnapshot; EF compares the current model to the snapshot to detect changes, and database update applies migrations to evolve the schema to match the model.
- id: dotnet-gc-generations-loh-01
  answer: |
    Generational GC: gen 0 holds new, short-lived objects and is collected frequently and cheaply; gen 1 holds survivors as a buffer; gen 2 holds long-lived objects and is collected in full (expensive) collections. Objects start in gen 0 and survivors are promoted through gen 1 to gen 2. LOH: objects >= 85,000 bytes go straight to the Large Object Heap (a gen-2 class of memory), collected only during full gen 2 collections and traditionally not compacted (.NET Core 3+ can compact on demand). Large short-lived allocations are especially costly because they hit the LOH immediately, force frequent expensive full/blocking collections, and can fragment the LOH.
- id: dotnet-gc-dispose-finalizer-02
  answer: |
    IDisposable.Dispose is deterministic cleanup called by user code (usually via using) to release unmanaged resources (handles, sockets, connections). A finalizer (~Class) is non-deterministic, run by the GC finalizer thread at collection time, only for unmanaged resources, and expensive (the object survives extra GC cycles). Implement a finalizer only when you directly own unmanaged resources (prefer SafeHandle), pair it with Dispose, and call GC.SuppressFinalize(this) in Dispose so the finalizer is skipped. IAsyncDisposable adds DisposeAsync for non-blocking async release of resources such as closing streams or connections.
- id: dotnet-gc-span-arraypool-03
  answer: |
    Span<T>: a stack-only ref struct over contiguous memory (array, stackalloc, native) that lets you slice and process buffers without allocating or copying. stackalloc: allocates a small buffer on the stack, avoiding the GC heap entirely for very short-lived use (risk: stack overflow if too large; only valid within a ref-struct scope). ArrayPool<T>: rents and returns reusable arrays from a shared pool, avoiding repeated large allocations and GC pressure (return via ArrayPool<T>.Shared.Return). All three reduce managed-heap allocation and GC load.
- id: dotnet-gc-struct-vs-class-04
  answer: |
    A struct is a value type: its data lives inline where declared — on the stack as a local, inside a class instance as a field, or contiguously in an array — with no separate heap allocation per instance. A class is a reference type: each instance is a heap object and variables hold references. Boxing is wrapping a value type in a heap object (object or interface), allocating and copying it; unboxing copies it back. Boxing causes allocation and GC pressure, e.g., structs stored as object/interface or in non-generic collections.
- id: dotnet-json-stj-defaults-01
  answer: |
    camelCase: set JsonSerializerOptions.PropertyNamingPolicy = JsonNamingPolicy.CamelCase (ASP.NET Core applies camelCase by default via AddJsonOptions in controllers and minimal APIs). Override a single property: apply the [JsonPropertyName("name")] attribute to that property.
- id: dotnet-json-sourcegen-02
  answer: |
    System.Text.Json source generation produces serialization code at compile time via a partial JsonSerializerContext marked with [JsonSerializable], instead of using reflection at runtime. It gives faster startup, lower memory, and no reflection emit; critically, the generated code is visible to the linker/trimmer, so it works with trimming and Native AOT where the reflection-based serializer can fail or require heavy trimming hints.
- id: dotnet-json-stj-vs-newtonsoft-03
  answer: |
    STJ: built-in, fast, low-allocation, UTF-8 native, trim/AOT-friendly, minimal built-in extensibility and fewer converters; the default in modern .NET. Newtonsoft.Json: mature, highly configurable, rich attributes and converters, handles more exotic types, but slower, more allocation, reflection-based (harder with Native AOT/trimming), and an extra dependency. Use STJ by default; reach for Newtonsoft for legacy contracts, complex custom serialization, or types STJ does not support.
- id: dotnet-json-options-reuse-04
  answer: |
    JsonSerializerOptions caches per-type metadata (property mappings, converters, naming policy); creating a new instance per call throws the cache away and repeats reflection-based inspection, adding CPU and GC overhead. Create once and reuse (static or as a singleton). Polymorphism: by default STJ serializes the declared (static) type, so derived members are lost; opt-in polymorphism requires [JsonPolymorphic]/[JsonDerivedType] on the base plus a JsonTypeInfoResolver/JsonSerializerContext that knows the derived types.
- id: dotnet-http-socket-exhaustion-01
  answer: |
    Three stages, in order: (a) Per-request new HttpClient() plus dispose: each client gets its own handler and connection pool; disposing closes the sockets, which sit in TIME_WAIT, and under load the OS runs out of ephemeral ports — socket exhaustion. (b) One shared static HttpClient: no exhaustion, but its pooled connections stay open and the hostname is never re-resolved, so after a deployment or failover it keeps talking to the old IP. (c) IHttpClientFactory: hands out cheap clients over a pooled set of HttpMessageHandlers (no exhaustion) and rotates those handlers on HandlerLifetime (default 2 minutes), so fresh connections pick up DNS changes. Register with AddHttpClient(...), inject IHttpClientFactory or a named/typed client. Stage (b) is why rotation exists — never skip it.
- id: dotnet-http-typed-clients-02
  answer: |
    Named client: registered with AddHttpClient("name", c => {...}) to configure base address, headers, etc., and resolved by name with IHttpClientFactory.CreateClient("name"). Typed client: a class with an HttpClient constructor parameter registered via AddHttpClient<MyClient>() (transient); the factory creates and configures the HttpClient per creation. Prefer a typed client because it centralizes base address, headers, and call logic in one testable class and lets DI manage creation. Caveat: typed clients are transient, so injecting one into a singleton pins its handler — see the handler-lifetime issue.
- id: dotnet-http-lifetime-dns-03
  answer: |
    An HttpClient is bound to the HttpMessageHandler it was created with for its whole life; factory rotation only affects clients created after the rotation. A factory client cached in a singleton, static field, or long-lived background service pins its original handler and never sees a new one, so it serves stale DNS exactly like the static client. A shorter SetHandlerLifetime(...) only changes how often NEW clients get a new handler; it does nothing for an already-cached client — never offer it as the mitigation for a held client; instead call CreateClient near each use (or inject the typed client only into scoped/transient consumers). The modern alternative is a primary SocketsHttpHandler configured with PooledConnectionLifetime (e.g., 2 minutes): the handler itself retires and re-creates pooled connections, re-resolving DNS, so a long-lived client/handler is then safe to cache. You can disable factory rotation with SetHandlerLifetime(Timeout.InfiniteTimeSpan) and let the handler do it. HandlerLifetime (factory, SetHandlerLifetime) and SocketsHttpHandler.PooledConnectionLifetime (connection recycling inside one handler) are two different settings — name the one you are configuring.
- id: dotnet-http-resilience-04
  answer: |
    Modern .NET 8+: Microsoft.Extensions.Http.Resilience (Polly v8). Chain AddStandardResilienceHandler() onto AddHttpClient for the default pipeline (retry, total timeout, per-attempt timeout, circuit breaker), or use AddResilienceHandler("name", b => b.AddRetry(...).AddCircuitBreaker(...).AddTimeout(...)) for a custom one — the builder methods carry the Add prefix; there are no .Retry(...), .CircuitBreaker(...) or .Timeout(...) methods. Legacy (Microsoft.Extensions.Http.Polly): AddTransientHttpErrorPolicy(...) / AddPolicyHandler(...) with HttpPolicyExtensions.HandleTransientHttpError(). Always flow a CancellationToken (e.g., HttpContext.RequestAborted or a linked CancellationTokenSource with a timeout) into every SendAsync/GetAsync; resilience timeouts are enforced through linked tokens and surface as OperationCanceledException/TaskCanceledException.
- id: dotnet-log-templates-01
  answer: |
    logger.LogInformation("Order {OrderId} shipped", orderId) uses structured logging: the value is captured as a named property with its original type, so it is queryable and filterable in structured stores (Seq, Application Insights, ELK) and the message template stays stable. Interpolating ($"Order {orderId} shipped") bakes the value into a plain string eagerly, so it cannot be queried as a field and formatting happens even when logging is disabled.
- id: dotnet-log-levels-02
  answer: |
    Levels, most to least severe: Critical, Error, Warning, Information, Debug, Trace (None disables logging). A log is written when its level is at or above the configured minimum for its category. Per-category filtering is set in appsettings.json under Logging:LogLevel:<Category> (e.g., "Microsoft.EntityFrameworkCore": "Warning") or in code via AddFilter("Microsoft.EntityFrameworkCore", LogLevel.Warning); more specific category names override general ones, falling back to "Default".
- id: dotnet-log-highperf-03
  answer: |
    The [LoggerMessage] source generator compiles strongly-typed log methods at compile time (logging source generation). On hot paths it is preferred because it avoids string interpolation and boxing allocations and skips formatting entirely when the level is disabled, giving near-zero-overhead logging versus direct logger.LogInformation calls.
- id: dotnet-log-scopes-otel-04
  answer: |
    Logging scopes (ILogger.BeginScope / BeginScope) attach contextual properties (OrderId, CorrelationId, etc.) to every log emitted within the disposable scope, correlating related log lines; the scope ends when the returned IDisposable is disposed. Distributed tracing is exposed via System.Diagnostics ActivitySource/Activity plus the OpenTelemetry .NET SDK (AddTracing with sources, OTLP/Console exporters); metrics via Meter with AddMetrics (OTLP/Prometheus/Console exporters); and the OpenTelemetry logging provider so logs carry TraceId/SpanId from Activity.Current.
