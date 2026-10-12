# step-5-preview-free — closed-book answers for dotnet (opencode-go, 2026-10-12); answerer saw only _prompts/dotnet.md
Closed-book answers drawn from my own knowledge — no corpus files, no repository access, no web lookups, one answer per question.

# cleanup: dropped a second `dotnet-di-lifetimes-01` record the model emitted at the end ("Duplicate check — answered as above"); first record kept.
- id: dotnet-di-lifetimes-01
  answer: |
    Transient: a new instance is created every time the service is requested,
    whether that request comes from the container or from another service —
    once per injection point per resolution. Scoped: one instance per DI scope,
    and in ASP.NET Core a scope is created per HTTP request, so a scoped service
    lives for the duration of that request and is disposed when the request ends.
    Singleton: exactly one instance for the lifetime of the root container; the
    first resolution creates it, every subsequent request (from any scope) gets
    the same instance, and it is disposed when the container itself is disposed.
    Practical mapping: stateless cheap services -> transient; per-request state
    (EF Core DbContext) -> scoped; thread-safe, genuinely global state or
    infrastructure (memory cache, HttpClient wrappers) -> singleton.

- id: dotnet-di-captive-02
  answer: |
    A captive dependency is a service whose lifetime is longer than that of a
    service that "captures" it by injection. Injecting a scoped service into a
    singleton is a bug because the scoped instance is created once (from the root
    scope) and then lives as long as the singleton — the whole application
    lifetime. Concretely with a DbContext: the context becomes effectively
    singleton with an open connection, it keeps accumulating tracked entities
    across requests (memory growth), it is not thread-safe so concurrent
    requests corrupt its state, and it never gets disposed per request, so its
    change tracker and connection leak. The scope it was scoped to ends, but the
    singleton keeps holding the reference. ASP.NET Core has "scope validation"
    (on by default in Development) that throws when it detects scoped services
    being resolved from the root provider.

- id: dotnet-di-scope-in-singleton-03
  answer: |
    Inject IServiceScopeFactory into the singleton service. On each iteration
    (or per work item), create a scope, resolve the scoped service from that
    scope's provider, use it, and dispose the scope:

        using var scope = _scopeFactory.CreateScope();
        var db = scope.ServiceProvider.GetRequiredService<AppDbContext>();
        // use db

    Do not inject the DbContext directly into the BackgroundService constructor
    — that is the captive dependency bug, since the service is registered
    singleton. Do not cache the DbContext in a field: the whole point is a fresh
    instance per unit of work. An equivalent pattern is injecting
    IServiceProvider and calling CreateScope() on it, but IServiceScopeFactory
    is the idiomatic choice.

- id: dotnet-di-keyed-04
  answer: |
    Keyed dependency injection, introduced in .NET 8. Register with
    services.AddKeyedSingleton<IThing, ThingA>("a") (also AddKeyedScoped /
    AddKeyedTransient). Consume it via [FromKeyedServices("a")] on a minimal API
    parameter or controller action parameter, or imperatively with
    provider.GetRequiredKeyedService<IThing>("a") (or GetKeyedService which
    returns null instead of throwing). Keys are arbitrary objects used as a
    dictionary lookup; strings are the common case for named implementations.

- id: dotnet-config-precedence-01
  answer: |
    Providers are applied in registration order and the LAST registered provider
    wins when the same key exists in multiple sources. The default host order is:
    appsettings.json (optional) -> appsettings.{Environment}.json (optional) ->
    User Secrets (only when the environment is Development) -> environment
    variables -> command-line arguments. So command-line args beat env vars,
    env vars beat user secrets, which beat the environment-specific JSON, which
    beats the base appsettings.json. Note environment variables also see a
    flattened, colon-replaced form like ConnectionStrings__Default, and command
    line args like --ConnectionStrings:Default=... follow the same key paths.
    "Last one wins" — not "most specific file wins".

- id: dotnet-config-options-interfaces-02
  answer: |
    IOptions<T>: singleton lifetime, the options instance is created and cached
    the first time it is read and never reevaluated, so it does NOT see config
    reloads. Safe to inject into anything, including singletons.
    IOptionsSnapshot<T>: scoped lifetime, a fresh instance is computed per
    request/scope, so it DOES reflect config changes (reloaded files, env
    changes that the provider can observe). It cannot be injected into a
    singleton because that would be a scoped-into-singleton captive dependency.
    IOptionsMonitor<T>: singleton lifetime, injectable anywhere, and always
    returns current values through .CurrentValue, so it sees reloads; it also
    exposes OnChange(...) callbacks that fire when the underlying config changes.
    Validation via IValidateOptions<T> runs at construction for all three.

- id: dotnet-config-secrets-03
  answer: |
    Locally during development, secrets belong in the .NET "User Secrets"
    store, managed with the dotnet user-secrets CLI
    (dotnet user-secrets set "ConnectionStrings:Default" "..."). It is a JSON
    file kept OUTSIDE the repo, in the user profile
    (%APPDATA%/Microsoft/UserSecrets/<UserSecretsId>/secrets.json on Windows,
    ~/.microsoft/usersecrets/<UserSecretsId>/secrets.json on macOS/Linux), keyed
    by a UserSecretsId generated in the .csproj. It is not encrypted — it is
    protected by the OS user account, and its security boundary is "my dev
    machine". In production, secrets must not be in source or in that file:
    use environment variables, or better a dedicated secret manager such as
    Azure Key Vault, AWS Secrets Manager, or HashiCorp Vault, typically accessed
    via managed identity / workload identity rather than a stored credential.
    Anything secret checked into source control is a leak.

- id: dotnet-config-options-binding-04
  answer: |
    Define a POCO options class whose property names match the configuration
    keys, then bind a section with
    builder.Services.Configure<MyOptions>(builder.Configuration.GetSection("MyOptions")).
    A common modern form is
    builder.Services.AddOptions<MyOptions>().Bind(section).ValidateDataAnnotations().ValidateOnStart(),
    which validates eagerly at startup instead of on first use. Fail-fast is
    achieved via validation: DataAnnotations attributes ([Required],
    [Range], etc.) checked with ValidateDataAnnotations(), or a custom
    Validate(IValidateOptions<T> implementation returning a list of failures.
    With ValidateOnStart() the host fails to start with an
    OptionsValidationException if validation fails; without it the failure still
    surfaces on first resolution, which is much worse.

- id: dotnet-pipeline-order-01
  answer: |
    The pipeline is a chain of delegates built in registration order; a request
    flows DOWN the chain (each middleware runs its code, then awaits next(),
    so any code after the next() call runs on the way BACK OUT, giving the
    nested ordering of response processing). Ordering matters because each
    middleware depends on work done by earlier ones. UseAuthentication must come
    after UseRouting because authorization policies come from endpoint metadata
    set during routing, and before UseAuthorization because authorization needs
    an authenticated identity (ClaimsPrincipal) to evaluate. Authentication must
    come before the endpoints run. The canonical order is exception handler /
    developer exception page, HSTS (prod), HTTPS redirection, static files,
    routing, CORS, authentication, authorization, then endpoint execution via
    UseEndpoints/MapControllers. Putting UseAuthorization before UseRouting in
    the endpoint-routing model means authorization checks run without
    knowing the endpoint — historically this caused the authorization
    middleware to throw or fail to apply the right policy. A middleware that
    does not call next() short-circuits the rest of the pipeline.

- id: dotnet-pipeline-use-vs-map-02
  answer: |
    Use(app.Use(async (ctx, next) => { ...; await next(); ... })) registers a
    middleware that may continue the chain via the RequestDelegate next. Run
    registers terminal middleware (app.Run(async ctx => ...)) — it receives no
    next and is intended for the end of the pipeline; anything after it is dead
    code. Map("/path", branch => ...) splits the pipeline by request path:
    matching requests go into the branched configuration, non-matching requests
    fall through to the rest of the main pipeline (MapWhen branches on an
    arbitrary predicate). Short-circuiting means a middleware produces a
    response and never invokes next(), so no downstream middleware runs for that
    request — for example a 401 response from UseAuthentication short-circuits
    routing, the endpoints, and everything after.

- id: dotnet-pipeline-minimal-results-03
  answer: |
    If a minimal API handler returns IResult (or Task<IResult>), the framework
    calls IResult.ExecuteAsync(HttpContext), and the result implementation
    writes the status code, headers, and body. The Results.* / TypedResults.*
    helper classes factory such implementations. Returning a plain object
    (e.g. a POCO or string) is also supported — the framework serializes an
    object as JSON with a 200 OK, and a string as text/plain 200. TypedResults
    is preferred over Results because its generic overloads (TypedResults.Ok<T>,
    TypedResults.Created<T>, ...) carry the payload type, giving compile-time
    clarity, better OpenAPI metadata generation (they implement
    IEndpointMetadataProvider), and unit-testability without an
    HttpContext — you can inspect the returned Ok<T> / NotFound / etc. It also
    avoids accidental 200s with a serialized-null body: returning an IResult
    forces you to decide on a status code explicitly.

- id: dotnet-pipeline-filters-vs-middleware-04
  answer: |
    Middleware is application-wide, cross-cutting, and coarse — it runs for
    every request including ones that never match an endpoint (404s), it sees
    the raw HttpContext before routing/metadata exist, and it should handle
    concerns like exception handling, HTTPS redirection, static files,
    compression, or authentication. An endpoint filter (minimal APIs, .NET 7+)
    or an MVC action filter is opt-in per endpoint (or registered globally but
    still invoked from the endpoint pipeline), and what it can do that
    middleware cannot is see and interact with the endpoint itself: the handler
    method, its arguments, and its metadata. Endpoint filters run in the DI
    scope shared with the endpoint, so they use the same scoped services, and
    they can validate/replace arguments, short-circuit by returning a result
    (e.g. Results.ValidationProblem), and wrap the handler invocation with
    before/after logic. Rule of thumb: request-level concerns before the
    endpoint is chosen -> middleware; endpoint-level concerns tied to specific
    endpoints, arguments, or metadata -> filter.

- id: dotnet-efcore-context-lifetime-01
  answer: |
    A DbContext must be scoped — one instance per unit of work, which in
    ASP.NET Core means one per web request (AddDbContext registers it as scoped
    by default). It is not safe to share across threads or concurrent async
    operations because a DbContext is a unit-of-work + change-tracking session
    with a single DbConnection and mutable internal state; it has no locking
    around that state. Two concurrent operations on the same instance throw
    InvalidOperationException ("A second operation was started on this context
    instance before a previous operation completed"), and parallel use of the
    tracked entity graph can interleave change tracking and produce corrupt
    state. The right patterns are: one context per request via DI, or one
    context per thread/operation when doing parallelism, never a static or
    singleton context, and awaiting each operation sequentially within one
    unit of work.

- id: dotnet-efcore-notracking-02
  answer: |
    The change tracker is EF Core's identity map plus snapshot store: as
    entities are materialized they are tracked in an Unchanged (or Added)
    state with their original values remembered, so when SaveChanges runs EF
    can diff current values against originals and generate the right
    INSERT/UPDATE/DELETE statements. It also ensures that within one query
    result (or across Include's) the same identity returns the same object
    instance. AsNoTracking() skips all of that — entities are materialized but
    never tracked. Use it for read-only scenarios: reporting queries, HTTP GET
    handlers that immediately serialize to JSON, high-volume reads. It reduces
    memory (no identity map bloat), removes change-detection overhead, and is
    measurably faster. The trade-off: tracked instances that you intend to
    modify and SaveChanges need tracking, and no-tracking queries cannot
    participate in lazy loading / automatic fixups, so entities attached
    later need Attach().

- id: dotnet-efcore-nplus1-03
  answer: |
    IQueryable is deferred: building it only composes an expression tree, and
    no SQL is sent to the database until it is enumerated — by foreach,
    ToList/ToArray, First/Single, Count/Any, or ToListAsync/ToArrayAsync. The
    N+1 problem arises when you load a parent list (1 query) and then trigger a
    lazy load of a related navigation per row: accessing .Children on each of
    the N parents fires N additional queries, so 1 + N round trips instead of
    2. Fixes: eager load with Include/ThenInclude so the related data arrives in
    one (or a split) query; use a Select projection into a DTO so EF only pulls
    the columns needed and no tracking of the graph is involved; and disable
    lazy loading entirely to make accidental N+1 loud rather than silent. When
    including multiple collections, be aware of the cartesian explosion and
    consider AsSplitQuery.

- id: dotnet-efcore-savechanges-tx-04
  answer: |
    Yes — a single SaveChanges/SaveChangesAsync call is implicitly
    transactional: EF Core wraps all the INSERT/UPDATE/DELETE statements it
    generates in that call into one transaction and commits it once, so a
    mid-way failure rolls the whole unit of work back. But an explicit
    transaction is needed when you need multiple SaveChanges calls, or stored
    procedures / raw SQL, to be atomic — wrap them in
    Database.BeginTransactionAsync() (or an ExecutionStrategy-based
    transaction, which is what you need with retryable providers like SQL
    Server). A migration is EF Core's schema-evolution unit: a class with
    Up()/Down() methods plus a model snapshot, generated from the diff between
    the current model and the last snapshot, that advances (or reverts) the
    database schema. Migrations are applied by dotnet ef database update /
    Database.Migrate() at startup, and recorded in the __EFMigrationsHistory
    table so a database knows which migrations have already been applied.

- id: dotnet-gc-generations-loh-01
  answer: |
    The generational hypothesis is that most objects die young, so the GC is
    generational: gen 0 holds new, small objects and is collected cheaply and
    frequently; survivors get promoted to gen 1 (a buffer/second chance to
    die), and then to gen 2, which holds long-lived objects and is collected
    only in a full, expensive mark-and-sweep that must traverse the whole live
    heap. The Large Object Heap holds allocations of roughly 85,000 bytes and
    above, and it is only cleaned up during a gen 2 collection. Large
    short-lived allocations are therefore especially costly: a 1 MB buffer
    goes straight to the LOH, triggers expensive gen 2 collections rather than
    cheap gen 0 collections, and leaves fragmentation because the LOH has
    historically not been compacted (it is configurable, but still moved only
    during a full GC). The std mitigations are to pool large buffers
    (ArrayPool) or stackalloc them instead of allocating.

- id: dotnet-gc-dispose-finalizer-02
  answer: |
    IDisposable/Dispose releases unmanaged resources deterministically: you
    call it (or `using` / `await using` does), you know exactly when the
    file handle or stream is closed, and it participates in DI container
    disposal and using blocks. A finalizer (~ClassName()) is non-deterministic
    cleanup that the GC runs on a background thread before reclaiming an object
    that was never disposed; you only need one when your type owns an
    unmanaged handle directly and you cannot guarantee consumers will call
    Dispose — hence the standard pattern of a public Dispose(), a
    protected virtual Dispose(bool) that does the real work, and
    GC.SuppressFinalize(this) so a disposed object never pays finalizer cost.
    Finalizers are expensive: objects with finalizers survive an extra GC cycle
    and queue work on the finalizer thread. IAsyncDisposable adds
    DisposeAsync() for a cleanup that itself needs async I/O (flushing a
    stream, releasing an async native resource), consumed with `await using`;
    it avoids blocking a thread on I/O during disposal. If a type implements
    both, implement DisposeAsync() to call Dispose(bool) for the synchronous
    part, and prefer using both patterns when the resource allows.

- id: dotnet-gc-span-arraypool-03
  answer: |
    Span<T> / ReadOnlySpan<T>: a type- and bounds-safe view over contiguous
    memory (an array, a string, native memory) that can be sliced without
    copying. It avoids allocating intermediate arrays and strings — for
    example parsing or scanning a payload, or splitting a string, without
    creating substrings. stackalloc: allocates a block on the stack instead of
    the heap (e.g. Span<char> buf = stackalloc char[256];), which is
    essentially free, needs no GC involved, and is automatically unreachable
    when the method returns; it avoids heap allocation plus GC pressure for
    small, short-lived buffers. ArrayPool<T>: rents arrays from a shared pool
    (ArrayPool<T>.Shared.Rent(n); ... Return(arr)) so temporary buffers are
    reused instead of garbage-collected; it avoids per-request large-array
    allocations — important because genuinely large arrays land on the Large
    Object Heap. All three avoid heap allocation and the resulting GC pressure.

- id: dotnet-gc-struct-vs-class-04
  answer: |
    A struct is a value type: its storage is inline — on the stack, inside
    another object, or inside an array element — and assigning it copies the
    whole value. A class is a reference type: the `new` expression allocates the
    object on the heap, tracked by the GC, and the variable only holds a
    reference, so assignment copies the reference and two variables can point
    at the same object. That structural difference is the reason a struct field
    avoids a separate heap allocation and its eventual collection. Structs
    therefore also have value semantics (copying is deep by default), which is
    why they should stay small and immutable to avoid expensive copies. Boxing
    is the conversion of a value type to a reference type — assigning an int to
    an object, an int[] where int[] is passed as an IEnumerable (which in
    ancient code passed a value-typed list as an interface) etc. Boxing
    allocates a new object on the heap containing a copy of the value, which
    makes the value GC-tracked and adds the risk of further boxing on each
    unboxed use.

- id: dotnet-json-stj-defaults-01
  answer: |
    Globally: new JsonSerializerOptions { PropertyNamingPolicy =
    JsonNamingPolicy.CamelCase } — the naming policy applies to every property
    on serialization and, combined with PropertyNameCaseInsensitive = true, on
    deserialization too. For an ASP.NET Core web host the built-in web defaults
    already use camelCase (JSON is camelCase in the request pipeline by default;
    JsonSerializerDefaults.Web). Per-property: annotate the member with
    [JsonPropertyName("my_custom_name")] on the class, or set the name on
    JsonPropertyInfo via JsonSerializerContext in the source-gen path. The
    attribute on a single property wins over the policy.

- id: dotnet-json-sourcegen-02
  answer: |
    System.Text.Json source generation emits, at build time, the serialization
    and deserialization code for specific types, instead of deriving the same
    behaviour at runtime through reflection. You declare a partial
    JsonSerializerContext decorated with [JsonSerializable(typeof(MyType))],
    and the analyzer produces the (fast, static) code for it; you pass the
    context type to JsonSerializerOptions (JsonSerializerOptions.Default with
    TypeInfoResolver = MyContext.Default). It matters for performance because it
    removes per-call reflection and metadata lookup, speeds startup (no
    reflection-based type inspection at runtime), and usually reduces
    allocations. It matters for trimming and Native AOT because reflection
    based serialization is exactly what the trimmer cannot see through — with
    source-gen the serializer contains no runtime type discovery, so trimming
    and AOT compilation produce working, warning-free binaries.

- id: dotnet-json-stj-vs-newtonsoft-03
  answer: |
    System.Text.Json pros: built in, no external dependency; generally faster
    and lower-allocation (works on UTF-8 bytes and spans, not intermediate
    strings); source generation support; trimming/AOT-friendly; the default for
    ASP.NET Core minimal APIs and MVC, so new templates and docs use it.
    Cons: historically stricter — it throws on trailing commas, comments,
    duplicate keys, and quirk-tolerant shapes that Newtonsoft accepted;
    polymorphism and some edge cases (e.g. non-serializable cases) need more
    work; it deliberately had fewer conversion hooks, and JObject/JToken
    manipulation is clunkier.
    Newtonsoft.Json pros: extremely mature, much richer feature surface —
    JToken/JObject/Dynamic DOM manipulation, a far deeper custom converter story,
    more flexible DateTime/ISO handling, more tolerant of malformed or odd JSON,
    a large existing ecosystem of packages and examples.
    Cons: slower and allocation-heavy by comparison; not designed for trimming
    or Native AOT; no built-in source generation; no longer the framework's own
    default. Practically: pick STJ for new greenfield work, especially AOT and
    performance-sensitive paths; pick Newtonsoft when you genuinely need its
    flexibility or its existing tooling.

- id: dotnet-json-options-reuse-04
  answer: |
    A JsonSerializerOptions instance is intended to be treated as an immutable
    singleton because the first use of each options instance causes the
    serializer to build and cache its converter and metadata caches against that
    exact instance; constructing a fresh options object per call throws that
    work away and re-does it, which is expensive (the docs warn about creating
    options in a loop) and can also break correctness — mutating an options
    object after its first use throws. The standard guidance is one static
    readonly JsonSerializerOptions per configuration and sharing it across all
    calls.
    Polymorphic serialization: System.Text.Json does NOT do it automatically —
    by contract it serializes the declared type, so serializing a derived
    instance through a base-typed reference emits only the base properties (or
    deserializing to a base type cannot know which derived type to build). You
    opt in by annotating the base class with [JsonDerivedType(typeof(Derived),
    typeDiscriminator: "derived")], which makes STJ emit a "$type"
    discriminator property (a string or int value you choose) and use it on
    deserialization to select the concrete type; alternatively you write a
    custom JsonConverter for full control.

- id: dotnet-http-socket-exhaustion-01
  answer: |
    Each `new HttpClient()` creates a new underlying handler (SocketsHttpHandler)
    with a brand-new connection pool, and disposing the HttpClient closes its
    sockets. Doing that per request means each request opens fresh TCP
    connections, and closing them in bulk leaves the OS's ephemeral ports
    sitting in TIME_WAIT for minutes; at volume you exhaust available local
    ports and subsequent connections fail with socket-level errors (the classic
    "Unable to connect... only the first N of M requests succeeded" /
    "address already in use"). Creating a new handler per call also defeats
    HttpClient's own internal connection-pooling semantics.
    IHttpClientFactory fixes it by pooling handlers: factory-created clients
    share a set of long-lived handlers, get their connections pooled and reused
    rather than torn down per request, and the factory additionally rotates
    those handlers on a timer (default 2 minutes, configurable) so pooled
    connections are periodically refreshed — which also prevents stale DNS.
    Net effect: bounded, reused connections instead of churn, and disposal of
    the per-outcome sockets is handled by the handler pool.

- id: dotnet-http-typed-clients-02
  answer: |
    A named client is a configured-but-unnamed HttpClient obtained at call
    time: services.AddHttpClient("github", c => c.BaseAddress = ...) then
    var client = _factory.CreateClient("github"). A typed client wraps the
    HttpClient in a dedicated class: you define a client type (typically with
    an interface) taking HttpClient as a constructor parameter, register it
    with services.AddHttpClient<ICatalogClient, CatalogClient>(c => {...}), and
    DI injects a ready-to-use instance (registered transient by default).
    Prefer a typed client because configuration (base address, default headers,
    timeouts, auth) lives in one place rather than being scattered at every
    call site; there are no magic strings to typo at runtime;
    endpoint-calling code is encapsulated in methods with proper signatures
    instead of raw string URLs; it is natural to unit test because the client
    is an interface whose HttpClient handler can be swapped with a
    delegating handler; and resilience handlers (retry, circuit breaker) attach
    to the client definition itself.

- id: dotnet-http-lifetime-dns-03
  answer: |
    The subtlety is that the pooled connections, not the client object, hold
    the DNS resolution. A handler (SocketsHttpHandler) keeps established
    connections alive and does not re-resolve hostnames while usable pooled
    connections exist, so if a single handler stays active forever — the
    singleton HttpClient scenario — changes to a service's DNS (redeploy to new
    IPs, load balancer changes) are not picked up and the client keeps talking
    to the old IP until a connection happens to break.
    IHttpClientFactory addresses this by rotating the primary handler on a
    timer (SetHandlerLifetime, default 2 minutes) so connections are rebuilt
    periodically and fresh DNS is picked up. If you manage SocketsHttpHandler
    yourself, the equivalent knob is PooledConnectionLifetime: when a pooled
    connection exceeds it, the connection is retired and a new one — with fresh
    DNS — is established. So handler lifetime is the balancing act: too short
    and you churn/brute-force sockets toward exhaustion, too long and you serve
    stale DNS.

- id: dotnet-http-resilience-04
  answer: |
    On modern .NET (8+) the built-in path is resilience handlers on
    IHttpClientFactory: services.AddHttpClient("name", ...).AddResilienceHandler(
    "pipeline", builder => builder
        .AddRetry(new HttpRetryStrategyOptions { ... })   // exponential backoff + jitter, honors 5xx/408/429 by default
        .AddCircuitBreaker(new HttpCircuitBreakerStrategyOptions { ... })  // break after N failures, half-open test, break duration
        .AddTimeout(TimeSpan.FromSeconds(30)))            // per-attempt attempt timeout
    backed by Microsoft.Extensions.Http.Resilience (Polly v8); there is also
    AddStandardHandler()/AddStandardResilienceHandler() as a ready-made default
    bundle. Older stacks use AddPolicyHandler(Policy<HttpResponseMessage>...)
    with Polly v7's HttpPolicyExtensions.Handle<HttpRequestException>()
    .WaitAndRetryAsync(...)/CircuitBreakerAsync(...) / Policy.TimeoutAsync.
    Cancellation tokens fit in as the cooperation mechanism: HttpClient
    requests accept a CancellationToken from the caller (often from the HTTP
    request itself, so client disconnect aborts everything downstream), the
    timeout strategies internally build linked tokens so a timeout aborts the
    whole attempt, and retry logic checks the caller's token between attempts
    so it stops retrying when the caller has cancelled — timeouts and
    cancellation surface as TaskCanceledException/OperationCanceledException.

- id: dotnet-log-templates-01
  answer: |
    Because the first argument is a message template: the provider records
    "Order {OrderId} shipped" as the template and the values as structured
    properties, so sinks (JSON console output, Seq, Application Insights, and
    friends) store OrderId as a first-class, queryable field rather than burying
    it inside pre-formatted prose. It also keeps the log message consistent for
    aggregation — the same template groups into one searchable event. When you
    write LogInformation($"Order {orderId} shipped") the interpolation is
    evaluated eagerly, so the string is allocated even when the level is
    disabled and the value is only ever present as text. Structured logging
    also avoids format-injection pitfalls (untrusted braces in data
    corrupting a format string) and is the convention every .NET logging
    provider understands.

- id: dotnet-log-levels-02
  answer: |
    In order of increasing severity: Trace, Debug, Information, Warning,
    Error, Critical. (Trace = most verbose tracing detail; Critical = app is
    down / data loss; None exists as "don't log anything" for filter overrides.)
    Filtering is configured by category name, where a category is the
    fully-qualified type name or an arbitrary category string obtained from
    ILogger<T>/ILoggerFactory.CreateLogger. Standard config:

        "Logging": {
          "LogLevel": {
            "Default": "Information",
            "Microsoft.EntityFrameworkCore": "Warning",
            "Microsoft.EntityFrameworkCore.Database.Command": "Information"
          }
        }

    The Default entry is the fallback; more specific category prefixes
    override it, so EF Core's own logs can be quieted to Warning while keeping
    your app talking at Information, and you can lift a single noisy or
    interesting child category (like the SQL command logger) back up. These
    are read through the normal IConfiguration chain, so appsettings,
    environment variables, and command-line args all apply the same precedence,
    and filters can also be set in code with AddFilter(...).

- id: dotnet-log-highperf-03
  answer: |
    [LoggerMessage] is a source generator (added in .NET 6 via
    Microsoft.Extensions.Logging.Abstractions) that turns a partial method
    declaration into a strongly typed logging method:

        [LoggerMessage(Level = LogLevel.LogLevelInformation, Message = "Order {OrderId} shipped")]
        static partial void OrderShipped(this ILogger logger, int orderId);

    It generates an implementation that calls LoggerMessage.Define under the
    hood. It is preferred on hot paths because the direct
    logger.LogInformation(...) call parses the message template and formats the
    result on every invocation; the generated helper caches that parsing and
    formatting, checks IsEnabled once, and avoids the boxing-into-
    FormattedLogValues array on each call — cheaper per call and much less
    garbage. Because it is generated, the compiler also validates the template
    against the parameter names and can warn you at build time about a
    mismatch, rather than failing silently at runtime.

- id: dotnet-log-scopes-otel-04
  answer: |
    BeginScope attaches ambient structured properties to every log entry
    produced within the scope:

        using (logger.BeginScope(new Dictionary<string, object> { ["TraceId"] = Activity.Current?.TraceId }))
        { ... }  // all logs inside share those properties

    Typical uses are per-request data such as a correlation/trace ID, tenant
    ID, user ID, or operation name; providers with IncludeScopes = true append
    them to every message, so diagnostics are correlated across multiple log
    lines. The scope object is disposable and exited when disposed.
    For distributed tracing, .NET (since .NET 5 as the cross-platform way) uses
    Activity from System.Diagnostics (System.Diagnostics.DiagnosticSource),
    created by an ActivitySource the library declares; the OpenTelemetry .NET
    SDK listens to those sources (via AddOpenTelemetry().WithTracing()
    .AddAspNetCoreInstrumentation() .AddHttpClientInstrumentation()) and exports
    spans to a collector (OTLP), with W3C tracecontext propagation automatically
    carried by HttpClient calls. Metrics work the same way: code defines a
    Meter with instruments (Counter<T>, Histogram<T>, ObservableGauge), and
    the OpenTelemetry SDK's WithMetrics() plus exporters (Prometheus, OTLP)
    scrape and publish them.

