```yaml
# Closed-book answers from my own knowledge only. No web searches, no repository
# or file access, no tools used. "Unsure" is flagged where applicable.
- id: dotnet-di-lifetimes-01
  answer: |
    Transient: a new instance is created every time the service is resolved — even twice
    within the same scope you get two different instances. Good for lightweight,
    stateless services.
    Scoped: one instance per scope. In ASP.NET Core a scope is created per HTTP request,
    so all consumers within the same request share one instance (e.g. DbContext).
    Outside a web app you control scopes explicitly via IServiceScopeFactory /
    CreateScope().
    Singleton: a single instance for the lifetime of the application/container, created
    lazily on first resolution (or immediately if ValidateOnBuild/eager singletons) and
    shared by everything.
    "When resolved": transient → every GetRequiredService/injection; scoped → first
    resolution within a scope, then reused until the scope is disposed; singleton →
    first resolution after the provider is built, then reused until the provider is
    disposed.
- id: dotnet-di-captive-02
  answer: |
    A captive dependency is a longer-lived service that has captured (holds a reference
    to) a shorter-lived service. Injecting a scoped service (e.g. a DbContext) into a
    singleton captures it forever: the scoped instance's "per-request" lifetime is
    effectively promoted to the app lifetime of the singleton. That's a bug because the
    scoped object was never designed to live that long — its state becomes stale across
    requests, it is typically not thread-safe (DbContext is definitely not), and it is
    never disposed until the singleton/container dies, leaking resources. The default
    service provider throws InvalidOperationException for this pattern when scope
    validation is enabled (Development), but it can slip through in Production.
- id: dotnet-di-scope-in-singleton-03
  answer: |
    Inject IServiceScopeFactory into the singleton/background service and create a fresh
    scope for each unit of work:
      using var scope = _scopeFactory.CreateScope();
      var db = scope.ServiceProvider.GetRequiredService<AppDbContext>();
      // use db, then the scope is disposed, releasing the DbContext
    Do this per iteration (e.g. each loop of a BackgroundService), so each unit of work
    gets its own short-lived scoped dependencies. Do not inject the scoped service
    directly into the singleton constructor.
- id: dotnet-di-keyed-04
  answer: |
    Keyed DI services, introduced in .NET 8. Register with AddKeyedSingleton /
    AddKeyedScoped / AddKeyedTransient and a key (string, type, or any object):
      services.AddKeyedScoped<IPayment, StripePayment>("stripe");
      services.AddKeyedScoped<IPayment, PayPalPayment>("paypal");
    Consume via [FromKeyedServices("stripe")] on a constructor parameter or property
    (supported by the MS DI container and ASP.NET Core), or resolve programmatically
    with IServiceProvider.GetKeyedService<T>(key) / GetRequiredKeyedService<T>(key).
- id: dotnet-config-precedence-01
  answer: |
    In a typical host (WebApplicationBuilder / Host.CreateApplicationBuilder) the
    default source order is roughly:
      1. appsettings.json
      2. appsettings.{Environment}.json (e.g. appsettings.Development.json)
      3. User secrets (Development only)
      4. Environment variables (including DOTNET_/ASPNETCORE_ prefixed ones)
      5. Command-line arguments
    Sources added later override earlier ones, so the LAST provider to define a key
    wins: command-line args beat environment variables, which beat user secrets, which
    beat appsettings.{Environment}.json, which beat appsettings.json. The merged
    IConfiguration then exposes the winning value for each key.
- id: dotnet-config-options-interfaces-02
  answer: |
    IOptions<T> — Singleton lifetime. Computes the value once on first access; it does
    NOT see config reloads. Injectable anywhere, including singletons. Simplest choice
    for static settings.
    IOptionsSnapshot<T> — Scoped lifetime. Rebuilt per scope (per request), so it DOES
    pick up config changes between requests, but only at scope boundaries. Injectable
    into scoped/transient services only — not singletons.
    IOptionsMonitor<T> — Singleton lifetime. Exposes CurrentValue (always fresh) plus an
    OnChange listener for push notifications on reload, and supports named options.
    Injectable anywhere, including singletons and background services — the right choice
    when a long-lived service must react to live config changes.
- id: dotnet-config-secrets-03
  answer: |
    Local development: the Secret Manager tool (dotnet user-secrets set "Key" "value").
    Secrets are stored outside the repo, in the user profile
    (~/.microsoft/usersecrets/<project-guid>/secrets.json), keyed to a UserSecretsId in
    the csproj, and are only read when the environment is Development. Never commit
    secrets to source control or appsettings files.
    Production: inject secrets via environment variables (or the platform equivalent)
    or, better, a proper secret store such as Azure Key Vault (via configuration
    providers), AWS Secrets Manager, HashiCorp Vault, or the cloud platform's built-in
    secret/config mechanisms — with access controlled by managed identity/service
    accounts rather than checked-in credentials.
- id: dotnet-config-options-binding-04
  answer: |
    Bind a section to an options class:
      builder.Services.Configure<MyOptions>(builder.Configuration.GetSection("MyOptions"));
    (GetSection("MyOptions").Get<MyOptions>() also works for one-off reads.) The binder
    maps keys by property name; [Required]/[Range] etc. from DataAnnotations can be
    declared on the class.
    Fail fast: use the options validation pipeline with ValidateOnStart:
      builder.Services.AddOptions<MyOptions>()
          .Bind(builder.Configuration.GetSection("MyOptions"))
          .ValidateDataAnnotations()            // or .Validate(o => ..., "message")
          .ValidateOnStart();
    ValidateOnStart runs validation when the app starts instead of lazily on first use,
    so invalid configuration crashes the app immediately at boot rather than at some
    unpredictable later point.
- id: dotnet-pipeline-order-01
  answer: |
    The middleware pipeline is a chain of delegates: each middleware receives the
    request and a `next` delegate. Execution flows in registration order on the way in,
    and back up the chain in reverse on the way out — so registration order is the
    request-processing order, and it is not re-orderable at runtime.
    Correct relative placement: UseRouting must run before anything that needs to know
    the matched endpoint (endpoint metadata such as [Authorize] or CORS policies is
    only available after routing selects an endpoint). UseAuthentication must run
    before UseAuthorization because authorization needs the authenticated user to make
    its decision. Both must run after UseRouting (to see the endpoint's authorization
    requirements) and before the endpoint actually executes (UseEndpoints / Map*
    calls), otherwise auth would run too late or without knowledge of the endpoint.
    Getting this wrong produces runtime errors (e.g. authorization middleware throwing
    when no endpoint was matched) or silently skipped work.
- id: dotnet-pipeline-use-vs-map-02
  answer: |
    Use(ctx, next): registers middleware that may do work, then either call
    `await next(ctx)` to continue down the pipeline, or not call next to short-circuit
    (it produces the response itself and later middleware never runs).
    Run(ctx): registers a terminal handler — it gets no `next` delegate; it is the end
    of that pipeline branch.
    Map(path, branch): conditionally branches the pipeline — if the request path starts
    with the given path, the branch pipeline handles it; MapWhen does the same with a
    predicate.
    Short-circuiting means a middleware returns a response without invoking `next`,
    so downstream middleware and the endpoint are skipped entirely (e.g. an exception
    middleware returning 500, or auth middleware returning 401).
- id: dotnet-pipeline-minimal-results-03
  answer: |
    The RequestDelegateFactory converts the handler's return value into a response:
    an IResult instance is executed (writes status/headers/body); a string is written
    as text/plain; anything else is JSON-serialized (WriteAsJsonAsync) with
    content-type application/json. A return type like Task<int> becomes a 200 with the
    JSON body; void/Task becomes 200 with empty body.
    Results.Ok(x) / Results.NotFound() etc. return IResult — flexible but weakly
    typed. TypedResults.Ok(x) / TypedResults.NotFound() return concrete result types
    (Ok<T>, NotFound, ...) which gives: (a) compile-time type safety and better
    unit-testability (you can assert on the concrete result and its value), and
    (b) automatic OpenAPI/metadata generation — TypedResults types implement
    IEndpointMetadataProvider, so the framework infers response types and status codes
    for the endpoint description without needing ProducesResponseType attributes.
- id: dotnet-pipeline-filters-vs-middleware-04
  answer: |
    Middleware is app-wide, request-level plumbing: it runs before/after routing for
    every request, has no knowledge of which endpoint matched, and cannot access
    route values, model binding, action arguments, filters' context objects, or MVC
    constructs like ModelState.
    Endpoint filters (minimal APIs) and MVC action/authorization/resource/result
    filters run inside the endpoint's own execution, so they can: inspect and modify
    the action arguments (and validated ModelState in MVC), short-circuit only that
    endpoint while still allowing the rest of the pipeline's response path, run code
    around model binding and result execution, participate per-action via attributes
    or DI-based filter factories, and read endpoint/route metadata.
    Rule of thumb: cross-cutting, endpoint-agnostic concerns (auth, CORS, exception
    handling, logging, static files) → middleware; per-endpoint/per-action concerns
    (validation, wrapping or replacing a specific action's result, caching a specific
    endpoint) → filters.
- id: dotnet-efcore-context-lifetime-01
  answer: |
    Scoped. AddDbContext registers the context as scoped by default: one DbContext per
    request (per unit of work), disposed at the end of the scope. It is deliberately
    short-lived.
    A DbContext instance is not thread-safe or safe for concurrent async operations
    because it bundles per-instance mutable state: a single database connection (with
    its own transaction state), the change tracker/identity map, and the current
    transaction. Two concurrent queries or SaveChanges on the same instance race on
    that state — you get exceptions like "A second operation started on this context
    before a previous operation completed" or silent state corruption. For parallel
    work, use IDbContextFactory<T> to create separate context instances per thread/task.
- id: dotnet-efcore-notracking-02
  answer: |
    The change tracker is the in-memory bookkeeping the context keeps for entities it
    materializes from queries: it stores the original snapshot values, maintains
    identity resolution (one instance per key per context), and computes the diff at
    SaveChanges to generate INSERT/UPDATE/DELETE statements.
    AsNoTracking() tells a query to materialize entities without registering them in
    the tracker. That saves the memory of storing snapshots and skips the diff
    computation, making read-only queries noticeably faster with much lower memory
    use — ideal for reporting, exports, APIs that only read data. Trade-off: the
    returned entities are detached — you cannot just mutate and SaveChanges them, and
    identity resolution doesn't apply (two rows for the same key give separate
    instances).
- id: dotnet-efcore-nplus1-03
  answer: |
    IQueryable<T> holds an expression tree, not data. Building Where/OrderBy/Include
    only composes the tree; the SQL is generated and executed when the query is
    enumerated — e.g. by ToListAsync(), foreach, or Any(). ToListAsync executes
    immediately and brings the full result set into memory; the unenumerated
    IQueryable just defers (and can be further composed) — that's deferred execution.
    N+1: for a list of N parents, code accesses a lazy-loaded navigation property per
    parent (order.Customer.Name inside a loop). Lazy loading fires one SQL query per
    access, so N parents → 1 query for the parents + N queries for the children.
    Fixes: eager loading with Include/ThenInclude so everything comes in 1–2 queries;
    better yet, project only needed columns into a DTO with Select (single query, no
    change-tracking); avoid lazy loading (don't configure it); for large includes use
    split queries or AsSplitQuery when a cartesian-product join would explode.
- id: dotnet-efcore-savechanges-tx-04
  answer: |
    Yes. A single SaveChangesAsync() call is transactional: EF wraps every tracked
    change it detects (all inserts, updates, deletes) in one implicit database
    transaction, so the batch is all-or-nothing — if any statement fails, the whole
    thing rolls back and SaveChanges throws. If you need a transaction spanning
    multiple SaveChanges calls or mixing raw SQL, use
    Database.BeginTransactionAsync/Commit/Rollback explicitly.
    A migration is a versioned, C# code representation of the schema delta between the
    EF model and the database. `dotnet ef migrations add X` (or Add-Migration)
    scaffolds Up/Down operations plus a snapshot of the model; `database update` (or
    Database.Migrate()) applies pending migrations and records them in a migrations
    history table. Migrations keep the database schema in sync with code changes
    reproducibly and are the standard way to evolve schema across environments.
- id: dotnet-gc-generations-loh-01
  answer: |
    The .NET GC is generational: new objects are allocated in gen 0. Gen 0 collections
    are frequent and very cheap (only young objects considered; survivors are promoted
    to gen 1). Gen 1 is a buffer between short-lived and long-lived; collecting it
    also collects gen 0. Gen 2 is the old generation — collecting it (a "full GC") is
    the most expensive. This exploits the empirical fact that most objects die young,
    so most work is done cheaply in gen 0.
    The Large Object Heap holds objects ≥ ~85,000 bytes (big arrays, big strings).
    LOH objects are considered old from the start (effectively gen 2), and by default
    the LOH is swept rather than compacted (compaction is opt-in via
    GCSettings.LargeObjectHeapCompactionMode), which risks fragmentation.
    Large, short-lived allocations are costly because each one immediately adds gen
    2/LOH pressure: they can't be reclaimed by cheap gen 0 collections, trigger
    expensive full collections, and their repeated alloc/free fragments the LOH.
    Hence patterns like ArrayPool for big buffers.
- id: dotnet-gc-dispose-finalizer-02
  answer: |
    Dispose (IDisposable.Dispose) is deterministic cleanup you call explicitly —
    directly or via using / await using. It should release unmanaged handles and any
    managed IDisposable members, and is where you put the "give resources back now"
    logic.
    A finalizer is a safety net run by the GC on a dedicated thread at some
    nondeterministic time before the memory is reclaimed. It delays collection (the
    object survives at least one extra collection) and you can't control when it runs.
    Only add a finalizer when a type owns a raw unmanaged resource directly (rare in
    modern code — prefer SafeHandle). The standard pattern: Dispose does the real
    cleanup and calls GC.SuppressFinalize(this) so the object can be collected in one
    pass; the finalizer repeats the cleanup in case Dispose was never called.
    IAsyncDisposable adds DisposeAsync — for cleanup that must do asynchronous I/O
    (flushing streams, closing network connections, DB calls). Await it with
    `await using`; .NET 8+ IAsyncDisposable.DisposeAsync on containers integrates
    with DI scopes as well.
- id: dotnet-gc-span-arraypool-03
  answer: |
    Span<T>/ReadOnlySpan<T>: a ref-struct, stack-only "view" over arbitrary memory
    (an array, a string, native memory). Slicing a span is O(1) and allocates nothing
    — it avoids the copy-into-a-new-array/substring allocations that dominate hot
    string/array parsing code.
    stackalloc: allocates a small buffer directly on the stack (often into a
    Span<T>), so tiny fixed-size scratch buffers never touch the heap or the GC at
    all (with a size guard and fallback to a pooled array for large sizes).
    ArrayPool<T>: rent a shared, pooled array (ArrayPool<byte>.Shared.Rent) and
    Return it when done. Avoids repeatedly allocating large arrays — especially LOH
    (>85KB) arrays — eliminating both GC pressure and LOH fragmentation in
    high-throughput buffer scenarios (networking, JSON buffers, file I/O).
- id: dotnet-gc-struct-vs-class-04
  answer: |
    A struct is a value type: when it's a local variable it typically lives on the
    stack, and when it's a field it lives inline inside its containing object or
    array. Assignment copies the whole value. No separate heap allocation and no GC
    tracking — until it's boxed or captured.
    A class is a reference type: the instance is always allocated on the managed
    heap and GC-managed; assignment/parameter passing copies only the reference.
    Boxing is the conversion of a value type to a reference type (object or an
    interface it implements): the runtime allocates a heap object and copies the
    struct's value into it; unboxing copies it back out. Boxing is silent and easy to
    trigger — casting an int to object, calling an interface method on a struct, or
    using non-generic collections — and it's a common hidden source of GC pressure in
    hot paths.
- id: dotnet-json-stj-defaults-01
  answer: |
    Set the naming policy on JsonSerializerOptions:
      new JsonSerializerOptions { PropertyNamingPolicy = JsonNamingPolicy.CamelCase }
    (ASP.NET Core's web defaults — JsonSerializerDefaults.Web — already use camelCase
    and case-insensitive matching.) For a single property, override declaratively
    with the attribute on the property:
      [JsonPropertyName("order_id")]
      public int OrderId { get; set; }
    [JsonPropertyName] wins over the naming policy for that property.
- id: dotnet-json-sourcegen-02
  answer: |
    System.Text.Json source generation is a Roslyn source generator that, at compile
    time, emits the serialization metadata for your types (from [JsonSerializable]
    attributes on a partial JsonSerializerContext class): the property metadata,
    getters/setters, naming logic, etc., as ordinary C# code.
    Why it matters: the reflection-based serializer must build that metadata at
    runtime (slow first use) and relies on reflection (and reflection emit), which
    (a) costs startup time and allocations on hot paths and (b) breaks under
    trimming and Native AOT, where the linker may remove types/constructors reflection
    would need and reflection emit is unavailable. With source generation the
    serializer needs no reflection, so it starts faster, works in trimmed/AOT
    deployments, and produces serialization logic visible to the compiler and to
    trimming analysis.
- id: dotnet-json-stj-vs-newtonsoft-03
  answer: |
    System.Text.Json: built into modern .NET, faster and lower-allocation, works
    with source generation for trimming/AOT, async-first APIs
    (JsonSerializer.SerializeAsync / Utf8JsonWriter over UTF-8 bytes). Trade-off: a
    smaller feature set — historically no JObject-style dynamic DOM (JsonNode is
    more limited), fewer built-in converters (e.g. no DataTable), stricter default
    behavior.
    Newtonsoft.Json: very mature and feature-rich — dynamic JObject/JToken LINQ
    queries, extensive converter and contract-resolution ecosystem, many
    third-party integrations, support for legacy targets. Trade-offs: slower and
    more allocation-heavy, string-oriented rather than UTF-8-oriented, no AOT/trim
    story, and it's a separate dependency now in maintenance mode.
    Practical rule: default to System.Text.Json for new code, especially ASP.NET
    Core and AOT; reach for Newtonsoft when you depend on its specific features
    (dynamic DOM, exotic converters) or a large legacy codebase built on it.
- id: dotnet-json-options-reuse-04
  answer: |
    JsonSerializerOptions caches, per instance, the metadata it builds for each type
    it serializes (property getters/setters, converters, naming decisions). Creating
    a new options object per call throws away those caches and forces the metadata
    to be rebuilt — a large hidden cost, plus the new instance eventually churns GC.
    So create one options instance (e.g. a static readonly field, or the app-wide
    one configured in DI), optionally call options.MakeReadOnly(), and reuse it
    everywhere.
    Polymorphism: .NET 7+ supports it declaratively with [JsonPolymorphic] on the
    base type plus [JsonDerivedType(typeof(Derived), "discriminator")] attributes —
    the serializer writes a type discriminator property so deserialization
    reconstructs the right derived type (also configurable in options
    via TypeInfoResolver/model customization for source-gen scenarios). Without it,
    serializing a derived instance via a base-typed property serializes only the
    base shape (STJ does not walk the runtime type for properties by default in
    some paths) and deserializing to the base type fails for unknown payloads.
- id: dotnet-http-socket-exhaustion-01
  answer: |
    Each `new HttpClient()` creates its own underlying handler (SocketsHttpHandler)
    with its own connection pool. Disposing the HttpClient disposes the handler and
    closes its sockets immediately — those sockets then sit in TIME_WAIT on the
    client for a while before the OS reclaims the ephemeral port. Under load (a new
    client per request) you open/close thousands of sockets, run out of ephemeral
    ports, and start seeing SocketException "Address already in use" — socket
    exhaustion. (Conversely, a single long-lived static HttpClient keeps one handler
    forever and never picks up DNS changes.)
    IHttpClientFactory solves it by pooling and reusing HttpMessageHandler instances
    across HttpClient instances: handlers live for a configurable HandlerLifetime
    (default 2 minutes) and are reference-counted so a handler isn't disposed while
    requests are still using it — you get connection reuse (no socket churn) plus
    periodic handler rotation (fresh DNS) without leaks.
- id: dotnet-http-typed-clients-02
  answer: |
    Named clients: services.AddHttpClient("github", c => { c.BaseAddress = ...; })
    registers configuration under a name; consumers call
    IHttpClientFactory.CreateClient("github") and get an HttpClient configured that
    way. Convenient but string-keyed and loose.
    Typed clients: services.AddHttpClient<GitHubService>(...) registers a class that
    receives a pre-configured HttpClient through its constructor (with DI injected
    for the rest). The service encapsulates all the API calls for that upstream.
    Prefer typed clients because the endpoint logic, base address, headers, and
    error handling live in one strongly-typed place; the class is injectable and
    mockable (interface-based), consumers depend on meaningful APIs instead of raw
    HttpClient, and configuration/delegating handlers can be attached per client
    type without magic strings.
- id: dotnet-http-lifetime-dns-03
  answer: |
    With IHttpClientFactory, an HttpClient is cheap but its pooled handler lives for
    HandlerLifetime (default 2 minutes) before being rotated. If you hold/extend the
    handler beyond that (e.g. cache the HttpClient yourself, or set a very long
    HandlerLifetime), the handler's connection pool keeps reusing existing
    connections whose endpoints were resolved when the connections were established
    — so a DNS change (service moved to a new IP, failover) isn't seen and requests
    go stale/dead. Rotating handlers more aggressively is one fix, but too-short
    lifetimes reintroduce the connection churn/socket pressure.
    The SocketsHttpHandler alternative: use SocketsHttpHandler and set
    PooledConnectionLifetime (e.g. 1–5 minutes) — each pooled connection is retired
    after that duration, so new connections re-resolve DNS, while the handler
    instance itself can live for the process lifetime. That lets you safely use a
    single long-lived (even singleton) HttpClient: fresh DNS via connection
    recycling, no handler rotation, no socket exhaustion. (Set
    PooledConnectionIdleTimeout too to trim idle connections.)
- id: dotnet-http-resilience-04
  answer: |
    Modern .NET (8+): the Microsoft.Extensions.Http.Resilience package, built on
    Polly v8 resilience pipelines. Add it to a client:
      builder.Services.AddHttpClient<GitHubService>()
          .AddStandardResilienceHandler();
    The standard handler chains timeout, retry, circuit breaker, and hedging with
    sensible defaults; for custom control use
    .AddResilienceHandler("name", b => b.AddRetry(...).AddTimeout(...).AddCircuitBreaker(...))
    (or .AddResilienceHandler with a Polly ResiliencePipeline). Older pattern:
    Microsoft.Extensions.Http.Polly with .AddPolicyHandler(...) and Polly policies.
    Circuit breaker: after a failure threshold, calls fail fast for a break duration
    instead of hammering a struggling endpoint.
    Cancellation tokens: always pass one through SendAsync/GetAsync. It cooperatively
    cancels the in-flight request, and it composes with timeouts — the resilience
    pipeline applies per-attempt and total timeouts on top of your token, so an
    aborted request (caller gone) or a hung endpoint is terminated promptly. Note
    which failures you retry (transient 5xx, 408, 429; connection failures), respect
    Retry-After, and use exponential backoff with jitter.
- id: dotnet-log-templates-01
  answer: |
    That's structured (semantic) logging: the message template is a constant with
    named placeholders, and the values are captured as named properties alongside
    the message. Sinks can therefore store and query the structured data — "show me
    all orders where OrderId = X" in Seq/Application Insights/ELK — instead of only
    parsing free-form strings. Other benefits: the template is a compile-time
    constant (certain analyzers verify placeholder/argument count and naming);
    string interpolation/concatenation eagerly formats and allocates even when the
    log level is disabled, and destroys the key–value structure so nothing can be
    searched. Message-template syntax (not interpolation) is also what the
    LoggerMessage source generator relies on.
- id: dotnet-log-levels-02
  answer: |
    In increasing severity: Trace, Debug, Information, Warning, Error, Critical
    (with None meaning "log nothing").
    Filtering is by minimum level per category, where the category is the logger's
    name — conventionally the fully qualified type name from
    ILogger<T>/CreateLogger. Configure in code:
      builder.Logging.AddFilter("Microsoft.EntityFrameworkCore", LogLevel.Warning);
    or in appsettings.json:
      "Logging": { "LogLevel": { "Default": "Information",
        "Microsoft.AspNetCore": "Warning",
        "Microsoft.EntityFrameworkCore.Database.Command": "Warning" } }
    The most specific matching category (longest prefix) wins; otherwise the nearest
    prefix and finally Default apply. Quieting EF Core is exactly this: raise the
    minimum level for the "Microsoft.EntityFrameworkCore" categories (e.g. to
    Warning) so per-SQL-command Information logs disappear.
- id: dotnet-log-highperf-03
  answer: |
    [LoggerMessage] is a source generator (Microsoft.Extensions.Logging.Abstractions):
    you write a partial method annotated with the attribute, its message template,
    and level, and the generator produces the implementation at compile time.
      [LoggerMessage(Level = LogLevel.Information,
          Message = "Order {OrderId} shipped")]
      public static partial void OrderShipped(ILogger logger, int orderId);
    Why prefer it on hot paths: the template is parsed once at compile time (not per
    call); the generated method uses the strongly typed parameters directly instead
    of object[] boxing (the classic LogInformation(params object[]) path boxes every
    argument); it guards on logger.IsEnabled(level) so disabled levels do no work and
    allocate nothing; and there is no string interpolation/allocation unless the log
    actually happens. Net effect: near-zero overhead logging suitable for very hot
    code paths.
- id: dotnet-log-scopes-otel-04
  answer: |
    Scopes: ILogger.BeginScope(state) pushes ambient context onto the logger for the
    duration of a block (dispose to pop, typically with `using`). Every log written
    inside that scope carries the scope's key–value state — e.g. BeginScope(new
    Dictionary<string, object> { ["RequestId"] = id }) around request processing — so
    sinks that support scopes (Serilog, Seq, Application Insights) can correlate all
    the log entries belonging to one logical operation, even across method calls,
    without threading an id through every method signature.
    Distributed tracing and metrics: .NET exposes them via System.Diagnostics.
    Tracing: ActivitySource/Activity (evolved from DiagnosticSource) represent spans;
    instrumentation libraries (ASP.NET Core, HttpClient, EF Core, SQL client)
    automatically create activities, and W3C traceparent propagation ties spans
    across service boundaries into one trace. Metrics: System.Diagnostics.Metrics
    (Meter and instruments: Counter, Histogram, Gauge, ObservableGauge) expose
    dimensional metrics. The OpenTelemetry .NET SDK collects these three signals —
    logs (ILogger), traces (activities), metrics — via AddOpenTelemetry() and
    exporters (e.g. OTLP) to backends such as Jaeger, Zipkin, Prometheus, or cloud
    APM.
```
