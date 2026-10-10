- id: dotnet-di-lifetimes-01
  answer: |
    Transient: a new instance is created every time the service is resolved (each injection point or Resolve call gets its own copy). Scoped: one instance per scope — in ASP.NET Core, one per request; resolving the same scoped service multiple times within the same scope returns the same instance. Singleton: one instance for the entire application lifetime, created on first resolution and reused thereafter.

- id: dotnet-di-captive-02
  answer: |
    A captive dependency is a service with a shorter lifetime that is held by a service with a longer lifetime. Injecting a scoped service into a singleton is a bug because the singleton captures the scoped instance at construction time and holds it forever — the scoped service never gets the fresh per-scope instance it was designed for, and in the case of something like DbContext it means the singleton keeps a single context alive indefinitely, leading to stale data, memory leaks, and thread-safety issues.

- id: dotnet-di-scope-in-singleton-03
  answer: |
    Inject IServiceScopeFactory into the singleton, then create a new scope per iteration (or per unit of work) using scopeFactory.CreateAsyncScope(), resolve the scoped service from scope.ServiceProvider, and dispose the scope when done. This gives each iteration its own fresh scoped instance (e.g. its own DbContext) without captive-dependency problems.

- id: dotnet-di-keyed-04
  answer: |
    Keyed services, introduced in .NET 8. Register with services.AddKeyedTransient<IService, ImplA>("a") and services.AddKeyedTransient<IService, ImplB>("b"). Consume via [FromKeyedServices("a")] on a constructor parameter, or resolve with serviceProvider.GetKeyedService<IService>("a"). This lets you resolve different implementations of the same interface by a string key.

- id: dotnet-config-precedence-01
  answer: |
    Later sources override earlier ones. The typical order (lowest to highest precedence) is: appsettings.json, appsettings.{Environment}.json, user secrets (development only), environment variables, command-line arguments. When the same key exists in multiple sources, the last-registered source wins — so command-line arguments beat environment variables, which beat appsettings files.

- id: dotnet-config-options-interfaces-02
  answer: |
    IOptions<T>: singleton, cached at first access, does not see config reloads. IOptionsSnapshot<T>: scoped, re-evaluated per scope (per request in ASP.NET Core), sees config reloads. IOptionsMonitor<T>: singleton, supports change notifications via OnChange, always returns current values. IOptions and IOptionsMonitor can be injected into singletons; IOptionsSnapshot must be injected into scoped or transient services.

- id: dotnet-config-secrets-03
  answer: |
    Local development secrets should live in user secrets (dotnet user-secrets) — stored outside the project directory in the user profile, so they never get committed to source control. In production, secrets should come from environment variables, Azure Key Vault, AWS Secrets Manager, or similar secret stores. The key difference: user secrets are a dev-only convenience; production uses hardened, access-controlled secret providers.

- id: dotnet-config-options-binding-04
  answer: |
    Bind a section with services.Configure<MyOptions>(Configuration.GetSection("MySection")), or use Configuration.GetSection("MySection").Get<MyOptions>(). To fail fast on invalid options, use the DataAnnotations validation approach: services.AddOptions<MyOptions>().Bind(Configuration.GetSection("MySection")).ValidateDataAnnotations() and call ValidateOnStart() (or inject IOptions<T> and let the framework validate at startup). Alternatively, implement IValidateOptions<T> for custom validation logic.

- id: dotnet-pipeline-order-01
  answer: |
    Middleware executes in the order it is registered — the first registered middleware is the outermost layer and sees the request first and the response last. UseRouting must come before UseAuthentication/UseAuthorization so the endpoint is selected before auth runs; UseAuthentication must come before UseAuthorization so the user is authenticated before authorization checks; and both must come before UseEndpoints so the endpoint executes only after auth has passed. Misordering means auth runs against no endpoint or after the response has already been generated.

- id: dotnet-pipeline-use-vs-map-02
  answer: |
    Use: registers inline middleware that can call next() to pass control downstream or return without calling next to short-circuit. Run: registers terminal middleware that always ends the pipeline (no next parameter). Map: branches the pipeline based on a path prefix, creating a separate sub-pipeline for matching requests. Short-circuiting means a middleware returns without invoking next, so downstream middleware never runs — useful for early exits like auth failures or health checks.

- id: dotnet-pipeline-minimal-results-03
  answer: |
    A handler's return value is implicitly converted to an HTTP response — returning a raw object serializes it as JSON with a 200 status. Results (IResult) and TypedResults (Results.Ok(), Results.NotFound(), etc.) give you explicit control over status code, content type, headers, and response shape, plus they are strongly typed so the API contract is discoverable and testable, and they integrate with OpenAPI generation.

- id: dotnet-pipeline-filters-vs-middleware-04
  answer: |
    Middleware runs for every request and sees the raw HTTP pipeline; filters (endpoint filters, MVC action filters) run only for matched endpoints/actions and have access to endpoint-specific metadata (route values, action arguments, model state). A filter can do what middleware can't: inspect or short-circuit a specific action, access action arguments and model binding results, run code before/after model binding, and apply per-action authorization. Use middleware for cross-cutting concerns that apply to all requests; use filters for endpoint-specific logic.

- id: dotnet-efcore-context-lifetime-01
  answer: |
    DbContext should be scoped — one per unit of work (one per request in ASP.NET Core). It is registered as Scoped by default with AddDbContext. A DbContext is not thread-safe because its change tracker and internal service provider are not designed for concurrent operations; sharing it across threads or concurrent async operations leads to corrupted state, race conditions, and InvalidOperationException.

- id: dotnet-efcore-notracking-02
  answer: |
    The change tracker monitors entity state (Added, Modified, Deleted, Unchanged) so SaveChanges can generate the right SQL. AsNoTracking() tells EF Core not to track the returned entities — queries are faster (no identity map lookups, no snapshot creation) and use less memory. Use it for read-only scenarios where you won't modify and save the entities back.

- id: dotnet-efcore-nplus1-03
  answer: |
    IQueryable defers execution — the query is built but not sent to the database until you enumerate it (e.g. via ToListAsync()). The N+1 problem arises when you load a list of parent entities (1 query), then access a navigation property for each parent in a loop, triggering a separate query per parent (N queries). Fix it with eager loading (Include/ThenInclude) or explicit loading, so all needed data is fetched in one or a few queries.

- id: dotnet-efcore-savechanges-tx-04
  answer: |
    Yes — a single SaveChangesAsync() call is transactional: all changes are committed in one transaction, and if any part fails, the entire transaction rolls back. A migration in EF Core is a code-first schema evolution mechanism — it captures model changes as versioned C# classes that update the database schema to match the current model, enabling incremental, reproducible schema updates without manual SQL scripts.

- id: dotnet-gc-generations-loh-01
  answer: |
    The GC uses three generations: Gen 0 (newly collected, most frequent), Gen 1 (survivors of Gen 0), Gen 2 (long-lived objects, collected least frequently). The Large Object Heap holds objects ≥ 85,000 bytes and is collected only during Gen 2 collections. Large short-lived allocations are costly because they go straight to the LOH, require a full Gen 2 collection to reclaim, and Gen 2 collections are the most expensive — so frequent large short-lived allocations trigger frequent full GCs.

- id: dotnet-gc-dispose-finalizer-02
  answer: |
    IDisposable/Dispose is deterministic cleanup — you call it (or use using) to release unmanaged resources immediately. A finalizer is non-deterministic cleanup — the GC calls it during collection, at an unpredictable time, and it adds overhead (finalizable objects survive to Gen 1 and require two collections). Use Dispose for deterministic resource release; use a finalizer only as a safety net for when Dispose wasn't called. IAsyncDisposable adds DisposeAsync for async cleanup (e.g. flushing async streams) and is preferred in async code paths.

- id: dotnet-gc-span-arraypool-03
  answer: |
    Span<T>: a stack-only, type-safe view over contiguous memory (arrays, stack, unmanaged) that avoids heap allocations for slicing and parsing. stackalloc: allocates memory on the stack instead of the heap, avoiding GC pressure for small, short-lived buffers. ArrayPool<T>: a reusable pool of arrays that you rent and return, avoiding repeated large array allocations and the GC pressure they cause.

- id: dotnet-gc-struct-vs-class-04
  answer: |
    A struct (value type) is allocated on the stack (or inline in a containing object) and copied by value — no GC pressure for the struct itself. A class (reference type) is allocated on the heap and tracked by the GC — every allocation adds GC work. Boxing is wrapping a value type in a reference type (e.g. assigning an int to an object), which forces a heap allocation and GC pressure; unboxing is the reverse cast. Structs avoid heap allocation but can cause copying overhead; classes enable polymorphism and reference semantics but cost GC.

- id: dotnet-json-stj-defaults-01
  answer: |
    Set PropertyNamingPolicy = JsonNamingPolicy.CamelCase in JsonSerializerOptions (or use JsonSerializerDefaults.Web which includes camelCase). To override a single property's name, use [JsonPropertyName("customName")] on that property.

- id: dotnet-json-sourcegen-02
  answer: |
    System.Text.Json source generation uses a source generator to create serialization/deserialization code at compile time instead of using reflection at runtime. It matters for performance (no reflection overhead, faster startup, smaller allocations) and for trimming/Native AOT (reflection-based serialization is incompatible with trimming and AOT because the trimmer can't see what reflection needs; source-gen code is statically analyzable and trimmable).

- id: dotnet-json-stj-vs-newtonsoft-03
  answer: |
    System.Text.Json is the default in modern .NET — faster, allocation-efficient, source-gen compatible, and trimming/AOT-friendly, but historically less feature-rich (no built-in polymorphic serialization, limited contract customization). Newtonsoft.Json is more mature and feature-rich (better polymorphic support, more converters, more flexible contract resolvers, better error handling) but slower, heavier on allocations, and reflection-based (problematic for trimming/AOT). Trade-off: STJ for performance and modern .NET compatibility; Newtonsoft when you need its advanced features and can accept the overhead.

- id: dotnet-json-options-reuse-04
  answer: |
    JsonSerializerOptions should be created once and reused because it caches contract metadata (property names, converters) internally — creating a new instance per call throws away that cache and causes repeated reflection/source-gen work. For polymorphic serialization, STJ supports [JsonPolymorphic] and [JsonDerivedType] attributes on the base type to control how derived types are serialized and discriminated.

- id: dotnet-http-socket-exhaustion-01
  answer: |
    Each new HttpClient creates a new underlying SocketsHttpHandler with its own connection pool. When the HttpClient is disposed, the connections are closed but the sockets enter TIME_WAIT state and remain occupied for a while. Creating and disposing an HttpClient per request exhausts the available ephemeral ports/sockets. IHttpClientFactory solves this by pooling and reusing HttpMessageHandler instances (and thus connection pools) while still rotating them periodically to handle DNS changes.

- id: dotnet-http-typed-clients-02
  answer: |
    Named clients: register an HttpClient with a string name (services.AddHttpClient("name", ...)) and resolve it via IHttpClientFactory.CreateClient("name"). Typed clients: register a class that takes HttpClient in its constructor (services.AddHttpClient<MyClient>()) and inject that class directly. Prefer typed clients because they provide a strongly-typed, testable abstraction — callers work with methods on the class rather than raw HTTP, and the class encapsulates endpoint URLs, serialization, and error handling.

- id: dotnet-http-lifetime-dns-03
  answer: |
    Even factory-created HttpClients can serve stale DNS if the underlying handler is held too long — the handler caches DNS resolutions and won't pick up changes until the connection is recycled. The factory's default handler lifetime is 2 minutes, after which handlers are rotated. The SocketsHttpHandler alternative is to set PooledConnectionLifetime (e.g. to a shorter interval) so connections are periodically recycled, forcing fresh DNS resolution and preventing stale DNS from being served indefinitely.

- id: dotnet-http-resilience-04
  answer: |
    Use the Polly library (or the built-in resilience extensions in .NET 8+ via AddResilienceHandler) to add retries (with exponential backoff and jitter), timeouts (via CancellationToken or TimeoutStrategy), and circuit breakers (open after consecutive failures, half-open to test recovery). Cancellation tokens fit in by propagating cancellation through the call chain — a timeout strategy cancels the request via a linked CancellationToken, and callers can cancel via their own token.

- id: dotnet-log-templates-01
  answer: |
    Structured logging templates keep the message template as a constant and pass values as parameters, so the logging provider can capture the values as named properties (enabling structured log querying and filtering). Interpolating the value into the string (e.g. $"Order {orderId} shipped") loses the structure — the value is baked into the message text, making it impossible to query by property name, and it also performs the string allocation even when the log level is disabled.

- id: dotnet-log-levels-02
  answer: |
    Levels in order: Trace (0), Debug (1), Information (2), Warning (3), Error (4), Critical (5), None (6). Per-category level filtering is configured in appsettings.json under "LogLevel" — set a default level and override per namespace/category (e.g. "LogLevel": { "Default": "Information", "Microsoft.EntityFrameworkCore": "Warning" }). You can also use ILoggingBuilder.AddFilter in code or configure filtering via configuration providers.

- id: dotnet-log-highperf-03
  answer: |
    The LoggerMessage source generator ([LoggerMessage] attribute on a partial method) generates a high-performance logging method at compile time. It avoids the overhead of calling logger.LogInformation directly — no params array allocation, no boxing of value types, no string interpolation, and it works with the logging infrastructure's fast path. It is preferred on hot paths because it eliminates per-call allocations and reduces CPU overhead.

- id: dotnet-log-scopes-otel-04
  answer: |
    Logging scopes (BeginScope) group a set of log messages with a shared context (e.g. a request ID or correlation ID), so all logs within the scope carry the same properties. For distributed tracing and metrics, .NET exposes OpenTelemetry via the System.Diagnostics.Metrics API (Meter, Counter, Histogram) and the OpenTelemetry SDK — you configure a Meter, create instruments, and export traces/metrics to an OTLP collector. The ActivitySource/Activity API underpins distributed tracing, and ILogger scopes integrate with the current Activity to propagate trace context.
