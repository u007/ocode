# step-5-preview-free — WITH-SKILL validation answers for dotnet (opencode-go, 2026-10-12); closed-book,
# derived/dotnet.step-5-preview-free.SKILL.md body prepended to _prompts/dotnet.md as active guidance.
- id: dotnet-di-lifetimes-01
  answer: |
    - Transient: a brand-new instance every time the service is resolved — every
      constructor injection point and every GetService call. For cheap, stateless
      services.
    - Scoped: one instance per scope. In ASP.NET Core a scope is created per HTTP
      request, so scoped services are effectively per-request; also per explicit
      IServiceScope (e.g. background work).
    - Singleton: one instance for the whole application / root container, shared by
      all consumers. Must be thread-safe and must not hold mutable state that leaks
      between requests.
    Resolution timing: transient is resolved anew on each request; scoped is
    created on first resolution inside a scope and cached for that scope only;
    singleton is created on first resolution on the container (or supplied eagerly
    via AddSingleton(instance)).
- id: dotnet-di-captive-02
  answer: |
    A captive dependency is a longer-lived service that captures a shorter-lived
    dependency — classically a singleton (or a cached factory-created client) that
    takes a scoped service such as a DbContext or IOptionsSnapshot. Injecting a
    scoped service into a singleton is a bug because the scoped instance outlives
    the scope that owns it: a DbContext is not thread-safe, accumulates
    change-tracked entities (unbounded memory growth), and is now shared by
    concurrent requests, which produces InvalidOperationException about a second
    operation on the same context and stale/leaked entities.
    The container does not stop you by default in production. In Development you
    can enable ValidateScopes on the default service provider (throwing when a
    scoped service is resolved from the root provider), and .NET 8 added
    ValidateOnBuild, which checks registrations at build time. Transient into
    singleton is also captive but less dangerous when the consumer is genuinely
    stateless and cheap.
- id: dotnet-di-scope-in-singleton-03
  answer: |
    Do not inject the scoped service (or the DbContext) directly into the
    BackgroundService constructor. Inject IServiceScopeFactory and create a fresh
    scope per iteration:

      using var scope = _scopeFactory.CreateScope();   // CreateAsyncScope() for async
      var db = scope.ServiceProvider.GetRequiredService<AppDbContext>();
      await db.WorkAsync(_stoppingToken);

    Dispose the scope (and therefore the DbContext) at the end of each iteration.
    The singleton itself is registered in the root container, so the scoped service
    has to be resolved from a child scope the singleton owns. Never cache the
    scoped instance on the singleton — that recreates the captive dependency inside
    your own scope.
- id: dotnet-di-keyed-04
  answer: |
    Keyed services, added in .NET 8. Registration keys the service with a string or
    any object usable as a map key:

      builder.Services.AddKeyedSingleton<INotifier, EmailNotifier>("email");
      builder.Services.AddKeyedSingleton<INotifier, SmsNotifier>("sms");

    Consumption options:
      - Constructor injection with [FromKeyedServices("sms")] INotifier sms
      - provider.GetRequiredKeyedService<INotifier>("email") /
        GetKeyedServices<INotifier>("x")
      - Minimal API parameter injection: handler([FromKeyedServices("sms")]
        INotifier n)

    All lifetimes have keyed variants (AddKeyedScoped / Transient / Singleton).
    KeyedService.AnyKey is the sentinel for "any key" when you need open
    resolution. This replaced hand-rolled dictionary-of-implementations or
    factory-function registrations.
- id: dotnet-config-precedence-01
  answer: |
    Sources are layered in registration order and the LAST source registered wins
    for a duplicate key. The typical host (WebApplication.CreateBuilder / generic
    host) registers, in order: appsettings.json, appsettings.{Environment}.json,
    User Secrets (Development only), Environment Variables, Command-line args.
    So the effective precedence, highest first, is: command-line args >
    environment variables > user secrets > appsettings.{Environment}.json >
    appsettings.json.
    Notes: environment variables use "__" (or ":" on some hosts) for nesting;
    JSON files are set to reloadOnChange; command-line args are added last so they
    override everything. IConfiguration is ordered by insertion, so an
    additional AddJsonFile appended after the defaults overrides them.
- id: dotnet-config-options-interfaces-02
  answer: |
    - IOptions<T>: registered as a singleton; the value is computed once on first
      resolution and cached for the app lifetime; it never sees config reloads.
      Safe to inject into singletons.
    - IOptionsSnapshot<T>: registered as scoped; re-read and re-bound for every
      scope (i.e. per request); sees reloads; must not be injected into a singleton
      (scoped-from-root throws / is blocked by scope validation).
    - IOptionsMonitor<T>: registered as a singleton; .CurrentValue is always the
      latest config, so it sees reloads; supports OnChange callbacks so you can
      react without polling.
    Rule of thumb: IOptions for immutable config, IOptionsSnapshot for fresh
    per-request options, IOptionsMonitor when you need change notifications.
- id: dotnet-config-secrets-03
  answer: |
    Local development: .NET user secrets — `dotnet user-secrets set "Key" "value"`
    from the project directory. Stored OUTSIDE the repo, so it cannot be
    committed: %APPDATA%\Microsoft\UserSecrets\<UserSecretsId>\secrets.json on
    Windows, ~/.microsoft/usersecrets/<UserSecretsId>/secrets.json on macOS/Linux.
    The id is set via a UserSecretsId property in the csproj (or the
    UserSecretsId assembly attribute). With ASP.NET Core's defaults they are
    registered automatically when the environment is Development.
    Production: the user-secrets mechanism is dev-only and is NOT encrypted, so it
    must not be used in production. Use environment variables or a real secret
    store (Azure Key Vault, AWS Secrets Manager, Kubernetes secrets) fetched with
    a managed identity, plus key rotation.
- id: dotnet-config-options-binding-04
  answer: |
    Bind a section to a class:

      builder.Services.Configure<MyOptions>(
          builder.Configuration.GetSection("MyOptions"));

    or AddOptions<T>().Bind(section) to keep the builder for further calls.
    Property matching is case-insensitive by default; nested objects and
    collections bind recursively.
    Fail fast on invalid options:
      builder.Services.AddOptions<MyOptions>()
        .Bind(cfg.GetSection("MyOptions"))
        .ValidateDataAnnotations()                               // [Required], [Range]
        .Validate(o => o.RetryCount > 0, "RetryCount must be greater than zero")
        .ValidateOnStart();

    Registering Validate on the OptionsBuilder (or an IValidateOptions<T> service)
    defines the checks, and ValidateOnStart (.NET 8) forces them to run during host
    startup, throwing OptionsValidationException instead of surfacing a null or bad
    value on the first request. Without ValidateOnStart, validation happens lazily
    on first resolution of IOptions<T>.
- id: dotnet-pipeline-order-01
  answer: |
    Middleware is a nested chain: each component receives the HttpContext plus a
    next delegate, does work on the way in in registration order, then calls the
    next component, and unwinds in reverse order on the way out. That is why order
    is everything — an outer component can observe, mutate, or suppress everything
    nested inside it.
    Canonical order: exception handler first (so it catches everything), then
    static files, then UseRouting, then authentication, then authorization, then
    endpoints (MapControllers / Run).
    UseRouting matches the request to an endpoint and stamps the endpoint (and its
    metadata, including authorization policies) onto HttpContext. UseAuthentication
    runs after routing so any endpoint-aware authentication has metadata available,
    and UseAuthorization must run after both — it evaluates policies using the
    endpoint metadata that routing selected, and it must execute BEFORE the endpoint
    runs; if it sat after the endpoint, the endpoint would already have executed.
    Authorization itself never establishes identity, so putting it before
    authentication means there is no ClaimsPrincipal to check.
- id: dotnet-pipeline-use-vs-map-02
  answer: |
    - app.Use(delegate): registers a middleware component into the chain. The
      delegate takes (HttpContext, RequestDelegate next) and may or may not call
      next.
    - app.Run(delegate): a terminal middleware — it only receives HttpContext,
      has no next, and always ends the pipeline.
    - app.Map(path, sub): branches the pipeline on a path prefix; the sub-pipeline
      built with Use/Run inside the lambda runs when the path matches. MapWhen /
      UseWhen branch on an arbitrary predicate; UseWhen rejoins the main pipeline
      if the branch's middleware calls next.
    Short-circuiting means a middleware stops calling next and writes the response
    itself, so the remaining components never run — e.g. static-file middleware
    serving a file, authentication issuing a 401, or HTTPS redirection. Run
    short-circuits by definition because there is nothing after it.
- id: dotnet-pipeline-minimal-results-03
  answer: |
    A minimal API handler's return value is matched to a result executor by the
    generated endpoint code. If the value implements IResult (Results.Ok,
    TypedResults.NoContent, Results.File, Results.BadRequest, etc.) the framework
    just invokes it, and it writes its own status code, headers, and body.
    Anything else — a POCO or a record — is serialized: the framework writes it as
    JSON with 200 OK (using the configured HttpJsonOptions), and a string is
    written as text/plain.
    Returning an object gives you no control over status code, headers, or content
    type. IResult implementations do, and they compose (combining multiple
    responses, validation errors with proper ProblemDetails). TypedResults returns
    statically typed result objects — e.g. Ok<T> implements IValueHttpResult<T> and
    exposes .Value — so unit tests can assert the payload without reflection, and
    overload resolution/compile-time type checking is clearer and avoids boxing.
- id: dotnet-pipeline-filters-vs-middleware-04
  answer: |
    Use middleware for cross-cutting concerns that apply to every request and that
    do not need to know which endpoint will run: exception handling, logging,
    compression, CORS, HTTPS redirection, static files.
    Use a filter (endpoint filter in minimal APIs, action filter in MVC) when the
    logic is per-handler or needs endpoint context: route-handler arguments,
    endpoint metadata, model binding, and DI. Endpoint filters intercept the
    endpoint delegate and can short-circuit by returning IResult, inspect or mutate
    method arguments, and run code before and after the handler. MVC filters run
    their stages in a documented order (authorization, action, result,
    exception) and can wrap execution, modify action arguments, or change results.
    What a filter can do that middleware cannot: see and branch on endpoint
    metadata, participate in model binding/validation, return an
    OpenAPI-annotated IResult, and wrap a single endpoint rather than the whole app.
- id: dotnet-efcore-context-lifetime-01
  answer: |
    A DbContext must be scoped — AddDbContext registers it scoped by default, so it
    is one instance per web request (per unit of work), resolved, used, and disposed
    with the scope. Never register it as a singleton, and never inject it into a
    singleton consumer.
    It is not thread-safe: it holds an identity map and a change tracker with
    per-entity snapshots plus a single DbConnection, and concurrent use corrupts
    that state — you get InvalidOperationException for a second operation started
    on the same context. Consequently you must not run multiple queries against
    the same instance concurrently; await them sequentially instead of firing N
    Task.WhenAll calls on one context, and use a separate context per thread or per
    parallel task.
- id: dotnet-efcore-notracking-02
  answer: |
    The change tracker is EF Core's identity map plus state store. When entities are
    materialized, EF tracks each one by primary key in its original value; it
    records state (Unchanged/Added/Modified/Deleted) and, for updates, compares
    current values against the snapshot during DetectChanges at SaveChanges to
    generate UPDATE statements.
    Add AsNoTracking() for read-only queries so EF does not snapshot or track the
    results. It reduces CPU and memory and speeds up large result sets; the
    returned entities are detached and cannot be Saved back. Use it for read-only
    DTO/paging/caching/list screens. If you need to update, keep tracking; if you
    need identity-map deduplication but no tracking, use AsNoTrackingWithIdentity
    Resolution. You can also set context.ChangeTracker.QueryTrackingBehavior =
    QueryTrackingBehavior.NoTracking globally.
- id: dotnet-efcore-nplus1-03
  answer: |
    Deferred execution: an IQueryable is only a description — it builds an
    expression tree; no SQL is sent until the query is enumerated by ToList(),
    ToArray, First/Single, Count/Any, foreach, or await ToListAsync(). Once
    produced, the query runs again on a fresh enumeration.
    N+1 arises when you load a parent list with one query and then load a related
    collection per item — via lazy loading or by looping over the parents and
    querying per row — giving 1 + N round trips and terrible latency under load.
    Fix it by: eager loading with Include/ThenInclude (a JOIN, or a split query),
    or projecting directly to a DTO with Select so the whole shape is fetched in
    one statement, or explicitly batching the child set and grouping in memory
    (e.g. ToLookup) instead of querying per row.
- id: dotnet-efcore-savechanges-tx-04
  answer: |
    Yes — a single SaveChanges/SaveChangesAsync call is transactional by default:
    EF Core starts a transaction on the underlying connection, writes all the
    inserts/updates/deletes the change tracker detected, and commits; if any
    statement fails, the whole batch is rolled back. Caveat: it is one transaction
    per call, so atomicity across multiple SaveChanges calls, or over non-EF work,
    needs an explicit transaction — context.Database.BeginTransactionAsync() /
    an IDbContextTransaction, or an execution strategy.
    A migration's role is schema evolution, not runtime queries: it is a
    code-based, versioned snapshot of the model with Up/Down methods
    (migrationBuilder.AlterColumn, CreateTable, etc.) created by
    `dotnet ef migrations add X` and applied with `dotnet ef database update`.
    Applied migrations are recorded in __EFMigrationsHistory; migrations bring the
    database to match the model and are distinct from data seeding (HasData or
    custom HasSql).
- id: dotnet-gc-generations-loh-01
  answer: |
    Generational: gen 0 holds new short-lived objects — collected often, cheaply,
    with cost proportional to the live set; gen 1 is a buffer for objects that
    survive a gen 0 collection; gen 2 is the long-lived heap, collected
    infrequently as a full/blocking GC (with a background GC doing the marking
    concurrently). Surviving objects are promoted gen 0 → 1 → 2, so anything that
    lives long enough to reach gen 2 is collected only during expensive gen 2
    passes.
    The Large Object Heap holds objects at or above 85,000 bytes (e.g. large
    arrays, big strings) and is collected/gen2 with the rest, historically not
    compacted by default.
    Large, short-lived allocations are costly because they skip gen 0/1 entirely
    and land in gen 2 / LOH: the heap grows fast, collection cost scales with the
    live set, and you force frequent full gen 2 collections plus LOH fragmentation,
    even though the data dies immediately. Mitigate by pooling (ArrayPool) or
    processing in chunks.
- id: dotnet-gc-dispose-finalizer-02
  answer: |
    Dispose (IDisposable): deterministic, explicit cleanup — you (or `using`)
    decide when it runs. It is where unmanaged handles, streams, event
    subscriptions, and other IDisposable children are released. Finalizers are
    also suppressed with GC.SuppressFinalize(this) so cleanup never runs twice.
    A finalizer (~Type): a non-deterministic safety net run by the GC before
    reclaiming the object. It cannot be relied on for timing, delays reclamation
    by an extra GC cycle, resurrects the object for the finalizer thread, and is
    only needed when your type holds unmanaged resources directly.
    IAsyncDisposable adds DisposeAsync() returning a ValueTask, for cleanup that
    is itself asynchronous (flushing/closing async I/O, async-safe handles),
    consumed via `await using`. It fits the async pipeline and avoids blocking.
    Typical pattern: implement both when you own unmanaged resources, with
    DisposeAsync as the primary path.
- id: dotnet-gc-span-arraypool-03
  answer: |
    - Span<T> / ReadOnlySpan<T>: a type-safe contiguous view over any memory
      (arrays, native memory, stackalloc). Slicing, substringing, and parsing
      become allocation-free views instead of copies; being stack-only, it cannot
      be boxed or captured on the heap.
    - stackalloc: allocates directly on the call stack and is reclaimed when the
      method returns — zero GC pressure, no locks — for small, fixed-size,
      per-call buffers: `Span<byte> buf = stackalloc byte[256];`.
    - ArrayPool<T>.Shared: rent a shared buffer and return it
      (pool.Return(arr)) to reuse a large array across calls instead of allocating
      and zeroing a fresh one each time, which is what avoids the GC churn (and
      LOH pressure) of big temporary buffers.
    Each removes a heap allocation: Span avoids copying, stackalloc avoids the heap,
    ArrayPool reuses it.
- id: dotnet-gc-struct-vs-class-04
  answer: |
    From an allocation standpoint: a struct is a value type — allocated inline (a
    local lives on the stack, a field is embedded in its containing object), so it
    costs no separate heap allocation and no GC work, but assignment COPIES the
    value. A class is a reference type — the object lives on the heap and the
    variable holds a reference, so assignment copies the reference (shared state),
    and every object is GC-tracked with a header/method-table pointer.
    Boxing is converting a value type to object or to an interface it implements
    (or to dynamic): the value is copied into a heap object and references now
    point at that box. It costs an allocation, changes semantics (the boxed copy
    is independent — mutating the struct afterwards does not change the box), and
    non-generic collections like ArrayList box every element.
- id: dotnet-json-stj-defaults-01
  answer: |
    camelCase globally with the naming policy:

      var opts = new JsonSerializerOptions {
          PropertyNamingPolicy = JsonNamingPolicy.CamelCase
      };

    In ASP.NET Core, configure the framework's options:
      builder.Services.ConfigureHttpJsonOptions(o =>
          o.SerializerOptions.PropertyNamingPolicy = JsonNamingPolicy.CamelCase);
    Override a single property with the attribute, which beats the policy:
      [JsonPropertyName("serialized_name_here")]
    Related: [JsonPropertyOrder] for ordering, PropertyNameCaseInsensitive =
    true for tolerant deserialization, and DictionaryKeyPolicy for dictionary keys.
- id: dotnet-json-sourcegen-02
  answer: |
    Source generation runs at compile time: you declare a partial JsonSerializer
    Context with [JsonSerializable(typeof(T))] attributes and point
    JsonSerializerOptions.TypeInfoResolver at TContext.Default. The generator
    emits C# that hard-codes the property names, getters, setters, converters, and
    serialization shape for those types, so no runtime reflection is used.
    It matters for performance (no per-type metadata building via reflection at
    startup, faster serialization, lower memory) and, critically, for trimming and
    Native AOT: under trimming the reflection-based resolver can lose required
    members at publish time, whereas source gen keeps everything statically known
    and AOT/trim-compatible. You also get compile-time validation of the
    serializable surface via JsonSourceGenerationOptions.
- id: dotnet-json-stj-vs-newtonsoft-03
  answer: |
    System.Text.Json trade-offs: it is built into modern .NET, faster, allocation
    light, supports source generation, trimming and Native AOT, and is the default
    in ASP.NET Core. Its costs are a stricter, less tolerant parser and fewer
    customization hooks than Newtonsoft.
    Newtonsoft.Json trade-offs: far more mature and flexible — rich contract
    resolvers, extensive attributes and converters, arbitrary JSON manipulation
    with JObject/JArray and LINQ-to-JSON, tolerant handling of malformed or
    unusual payloads, and established polymorphism via TypeNameHandling ($type).
    Its costs are slower serialization, much higher allocations, and reliance on
    runtime reflection, which breaks trimming and Native AOT.
    Practical choice: STJ for all new work and for AOT/trim; Newtonsoft where a
    legacy or unusual serialization need cannot be expressed by STJ.
- id: dotnet-json-options-reuse-04
  answer: |
    A JsonSerializerOptions instance builds and caches serialization metadata
    (property names, converters, accessors) on first use and reuses it on
    subsequent calls, so a shared static/singleton instance gets fast
    steady-state behavior; constructing new options per call discards that cache
    and pays reflection/metadata cost plus allocations every time. The options
    object is also not thread-safe to mutate after it has been used for
    serialization, so the contract is "create once, configure, then treat as
    immutable and share".
    Polymorphism: by default only the declared/base type's properties are
    serialized. From .NET 8 the recommended approach is [JsonDerivedType] on the
    base type with a type discriminator: e.g. [JsonDerivedType(typeof(Dog), "dog")]
    on Animal, which writes and reads the discriminator; older workarounds are a
    custom JsonConverter that switches on the runtime type. (For reads, declaring
    the variable/parameter as the base type still shapes deserialization.)
- id: dotnet-http-socket-exhaustion-01
  answer: |
    Three stages, in order, and each flaw:
    (a) `new HttpClient()` per request: every client gets its own
    HttpMessageHandler and connection pool, and disposing the client closes the
    sockets — which the OS keeps in TIME_WAIT (MSL-dependent, minutes) before the
    ephemeral port can be reused. Under load you run out of ports.
    (b) One long-lived `static HttpClient`: exhausts nothing, because the
    connections stay open — but those pooled connections are never re-created, so
    the hostname is never re-resolved and it keeps talking to the old IP after a
    deployment or failover. THAT is the flaw the factory's rotation exists to fix.
    (c) IHttpClientFactory: pools HttpMessageHandlers and hands out cheap clients
    built on them (no exhaustion, no per-instance pool), AND rotates the handlers
    on HandlerLifetime (default 2 minutes), so pooled connections are recycled
    periodically and DNS changes are picked up — fixing both (a) and (b).
    Use it via AddHttpClient(...) and inject IHttpClientFactory or a typed client.
- id: dotnet-http-typed-clients-02
  answer: |
    Named clients: registered with a string key,
    `services.AddHttpClient("catalog", c => c.BaseAddress = ...)`, and created at
    use time with `factory.CreateClient("catalog")`. Configuration and policies
    live under that name.
    Typed clients: a strongly typed wrapper class that takes an HttpClient in its
    constructor and is registered as transient by the factory —
    `services.AddHttpClient<CatalogClient>(c => c.BaseAddress = ...)` — so the
    wrapper is injected directly.
    Prefer a typed client because it encapsulates the API surface (base address,
    endpoints, headers, deserialization) behind a domain-friendly class, has no
    magic strings, is naturally unit-testable (the constructor HttpClient can be
    given a fake HttpMessageHandler), takes per-client resilience configuration,
    and gives compile-time safety and better single-responsibility separation than
    sprinkling CreateClient("...") calls around.
- id: dotnet-http-lifetime-dns-03
  answer: |
    An HttpClient keeps the HttpMessageHandler it was constructed with for its
    entire life, and factory rotation only affects clients created AFTER the
    rotation. So a factory client (or a transient typed client) that is cached in a
    singleton, a static field, or a long-lived background service pins its original
    handler and never sees a new one — it serves stale DNS exactly like the static
    client. Say it plainly: holding onto a factory client longer than a request
    defeats the rotation that is the entire point of the factory.
    The modern alternative is a primary SocketsHttpHandler configured with
    PooledConnectionLifetime (e.g. 2 minutes), so the handler itself retires and
    re-creates its pooled connections, re-resolving DNS — a long-lived
    client/handler is then safe to cache. You can disable factory rotation with
    SetHandlerLifetime(Timeout.InfiniteTimeSpan) and let the handler do the work.
    These are two different settings: HandlerLifetime / SetHandlerLifetime govern
    how long the factory reuses a handler; SocketsHttpHandler.PooledConnection
    Lifetime governs connection recycling inside a single handler. Name the one you
    are configuring.
- id: dotnet-http-resilience-04
  answer: |
    Modern .NET 8: package Microsoft.Extensions.Http.Resilience (Polly v8):

      builder.Services.AddHttpClient("api")
          .AddStandardResilienceHandler();   // default retry + timeouts + breaker

    For custom pipelines:

      builder.Services.AddHttpClient("api")
          .AddResilienceHandler("custom", b => b
              .AddRetry(new HttpRetryStrategyOptions { ... })
              .AddTimeout(...)
              .AddCircuitBreaker(new HttpCircuitBreakerStrategyOptions { ... }));

    (There is no AddStandardHandler() — the real names are AddStandardResilience
    Handler and AddResilienceHandler.) Legacy Polly (Microsoft.Extensions.Http.
    Polly) used AddTransientHttpErrorPolicy(...) and AddPolicyHandler(...) with
    HttpPolicyExtensions.HandleTransientHttpError() — not Handle<HttpRequestExcept
    ion>() on HttpPolicyExtensions.
    Cancellation: always flow a CancellationToken into SendAsync/GetAsync (e.g.
    HttpContext.RequestAborted, or a linked token from a CancellationTokenSource
    with a timeout). The resilience pipeline enforces attempt and total timeouts by
    linking tokens, so an elapsed timeout surfaces as OperationCanceledException /
    TaskCanceledException — not TimeoutException — and the circuit breaker
    transitions to Open after consecutive failures, then Half-Open to probe.
- id: dotnet-log-templates-01
  answer: |
    Structured logging: the placeholders in the template become named properties on
    the log event, so consumers (Seq, Elasticsearch, Application Insights, or a
    database sink) can index and query them — "OrderId = 42", count by OrderId,
    correlate with the trace — instead of having one opaque message string.
    Interpolation splats the value into the message and loses the structured
    field forever, so you are left with brittle substring matching. A second
    benefit: the template is a constant string, so message formatting is deferred
    until a sink actually writes the event, and the template is reused once instead
    of being re-parsed for every distinct interpolated string.
- id: dotnet-log-levels-02
  answer: |
    In increasing severity: Trace < Debug < Information < Warning < Error <
    Critical (plus None, to switch logging off).
    Per-category filtering is configuration-driven, layered exactly like any other
    config: appsettings.json sets Logging:LogLevel:Default (e.g. Information) and a
    specific category overrides it, e.g.

      "Logging": { "LogLevel": {
        "Default": "Information",
        "Microsoft.AspNetCore": "Warning",
        "Microsoft.EntityFrameworkCore": "Warning" } }

    appsettings.{Environment}.json / environment variables / command-line args
    override those. Code-side equivalents: builder.Logging.SetMinimumLevel(...) and
    builder.Logging.AddFilter("Microsoft.EntityFrameworkCore", LogLevel.Warning)
    (or AddFilter<T>). Category matching is by longest prefix against the name
    passed to CreateLogger, typically the fully qualified type name, so
    "Microsoft.EntityFrameworkCore" quiets EF's queries while leaving your own logs
    untouched. Filters can also target a provider.
- id: dotnet-log-highperf-03
  answer: |
    The LoggerMessage source generator (Microsoft.Extensions.Logging source
    generators, .NET 6+) is used by declaring a partial method in a partial class
    with the [LoggerMessage] attribute:

      [LoggerMessage(EventId = 1, Level = LogLevel.Information,
          Message = "Order {OrderId} shipped")]
      partial void OrderShipped(ILogger logger, int orderId);

    The generator emits the implementation, which uses a pre-created,
    generically-typed LoggerMessage.Define delegate (LoggerMessage.Define<int>)
    cached in a static field. On hot paths this is preferred over calling
    logger.LogInformation directly because the default path re-parses the message
    template on every call, boxes value-type arguments, and allocates; the
    generated code does none of that. It also validates the template against the
    method's parameters at compile time, so a typo in the placeholder or argument
    count is a build error.
- id: dotnet-log-scopes-otel-04
  answer: |
    BeginScope attaches ambient key/value state to every log written inside the
    using block, so the same correlation id, request id, or tenant does not have
    to be repeated in each message:

      using (logger.BeginScope("Order {OrderId}", id)) { ... }

    It flows through async call chains and is attached to the event by providers
    that support scopes (the console and EF providers honour it by default).
    Distributed tracing and metrics go through the built-in System.Diagnostics
    abstractions that OpenTelemetry subscribes to: tracing via ActivitySource /
    Activity (ASP.NET Core hosting creates an Activity per request, W3C
    traceparent headers propagate the trace context across services, and libraries
    create their own activities); metrics via Meter with instruments — Counter<T>,
    Histogram<T>, UpDownCounter<T>, ObservableGauge<T>. The OpenTelemetry .NET SDK
    (AddOpenTelemetry().WithTracing(...).WithMetrics(...)) listens to
    ActivityListener and MeterListener and exports OTLP spans and metrics (also
    Jaeger/Zipkin/Prometheus with their exporters), so logs, traces, and metrics
    correlate through the shared trace id.
