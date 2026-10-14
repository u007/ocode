# cat-jam-free — WITH-SKILL closed-book answers for dotnet (opencode-go, 2026-10-14)
# Delivery: derived/dotnet.cat-jam-free.SKILL.md body (front matter stripped) prepended to _prompts/dotnet.md; one
# `ocode run -m opencode-go/cat-jam-free -effort med` session, isolated HOME with every tool denied, cwd holding only the sheet;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: dotnet-di-lifetimes-01
  answer: |
    Transient creates a new instance on every resolution/request. Scoped creates one instance per DI scope (commonly one HTTP request) and reuses it within that scope. Singleton creates one instance for the container lifetime and reuses it everywhere. New instances are created for transient every time, for scoped once per scope, and for singleton only on the first resolution.

- id: dotnet-di-captive-02
  answer: |
    A captive dependency is a shorter-lived service captured by a longer-lived service, such as a scoped service stored in a singleton. It is a bug because the scoped instance can live beyond its intended scope, be used after disposal, shared across requests or threads, and cause stale state, cross-tenant data leakage, or ObjectDisposedException.

- id: dotnet-di-scope-in-singleton-03
  answer: |
    Do not inject the scoped service into the singleton. Create a new scope per unit of work, e.g. using IServiceScopeFactory, then resolve the scoped service from that scope's ServiceProvider and dispose the scope when the work is complete. Alternatively use a separate DbContext per operation or IDbContextFactory when the work needs DbContext specifically.

- id: dotnet-di-keyed-04
  answer: |
    Keyed services, added in .NET 8, support multiple registrations of the same interface with a key. Register with AddKeyedSingleton/AddKeyedScoped/AddKeyedTransient and consume via [FromKeyedServices("key")] parameters or IServiceProvider.GetKeyedService<T>("key").

- id: dotnet-config-precedence-01
  answer: |
    In a typical host, configuration sources are layered and later sources override earlier ones. Common order is appsettings.json, appsettings.{Environment}.json, user secrets in development, environment variables, and command-line arguments. The last applicable source wins for the same key.

- id: dotnet-config-options-interfaces-02
  answer: |
    IOptions<T> is singleton and captures one immutable snapshot for the container lifetime; it does not see reloads and can be injected anywhere. IOptionsSnapshot<T> is scoped, recomputed per scope, sees reloads, and should be injected into scoped or transient services. IOptionsMonitor<T> is singleton, tracks live changes, supports change callbacks, exposes CurrentValue, and can be injected anywhere including singletons.

- id: dotnet-config-secrets-03
  answer: |
    Local development secrets should stay outside source control, commonly in User Secrets via dotnet user-secrets or environment variables. Production should use a production secret store such as Azure Key Vault, environment variables, managed identities, or similar platform secret injection; never commit secrets or keep production values in appsettings committed to the repo.

- id: dotnet-config-options-binding-04
  answer: |
    Bind a section with builder.Services.Configure<MyOptions>(config.GetSection("MySection")), or use AddOptions<MyOptions>().Bind(...). To fail fast, add .ValidateDataAnnotations() and .ValidateOnStart(), and/or a custom Validate callback, so invalid configuration throws at startup rather than during request handling.

- id: dotnet-pipeline-order-01
  answer: |
    Requests pass through middleware in registration order, and responses unwind in reverse. UseRouting must usually come before UseAuthentication and UseAuthorization so routing can attach endpoint metadata used by auth; authentication must come before authorization. Endpoints are then mapped after these. Putting them out of order can skip auth checks or leave endpoint metadata unavailable.

- id: dotnet-pipeline-use-vs-map-02
  answer: |
    Use registers middleware that normally calls next and can short-circuit by not calling it. Run registers terminal middleware with no next delegate. Map branches the pipeline for a path prefix. Short-circuiting means a middleware returns a response and stops later middleware and endpoints from running.

- id: dotnet-pipeline-minimal-results-03
  answer: |
    A minimal API handler's return value is interpreted automatically: null or void often becomes 204, strings become text/plain, and other objects become JSON. Results and TypedResults provide explicit control over status code, content type, headers, redirects, files, and ProblemDetails. TypedResults also gives compile-time result metadata and better OpenAPI accuracy.

- id: dotnet-pipeline-filters-vs-middleware-04
  answer: |
    Use middleware for broad, infrastructure-level cross-cutting concerns across the whole pipeline. Use endpoint or action filters when logic is tied to endpoints/actions and needs endpoint context, parameter values, model state, result manipulation, or execution-order control around the handler. Filters can inspect validation/model binding results and manipulate action results in ways middleware cannot as naturally.

- id: dotnet-efcore-context-lifetime-01
  answer: |
    DbContext should be short-lived and scoped, such as one per HTTP request. It is not thread-safe and not safe for concurrent async operations because its change tracker and internal state are shared; concurrent operations throw InvalidOperationException. Await each operation before starting the next on the same context, or use a separate context per concurrent task.

- id: dotnet-efcore-notracking-02
  answer: |
    EF Core's change tracker monitors loaded entities' states and navigations so SaveChanges can generate inserts, updates, and deletes. Add AsNoTracking to read-only queries to avoid tracking overhead, reduce memory, and prevent accidental modifications. Use AsNoTrackingWithIdentityResolution when you need identity resolution for read-only graph queries.

- id: dotnet-efcore-nplus1-03
  answer: |
    IQueryable defers execution until it is enumerated. ToListAsync materializes the query and executes SQL. N+1 occurs when code queries a parent collection then queries children separately inside a loop, often via lazy loading. Fix it with eager loading such as Include, a projection selecting needed data, and appropriate split-query or single-query strategy.

- id: dotnet-efcore-savechanges-tx-04
  answer: |
    A single SaveChangesAsync call is atomic: its statements commit or roll back together. Multiple SaveChanges calls or raw SQL plus SaveChanges need an explicit transaction, e.g. await using var tx = await db.Database.BeginTransactionAsync(); ... await tx.CommitAsync();. A migration is a versioned schema change that updates the database structure, generated with dotnet ef migrations add <Name> and applied with dotnet ef database update or Database.MigrateAsync().

- id: dotnet-gc-generations-loh-01
  answer: |
    Gen 0 collects short-lived objects frequently, gen 1 collects survivors of gen 0, and gen 2 collects long-lived objects. The Large Object Heap holds objects of roughly 85,000 bytes or larger. Large short-lived allocations are costly because they land on the LOH, are not compacted by default, can fragment memory, and may increase gen 2 collection pressure.

- id: dotnet-gc-dispose-finalizer-02
  answer: |
    IDisposable gives deterministic cleanup via Dispose; use it for unmanaged resources or pooled resources. A finalizer is a nondeterministic GC fallback and should only be used when unmanaged resources might not be disposed; prefer SafeHandle. IAsyncDisposable adds asynchronous cleanup, used with await using for resources such as streams or pipes.

- id: dotnet-gc-span-arraypool-03
  answer: |
    Span<T> provides a lightweight view over memory without allocating for slicing. stackalloc places small arrays on the stack. ArrayPool<T> rents and returns pooled arrays to avoid repeated heap allocations and GC pressure. Together they reduce allocations in hot paths.

- id: dotnet-gc-struct-vs-class-04
  answer: |
    A class is a reference type allocated on the heap and accessed by reference, involving GC tracking. A struct is a value type, usually stored inline or on the stack, minimizing heap allocations. Boxing occurs when a value type is converted to object or an interface and is allocated on the heap.

- id: dotnet-json-stj-defaults-01
  answer: |
    Set JsonSerializerOptions.PropertyNamingPolicy to JsonNamingPolicy.CamelCase. Override one property with [JsonPropertyName("desiredName")]. Other attributes such as naming policies, casing, and converters can also customize output.

- id: dotnet-json-sourcegen-02
  answer: |
    System.Text.Json source generation uses a JsonSerializerContext to generate serialization code at compile time. It avoids reflection, improves startup and throughput, and works with trimming and Native AOT where runtime reflection is limited.

- id: dotnet-json-stj-vs-newtonsoft-03
  answer: |
    System.Text.Json is faster, built in, and trimming/AOT friendly, while Newtonsoft.Json has richer legacy features such as dynamic JSON, LINQ to JSON, broader polymorphic support, and easier migration for older code. Newtonsoft is often chosen for flexible or unusual serialization needs; STJ is preferred for modern .NET performance and deployment.

- id: dotnet-json-options-reuse-04
  answer: |
    Reuse JsonSerializerOptions because it caches metadata and is thread-safe, avoiding repeated setup and allocation. For polymorphism, modern STJ supports attributes such as [JsonPolymorphic] and [JsonDerivedType] with type discriminators, so derived types can be serialized and deserialized when all known types are declared.

- id: dotnet-http-socket-exhaustion-01
  answer: |
    Using `using var client = new HttpClient()` per request leaves sockets in TIME_WAIT and exhausts available ports. IHttpClientFactory solves this by pooling HttpMessageHandlers and reusing sockets, while creating lightweight HttpClient wrappers and rotating handlers so DNS stays current.

- id: dotnet-http-typed-clients-02
  answer: |
    Named clients are registered by name and created with CreateClient("name"). Typed clients are registered as a class that takes an HttpClient constructor parameter and are injected as that class. Prefer typed clients for encapsulating endpoint logic, strong typing, and easier testing.

- id: dotnet-http-lifetime-dns-03
  answer: |
    An HttpClient is bound to the handler it was created with. Caching a factory-created client, or a transient typed client, in singleton or static state pins its original handler, so it never sees handler rotation and can hold stale DNS. Prefer creating clients per request or use a primary SocketsHttpHandler with PooledConnectionLifetime; factory HandlerLifetime and PooledConnectionLifetime are separate settings. Factory rotation can be disabled with SetHandlerLifetime(Timeout.InfiniteTimeSpan) when SocketsHttpHandler handles lifetime.

- id: dotnet-http-resilience-04
  answer: |
    In .NET 8 and later, use Microsoft.Extensions.Http.Resilience and chain .AddStandardResilienceHandler or .AddResilienceHandler onto AddHttpClient. This provides retries, timeouts, and circuit breaker behavior. Always flow a CancellationToken into HTTP calls.

- id: dotnet-log-templates-01
  answer: |
    Message templates preserve named placeholders and structured properties, enabling filtering and efficient formatting. String interpolation evaluates the value before checking whether the level is enabled, causes unnecessary work and allocations, and loses structured field names for logging sinks and distributed tracing.

- id: dotnet-log-levels-02
  answer: |
    Levels in order are Trace, Debug, Information, Warning, Error, Critical, and None. Per-category filtering can be configured in appsettings under Logging:LogLevel, via builder.Logging.AddFilter("Category", LogLevel.Warning), or by configuring logger factories/filters in code.

- id: dotnet-log-highperf-03
  answer: |
    The LoggerMessage source generator generates high-performance logging code from partial methods marked with [LoggerMessage]. It avoids runtime template parsing, boxing, and many allocations, making it preferred for hot paths and avoiding repeated logging overhead.

- id: dotnet-log-scopes-otel-04
  answer: |
    BeginScope adds ambient contextual properties to every log written within the disposable scope, such as request or user identifiers. Distributed tracing and metrics are exposed through System.Diagnostics APIs and OpenTelemetry instrumentation, typically using ActivitySource for spans and Meter for metrics with exporters configured by the OpenTelemetry SDK.
