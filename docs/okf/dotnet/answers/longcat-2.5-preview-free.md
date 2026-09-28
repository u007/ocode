- id: dotnet-di-lifetimes-01
  answer: |
    Transient: a new instance is created every time the service is resolved (each injection point or Resolve call gets its own instance). Scoped: one instance is created per scope — in ASP.NET Core that means one instance per request; resolving the same scoped service multiple times within the same scope returns the same instance. Singleton: one instance is created on first resolution and reused for the entire application lifetime; all subsequent resolutions return that same instance.

- id: dotnet-di-captive-02
  answer: |
    A captive dependency is a service with a shorter lifetime that is held by a service with a longer lifetime. Injecting a scoped service into a singleton is a bug because the singleton captures the scoped instance at construction time and holds it forever. That scoped instance is never disposed at the end of its intended scope, so it lives for the application's lifetime — leading to stale data, memory leaks, and (for scoped resources like DbContext) potential concurrency issues. The DI container will typically throw an exception for this in development if scope validation is enabled.

- id: dotnet-di-scope-in-singleton-03
  answer: |
    Inject IServiceScopeFactory into the singleton. On each iteration (or unit of work), call scopeFactory.CreateAsyncScope() to create a new scope, resolve the scoped service from scope.ServiceProvider, use it, and dispose the scope when done. This gives the scoped service a proper lifetime boundary and ensures it is disposed correctly.

- id: dotnet-di-keyed-04
  answer: |
    Keyed services, introduced in .NET 8. Register with AddKeyedSingleton/AddKeyedScoped/AddKeyedTransient (or AddKeyedTransient) passing a key. Consume via the [FromKeyedServices("key")] attribute on a constructor parameter, or by resolving from IKeyedServiceProvider. This allows multiple registrations of the same interface distinguished by a key.

- id: dotnet-config-precedence-01
  answer: |
    The default precedence (last source wins) in a typical ASP.NET Core host, from lowest to highest: appsettings.json, appsettings.{Environment}.json, user secrets (Development only), environment variables, command-line arguments. When the same key exists in multiple sources, the last-registered source's value wins — so command-line arguments override environment variables, which override appsettings files.

- id: dotnet-config-options-interfaces-02
  answer: |
    IOptions<T>: singleton lifetime; the options value is computed once and cached; does not see config reloads. IOptionsSnapshot<T>: scoped lifetime; recomputed on each scope (each request), so it picks up config reloads; safe to inject into scoped/transient services. IOptionsMonitor<T>: singleton lifetime; supports change notifications via OnChange and always returns the current value; can be injected anywhere including singletons.

- id: dotnet-config-secrets-03
  answer: |
    In local development, secrets should live in user secrets (dotnet user-secrets) or environment variables, keeping them out of source control. In production, secrets should come from a dedicated secret store such as Azure Key Vault, AWS Secrets Manager, or environment variables injected by the hosting platform. The key difference is that user secrets are a dev-only convenience backed by a local file, while production uses a managed, audited, access-controlled secret store.

- id: dotnet-config-options-binding-04
  answer: |
    Bind a section to a strongly-typed class via configuration.GetSection("Section").Bind(options) or by using services.Configure<T>(configuration.GetSection("Section")). To fail fast on invalid options, implement IValidateOptions<T> (returning a ValidationResult with failures) or use DataAnnotations ([Required], [Range], etc.) on the options class and call ValidateDataAnnotations in Configure. The validation runs when the options are first resolved, throwing OptionsValidationException on failure.

- id: dotnet-pipeline-order-01
  answer: |
    Middleware executes in the order registered, forming a pipeline where each middleware can act before and after the next. UseRouting must come before UseAuthentication/UseAuthorization so the endpoint is known; UseAuthentication must come before UseAuthorization so the user is identified before authorization checks; both must run before UseEndpoints so the endpoint executes with auth applied. Misordering means auth runs too late (endpoint already executed) or too early (no endpoint metadata available).

- id: dotnet-pipeline-use-vs-map-02
  answer: |
    Use: registers middleware that can call the next middleware (pass-through or short-circuit by not calling next). Run: registers terminal middleware that never calls next — it always ends the pipeline. Map: branches the pipeline based on a path prefix, creating a separate sub-pipeline for matching requests. Short-circuiting means a middleware does not call next, so downstream middleware and the endpoint never run.

- id: dotnet-pipeline-minimal-results-03
  answer: |
    A handler's return value is serialized to the response body with an implicit 200 OK. Results/TypedResults (Results.Ok, Results.Created, TypedResults.Ok, etc.) give you explicit control over status code, content type, and response shape at compile time, enable OpenAPI/Swagger documentation generation, and provide type-safe return values that the framework can validate and document.

- id: dotnet-pipeline-filters-vs-middleware-04
  answer: |
    Use middleware for cross-cutting concerns that apply to all requests (auth, logging, exception handling). Use endpoint/MVC filters when you need per-endpoint behavior or access to endpoint metadata (e.g., action arguments, model state, route data). Filters run in the endpoint execution pipeline, can short-circuit before/after the action, and have access to MVC-specific context (ActionExecutingContext, etc.) that middleware cannot see.

- id: dotnet-efcore-context-lifetime-01
  answer: |
    The correct lifetime is scoped (one per request in ASP.NET Core). A DbContext is not thread-safe because its internal change tracker and connection state are not synchronized for concurrent use. Sharing one instance across threads or concurrent async operations leads to corrupted state, InvalidOperationException, and data integrity issues.

- id: dotnet-efcore-notracking-02
  answer: |
    The change tracker monitors entity state (Added, Modified, Deleted, Unchanged) so SaveChanges can generate the correct SQL. AsNoTracking() tells EF Core not to track the returned entities — use it for read-only queries to reduce memory overhead and improve query performance, since the tracker doesn't need to materialize and maintain entity state.

- id: dotnet-efcore-nplus1-03
  answer: |
    IQueryable defers execution — the query is built but not sent to the database until enumerated (e.g., by ToListAsync()). ToListAsync() executes immediately. The N+1 problem arises when you query a parent list (1 query) then access a navigation property in a loop, triggering a separate query per parent (N queries). Fix it with eager loading (.Include()), explicit loading, or projection (.Select()) to fetch only needed data in one query.

- id: dotnet-efcore-savechanges-tx-04
  answer: |
    Yes, a single SaveChangesChangesAsync() call is transactional — all changes succeed or all fail together. A migration in EF Core is a code-first schema evolution mechanism: it captures model changes as versioned C# classes that update the database schema to match the current model, enabling incremental, reproducible schema updates.

- id: dotnet-gc-generations-loh-01
  answer: |
    The GC uses three generations: Gen 0 (newly collected, short-lived), Gen 1 (survived one collection), Gen 2 (long-lived, collected least frequently). The Large Object Heap holds objects ≥ 85,000 bytes. Large short-lived allocations are especially costly because they go straight to the LOH, which is only collected during full (Gen 2) GCs — expensive, infrequent collections that also promote surviving objects, increasing memory pressure.

- id: dotnet-gc-dispose-finalizer-02
  answer: |
    IDisposable/Dispose provides deterministic cleanup — you control exactly when unmanaged resources are released (via using). A finalizer is non-deterministic — the GC calls it at an unspecified time during collection, as a safety net for unmanaged resources. IAsyncDisposable adds DisposeAsync for asynchronous cleanup (e.g., flushing streams, closing connections) without blocking, which is important in async-heavy code.

- id: dotnet-gc-span-arraypool-03
  answer: |
    Span<T>: a stack-only type that provides a view over any memory (stack, heap, native) without allocating — avoids heap allocations for slicing/buffering. stackalloc: allocates memory on the stack for small, short-lived buffers — avoids GC pressure entirely. ArrayPool<T>: a shared pool of reusable arrays — avoids repeated large array allocations and the associated GC cost by renting and returning arrays.

- id: dotnet-gc-struct-vs-class-04
  answer: |
    A struct (value type) is allocated on the stack (or inline in a containing object) and copied by value — no GC pressure for small, short-lived data. A class (reference type) is allocated on the heap, referenced by pointer, and tracked by the GC. Boxing is wrapping a value type in a reference type (object), which allocates on the heap and causes GC pressure — it happens when a struct is cast to object or an interface.

- id: dotnet-json-stj-defaults-01
  answer: |
    Set PropertyNamingPolicy = JsonNamingPolicy.CamelCase in JsonSerializerOptions (or via [JsonSerializerOptions] defaults). To override a single property's name, use the [JsonPropertyName("customName")] attribute on that property.

- id: dotnet-json-sourcegen-02
  answer: |
    System.Text.Json source generation uses a C# source generator to create serialization/deserialization code at compile time instead of using reflection at runtime. It matters for performance (no reflection overhead, faster startup) and for trimming/Native AOT (reflection-based serialization is incompatible with trimming and AOT; source-generated code is fully trim-compatible and AOT-friendly).

- id: dotnet-json-stj-vs-newtonsoft-03
  answer: |
    System.Text.Json: faster, lower allocation, built into .NET, trimming/AOT-compatible, but historically less flexible (fewer converters, less customizable). Newtonsoft.Json: more mature, more features (custom converters, contract resolvers, more lenient parsing), better handling of edge cases, but higher allocation and not trimming/AOT-friendly. Trade-off: STJ for performance and modern .NET; Newtonsoft when you need its advanced features or are maintaining legacy code.

- id: dotnet-json-options-reuse-04
  answer: |
    JsonSerializerOptions is thread-safe and caches internal state (converters, naming policies) — creating it repeatedly wastes CPU and memory. Create once and reuse (e.g., as a singleton or static). For polymorphic serialization, use [JsonPolymorphic] and [JsonDerivedType] attributes on the base type, or write a custom JsonConverter that handles base/derived type discrimination.

- id: dotnet-http-socket-exhaustion-01
  answer: |
    Disposing HttpClient per request closes the underlying connection, but the socket enters TIME_WAIT state and isn't released immediately. Under load, available sockets are exhausted, causing SocketException. IHttpClientFactory solves this by pooling and reusing HttpMessageHandler instances (which manage connections), rotating them periodically to refresh DNS, while still allowing per-client configuration.

- id: dotnet-http-typed-clients-02
  answer: |
    Named clients: register an HttpClient with a string name (AddHttpClient("name", ...)) and inject via IHttpClientFactory.CreateClient("name"). Typed clients: register a class that takes HttpClient in its constructor (AddHttpClient<TClient>()) — the class defines typed methods for API calls. Prefer typed clients for type safety, testability (mock the class, not HttpClient), and cleaner API surface.

- id: dotnet-http-lifetime-dns-03
  answer: |
    Even factory-created HttpClients can serve stale DNS if the underlying handler lives too long — the handler caches the connection and its DNS resolution. The fix is to set SocketsHttpHandler.PooledConnectionLifetime to a short interval (e.g., 2 minutes), which forces periodic connection rotation and DNS refresh. IHttpClientFactory configures this automatically with a default lifetime, but you can override it per client.

- id: dotnet-http-resilience-04
  answer: |
    Use Polly (via Microsoft.Extensions.Http.Polly) to add retries (AddRetryPolicy), timeouts (AddTimeoutPolicy), and circuit breakers (AddCircuitBreakerPolicy) to HttpClient. Cancellation tokens (CancellationToken) flow through all async calls and allow cooperative cancellation — pass them to GetAsync/PostAsync etc. so that when a request is cancelled (timeout, user abort), the underlying HTTP call is aborted promptly.

- id: dotnet-log-templates-01
  answer: |
    Using structured logging templates ("Order {OrderId} shipped") instead of interpolation means the log provider receives the template and the value separately. This avoids string allocation when the log level is disabled (the template is only formatted if the log is actually written), enables structured logging systems to index and query by property name, and prevents sensitive data from being written as plain text.

- id: dotnet-log-levels-02
  answer: |
    Levels in order: Trace (0), Debug (1), Information (2), Warning (3), Error (4), Critical (5), None (6). Per-category filtering is configured in appsettings.json under "LogLevel" with category-specific overrides (e.g., "LogLevel": { "Default": "Information", "Microsoft.EntityFrameworkCore": "Warning" }) — each category can have its own minimum level, quieting verbose subsystems while keeping others detailed.

- id: dotnet-log-highperf-03
  answer: |
    The LoggerMessage source generator ([LoggerMessage] attribute on a partial method) generates efficient logging code at compile time. It avoids boxing of value types, avoids allocating the log message string when the level is disabled, and avoids the params array allocation — making it significantly faster and lower-allocation than calling logger.LogInformation directly on hot paths.

- id: dotnet-log-scopes-otel-04
  answer: |
    BeginScope creates a logical operation context that attaches key-value pairs to all log entries within the scope — useful for correlating logs by request ID, transaction ID, etc. .NET exposes distributed tracing and metrics via OpenTelemetry: the System.Diagnostics API (ActivitySource for traces, Meter for metrics) integrates with the OpenTelemetry SDK, which exports traces, metrics, and logs to backends like Jaeger, Prometheus, and OTLP collectors.
