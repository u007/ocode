- id: dotnet-di-lifetimes-01
  answer: |
    The three built-in DI service lifetimes in .NET are:
    - Transient: A new instance is created every time the service is resolved. Use for lightweight, stateless services.
    - Scoped: A new instance is created once per scope (e.g., per HTTP request in ASP.NET Core). All resolutions within the same scope share the same instance.
    - Singleton: A single instance is created the first time it is resolved and reused for the entire application lifetime. All resolutions share the same instance.

- id: dotnet-di-captive-02
  answer: |
    A captive dependency occurs when a service with a shorter lifetime is injected into a service with a longer lifetime. The classic bug is injecting a scoped service into a singleton. The singleton holds a reference to the scoped instance forever, so that scoped instance effectively becomes a singleton — it is never disposed at the end of its intended scope, and any state it accumulates or resources it holds (like a DbContext) are never released. This causes memory leaks, stale data, and concurrency bugs.

- id: dotnet-di-scope-in-singleton-03
  answer: |
    To use a scoped service from a singleton (e.g., a background service), inject IServiceScopeFactory into the singleton. On each iteration, create a new scope using scopeFactory.CreateScope(), resolve the scoped service from scope.ServiceProvider, use it, and dispose the scope when done. This ensures the scoped service (and its resources like DbContext) are properly disposed after each unit of work.

- id: dotnet-di-keyed-04
  answer: |
    Keyed services, introduced in .NET 8, allow multiple implementations of the same interface to be registered and resolved by a string key. Register with services.AddKeyedTransient<IService, ImplA>("a") and services.AddKeyedTransient<IService, ImplB>("b"). Consume by injecting IKeyedService<IService> or using [FromKeyedServices("a")] on a constructor parameter, or by resolving from IServiceProvider with GetKeyedService<IService>("a").

- id: dotnet-config-precedence-01
  answer: |
    In a typical host, configuration sources are applied in order, and later sources override earlier ones when the same key exists. The default order (lowest to highest precedence) is: appsettings.json, appsettings.{Environment}.json, user secrets (development only), environment variables, and command-line arguments. The last source that contains a key wins, so command-line arguments override environment variables, which override appsettings files.

- id: dotnet-config-options-interfaces-02
  answer: |
    - IOptions<T>: Singleton lifetime. Captures a snapshot of config at first resolution; does NOT see config reloads. Can be injected anywhere.
    - IOptionsSnapshot<T>: Scoped lifetime. Re-reads config on each scope (each request), so it sees reloads. Can only be injected into scoped or transient services, not singletons.
    - IOptionsMonitor<T>: Singleton lifetime. Provides change notifications via OnChange and always returns current values. Can be injected anywhere, including singletons.

- id: dotnet-config-secrets-03
  answer: |
    In local development, secrets should live in user secrets (dotnet user-secrets) or environment variables, never in source-controlled appsettings.json. User secrets are stored outside the project directory in a per-user location. In production, secrets should come from a dedicated secret manager (Azure Key Vault, AWS Secrets Manager, etc.) or environment variables injected by the hosting platform. The key difference: local dev secrets are per-developer and never committed; production secrets are centrally managed, audited, and rotated.

- id: dotnet-config-options-binding-04
  answer: |
    Bind a configuration section to a strongly-typed options class by calling services.Configure<MyOptions>(Configuration.GetSection("MySection")) in startup, or by using Configuration.GetSection("MySection").Get<MyOptions>(). To fail fast on invalid options, use services.AddOptions<MyOptions>().Bind(Configuration.GetSection("MySection")).ValidateDataAnnotations() or .Validate(...) with a custom validation action, and call ValidateOnStart() to force validation at application startup.

- id: dotnet-pipeline-order-01
  answer: |
    Middleware in ASP.NET Core executes in the order they are registered, forming a pipeline where each middleware can act before and after the next. UseRouting must come before UseAuthentication/UseAuthorization so the endpoint is identified before auth decisions are made. UseAuthentication must precede UseAuthorization because authorization needs the authenticated user. UseAuthorization must precede endpoint execution (UseEndpoints/MapControllers) so unauthorized requests are rejected before the handler runs. Incorrect ordering means auth is bypassed or the endpoint is unknown when auth runs.

- id: dotnet-pipeline-use-vs-map-02
  answer: |
    - Use: Registers inline middleware that can call next() to pass control to the next middleware, or short-circuit by not calling next().
    - Run: Registers terminal middleware that always short-circuits — it never calls next(). Used for endpoints of a pipeline branch.
    - Map: Branches the pipeline based on a path prefix. The branch is a separate pipeline that does not rejoin the main pipeline.
    A middleware short-circuits when it writes a response or otherwise returns without calling next(), preventing downstream middleware from executing.

- id: dotnet-pipeline-minimal-results-03
  answer: |
    In minimal APIs, a handler's return value is implicitly converted to an HTTP response. Returning a raw object serializes it with a 200 OK. Results (IResult) and TypedResults (e.g., TypedResults.Ok(), TypedResults.NotFound()) give you explicit control over status code, content type, headers, and response body. They also provide better type safety, OpenAPI/Swagger documentation, and avoid reflection-based serialization by using strongly-typed return values.

- id: dotnet-pipeline-filters-vs-middleware-04
  answer: |
    Middleware runs for every request and is cross-cutting. Endpoint filters (minimal APIs) and MVC action filters run only for matched endpoints/actions and have access to endpoint-specific metadata (action arguments, model state, the specific handler). Filters can do what middleware can't: access action parameters, short-circuit model binding, modify action results, and run code specifically before/after action execution with knowledge of the action's context. Middleware is better for truly cross-cutting concerns like CORS, exception handling, and authentication.

- id: dotnet-efcore-context-lifetime-01
  answer: |
    The correct lifetime for a DbContext is scoped — one instance per unit of work (typically per HTTP request in web apps). A DbContext is not thread-safe because it tracks changes in its internal ChangeTracker and maintains state (cached entities, connection state) that assumes single-threaded access. Sharing across threads or concurrent async operations causes race conditions, corrupted state, and InvalidOperationException. Each concurrent operation needs its own DbContext instance.

- id: dotnet-efcore-notracking-02
  answer: |
    EF Core's change tracker monitors entity state (Added, Modified, Deleted, Unchanged) so it can generate the correct SQL on SaveChanges. AsNoTracking() tells EF Core not to track the returned entities. Use it for read-only queries where you won't save changes back — it avoids the overhead of snapshot creation and change tracking, improving performance and reducing memory usage. It is especially beneficial for large read-only result sets.

- id: dotnet-efcore-nplus1-03
  answer: |
    IQueryable uses deferred execution — the query is not executed until enumerated (e.g., by ToListAsync()). ToListAsync() forces immediate execution and materializes results. The N+1 problem arises when you load a parent collection (1 query), then access a navigation property on each child in a loop, triggering a separate query per child (N queries). Fix it with eager loading (.Include()/.ThenInclude()) to fetch related data in a single query, or with explicit loading, or projection (.Select()) to fetch only needed fields.

- id: dotnet-efcore-savechanges-tx-04
  answer: |
    Yes, a single SaveChangesAsync() call is transactional — EF Core wraps all changes in a single database transaction, so either all succeed or all roll back. A migration in EF Core is a code-first mechanism to evolve the database schema over time. Migrations capture model changes as incremental steps (Up/Down methods) that can be applied to bring the database schema in sync with the entity model, and can be rolled back if needed.

- id: dotnet-gc-generations-loh-01
  answer: |
    The .NET GC is generational with three generations: Gen 0 (newly allocated, collected most frequently), Gen 1 (survived one collection), and Gen 2 (long-lived objects, collected least frequently). The Large Object Heap (LOH) holds objects ≥ 85,000 bytes and is collected only during Gen 2 collections. Large, short-lived allocations are especially costly because they go straight to the LOH, are not compacted by default, and force expensive Gen 2 collections that scan the entire heap, causing long pauses.

- id: dotnet-gc-dispose-finalizer-02
  answer: |
    IDisposable/Dispose provides deterministic cleanup of unmanaged resources (file handles, sockets) — you call it explicitly or via using. A finalizer (destructor) is a non-deterministic backup that the GC calls during collection if Dispose was not called. Use Dispose for deterministic cleanup; add a finalizer only when you directly hold unmanaged resources and need a safety net. IAsyncDisposable adds DisposeAsync() for asynchronous cleanup, which is preferred when disposal involves I/O operations that should not block.

- id: dotnet-gc-span-arraypool-03
  answer: |
    - Span<T>: A stack-only type that provides a safe, allocation-free view over contiguous memory (arrays, stack, heap). Avoids heap allocations for slicing and parsing.
    - stackalloc: Allocates memory on the stack instead of the heap, avoiding GC pressure for small, short-lived buffers. Limited to stack-only contexts.
    - ArrayPool<T>: A reusable pool of array buffers. Rent() returns a buffer and Return() recycles it, avoiding repeated large array allocations and the associated GC pressure.

- id: dotnet-gc-struct-vs-class-04
  answer: |
    A struct (value type) is allocated on the stack (or inline in a containing object) and copied by value — no heap allocation, no GC pressure. A class (reference type) is allocated on the heap, tracked by the GC, and copied by reference. Boxing occurs when a struct is cast to an interface or object — it is wrapped in a heap-allocated object, causing an allocation and GC pressure. Unboxing reverses this. Frequent boxing of structs is a performance concern.

- id: dotnet-json-stj-defaults-01
  answer: |
    To get camelCase property names globally, set JsonSerializerOptions.PropertyNamingPolicy = JsonNamingPolicy.CamelCase. To override the serialized name of a single property, use the [JsonPropertyName("customName")] attribute on that property. The attribute takes precedence over the naming policy.

- id: dotnet-json-sourcegen-02
  answer: |
    System.Text.Json source generation uses a C# source generator to create serialization/deserialization code at compile time instead of using reflection at runtime. This matters for performance because it eliminates reflection overhead and reduces startup time. For trimming and Native AOT, it is essential because reflection-based serialization is incompatible with trimming (the trimmer cannot see which members are used) and Native AOT (which does not support reflection emit). Source generation provides a trim-safe, AOT-compatible path.

- id: dotnet-json-stj-vs-newtonsoft-03
  answer: |
    System.Text.Json is the default in modern .NET — it is faster, allocation-free in many cases, built-in, and supports source generation for AOT/trimming. Newtonsoft.Json has a richer feature set: more flexible contract resolvers, better handling of polymorphic serialization, more converters, conditional serialization, and broader edge-case coverage. Trade-off: STJ is faster and more modern but sometimes requires more manual configuration for complex scenarios; Newtonsoft is more battle-tested and feature-rich but is an external dependency with more overhead.

- id: dotnet-json-options-reuse-04
  answer: |
    A JsonSerializerOptions instance should be created once and reused because constructing it is expensive (it sets up converters, naming policies, etc.) and the instance is thread-safe for serialization. Reusing avoids repeated setup cost. For polymorphic serialization, STJ supports [JsonPolymorphic] and [JsonDerivedType] attributes on base types to control how derived types are serialized and discriminated, or you can write a custom JsonConverter for full control.

- id: dotnet-http-socket-exhaustion-01
  answer: |
    Creating and disposing an HttpClient per request leaves its sockets in TIME_WAIT, so under load the machine runs out of ports. The obvious fix, one shared static HttpClient, avoids exhaustion but never re-resolves DNS, so it keeps talking to stale IPs after a failover. IHttpClientFactory solves BOTH: it pools HttpMessageHandlers (no exhaustion) and rotates them on HandlerLifetime (default 2 min), so DNS changes are picked up.

- id: dotnet-http-typed-clients-02
  answer: |
    Named clients are registered with a string name (services.AddHttpClient("name", ...)) and resolved via IHttpClientFactory.CreateClient("name"). Typed clients are registered as a class that takes HttpClient in its constructor (services.AddHttpClient<MyClient>()), encapsulating HTTP logic in a strongly-typed class. Prefer typed clients because they provide a clean API surface, encapsulate endpoint configuration and response handling, are easier to test (mock HttpClient), and avoid scattering HTTP calls throughout the codebase.

- id: dotnet-http-lifetime-dns-03
  answer: |
    An HttpClient is bound to the handler it was created with; the factory rotates handlers on HandlerLifetime, so caching a factory client in a singleton/static field pins the old handler and defeats rotation. This means the client keeps using the old handler's connections, which never re-resolve DNS, so it serves stale DNS exactly like a static client. The alternative is to configure a SocketsHttpHandler with PooledConnectionLifetime (e.g., 2 min) as the primary handler, which recycles connections inside one handler. Factory rotation can then be disabled with SetHandlerLifetime(Timeout.InfiniteTimeSpan). HandlerLifetime and PooledConnectionLifetime are separate settings.

- id: dotnet-http-resilience-04
  answer: |
    On .NET 8+, add the Microsoft.Extensions.Http.Resilience package and chain .AddStandardResilienceHandler() onto AddHttpClient. It gives retry with backoff, a total timeout, a per-attempt timeout, and a circuit breaker. Use .AddResilienceHandler("name", builder => ...) for a custom pipeline. AddRetryPolicy, AddTimeoutPolicy and AddCircuitBreakerPolicy do not exist. The legacy Microsoft.Extensions.Http.Polly API is AddTransientHttpErrorPolicy / AddPolicyHandler. Always pass a CancellationToken (e.g., HttpContext.RequestAborted or a linked CancellationTokenSource with a timeout) into every GetAsync/SendAsync call so callers can cancel in-flight work.

- id: dotnet-log-templates-01
  answer: |
    Using structured logging templates (logger.LogInformation("Order {OrderId} shipped", orderId)) instead of string interpolation preserves the template as a constant and passes the value as a parameter. This allows log providers to capture the template and parameters separately, enabling structured logging systems to index and query by field name (OrderId) rather than parsing a formatted string. It also avoids the cost of string interpolation when the log level is disabled, and prevents sensitive data from being baked into the message template.

- id: dotnet-log-levels-02
  answer: |
    The ILogger log levels in order from least to most severe are: Trace (0), Debug (1), Information (2), Warning (3), Error (4), Critical (5), and None (6). Per-category level filtering is configured in appsettings.json under the Logging:LogLevel section, where you can set a Default level and override per namespace/category (e.g., "Microsoft.EntityFrameworkCore": "Warning" to quiet EF Core logs). You can also use ILoggingBuilder.AddFilter in code or configure filtering via configuration providers.

- id: dotnet-log-highperf-03
  answer: |
    The LoggerMessage source generator ([LoggerMessage] attribute on a partial method) generates efficient logging code at compile time. It is preferred over calling logger.LogInformation directly on hot paths because it avoids the cost of capturing the format string and arguments into a params array, avoids boxing of value types, and skips the check for whether the log level is enabled before building the message. The generated code is allocation-free and significantly faster for high-frequency logging.

- id: dotnet-log-scopes-otel-04
  answer: |
    Logging scopes (BeginScope) group a set of log messages with a shared context (e.g., a request ID or correlation ID), so all logs within the scope carry the same scope properties. .NET exposes distributed tracing and metrics through OpenTelemetry via the System.Diagnostics.DiagnosticSource and OpenTelemetry SDK packages. ActivitySource and Activity represent distributed traces (spans), and Meter/Instrument represent metrics. The OpenTelemetry .NET SDK integrates with ILogger to automatically create spans from log scopes and export traces and metrics to backends like Jaeger, Prometheus, or OTLP endpoints.
