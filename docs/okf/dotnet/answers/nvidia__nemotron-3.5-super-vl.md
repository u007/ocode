# nvidia/nemotron-3.5-super-vl — closed-book answers for dotnet (openrouter, 2026-10-16); answerer saw only _prompts/dotnet.md
- id: dotnet-di-lifetimes-01
  answer: |
    Transient: a new instance is created every time the service is resolved/injected; each consumer gets its own instance. Best for lightweight, stateless services.

    Scoped: one instance per scope (typically one per HTTP request via IServiceScope); it is reused within that scope, and a new instance is created for each new scope. Best for per-request state such as DbContext.

    Singleton: a single instance is created on first resolution and reused by every consumer for the lifetime of the container/application. Best for stateless, shared services or caches.

    Resolution points: transient = new instance on every resolve; scoped = new instance per scope; singleton = one instance total.
- id: dotnet-di-captive-02
  answer: |
    A captive dependency is when a longer-lived component holds onto a dependency whose intended lifetime is shorter — for example, a singleton holding a scoped service. The scoped instance outlives its scope and is shared beyond it.

    Injecting a scoped service into a singleton is a bug because the singleton is created once, so the scoped service (e.g., a DbContext) is created once and reused across all requests/scopes instead of per request. That leaks state between requests, breaks per-request disposal (the context is disposed with the wrong scope or not at all per request), and is unsafe since DbContext is not thread-safe for concurrent use across requests.
- id: dotnet-di-scope-in-singleton-03
  answer: |
    Do not inject the scoped service directly into the singleton. Instead inject IServiceScopeFactory (which is singleton-safe) into the background service. On each iteration:

    using var scope = scopeFactory.CreateScope();
    var db = scope.ServiceProvider.GetRequiredService<MyDbContext>();
    // use db
    // scope disposal disposes the DbContext

    This creates a fresh scoped DbContext per iteration and disposes it properly. An alternative is to inject IDbContextFactory<MyDbContext> and call CreateDbContext() per iteration.
- id: dotnet-di-keyed-04
  answer: |
    Keyed services, introduced in .NET 8. Register implementations by key:

    builder.Services.AddKeyedTransient<IService, ImplA>("A");
    builder.Services.AddKeyedTransient<IService, ImplB>("B");

    (AddKeyedScoped and AddKeyedSingleton also exist.)

    Consume with the [FromKeyedServices("A")] attribute on a constructor or endpoint parameter, or programmatically via IKeyedServiceProvider (sp.GetKeyedService<IService>("A")). This lets two implementations of the same interface be resolved by name.
- id: dotnet-config-precedence-01
  answer: |
    Default precedence in a typical host, low to high: appsettings.json, then appsettings.{Environment}.json, then Secret Manager (Development), then environment variables, then command-line arguments. Later sources override earlier ones, so the last source that provides a key wins — command-line arguments win over environment variables, which win over environment-specific appsettings, and so on. Keys not overridden by a later source fall through to earlier sources.
- id: dotnet-config-options-interfaces-02
  answer: |
    IOptions<T>: singleton lifetime; value computed once and cached (snapshot at first access); does not see configuration reloads; injectable anywhere, including singletons.

    IOptionsSnapshot<T>: scoped; recomputed per scope so it sees config changes within that scope (per-request snapshot); cannot be injected into a singleton (would be captive).

    IOptionsMonitor<T>: singleton; reloads when the underlying config changes; exposes CurrentValue and OnChange(callback) for change notifications; injectable anywhere, including singletons.

    Use IOptions for static config, IOptionsSnapshot for per-request current values, and IOptionsMonitor when a singleton needs to react to reloads.
- id: dotnet-config-secrets-03
  answer: |
    Local development secrets live in Secret Manager (dotnet user-secrets), stored in a JSON file under the user profile outside the project tree (e.g., ~/.microsoft/usersecrets/<UserSecretsId>/secrets.json), keyed per project via UserSecretsId, and never committed to source control.

    Production should never use user secrets; use environment variables or an external secret store (Azure Key Vault, AWS Secrets Manager, HashiCorp Vault, etc.) via configuration providers, typically accessed with a managed identity.

    Difference: development = local user-profile file; production = external secret service/environment variables, with no secrets file on disk.
- id: dotnet-config-options-binding-04
  answer: |
    Bind with the options pattern: services.Configure<T>(configuration.GetSection("Section")) (or services.AddOptions<T>().Bind(section)), then inject IOptions<T> and read .Value.

    To fail fast, annotate the options class with DataAnnotations attributes ([Required], [Range], [StringLength]) and call .ValidateDataAnnotations() on the options builder; in .NET 8 add .ValidateOnStart() so validation runs eagerly at startup instead of lazily on first access. This throws at startup when configuration is invalid rather than failing later at runtime.
- id: dotnet-pipeline-order-01
  answer: |
    Middleware forms a chain in registration order: the request travels down through each component, which may call next() to pass the request onward; after downstream components finish, control unwinds back up so each component can post-process the response. A component that never calls next() short-circuits the chain.

    Order matters because some middleware depends on work done earlier. Routing must run before authorization so endpoint metadata (policies, authentication schemes) is known; authentication must run before authorization (identity is established first, then policies are checked). Typical order: UseExceptionHandler/UseHsts/UseHttpsRedirection/UseStaticFiles, then UseRouting, UseCors, UseAuthentication, UseAuthorization, and finally UseEndpoints/MapControllers.
- id: dotnet-pipeline-use-vs-map-02
  answer: |
    Use: adds middleware that receives HttpContext and a next delegate; it can inspect/modify the request, call next() to pass downstream, and post-process the response on the way back. It is not terminal unless it chooses not to call next.

    Run: terminal middleware; it writes a response directly and never calls next(), ending the pipeline.

    Map: branches the pipeline based on a predicate (e.g., path, as in Map("/api", ...)); matching requests continue into the branch, others skip it.

    Short-circuit means a middleware returns without invoking next(), so no downstream middleware runs (Run always short-circuits; Use can too).
- id: dotnet-pipeline-minimal-results-03
  answer: |
    The minimal API infrastructure converts the handler return into an HTTP response: an IResult is used directly; a Task<T> is awaited; a plain object is serialized to JSON with 200 OK; a string becomes text/plain; a bool maps to 200/404, etc.

    Results and TypedResults are factory methods (Results.Ok, Results.NotFound, Results.Json, TypedResults.Oak<T>, etc.) returning typed IResult implementations. They give explicit control over status code, content type, headers, and body, and TypedResults additionally improves OpenAPI metadata for endpoint documentation. Returning raw objects is convenient but always yields 200/JSON with less control.
- id: dotnet-pipeline-filters-vs-middleware-04
  answer: |
    Middleware runs before endpoint selection, sees only HttpContext, and runs once per request (or per matched branch). Use it for request-level cross-cutting concerns: logging, error handling, static files, authentication/authorization at the transport level.

    Filters run after routing within the MVC/endpoint pipeline around action execution (authorization, resource, action, exception, result filters). Filters can inspect and change action arguments, read model state, short-circuit the action, wrap action execution (transform results, catch exceptions per action), and apply per-controller/action via attributes — things middleware cannot do, since middleware has no access to action metadata or arguments. Conversely, filters cannot run before routing. Use middleware for request-level concerns and filters for action-level behavior.
- id: dotnet-efcore-context-lifetime-01
  answer: |
    Scoped (AddDbContext registers DbContext as scoped): one DbContext per unit of work/request, disposed when the scope ends. Never register it as a singleton — that is a captive dependency that shares state across requests.

    A DbContext is not thread-safe: it owns the change tracker, identity map, connections, and pending operation state. Concurrent async operations on the same instance throw ("A second operation started on this context before a previous operation completed") and can corrupt tracker state. Use one context per logical operation and never share across threads or requests.
- id: dotnet-efcore-notracking-02
  answer: |
    The change tracker keeps snapshots of entity original values, entity states, identity resolution, relationship fixups, and produces the INSERT/UPDATE/DELETE SQL that SaveChanges executes.

    AsNoTracking() makes query results untracked: no snapshots, no identity resolution, less memory and CPU, faster read-only queries; changes to those entities will not be persisted by SaveChanges. Use it for read-only queries (reports, lookups) where you will not call SaveChanges on the returned entities.
- id: dotnet-efcore-nplus1-03
  answer: |
    IQueryable defers execution: LINQ builds an expression tree and the SQL runs only when the query is enumerated or materialized (ToListAsync, FirstAsync, foreach, etc.). ToListAsync() executes the query immediately.

    The N+1 problem arises when a query returns N parents and then accessing each parent's navigation property triggers N additional queries (via lazy loading or post-query access) — N+1 round trips total. Fix it with eager loading (Include/ThenInclude, usually a single joined query) or AsSplitQuery (bounded multiple queries), and avoid touching unloaded navigation properties outside the query.
- id: dotnet-efcore-savechanges-tx-04
  answer: |
    Yes. A single SaveChanges/SaveChangesAsync sends all pending inserts, updates, and deletes in one database transaction (BEGIN … COMMIT), so the changes are all-or-nothing.

    A migration is a code file (Up/Down plus a model snapshot) describing a schema change. add-migration generates it from differences between the EF model and the last snapshot, and update-database applies it to evolve the schema incrementally, keeping the database in sync with the EF model.
- id: dotnet-gc-generations-loh-01
  answer: |
    Gen 0 holds short-lived small objects and is collected frequently and cheaply. Gen 1 is a buffer for survivors. Gen 2 holds long-lived objects; a gen 2 collection is a full, blocking (stop-the-world) GC.

    The Large Object Heap (LOH) holds objects ≥ 85,000 bytes (large arrays, big strings). They are allocated directly on the LOH and collected only during full gen 2 collections, and historically are not compacted, causing fragmentation.

    Large short-lived allocations are costly because they skip cheap gen 0 collections, linger until an expensive full GC, fragment the heap, cause long pauses, and increase promotion pressure.
- id: dotnet-gc-dispose-finalizer-02
  answer: |
    IDisposable.Dispose provides deterministic cleanup (called via using) and releases unmanaged resources immediately; call GC.SuppressFinalize(this) afterward so the finalizer is skipped.

    A finalizer (~Class) is a non-deterministic fallback run by the GC finalizer thread if Dispose was never called; the object survives at least two GC cycles, adding latency and CPU. Implement one only when directly owning unmanaged resources, and prefer SafeHandle instead.

    IAsyncDisposable.DisposeAsync adds asynchronous cleanup (closing streams/database connections asynchronously) without blocking threads; a class can implement both IDisposable and IAsyncDisposable.
- id: dotnet-gc-span-arraypool-03
  answer: |
    Span<T>: a stack-only ref struct that views contiguous memory without copying; slicing arrays or strings avoids allocations for sub-ranges and parsing.

    stackalloc: allocates a small buffer on the stack, reclaimed automatically when the method returns; avoids heap/GC for tiny short-lived scratch buffers (keep them small).

    ArrayPool<T>: rents and returns array buffers from a shared pool; avoids repeated allocation and GC pressure for large temporary buffers. The caller must return the array (and ideally clear it) to avoid leaking data or stale contents.

    Memory<T>/ReadOnlyMemory<T> are heap/field-safe cousins of Span<T> for scenarios where a ref struct cannot be used.
- id: dotnet-gc-struct-vs-class-04
  answer: |
    Struct (value type): stored inline where declared — inside arrays, fields, or stack slots — with no per-instance heap object or GC tracking; assignment copies the value.

    Class (reference type): allocated on the managed heap; variables hold references; the GC tracks it.

    Boxing converts a struct to object or an interface, allocating a heap object and copying the value in (unboxing reverses it). Boxing a struct in loops or collections reintroduces heap allocation and GC pressure, defeating the inline-storage advantage of structs.
- id: dotnet-json-stj-defaults-01
  answer: |
    Set JsonSerializerOptions.PropertyNamingPolicy = JsonNamingPolicy.CamelCase (or use JsonSerializerDefaults.Web) when serializing, or register it globally via AddJsonOptions<JsonOptions>(o => o.JsonSerializerOptions.PropertyNamingPolicy = ...).

    Override a single property with the [JsonPropertyName("customName")] attribute on that property; attributes take precedence over the naming policy.
- id: dotnet-json-sourcegen-02
  answer: |
    System.Text.Json source generation: create a JsonSerializerContext-derived class with [JsonSerializable(typeof(T))] attributes; at build time the source generator emits reflection-free serialization logic for those types. Pass the generated context to Serialize/Deserialize instead of relying on runtime reflection.

    It matters because it improves startup and throughput, uses less memory, avoids runtime IL emission, is trimming-safe (no trim warnings), and works under Native AOT, where the reflection-based serializer is not supported.
- id: dotnet-json-stj-vs-newtonsoft-03
  answer: |
    System.Text.Json: built-in, fastest, lowest allocation, trimming/AOT-friendly, configured via options and converters; fewer built-in conveniences.

    Newtonsoft.Json: mature and highly customizable (contract resolvers, JsonConverter, type-name handling with $type, JObject/JToken LINQ-to-JSON), supports more types (e.g., DataTable); slower, heavier, and less trim/AOT-friendly.

    Trade-off: STJ for new apps, performance, and AOT; Newtonsoft for legacy compatibility, dynamic JSON, complex contracts, or features STJ lacks. Teams often standardize on one to avoid maintaining two serialization models.
- id: dotnet-json-options-reuse-04
  answer: |
    JsonSerializerOptions caches resolved type info, converters, and naming policy. Creating it per call discards that cache and re-runs reflection, adding CPU and GC cost on every serialize/deserialize. Reuse a single instance (static readonly or registered once via DI) and mutate it only before first use.

    Polymorphism: by default STJ serializes the declared (static) type's members, so derived-type members are ignored unless you opt in with [JsonPolymorphic] and [JsonDerivedType] (type discriminator) on the base or a custom JsonTypeInfoResolver; then the concrete type is emitted and deserialized correctly.
- id: dotnet-http-socket-exhaustion-01
  answer: |
    using (var client = new HttpClient()) per request disposes the client, but the underlying sockets linger in TIME_WAIT and ephemeral ports are consumed faster than they are recycled, leading to socket/port exhaustion (SocketException). Each client also pins one DNS resolution.

    IHttpClientFactory creates and pools SocketsHttpHandler handlers per named client, reuses connections, limits handler lifetime (default ~2 minutes) so handlers, connections, and DNS are recreated periodically, and disposes handlers properly.
- id: dotnet-http-typed-clients-02
  answer: |
    Named clients: named registrations via AddHttpClient("name", c => {...}) (base address, default headers), obtained with IHttpClientFactory.CreateClient("name").

    Typed clients: a custom class whose constructor takes HttpClient, registered with AddHttpClient<TClient>() (optionally configuring the inner client); the factory injects a configured HttpClient.

    Prefer typed clients because HTTP concerns (base URL, default headers, JSON shape) are encapsulated in one DI-registered class; callers do not repeat client setup, and the result is easier to test, maintain, and reason about with strongly typed endpoints.
- id: dotnet-http-lifetime-dns-03
  answer: |
    Factory-created handlers are reused for the handler lifetime (default 2 minutes). HttpClient holds the handler; SocketsHttpHandler resolves DNS when a connection is created and pools connections per endpoint. If the client/handler outlives a DNS change (cluster IP rotation, failover, container restart), the app keeps connecting to the stale IP — stale DNS.

    Mitigations: shorten handler lifetime with AddHttpClient(...).SetHandlerLifetime(TimeSpan) so handlers/connections/DNS refresh; use SocketsHttpHandler connection-pooling limits (PooledConnectionLifetime) so connections do not stick indefinitely; avoid long-lived singleton HttpClients for DNS-sensitive scenarios.
- id: dotnet-http-resilience-04
  answer: |
    In modern .NET (.NET 8), add resilience via Polly-backed pipelines on IHttpClientFactory: builder.AddResilienceHandler(...) or AddStandardResilienceHandler(...), chaining .Retry(...) (with backoff), .CircuitBreaker(...) (trips after failures, open/half-open states), and .Timeout(...).

    Timeouts bound each attempt. Cancellation tokens passed to SendAsync flow into the pipeline and honor cooperative cancellation (linked CTS, OperationCanceledException); token cancellation typically aborts the pipeline and races with the timeout.
- id: dotnet-log-templates-01
  answer: |
    Placeholders ({OrderId}) are captured as named properties by structured logging providers (Serilog, Seq, Application Insights), so you can query and filter by OrderId and the value is not just embedded in text.

    String interpolation renders the value into the message eagerly — the structured property is lost, queries cannot use it, and formatting runs even when the log level is disabled. Templates keep the value as a first-class field.
- id: dotnet-log-levels-02
  answer: |
    Levels, least to most severe: Trace, Debug, Information, Warning, Error, Critical (None disables logging).

    Per-category filtering is configured in appsettings.json under Logging:LogLevel:{CategoryName} (e.g., "Microsoft.EntityFrameworkCore": "Warning" quiets EF Core) or via ILoggingBuilder.AddFilter in code. More specific category names win over prefixes, with a Default fallback for unspecified categories.
- id: dotnet-log-highperf-03
  answer: |
    The [LoggerMessage] source generator (Microsoft.Extensions.Logging): write partial static methods marked [LoggerMessage(Level, EventId, "template")] in a partial logger class; the compiler generates the logging implementation.

    It precomputes templates, avoids params object[] allocations and closures, skips scope-enumeration overhead, and has a cheap IsEnabled fast path. On hot paths it is preferred over direct LogInformation with interpolation because per-call allocation is near zero, with little overhead even when the level is disabled.
- id: dotnet-log-scopes-otel-04
  answer: |
    Logging scopes (ILogger.BeginScope) attach properties (correlation ID, request ID, user ID) to every log emitted within the disposable scope, grouping and correlating related logs without changing the level.

    Distributed tracing: System.Diagnostics.Activity (ActivitySource) with Activity.Current and W3C trace-context propagation, wired through OpenTelemetry.Extensions.Hosting (AddTracing) with exporters such as OTLP/Jaeger/Zipkin. Metrics: Meter instruments (Counter, Histogram, Gauge) via AddMetrics with the same exporters. High level: AddOpenTelemetry configures traces and metrics together, propagating ActivityId/traceparent across service boundaries.
