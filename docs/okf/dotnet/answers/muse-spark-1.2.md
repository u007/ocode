# dotnet knowledge benchmark — answers
# Model: opencode-go/muse-spark-1.2   (provider-stripped id: muse-spark-1.2)
# CLOSED-BOOK via ocode2 run -dir /tmp/kaizen-dotnet-answer (empty, no corpus access). corpus_rev 1.

```yaml
- id: dotnet-di-lifetimes-01
  answer: |
    Three lifetimes registered with AddTransient/AddScoped/AddSingleton:
    Transient: new instance every time it is resolved, even within same scope/request. Use for lightweight stateless services.
    Scoped: one instance per IServiceScope. In ASP.NET Core a scope = one HTTP request (created by framework). Same scope gets same instance; different request gets new instance. Use for per-request state like DbContext.
    Singleton: one instance for lifetime of root IServiceProvider (application lifetime). Created on first resolve (or at startup if eagerly created) and reused everywhere. Use for stateless shared, thread-safe services and caches.

- id: dotnet-di-captive-02
  answer: |
    A captive dependency occurs when a longer-lived service captures a shorter-lived service, extending its lifetime beyond intended. Most common: Singleton -> depends on -> Scoped.
    Injecting Scoped into Singleton is a bug because the Scoped instance is resolved from the root provider at Singleton creation time and then held forever. It effectively becomes a singleton: it won't be disposed until app shutdown, will hold stale state/cache from first request, is not thread-safe for concurrent requests, and for DbContext will accumulate tracked entities and concurrency failures. The container in Development detects this with ValidateScopes/ValidateOnBuild and throws InvalidOperationException when trying to resolve Scoped from root.

- id: dotnet-di-scope-in-singleton-03
  answer: |
    Do not inject the Scoped service directly. Inject IServiceScopeFactory (or IServiceProvider) into the singleton, and create a scope per unit of work:
    ```csharp
    public class MyBackgroundService : BackgroundService {
      private readonly IServiceScopeFactory _scopes;
      public MyBackgroundService(IServiceScopeFactory scopes) => _scopes = scopes;
      protected override async Task ExecuteAsync(CancellationToken ct) {
        while(!ct.IsCancellationRequested) {
          using var scope = _scopes.CreateScope(); // or CreateAsyncScope() in .NET 8+
          var db = scope.ServiceProvider.GetRequiredService<MyDbContext>();
          await db.DoWorkAsync(ct);
          await Task.Delay(TimeSpan.FromMinutes(1), ct);
        }
      }
    }
    ```
    This gives a fresh Scoped instance per iteration, correctly disposed at end of using. For single method calls, same pattern: create scope, resolve, use, dispose.

- id: dotnet-di-keyed-04
  answer: |
    Keyed DI, added in .NET 8. Allows multiple registrations of same service type distinguished by a key (string or object).
    Registration: services.AddKeyedTransient<IMessageSender, SmsSender>("sms"); services.AddKeyedTransient<IMessageSender, EmailSender>("email");
    Resolution: provider.GetRequiredKeyedService<IMessageSender>("sms") or injection via attribute: public void Consume([FromKeyedServices("sms")] IMessageSender sender). Also GetKeyedServices, GetKeyedService. Before .NET 8 you had to use manual factories, IEnumerable<T> with metadata, or third-party containers (Autofac named services).

- id: dotnet-config-precedence-01
  answer: |
    IConfiguration is composite; later providers override earlier on same key (last wins). Default Host.CreateApplicationBuilder/WebApplicationBuilder order (lowest to highest priority):
    1. appsettings.json
    2. appsettings.{Environment}.json (e.g. Development)
    3. User secrets (Development only, via AddUserSecrets)
    4. Environment variables (AddEnvironmentVariables) - naming with __ for :
    5. Command-line arguments (AddCommandLine)
    Custom providers added after override defaults. So command-line > env vars > user-secrets > environment-specific json > base json. Within same provider, more specific wins.

- id: dotnet-config-options-interfaces-02
  answer: |
    All wrap same underlying IConfiguration but differ in lifetime and reload behavior:
    IOptions<T>: Singleton, computed once at first resolve, never updates after startup. Can be injected anywhere (singleton, scoped, transient). Use when config is immutable.
    IOptionsSnapshot<T>: Scoped, recomputed per scope/request. Sees reloads (if providers support reloadOnChange) at next request. Cannot be injected into Singleton (would be captive). Use in scoped/transient services that need per-request fresh config.
    IOptionsMonitor<T>: Singleton, always current. CurrentValue reflects latest config, supports OnChange(Action<T, string>) notifications and Get(name) for named options. Can be injected into Singleton. Use for singleton services that must react to reloads.
    Snapshot vs Monitor: Snapshot caches per-scope, Monitor is live singleton with polling.

- id: dotnet-config-secrets-03
  answer: |
    Local development: use User Secrets (dotnet user-secrets init/set) - stored as secrets.json in user profile outside repo (%APPDATA%/Microsoft/UserSecrets or ~/.microsoft/usersecrets) and only loaded in Development; or environment variables locally. Never commit secrets to appsettings.json or source control.
    Production: never use user-secrets. Use hosted secret store: Azure Key Vault (AddAzureKeyVault provider), AWS Secrets Manager/Parameter Store, HashiCorp Vault, or environment variables injected by host/orchestrator (Container Apps, Kubernetes secrets). Production provider is wired in configuration with managed identity, with key rotation and auditing. Difference: dev is machine-local plaintext file for convenience; prod is centralized, encrypted, access-controlled, audited.

- id: dotnet-config-options-binding-04
  answer: |
    Bind via Options pattern:
    ```csharp
    services.Configure<MyOptions>(configuration.GetSection("MySection"));
    // or services.AddOptions<MyOptions>().Bind(configuration.GetSection("MySection"))
    class MyOptions { public string Endpoint {get;set;} public int TimeoutMs {get;set;} }
    ```
    appsettings: { "MySection": { "Endpoint":"...", "TimeoutMs":5000 } }
    Fail-fast validation: add DataAnnotations and validate on start:
    ```csharp
    services.AddOptions<MyOptions>()
      .Bind(configuration.GetSection("MySection"))
      .ValidateDataAnnotations()
      .Validate(o => Uri.TryCreate(o.Endpoint, UriKind.Absolute, out _), "Invalid endpoint")
      .ValidateOnStart(); // throws at app startup instead of first use
    ```
    ValidateOnStart (added .NET 6) forces validation during startup; without it validation only occurs when IOptions<T> is first resolved. Alternative: implement IValidateOptions<T>.

- id: dotnet-pipeline-order-01
  answer: |
    Pipeline is ordered chain of middleware delegates (Russian doll/onion). Request flows forward through each Use... in registration order, then response flows backward. Order matters because each middleware can act before/after next.
    Typical order: ExceptionHandler/HSTS -> HttpsRedirection -> StaticFiles -> Routing (UseRouting) -> Authentication (UseAuthentication) -> Authorization (UseAuthorization) -> Endpoints (Map*).
    UseAuthentication/UseAuthorization must be after UseRouting and before Map* because Authentication establishes ClaimsPrincipal and Authorization needs the selected endpoint's metadata ( [Authorize] attributes, policies, CORS) which is only known after routing. If placed before Routing, Authorization has no endpoint to authorize; if after endpoints, it never runs. Similarly UseCors often between Routing and Authorization.

- id: dotnet-pipeline-use-vs-map-02
  answer: |
    Use: app.Use(async (ctx, next) => { /* before */ await next(ctx); /* after */ }); Adds middleware that can call next to continue pipeline. Branches but rejoins.
    Run: app.Run(async ctx => { await ctx.Response.WriteAsync("..."); }); Terminal middleware - does NOT take next parameter, ends pipeline. Nothing registered after Run executes. Use to short-circuit.
    Map: app.Map("/api", branch => { branch.Use(...); }); Branches pipeline by request path prefix - if path starts with /api, that branch executes; otherwise continues main pipeline. MapWhen/MapWhen predicate variant.
    Short-circuit = middleware handles request without calling next (e.g., auth failure returns 401, static file found, endpoint writes response). Subsequent middleware is skipped and response returns up the stack.

- id: dotnet-pipeline-minimal-results-03
  answer: |
    Minimal API handler return value is processed by the framework: string -> text/plain, IResult -> executes to produce response, object -> serialized to JSON (application/json) with content negotiation via System.Text.Json, Task<T> unwrapped.
    Raw `return new Todo{...}` implicitly becomes 200 OK JSON with inferred status, no control over headers/status.
    Results / TypedResults give explicit control: Results.Ok(), NotFound(), BadRequest(), Created("/todos/1", todo), NoContent(), Json(), File(), etc., setting status code, headers, content type. TypedResults.Ok<Todo>(todo) is same but strongly-typed (returns union type like Results<Ok<Todo>, NotFound>) which improves OpenAPI generation, compile-time checking, and analyzers vs untyped IResult. Prefer TypedResults for APIs.

- id: dotnet-pipeline-filters-vs-middleware-04
  answer: |
    Middleware: global, runs for all requests, outermost. Operates on HttpContext, ordered at startup. Good for cross-cutting concerns (logging, exception handling, auth, CORS, compression). Knows nothing about selected endpoint arguments until after routing, and can't easily access typed handler parameters/return values.
    Endpoint filter (AddEndpointFilter) / MVC action filter: runs after routing, close to endpoint invocation. Scoped to specific route or group, can inspect/modify handler arguments and result, and short-circuit before handler executes. Example: validation filter that checks TParam before handler, or transforms response.
    Use filter when logic is per-endpoint, needs argument binding, validation, or result transformation, or should run after routing/before handler. Use middleware when logic is request-wide and endpoint-agnostic. Filters can access EndpointFilterInvocationContext.Arguments and set result without middleware needing to parse body.

- id: dotnet-efcore-context-lifetime-01
  answer: |
    Correct lifetime is Scoped (AddDbContext defaults to Scoped). One DbContext per request/unit-of-work/scope.
    Not safe to share across threads/concurrent operations because DbContext is not thread-safe: it holds a change tracker, identity map, DbConnection, and internal state mutated by queries and SaveChanges. Concurrent use throws `A second operation was started...` or corrupts tracking. Even parallel async awaits on same instance is unsafe. Singleton or shared static DbContext would accumulate stale tracking, memory leak, and race conditions. For parallel work, create separate scope/context per operation. New DbContextFactory (AddDbContextFactory) allows transient creation when needed.

- id: dotnet-efcore-notracking-02
  answer: |
    Change tracker keeps identity map of all entities loaded via context, snapshots original values, tracks state (Added/Modified/Unchanged/Deleted) to generate UPDATE/DELETE on SaveChanges, supports navigation fixup and identity resolution.
    AsNoTracking() tells EF to not put results in tracker: entities returned are detached, no snapshot, SaveChanges will ignore changes to them. Use for read-only queries (list display, reports, API GET) to improve performance (less memory/CPU, faster materialization) and avoid tracking overhead. Also needed when querying same entities many times or large result sets. Don't use when you intend to edit and SaveChanges - then you need tracking or need to Attach manually.

- id: dotnet-efcore-nplus1-03
  answer: |
    IQueryable is deferred: building query does not execute SQL; execution occurs on enumeration or ToListAsync()/FirstAsync()/CountAsync() etc. Allows composing Where/Include/OrderBy before sending one SQL.
    N+1 arises when you execute 1 query for parents (e.g., `var blogs = await ctx.Blogs.ToListAsync()`) then lazily access navigation in loop: `foreach(var b in blogs) Console.Write(b.Posts.Count)` causes one additional query per blog (N queries) if lazy loading or separate query. With N=100, 101 total queries.
    Fix: eager load with Include/ThenInclude (`ctx.Blogs.Include(b=>b.Posts)`), or projection with Select (`ctx.Blogs.Select(b=> new {b.Name, PostCount=b.Posts.Count})`), or single join via filtered Include, or Load explicitly. Also use AsSplitQuery for multiple collections to avoid cartesian explosion.

- id: dotnet-efcore-savechanges-tx-04
  answer: |
    Yes, a single SaveChangesAsync() is transactional: EF Core wraps all inserts/updates/deletes for that call in a single database transaction (creates one if no explicit transaction). All succeed or all rollback. It does NOT span multiple SaveChanges calls - for that use `await using var tx = await ctx.Database.BeginTransactionAsync()` or ExecutionStrategy's `ExecuteInTransactionAsync`.
    Migration role: migration is code-based schema evolution. `dotnet ef migrations add <Name>` scaffolds Up()/Down() with CreateTable/AddColumn etc. from model diff, and `dotnet ef database update` applies pending migrations to DB via __EFMigrationsHistory table. Replaces manual SQL scripts, provides versioned, repeatable schema changes.

- id: dotnet-gc-generations-loh-01
  answer: |
    Generational GC assumes most objects die young: Gen0: new, small objects; collected frequently, cheap. Survivors promoted to Gen1 (short-lived buffer), then Gen2 (long-lived) collected rarely, expensive full collection. GC compacts Gen0/1 to avoid fragmentation.
    LOH (Large Object Heap) (and since .NET 8+ Large objects also in Gen2 but separate segment) holds objects >=85,000 bytes (e.g., large arrays). Collected only with Gen2, not compacted by default (compact only on demand due to cost), so fragmented.
    Large short-lived allocations are costly because they go straight to LOH/Gen2, triggering expensive Gen2 collection, cause fragmentation leaving holes that can't be compacted, increase memory pressure and CPU pause times. Mitigate with pooling (ArrayPool), reusing buffers.

- id: dotnet-gc-dispose-finalizer-02
  answer: |
    IDisposable/Dispose: deterministic cleanup called explicitly or via using/using var. Releases unmanaged resources (handles, sockets, streams) promptly. Implement when type owns unmanaged or disposable resources. Caller responsible.
    Finalizer (~TypeName, now with C# 12: `~` syntax deprecated for new code in favor of SafeHandle): non-deterministic, run by GC on finalizer queue before reclaiming object. Safety net if Dispose not called, but very expensive (delays collection by one generation, runs on dedicated thread). Only need if directly holding unmanaged resource (IntPtr) without SafeHandle. Prefer SafeHandle over writing finalizer.
    IAsyncDisposable/DisposeAsync: async deterministic cleanup for resources needing async flush (e.g., async streams, DbContext, IAsyncEnumerable). Used with await using. Allows async I/O in dispose path.

- id: dotnet-gc-span-arraypool-03
  answer: |
    Tools to avoid allocations/GC pressure:
    Span<T> / ReadOnlySpan<T>: ref struct view over contiguous memory (array, stack, unmanaged) without allocation, bounds-checked, stack-only. Enables slicing and parsing without substring allocations. Used with Span-based APIs (e.g., int.Parse(ReadOnlySpan<char>)).
    stackalloc: allocates buffer on stack, not heap, scoped to method, no GC. Example `Span<char> buf = stackalloc char[256];` for small temporary buffers. Must keep size small to avoid stack overflow.
    ArrayPool<T>.Shared: pool of reusable arrays (bucketed by size). Rent returns array >= requested size, Return gives it back. Avoids allocating large temporary arrays per request (especially for I/O, LOH avoidance). Always clear if sensitive and return in finally.

- id: dotnet-gc-struct-vs-class-04
  answer: |
    class is reference type: instance allocated on heap, variable holds reference (pointer), passed by reference (copy reference), collected by GC, overhead object header + pointer indirection.
    struct is value type: typically allocated inline where declared - on stack if local, inline in object/array if field/element. Variable holds value directly, copying copies entire value, no GC if not boxed, more cache-friendly but copying large structs is costly. Constrained to not have parameterless ctor historically, no inheritance.
    Boxing: wrapping value type into object/reference (e.g., `object o = 42;` or passing struct to interface parameter). Allocates heap object, copies value, later unboxing copies back. Causes allocation and GC pressure; avoid via generics.

- id: dotnet-json-stj-defaults-01
  answer: |
    System.Text.Json defaults to exact .NET property name (PascalCase). For camelCase globally set JsonSerializerOptions.PropertyNamingPolicy = JsonNamingPolicy.CamelCase, and often PropertyNameCaseInsensitive = true for deserialization, or annotate model with [JsonPropertyName]:
    ```csharp
    var opts = new JsonSerializerOptions { PropertyNamingPolicy = JsonNamingPolicy.CamelCase };
    JsonSerializer.Serialize(obj, opts);
    // single property override
    public class Dto {
      [JsonPropertyName("order_id")]
      public int OrderId {get;set;}
    }
    ```
    In ASP.NET Core, configure via builder.Services.Configure<JsonOptions>(o => o.SerializerOptions.PropertyNamingPolicy = JsonNamingPolicy.CamelCase) - but Web defaults already use camelCase.

- id: dotnet-json-sourcegen-02
  answer: |
    Source generation (System.Text.Json, .NET 6+) uses [JsonSerializable] on a partial JsonSerializerContext to generate serialization code at compile time:
    ```csharp
    [JsonSerializable(typeof(MyDto))]
    partial class MyContext : JsonSerializerContext {}
    JsonSerializer.Serialize(dto, MyContext.Default.MyDto);
    ```
    Benefits: 1) Performance - no runtime reflection, faster startup and throughput, no runtime codegen. 2) Trimming/Native AOT compatibility - reflection-based serialization requires metadata that trimmer removes; source gen emits explicit code that is trim-safe and AOT compatible (no MakeGenericType). Required for publishing trim/AOT. Also enables fast path for immutable types.

- id: dotnet-json-stj-vs-newtonsoft-03
  answer: |
    System.Text.Json (STJ): built-in, high-performance (Span<byte> UTF-8, low allocations), trim/AOT friendly, secure defaults (strict, no auto polymorphism). Less feature-rich, stricter JSON handling, limited contract customization until recent .NET versions.
    Newtonsoft.Json (Json.NET): rich features (flexible naming, contract resolvers, JsonPath, converters for F#, missing members, reference loop handling PreserveReferencesHandling, polymorphic TypeNameHandling, comments/trailing commas lenient, extensive converters). More forgiving deserialization.
    Trade-off: choose STJ for performance, native AOT, and modern ASP.NET Core default (no extra dep). Reach for Newtonsoft when you need legacy features, complex polymorphism, custom contracts, or interop with existing Newtonsoft payloads. Many add Newtonsoft for migration.

- id: dotnet-json-options-reuse-04
  answer: |
    JsonSerializerOptions is expensive to create: it builds and caches converters, property metadata, naming policies. Creating per-serialization causes allocations and CPU overhead. It is thread-safe after construction, should be created once and reused (static singleton, or DI singleton). STJ also caches via JsonSerializerOptions.Default or source-gen context.
    Polymorphic serialization: STJ does not emit type discriminators by default (security). Need to opt-in: annotate base with [JsonDerivedType(typeof(Cat), "cat"), JsonDerivedType(typeof(Dog), "dog")] and [JsonPolymorphic(TypeDiscriminatorPropertyName="$type")] (added .NET 7) or write custom JsonConverter<TBase>. Deserialization then reads discriminator to create derived type. Without this, serializing base reference only emits base properties and loses derived info.

- id: dotnet-http-socket-exhaustion-01
  answer: |
    HttpClient itself is lightweight but its underlying HttpMessageHandler/SocketsHttpHandler holds TCP sockets (connection pool). Disposing HttpClient disposes handler, but sockets linger in TIME_WAIT for 240s (TCP close). Creating new HttpClient per request leaves many sockets in TIME_WAIT, exhausts available ports (socket exhaustion) and loses connection pooling/keep-alive benefits, plus DNS not refreshed efficiently.
    IHttpClientFactory manages handler lifetime: factory pools HttpMessageHandler instances separately from HttpClient instances, reuses handlers across requests, pools connections, respects PooledConnectionLifetime, disposes handlers on schedule, avoiding TIME_WAIT storm and enabling central config (retry, logging). Code gets new HttpClient that wraps pooled handler without owning socket.

- id: dotnet-http-typed-clients-02
  answer: |
    Both are patterns over IHttpClientFactory:
    Named client: string-keyed config: services.AddHttpClient("github", c=>c.BaseAddress=new Uri("https://api.github.com")); Consumed via IHttpClientFactory.CreateClient("github"). Useful for simple or dynamic names, but magic strings.
    Typed client: strongly-typed class that takes HttpClient via DI: class GitHubClient(HttpClient http) { ... } registered via services.AddHttpClient<GitHubClient>(c=>c.BaseAddress=...). Consumed by injecting GitHubClient directly. Prefer typed because it gives explicit interface, encapsulates API endpoints, enables per-client config, Polly policies, and testability via mock HttpMessageHandler, and avoids string keys. Supports AddTypedClient with additional DI.

- id: dotnet-http-lifetime-dns-03
  answer: |
    Factory pools handlers for handlerLifetime (default 2 minutes) (SetHandlerLifetime). While handler lives, its SocketsHttpHandler's connection pool reuses established TCP connections, and DNS resolution is cached per connection. So if DNS record changes (e.g., load balancer failover, blue-green), existing pooled connections still point to old IP until handler is recycled - stale DNS.
    Fix: tune PooledConnectionLifetime (e.g., .PooledConnectionLifetime = TimeSpan.FromMinutes(5)) on SocketsHttpHandler to rotate connections, or reduce HandlerLifetime. In .NET 8+ SocketsHttpHandler.PooledConnectionLifetime can be set directly via AddHttpClient(...).ConfigurePrimaryHttpMessageHandler(() => new SocketsHttpHandler { PooledConnectionLifetime = TimeSpan.FromMinutes(2)}). Alternative: use SocketsHttpHandler directly instead of factory's HttpClientHandler for finer control; SocketsHttpHandler is the default handler now and respects its own pooling correctly.

- id: dotnet-http-resilience-04
  answer: |
    Modern .NET (.NET 8+) uses Microsoft.Extensions.Http.Resilience (Polly v8 wrapper): services.AddHttpClient<T>().AddStandardResilienceHandler() gives sensible retry (3x with jitter backoff), timeout, circuit breaker, and total request timeout; or AddResilienceHandler("custom", builder => builder.AddRetry(...).AddCircuitBreaker(...).AddTimeout(...)).
    Before .NET 8: AddPolicyHandler(Policy.Handle<HttpRequestException>().WaitAndRetryAsync(...)) with Polly.
    CancellationTokens: pass HttpContext.RequestAborted or caller token to GetAsync/SendAsync - it aborts waiting, triggers cooperative cancellation through Polly (retry checks cancellation). Ensure Token flows via CreateAsync scope and use CancellationTokenSource for timeouts via AddTimeout. Always forward token to avoid orphaned requests.

- id: dotnet-log-templates-01
  answer: |
    Use message templates to enable structured logging. `logger.LogInformation("Order {OrderId} shipped", orderId)` treats string as template, extracts OrderId as named property, preserved by providers (Console JSON, Seq, OTel, Application Insights) for filtering/search (`OrderId=123`). If you interpolate (`$"Order {orderId} shipped"`), the value is baked into message string, cardinality explodes, cannot query by field, and incurs string allocation even when log level disabled (unless LoggerMessage). Templates keep message constant (low cardinality) and defer rendering until enabled, with semantic parameters.

- id: dotnet-log-levels-02
  answer: |
    In order lowest to highest: Trace (0) - verbose, Debug (1) - debugging, Information (2) - general flow, Warning (3) - abnormal but handled, Error (4) - failure, Critical (5) - crash. None (6) disables.
    Configured via appsettings Logging: { "LogLevel": { "Default":"Information", "Microsoft":"Warning", "Microsoft.EntityFrameworkCore":"Warning" }, "Logging": {"LogLevel":...}} or AddFilter. Per-category filter overrides default: e.g., `"Microsoft.AspNetCore.Routing":"Debug"` or code: builder.Logging.AddFilter("Microsoft.EntityFrameworkCore.Database.Command", LogLevel.Warning). Lower levels below min are suppressed without evaluating template args.

- id: dotnet-log-highperf-03
  answer: |
    [LoggerMessage] source generator (Microsoft.Extensions.Logging, .NET 6+) generates at compile time a static partial method that creates cached Action<ILogger,...> delegate with precomputed EventId and message template:
    ```csharp
    public static partial class Log {
      [LoggerMessage(EventId=1001, Level=LogLevel.Information, Message="Order {OrderId} shipped")]
      public static partial void OrderShipped(this ILogger logger, int orderId);
    }
    ```
    Preferred on hot paths because it avoids boxing, params array allocation, parsing template each call, string interpolation when disabled, and allocates zero when level disabled (ILogger.IsEnabled check inlined). Also enforces structured logging and gives strong-typed analyzer support vs LogInformation with boxing.

- id: dotnet-log-scopes-otel-04
  answer: |
    BeginScope attaches contextual properties to all logs within using block: `using (logger.BeginScope(new Dictionary<string,object>{["OrderId"]=id})) { logger.LogInfo("..."); }` - scope flows via AsyncLocal, providers include scope values (e.g., Console includes => Scope). Useful to add request/order/user correlation without passing param to every log call. Often used with RequestId.
    For distributed tracing/metrics: .NET exposes System.Diagnostics.Activity (W3C trace correlation) and System.Diagnostics.Metrics (Meter). OpenTelemetry .NET SDK collects these: AddOpenTelemetry().WithTracing(b=>b.AddAspNetCoreInstrumentation().AddHttpClientInstrumentation().AddOtlpExporter()) and WithMetrics similarly. Logs correlate via TraceId/SpanId. This gives cross-service traces, histograms, counters exported to Jaeger/Prometheus/OTLP.

```
