```yaml
- id: dotnet-di-lifetimes-01
  answer: |
    **Transient**: A new instance is created every time it is requested from the container. Use for lightweight, stateless services.
    **Scoped**: One instance is created per DI scope (e.g. per HTTP request in a web app). All consumers within that scope share the same instance. A new scope is created for each request, so a new instance is resolved per request.
    **Singleton**: One instance is created the first time it is requested (or when the container is built, if eagerly started) and shared for the entire lifetime of the application.
- id: dotnet-di-captive-02
  answer: |
    A "captive dependency" is when a service with a longer lifetime holds a reference to a service with a shorter lifetime. Injecting a scoped service (e.g. DbContext) into a singleton is a bug because the singleton will capture and hold that one scoped instance forever (or until the singleton is disposed). That scoped instance becomes stale — its internal state (change tracker, connection, etc.) goes out of sync with subsequent scopes, causing data corruption, connection leaks, or stale reads. The scoped service was designed to be short-lived; the singleton forces it to live indefinitely.
- id: dotnet-di-scope-in-singleton-03
  answer: |
    Inject `IServiceScopeFactory` into the singleton. On each iteration, create a new scope using `IServiceScopeFactory.CreateScope()`, resolve the scoped service (e.g. DbContext) from `IServiceProvider` of that scope, use it, then dispose the scope. This ensures the scoped service gets a fresh instance each iteration and is properly cleaned up. Example:
    ```csharp
    var scope = _scopeFactory.CreateScope();
    var db = scope.ServiceProvider.GetRequiredService<AppDbContext>();
    // use db...
    ((IDisposable)scope).Dispose();
    ```
    Alternatively, inject `IServiceProvider` directly, but `IServiceScopeFactory` is preferred because `CreateScope()` does not resolve anything from the root provider, avoiding potential deadlocks or misaligned scopes.
- id: dotnet-di-keyed-04
  answer: |
    .NET 8 introduced **keyed services**. You register implementations with a string or enum key:
    ```csharp
    builder.Services.AddKeyedTransient<IEmailSender, SmtpEmailSender>("smtp");
    builder.Services.AddKeyedTransient<IEmailSender, SendGridEmailSender>("sendgrid");
    ```
    You consume them using `[FromKeyedServices("name")]` on constructor parameters in minimal APIs, or by resolving with `GetKeyedService<T>(key)` or `GetRequiredKeyedService<T>(key)` from `IServiceProvider`. In MVC controllers you can use `[FromKeyedServices("name")]` on constructor parameters as well.
- id: dotnet-config-precedence-01
  answer: |
    In a typical .NET host, configuration sources are layered. The default precedence (last source wins) for a typical ASP.NET Core app is roughly:
    1. appsettings.json
    2. appsettings.{Environment}.json
    3. Environment variables
    4. Command-line arguments
    When the same key appears in multiple sources, the **last source added wins** (it overrides earlier ones). For example, an environment variable overrides appsettings.json for the same key. Custom sources added via `IConfigurationBuilder` are ordered by the sequence of `Add` calls; the last one added has highest priority.
- id: dotnet-config-options-interfaces-02
  answer: |
    **IOptions<T>**: Registered as singleton. Resolved once when first accessed and cached forever. Does NOT see config changes after startup. Can be injected anywhere (singletons, scoped, transient).
    **IOptionsSnapshot<T>**: Scoped lifetime. Re-reads the configuration on every new scope (e.g. per HTTP request), so it picks up config reloads within that request. Can be injected into scoped and transient services (not singletons, since it is scoped itself).
    **IOptionsMonitor<T>**: Singleton lifetime. Supports config change notifications via `OnChange(callback)`. Always returns the current value. Can be injected anywhere. Use when you need to react to config changes at runtime (e.g. feature flags that update).
- id: dotnet-config-secrets-03
  answer: |
    Local development secrets should live in the **.NET Secret Manager** (`dotnet user-secrets`), which stores them in a JSON file on disk outside the project (typically under `~/.microsoft/usersecrets/{id}/secrets.json`). This file is excluded from source control. In production, secrets come from environment variables, Azure Key Vault, AWS Secrets Manager, or similar secret-management services — never from source-controlled config files. The key difference is separation of concerns: local secrets are developer-machine-only and never committed; production secrets are injected at deploy time from a secure vault or environment, with access controls and audit trails.
- id: dotnet-config-options-binding-04
  answer: |
    Use `IConfiguration.GetSection("SectionName").Bind(myOptionsInstance)` or, more commonly, register via `builder.Services.Configure<MyOptions>(builder.Configuration.GetSection("SectionName"))`. This binds the section to a strongly-typed class. To fail fast on invalid options, use `DataAnnotations` attributes (e.g. `[Required]`, `[Range]`) on the options class and call `ValidateDataAnnotations()` during registration, or implement `IValidateOptions<T>` for custom validation and use `ValidateOnStart()` (available in .NET 7+). This causes validation at startup rather than when the option is first accessed, making the app fail fast.
- id: dotnet-pipeline-order-01
  answer: |
    The middleware pipeline is a chain of delegates. Each middleware calls `next()` to pass control to the next one, or short-circuits to stop. They execute in the order they are added via `Use*` / `Map*` calls. For example:
    - `UseRouting` matches the endpoint.
    - `UseAuthentication` must come after routing (to know which endpoint matched and its auth requirements) but before `UseAuthorization` (which checks the authenticated user against the endpoint's policy).
    - `UseAuthorization` must come after `UseAuthentication` and after `UseRouting` but before the endpoint is invoked.
    Putting `UseAuthentication` before `UseRouting` means no endpoint context is available yet, so authentication may not know what scheme to use. The typical pattern is: `UseRouting()` → `UseAuthentication()` → `UseAuthorization()` → `UseEndpoints()`.
- id: dotnet-pipeline-use-vs-map-02
  answer: |
    **Use**: Adds a middleware that receives the `HttpContext` and a `next` delegate. It can inspect/modify the request/response AND call `next()` to continue to the next middleware, or short-circuit by not calling `next()`.
    **Run**: Terminal middleware — adds a delegate that handles the request but does NOT receive a `next` delegate. Nothing runs after it. It is always the end of the line for that branch.
    **Map**: Branches the pipeline based on a condition (typically a path prefix). Everything under `Map` runs only when the condition matches. Each branch has its own pipeline.
    Short-circuiting means the middleware processes the request and writes a response WITHOUT calling `next()`, so subsequent middleware and endpoints never execute. This is useful for early returns (e.g. returning 401 from an authentication middleware before reaching the endpoint).
- id: dotnet-pipeline-minimal-results-03
  answer: |
    In minimal APIs, the return value of the handler is automatically serialized to the response body. If you return a plain object, it is serialized as JSON with a 200 status code. `Results` and `TypedResults` give you explicit control: `Results.Ok(value)`, `Results.NotFound()`, `Results.BadRequest(...)`, etc. `TypedResults` (introduced in .NET 7) is a generic version that provides compile-time type safety — the return type of the handler can be inferred, enabling better OpenAPI documentation and type-safe minimal APIs. Both let you set status codes, content types, and return specific HTTP responses rather than relying on implicit serialization.
- id: dotnet-pipeline-filters-vs-middleware-04
  answer: |
    Use **middleware** for cross-cutting concerns that apply to ALL requests (logging, auth, exception handling, response compression, rate limiting). Use **endpoint filters** (or MVC action filters) when you need behavior that is specific to certain endpoints or needs access to endpoint metadata/model binding context. A filter can:
    - Access the `EndpointFilterInvocationContext` (arguments, `HttpContext`, endpoint metadata).
    - Short-circuit with a specific result.
    - Run validation logic tied to model binding (e.g. `IEndpointFilter` runs after model binding in minimal APIs).
    - Target specific endpoints via attributes or conventions rather than running on every request.
    Filters have access to endpoint-level concerns (model binding results, action descriptors) that middleware does not. Middleware runs before routing is resolved, so it cannot act on endpoint-specific metadata.
- id: dotnet-efcore-context-lifetime-01
  answer: |
    The correct lifetime is **scoped** (one per HTTP request). A DbContext is NOT thread-safe — its internal `DbContextOptions`, change tracker, and database connection are designed for single-threaded use. Sharing it across threads or concurrent async operations causes race conditions, corrupt change tracking, connection pool issues, and potential data corruption. Each async operation should either use its own scoped DbContext or await sequentially on the same instance.
- id: dotnet-efcore-notracking-02
  answer: |
    The change tracker monitors entities loaded into the context, recording their state (Added, Modified, Deleted, Unchanged) and original values so that `SaveChangesAsync()` can generate the right SQL. `AsNoTracking()` tells EF Core to skip change tracking for the query results. Use it when you need read-only data (e.g. displaying data in a list, reporting) because: (1) it avoids the overhead of tracking (faster queries, lower memory), (2) it prevents accidental modifications to the returned entities from being persisted, and (3) it allows the context to be used more efficiently since it has fewer tracked entities. Do NOT use `AsNoTracking()` if you intend to modify and save the entities.
- id: dotnet-efcore-nplus1-03
  answer: |
    `IQueryable` defers execution — building a query expression tree that is only executed when enumerated (via `ToListAsync()`, `FirstOrDefaultAsync()`, etc.). The N+1 problem occurs when you load N parent entities, then for each one access a navigation property (e.g. `order.Items`), triggering a separate SQL query each time. You end up with 1 query for parents + N queries for children = N+1 total. Fix it using eager loading (`Include()` / `ThenInclude()`) to load navigation properties in a single query, or using projection (`Select()`) to load only the needed data in one query. `ToListAsync()` materializes everything eagerly, so if you `Include()` before materializing, all related data loads in one shot.
- id: dotnet-efcore-savechanges-tx-04
  answer: |
    A single `SaveChangesAsync()` call is transactional — EF Core wraps all the generated SQL commands in a single database transaction. If any command fails, the entire batch rolls back. However, calling `SaveChangesAsync()` multiple times creates separate transactions unless you explicitly wrap them in `Database.BeginTransactionAsync()` or use `IDbContextTransaction`. A migration is EF Core's mechanism for evolving the database schema over time — it generates SQL DDL scripts to create/alter tables, columns, indexes, etc. to match the current model. Migrations are applied in order and maintain a history table to track which migrations have been applied.
- id: dotnet-gc-generations-loh-01
  answer: |
    The .NET GC divides the managed heap into three generations:
    - **Gen 0**: Recently allocated objects. Collected frequently. Most objects die young, so this is efficient — collecting short-lived objects quickly.
    - **Gen 1**: Objects that survived one Gen 0 collection. Acts as a buffer between short-lived and long-lived objects.
    - **Gen 2**: Objects that survived Gen 1 collection. Collected less frequently (full GC). These are long-lived objects.
    The **Large Object Heap (LOH)** holds objects >= 85,000 bytes (approx). LOH is collected only during Gen 2 collections. Large, short-lived allocations are costly because: (1) they go directly to LOH, bypassing the efficient generational collection, (2) LOH collections only happen during expensive Gen 2 GCs, (3) LOH fragmentation can cause memory pressure since it is compacted less frequently (only in .NET 4.5.1+ when explicitly requested or when the system is under memory pressure).
- id: dotnet-gc-dispose-finalizer-02
  answer: |
    **IDisposable/Dispose**: Deterministic cleanup — called explicitly by the consumer (`using` block or manual call) when the resource is no longer needed. Frees unmanaged resources (file handles, database connections, native memory) promptly.
    **Finalizer (~ClassName)**: Non-deterministic cleanup — called by the GC at an indeterminate time when the object is collected. It is a safety net for when `Dispose()` was never called. Finalization has overhead: finalizable objects survive at least one GC generation, and finalization runs on a dedicated thread, delaying memory reclamation.
    You need `Dispose()` when you hold unmanaged resources or other `IDisposable` objects. The finalizer is a backup if `Dispose()` was missed.
    **IAsyncDisposable**: Adds `DisposeAsync()` for async cleanup scenarios (e.g. closing an async HTTP connection, flushing a stream asynchronously). Enables `await using` syntax, which properly awaits async disposal. Critical for avoiding thread blocking in async contexts.
- id: dotnet-gc-span-arraypool-03
  answer: |
    - **Span\<T\>**: A stack-allocated, ref-like type that provides a view over a contiguous block of memory (array, stackalloc, native memory). It avoids heap allocation by representing a slice without copying. Cannot be stored on the heap (not a class), so it lives only on the stack. Reduces allocations in parsing, slicing, and buffer operations.
    - **stackalloc**: Allocates memory directly on the stack (within a method frame). Avoids GC entirely — the memory is freed when the method returns. Useful for small, fixed-size buffers. Cannot exceed stack size limits.
    - **ArrayPool\<T\>**: A shared pool of reusable arrays. Instead of allocating and discarding arrays repeatedly (e.g. in high-throughput scenarios), rent from the pool and return when done. Reduces GC pressure from frequent large array allocations.
- id: dotnet-gc-struct-vs-class-04
  answer: |
    **Struct (value type)**: Allocated inline wherever it is used — on the stack when declared as a local variable or parameter, or inline within the containing object when it is a field of a class/struct. No heap allocation for the struct itself (though if embedded in a class, it lives on the heap as part of that object). No GC overhead.
    **Class (reference type)**: Always allocated on the heap. The variable holds a reference (pointer) to the heap object. The GC tracks and collects class instances.
    **Boxing** occurs when a value type is assigned to a variable of type `object` or an interface type — the value is copied from the stack to the heap, and a wrapper object is created. Boxing is costly: it allocates on the heap, requires GC, and unboxing to retrieve the value involves a type check and copy. Avoid boxing by using generics (`IComparable<T>` not `IComparable`).
- id: dotnet-json-stj-defaults-01
  answer: |
    To get camelCase, set `JsonSerializerOptions.PropertyNamingPolicy = JsonNamingPolicy.CamelCase` (or use the `WebDefaults` options). Alternatively, the `JsonSerializerDefaults.Web` preset uses camelCase by default. To override a single property's serialized name, use `[JsonPropertyName("specificName")]` attribute on that property. This attribute takes precedence over the naming policy.
- id: dotnet-json-sourcegen-02
  answer: |
    System.Text.Json source generation is a compile-time mechanism that generates serialization/deserialization code for your types at build time, rather than using reflection at runtime. It produces a `JsonSerializerContext` with `[JsonSerializable]` attributes. This matters because: (1) **Performance**: no reflection overhead at runtime, faster serialization, (2) **Trimming/Native AOT compatibility**: reflection-based code can be trimmed away or unavailable in AOT scenarios; source-generated code is always preserved, and (3) **Startup speed**: no runtime reflection metadata is needed. Essential for Native AOT, Blazor WebAssembly (trimmed), and performance-sensitive scenarios.
- id: dotnet-json-stj-vs-newtonsoft-03
  answer: |
    **System.Text.Json (STJ)**: Built into .NET Core 3.0+. Faster for most scenarios, better integration with ASP.NET Core, supports source generation, designed for trimming/AOT. Limitations: fewer features historically (e.g. no `[JsonConverter]` on properties initially, limited polymorphism support, no `JObject`/`JToken` equivalent, less flexible attribute model).
    **Newtonsoft.Json (Json.NET)**: Feature-rich, battle-tested, extensive ecosystem. Supports `[JsonProperty]`, `JObject`/`JToken` for dynamic JSON manipulation, extensive converter ecosystem, handles more edge cases (e.g. circular references, `ISerializationBinder`). Slower, requires a NuGet package, not trimming-friendly.
    Trade-offs: STJ is preferred for new projects (performance, trim/AOT support, no extra dependency). Newtonsoft is chosen when you need its richer feature set (dynamic JSON manipulation, specific converters) or are migrating legacy code that depends on it.
- id: dotnet-json-options-reuse-04
  answer: |
    `JsonSerializerOptions` instances are heavyweight — they cache reflection metadata and compiled converters internally. Creating a new instance per serialization call forces re-compilation of converters each time, causing significant performance degradation. Always create once (e.g. as a `static readonly` field) and reuse. STJ handles concurrent access safely after the first serialization call (once caches are built).
    For polymorphic serialization, STJ in .NET 8+ supports `[JsonDerivedType]` attributes on base types to map derived types to discriminator values. In earlier versions, you needed a custom `JsonConverter` to handle polymorphism. The `[JsonPolymorphic]` attribute (introduced in .NET 8) combined with `[JsonDerivedType(typeof(Derived), "typeValue")]` enables clean type-discriminated polymorphic serialization.
- id: dotnet-http-socket-exhaustion-01
  answer: |
    Each `HttpClient` uses an underlying `HttpMessageHandler` (typically `SocketsHttpHandler`) which maintains a connection pool. When you create a new `HttpClient` per request and dispose it, the underlying socket enters `TIME_WAIT` state (typically 240 seconds on Windows) before being released. Under load, this exhausts the available ephemeral ports and sockets, causing `SocketException` failures. `IHttpClientFactory` solves this by managing a pool of `HttpMessageHandler` instances — handlers are reused across `HttpClient` instances and rotated when their lifetime expires (default 2 minutes). This ensures sockets are properly recycled without exhausting the pool.
- id: dotnet-http-typed-clients-02
  answer: |
    **Named clients**: Registered with a string name (`AddHttpClient("github")`), resolved via `IHttpClientFactory.CreateClient("github")`. You configure the base URL, default headers, etc. by name. Downside: callers depend on a string, no compile-time safety.
    **Typed clients**: Registered with `AddHttpClient<IGitHubClient, GitHubClient>()` where `GitHubClient` is a class that wraps `HttpClient` in its constructor. The factory injects a configured `HttpClient` automatically. You access it through the interface.
    Prefer typed clients because: (1) compile-time type safety (no magic strings), (2) encapsulation of HTTP logic in a dedicated class, (3) easier to mock/test, (4) clearer dependency graph — consumers depend on the interface, not the factory.
- id: dotnet-http-lifetime-dns-03
  answer: |
    `SocketsHttpHandler` (the default since .NET 5) has a `PooledConnectionLifetime` (default: infinite). If a handler lives indefinitely, DNS records can change (e.g. cloud services rotate IPs) but the client keeps using stale, resolved IPs for existing connections. The factory's handler lifetime rotation helps: when `IHttpClientFactory` creates a new handler after the configured `Lifetime` (default 2 min), the old handler's connections drain and new ones resolve DNS fresh. For scenarios where `IHttpClientFactory` is not used, you can set `SocketsHttpHandler.PooledConnectionLifetime` directly (e.g. `TimeSpan.FromMinutes(2)`) to force periodic DNS re-resolution. In older .NET, `HttpClientHandler` had `ConnectionLeaseTimeout` (Windows only) or required manual `HttpClient` recreation.
- id: dotnet-http-resilience-04
  answer: |
    In .NET 8+, use the **Microsoft.Extensions.Http.Resilience** package (or the older `Polly` integration):
    - **Retries**: `AddStandardResilienceHandler()` includes retries with exponential backoff, or configure `HttpRetryStrategyOptions`.
    - **Timeouts**: `HttpTimeoutStrategyOptions` sets per-request or total timeouts.
    - **Circuit breaker**: `HttpCircuitBreakerStrategyOptions` stops sending requests after repeated failures, with a break duration and recovery.
    Combine with `ResiliencePipelineBuilder` for fine-grained control.
    **Cancellation tokens**: Pass a `CancellationToken` to every `HttpClient` method (`GetAsync`, etc.). The token enables cooperative cancellation — the caller can cancel requests (e.g. when the user navigates away), and the resilience pipeline respects it. A timeout sets a token that cancels after the duration; retries check the token before retrying. Always propagate the token through the call chain.
- id: dotnet-log-templates-01
  answer: |
    Structured logging with message templates (e.g. `"Order {OrderId} shipped"`) lets the logging framework capture `OrderId` as a structured data field, not baked into the string. This enables: (1) **Efficient log storage/querying** — you can filter and aggregate by `OrderId` in tools like Seq, Elastic, or Application Insights without parsing strings, (2) **Performance** — the string formatting/interpolation only happens if the log level is enabled (deferred formatting), and (3) **Reduced allocations** — interpolated strings always allocate a string even if the level is disabled, while message templates with placeholders defer string construction. Interpolation also defeats structured logging — the value is baked into the message and lost as a queryable field.
- id: dotnet-log-levels-02
  answer: |
    In order from most to least severe:
    1. **Trace** (0) — finest-grained, typically only for debugging
    2. **Debug** (1) — diagnostic information for development
    3. **Information** (2) — normal operation events (request completed, job finished)
    4. **Warning** (3) — unexpected but handled situations
    5. **Error** (4) — failures that affect a single operation
    6. **Critical** (5) — system-level failures requiring immediate attention
    7. **None** (6) — disables all logging
    Per-category filtering is configured via `logging:LogLevel` in `appsettings.json`:
    ```json
    {
      "Logging": {
        "LogLevel": { "Default": "Information" },
        "Microsoft.EntityFrameworkCore": { "LogLevel": "Warning" }
      }
    }
    ```
    Or programmatically via `builder.Logging.AddFilter("Microsoft.EntityFrameworkCore", LogLevel.Warning)` to quiet verbose EF Core logs while keeping other logs at the default level.
- id: dotnet-log-highperf-03
  answer: |
    The `[LoggerMessage]` source generator (introduced in .NET 6) generates optimized logging methods at compile time. Instead of `logger.LogInformation("Processing {Id}", id)` which uses reflection-based message template parsing at runtime, you define:
    ```csharp
    [LoggerMessage(Level = LogLevel.Information, Message = "Processing {Id}")]
    static partial void LogProcessing(this ILogger logger, int id);
    ```
    This is preferred on hot paths because: (1) the message template is parsed at compile time, not runtime (no reflection), (2) the generated method avoids boxing of value-type arguments, (3) it uses cached delegates for event IDs and categories, and (4) it reduces the overhead of logging calls that may be invoked thousands of times per second. The performance improvement is significant for high-frequency logging.
- id: dotnet-log-scopes-otel-04
  answer: |
    **Logging scopes** (via `BeginScope`) create contextual metadata that is attached to all log entries within that scope. For example, you might add `{"RequestPath": "/api/orders", "CorrelationId": "abc"}` — every log written within that scope includes these properties. This provides correlation and context without polluting every individual log call.
    **OpenTelemetry (.NET integration)**:
    - **Distributed tracing**: `ActivitySource` and `Activity` (built on `System.Diagnostics`) create trace spans. `Activity.Current` propagates trace context across service boundaries. The OpenTelemetry SDK exports these to backends (Jaeger, Zipkin, etc.).
    - **Metrics**: `System.Diagnostics.Metrics.Meter` and counters/histograms collect numeric metrics. The OpenTelemetry SDK exports them via OTLP.
    - .NET's `ILogger` integrates with OTel: log scopes can carry trace/span IDs, and log events can be correlated with trace spans. `Activity.Current?.TraceId` links logs to traces. The `Microsoft.Extensions.Logging.OpenTelemetry` package bridges structured logging to OTel's logging pipeline.
```
