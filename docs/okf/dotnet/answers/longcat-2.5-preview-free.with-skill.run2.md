- id: dotnet-di-lifetimes-01
  answer: |
    Transient: a new instance is created every time the service is resolved from the container. Scoped: a new instance is created once per scope — in ASP.NET Core this means one instance per HTTP request. Singleton: the first resolution creates a single instance that is reused for the entire application lifetime; subsequent resolutions return the same object.

- id: dotnet-di-captive-02
  answer: |
    A captive dependency occurs when a longer-lived service holds a reference to a shorter-lived service. Injecting a scoped service into a singleton is a bug because the scoped instance is captured at singleton creation time and never replaced — it effectively becomes a singleton itself. This means it never gets disposed at the end of its intended scope, can hold stale state, and in the case of something like DbContext it can accumulate tracked entities indefinitely, leading to memory leaks and incorrect behavior.

- id: dotnet-di-scope-in-singleton-03
  answer: |
    Inject IServiceScopeFactory into the singleton. On each iteration, call scopeFactory.CreateAsyncScope() to create a new scope, resolve the scoped service from scope.ServiceProvider, use it, and then dispose the scope when done. This ensures the scoped service (e.g., DbContext) is created and disposed correctly per operation rather than living for the entire singleton lifetime.

- id: dotnet-di-keyed-04
  answer: |
    Keyed services, introduced in .NET 8. Register with services.AddKeyedSingleton<IService, ImplA>("a") and services.AddKeyedSingleton<IService, ImplB>("b"). Consume via constructor injection using the [FromKeyedServices("a")] attribute on a parameter, or resolve manually with IServiceProvider.GetKeyedService<IService>("a").

- id: dotnet-config-precedence-01
  answer: |
    Later sources override earlier ones. The typical default order from lowest to highest precedence is: appsettings.json, appsettings.{Environment}.json, user secrets (development only), environment variables, and command-line arguments. When the same key exists in multiple sources, the last-loaded source wins — so command-line arguments beat environment variables, which beat appsettings files.

- id: dotnet-config-options-interfaces-02
  answer: |
    IOptions<T> is singleton-bound; it captures the config value at first resolution and never sees reloads. IOptionsSnapshot<T> is scoped; it re-reads configuration on each scope creation, so it picks up reloads within its scope. IOptionsMonitor<T> is singleton and supports change notifications via OnChange, so it sees reloads immediately. IOptions<T> and IOptionsMonitor<T> can be injected into singletons; IOptionsSnapshot<T> should only be injected into scoped or transient services.

- id: dotnet-config-secrets-03
  answer: |
    In local development, secrets should live in User Secrets (dotnet user-secrets init and set), stored in a JSON file outside the project directory, or in environment variables. User secrets are never committed to source control. In production, secrets should come from environment variables, Azure Key Vault, AWS Secrets Manager, or a similar secret store — never from appsettings.json or source control.

- id: dotnet-config-options-binding-04
  answer: |
    Bind a section with services.Configure<TOptions>(Configuration.GetSection("MySection")) or Configuration.GetSection("MySection").Get<TOptions>(). To fail fast on invalid options, chain .ValidateDataAnnotations() (requires the Options.DataAnnotations package) to run DataAnnotations validation, or call .ValidateOnStart() in .NET 6+ to validate at application startup and throw immediately if invalid.

- id: dotnet-pipeline-order-01
  answer: |
    Middleware executes in the exact order it is added to the pipeline. UseRouting must come before UseAuthentication and UseAuthorization so the endpoint is selected and its metadata (e.g., authorization requirements) is available. UseAuthorization must come before UseEndpoints (or the endpoint middleware) so authorization is enforced before the endpoint handler runs. If the order is wrong, authentication may have no endpoint to inspect, or authorization may be skipped entirely.

- id: dotnet-pipeline-use-vs-map-02
  answer: |
    Use registers middleware that can call next to pass control downstream. Run registers terminal middleware that never calls next — it ends the pipeline. Map branches the pipeline based on a path prefix, creating a separate sub-pipeline for matching requests. Short-circuiting means a middleware does not call next, so downstream middleware and the endpoint are never invoked — useful for early exits like authentication failures or health checks.

- id: dotnet-pipeline-minimal-results-03
  answer: |
    A handler's return value is serialized to JSON by default. Results (IResult) and TypedResults (e.g., TypedResults.Ok<T>) give you explicit control over status code, content type, headers, and other HTTP semantics with strong typing and IntelliSense, rather than relying on convention-based defaults that return 200 OK with a raw object.

- id: dotnet-pipeline-filters-vs-middleware-04
  answer: |
    Middleware runs early in the pipeline, before routing, and has no knowledge of the specific endpoint being invoked. Endpoint filters (and MVC action filters) run within the endpoint execution pipeline and have access to endpoint-specific metadata: action arguments, model state, route values, and the selected endpoint. Filters can perform model validation, action-specific authorization, and result transformation — things middleware cannot do because it lacks endpoint context.

- id: dotnet-efcore-context-lifetime-01
  answer: |
    The correct lifetime is scoped — one DbContext per unit of work, typically per HTTP request in ASP.NET Core. A DbContext is not safe to share across threads or concurrent async operations because its internal change tracker, connection state, and entity cache are not thread-safe. Concurrent operations on the same instance can cause corrupted state, exceptions, and data integrity issues.

- id: dotnet-efcore-notracking-02
  answer: |
    The change tracker monitors entity state (Added, Modified, Deleted, Unchanged) so SaveChanges can generate the correct SQL. AsNoTracking() tells EF Core to skip tracking for query results. Use it for read-only queries where you never intend to update the entities — it reduces memory overhead (no snapshot creation) and speeds up query execution because EF Core doesn't need to set up change tracking infrastructure.

- id: dotnet-efcore-nplus1-03
  answer: |
    IQueryable defers execution — the query is built but not sent to the database until enumerated. ToListAsync() forces immediate execution and materializes results. The N+1 problem arises when you query a parent collection (1 query), then access a navigation property on each child, triggering a separate query per child (N queries). Fix it with eager loading using Include() to fetch related data in a single query, or with explicit loading via LoadAsync().

- id: dotnet-efcore-savechanges-tx-04
  answer: |
    Yes, a single SaveChangesAsync() call is transactional by default — all changes are committed in one transaction, and if any operation fails, the entire transaction is rolled back. Migrations in EF Core manage schema evolution over time: they generate versioned C# classes that apply incremental changes (create tables, add columns, etc.) to keep the database schema in sync with the entity model.

- id: dotnet-gc-generations-loh-01
  answer: |
    The GC uses three generations: Gen 0 (newly collected, short-lived), Gen 1 (survived one collection), and Gen 2 (long-lived, collected least frequently). The Large Object Heap holds objects larger than 85,000 bytes. Large short-lived allocations are especially costly because they go straight to the LOH, are only collected during expensive Gen 2 collections, and cause LOH fragmentation since the LOH is not compacted by default.

- id: dotnet-gc-dispose-finalizer-02
  answer: |
    IDisposable/Dispose provides deterministic cleanup — you control exactly when unmanaged resources are released. A finalizer (destructor) provides non-deterministic backup cleanup — the GC calls it eventually, but timing is unpredictable. IAsyncDisposable adds DisposeAsync() for asynchronous cleanup, which is preferred when releasing resources involves I/O (e.g., closing a network connection) to avoid blocking the calling thread.

- id: dotnet-gc-span-arraypool-03
  answer: |
    Span<T> provides a type-safe view over contiguous memory (stack or heap) without allocating — it avoids heap allocations for slicing and parsing. stackalloc allocates memory on the stack, avoiding heap allocation entirely for small, short-lived buffers. ArrayPool<T> rents and returns reusable arrays, avoiding repeated large array allocations and GC pressure.

- id: dotnet-gc-struct-vs-class-04
  answer: |
    A struct (value type) is stored inline — on the stack or embedded within another object — and copied by value. A class (reference type) is stored on the heap and accessed by reference. Boxing is the process of wrapping a value type in a reference type object on the heap, which incurs an allocation and a copy; unboxing reverses it. Frequent boxing/unboxing causes GC pressure and performance overhead.

- id: dotnet-json-stj-defaults-01
  answer: |
    Set PropertyNamingPolicy = JsonNamingPolicy.CamelCase on JsonSerializerOptions to get camelCase property names globally. To override the serialized name of a single property, apply the [JsonPropertyName("customName")] attribute to that property.

- id: dotnet-json-sourcegen-02
  answer: |
    System.Text.Json source generation uses a C# source generator to create serialization/deserialization code at compile time instead of using reflection at runtime. This matters for performance because it eliminates reflection overhead and boxing, and it matters for trimming and Native AOT because reflection-based serialization is incompatible with trimming (which removes unused code) and AOT (which requires all code to be pre-compiled).

- id: dotnet-json-stj-vs-newtonsoft-03
  answer: |
    System.Text.Json is built into modern .NET, faster, and allocation-efficient, but has fewer features: less flexible custom converter support, no built-in reference loop handling by default, and fewer formatting options. Newtonsoft.Json (Json.NET) is more mature and feature-rich — better reference handling, more customization options, broader community knowledge — but is external, slower, and allocates more. For new projects, STJ is the default choice; Newtonsoft is still used when its advanced features are needed.

- id: dotnet-json-options-reuse-04
  answer: |
    JsonSerializerOptions is expensive to create because it builds internal caches and converters. Create it once and reuse it (e.g., as a static instance or via dependency injection). For polymorphic serialization, use [JsonPolymorphic] on the base type and [JsonDerivedType(typeof(Derived), "discriminator")] on derived types to control type discrimination during serialization and deserialization.

- id: dotnet-http-socket-exhaustion-01
  answer: |
    Creating and disposing an HttpClient per request leaves its sockets in TIME_WAIT, so under load the machine runs out of ports. The obvious fix, one shared static HttpClient, avoids exhaustion but never re-resolves DNS, so it keeps talking to stale IPs after a failover. IHttpClientFactory solves BOTH: it pools HttpMessageHandlers (no exhaustion) and rotates them on HandlerLifetime (default 2 min), so DNS changes are picked up.

- id: dotnet-http-typed-clients-02
  answer: |
    Named clients are pre-configured HttpClient instances registered by a string key via services.AddHttpClient("name", client => ...). Typed clients are classes that encapsulate an HttpClient and expose strongly-typed methods, registered via services.AddHttpClient<TClient>(). Typed clients are preferred because they provide better testability (the class can be mocked), clearer API surface, and separation of HTTP concerns from business logic.

- id: dotnet-http-lifetime-dns-03
  answer: |
    An HttpClient is bound to the handler it was created with; the factory rotates handlers on HandlerLifetime, so caching a factory client in a singleton/static field pins the old handler and defeats rotation. The alternative is to configure a SocketsHttpHandler with PooledConnectionLifetime (e.g., 2 minutes) as the primary handler — this recycles connections inside one handler. Factory rotation can then be disabled with SetHandlerLifetime(Timeout.InfiniteTimeSpan). HandlerLifetime and PooledConnectionLifetime are separate settings.

- id: dotnet-http-resilience-04
  answer: |
    On .NET 8+, add the Microsoft.Extensions.Http.Resilience package and chain .AddStandardResilienceHandler() onto AddHttpClient — this gives retry with backoff, a total timeout, a per-attempt timeout, and a circuit breaker. Use .AddResilienceHandler("name", builder => ...) for a custom pipeline. AddRetryPolicy, AddTimeoutPolicy and AddCircuitBreakerPolicy do not exist. The legacy Polly API is AddTransientHttpErrorPolicy / AddPolicyHandler. Always flow a CancellationToken (e.g., HttpContext.RequestAborted or a linked CancellationTokenSource with a timeout) into every GetAsync/SendAsync call so callers can cancel in-flight work.

- id: dotnet-log-templates-01
  answer: |
    Structured logging preserves the template and parameters separately, so the logging provider can store them as structured data (e.g., in a database or log aggregator) without parsing the rendered string. It also avoids the cost of string interpolation when the log level is disabled — the template is only rendered if the message will actually be written. This improves performance and enables better log querying and analysis.

- id: dotnet-log-levels-02
  answer: |
    The ILogger levels in order from least to most severe are: Trace, Debug, Information, Warning, Error, Critical, and None (disables logging). Per-category level filtering is configured in appsettings.json under the Logging:LogLevel section, where you can set a Default level and override specific namespaces — e.g., "Microsoft.EntityFrameworkCore": "Warning" quiets EF Core's verbose logs while keeping your app's Information-level logs.

- id: dotnet-log-highperf-03
  answer: |
    The LoggerMessage source generator ([LoggerMessage] attribute) generates strongly-typed, high-performance logging methods at compile time. It is preferred over calling logger.LogInformation directly on hot paths because it avoids boxing of value types, avoids string interpolation when the log level is disabled, and eliminates the overhead of parsing the template string at runtime — the generated code is essentially a direct call with pre-parsed template metadata.

- id: dotnet-log-scopes-otel-04
  answer: |
    BeginScope creates a logical operation context that attaches key-value pairs to all log messages emitted within the scope, enabling correlation of related log entries. For distributed tracing and metrics, .NET exposes OpenTelemetry via the OpenTelemetry SDK packages: OpenTelemetry (core), OpenTelemetry.Extensions.Hosting (integration), and exporters like OpenTelemetry.Exporter.OpenTelemetryProtocol. Tracing is done via ActivitySource/Activity, and metrics via Meter/Instrument — both integrate with ILogger and can be exported to backends like Jaeger, Prometheus, or OTLP collectors.
