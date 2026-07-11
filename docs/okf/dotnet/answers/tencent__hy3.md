# dotnet knowledge benchmark — answers
# Model: novita-ai/tencent/hy3   (provider-stripped id: tencent/hy3)
# CLOSED-BOOK via ocode run -dir <empty> (no corpus access). corpus_rev 1.

- id: dotnet-di-lifetimes-01
  answer: |
    ASP.NET Core DI has three built-in lifetimes:
    - Transient: a NEW instance is created every time the service is resolved (every `GetService`/`GetRequiredService` call, and again for each constructor that takes it).
    - Scoped: one instance is created per scope. In an HTTP app there is one scope per request, so every resolution within that request returns the same instance; a new scope (e.g. a background loop) gets a new instance.
    - Singleton: a single instance is created once for the entire application lifetime and shared by all consumers.
    General rule: use transient for stateless services, scoped for per-request state (e.g. DbContext), and singleton only for thread-safe, stateless services.

- id: dotnet-di-captive-02
  answer: |
    A "captive dependency" is when a service with a SHORTER lifetime is injected into a service with a LONGER lifetime. The shorter-lived service then gets "captured" and effectively lives as long as its container — longer than intended.
    Injecting a scoped service into a singleton is the classic bug: the scoped service (e.g. a DbContext) is created once on first resolution and then reused for the whole app lifetime. This defeats scoping (state bleeds across requests), breaks thread-safety assumptions, and in some configurations DI will throw at startup (a "singleton consuming scoped" validation error). The scoped service should instead be resolved per-operation inside a created scope.

- id: dotnet-di-scope-in-singleton-03
  answer: |
    Inject `IServiceScopeFactory` (or `IServiceProvider` and call `CreateScope`). On each iteration, create a new scope (`using var scope = scopeFactory.CreateScope()`), then resolve the scoped service from the scope (`scope.ServiceProvider.GetRequiredService<IMyScoped>()` or `GetRequiredService<DbContext>()`), use it, and dispose the scope so the scoped service (and its DbContext) is disposed correctly. This gives a fresh, properly-scoped instance each loop without capturing it in the singleton.

- id: dotnet-di-keyed-04
  answer: |
    Keyed services — added in .NET 8 — let multiple implementations of one interface be registered and resolved by a key (string or enum).
    Registration: `services.AddKeyedSingleton<IService, ImplA>("a"); services.AddKeyedSingleton<IService, ImplB>("b");`
    Consumption: inject `IService` with `[FromKeyedServices("a")]` on a constructor parameter / method parameter (MVC/minimal APIs), or resolve manually via `provider.GetKeyedService<IService>("a")`. Without a key you still get the non-keyed registration, so mixing keyed and non-keyed is possible.

- id: dotnet-config-precedence-01
  answer: |
    `IConfiguration` is built from multiple providers added in order; when the same key exists in more than one source, the LAST source added WINS (later sources override earlier ones).
    A typical ASP.NET Core host builds (in increasing precedence): `appsettings.json` → `appsettings.{Environment}.json` → User Secrets (only in Development) → environment variables → command-line arguments. So env vars override appsettings, and command-line args override everything. `AddJsonFile`/`AddEnvironmentVariables` etc. are additive; order is what determines precedence.

- id: dotnet-config-options-interfaces-02
  answer: |
    - `IOptions<T>`: singleton lifetime, reads config once at startup and does NOT see later reloads. Can be injected anywhere.
    - `IOptionsSnapshot<T>`: scoped lifetime, re-reads config per scope/request and DOES reflect reloads (good for per-request consistency, e.g. in a web request). Injectable in scoped/transient contexts.
    - `IOptionsMonitor<T>`: singleton lifetime, also sees reloads and exposes `OnChange(callback)` for change notifications; can be injected anywhere, including singletons, to react to live config changes.
    So: snapshot = per-request reloadable view; monitor = singleton with live reload + change events; plain IOptions = frozen snapshot.

- id: dotnet-config-secrets-03
  answer: |
    Local dev secrets should live in the .NET Secret Manager — `dotnet user-secrets set`, stored in a `secrets.json` OUTSIDE the project tree (user profile), so they are never committed to source control. You enable it with `dotnet user-secrets init`.
    In production you must NOT use user secrets. Store secrets in environment variables, a secure store such as Azure Key Vault (often via the Key Vault configuration provider / managed identity), or your platform's secret manager. The constant rule: never commit plaintext secrets into the repo.

- id: dotnet-config-options-binding-04
  answer: |
    Binding: register with `services.Configure<MyOptions>(configuration.GetSection("MySection"))` (often inside `AddOptions<MyOptions>().Bind(...)`). Then inject `IOptions<MyOptions>` (or Snapshot/Monitor).
    Fail fast on invalid options: use `AddOptions<MyOptions>().Bind(config).ValidateDataAnnotations().ValidateOnStart();` (DataAnnotations attributes like `[Required]`) or `Validate(o => ...)` with a predicate. `ValidateOnStart()` makes the host throw during startup if validation fails, so bad config is caught immediately rather than later at runtime.

- id: dotnet-pipeline-order-01
  answer: |
    Middleware forms a linked pipeline/delegate chain: the request flows in through each `next()` call and the response flows back out in reverse. Because each piece can inspect/modify both directions, ORDER IS SEMANTIC, not cosmetic.
    Typical correct order: `UseRouting` (matches the route/endpoint) → `UseAuthentication` (who are you) → `UseAuthorization` (are you allowed) → `UseEndpoints`/Map handlers (runs the chosen endpoint). Auth must come AFTER routing (it needs to know the endpoint to apply the right policy) and BEFORE endpoints (must gate access before the handler runs). Putting them in the wrong place means auth is skipped or always fails.

- id: dotnet-pipeline-use-vs-map-02
  answer: |
    - `Use` (app.Use(...)): registers middleware that receives `(context, next)` and can choose to call `await next()` to pass control down the pipeline, then run code on the way back out.
    - `Run` (app.Run(...)): terminal middleware; it takes only the context and does NOT call `next()`, ending the pipeline.
    - `Map` (app.Map(...)): branches the pipeline by path/predicate — it forks to a separate sub-pipeline when the path matches, without affecting the main branch.
    "Short-circuiting" means a middleware writes the response and does NOT call `next()`, so downstream middleware never runs (e.g. authentication failing and returning 401 immediately).

- id: dotnet-pipeline-minimal-results-03
  answer: |
    In minimal APIs, the framework serializes the handler's return value into the response body automatically (200 + JSON for an object, or the appropriate status for an `IResult`). Returning a raw object works but gives you little control and no typed status codes.
    `Results` (`Results.Ok(...)`, `Results.NotFound()`, `Results.BadRequest()`, etc.) and `TypedResults` (e.g. `TypedResults.Ok<MyDto>(dto)`) let you explicitly set status codes, headers, and content negotiation, and — importantly — `TypedResults` preserves the return type for OpenAPI/swagger metadata and compile-time checking, whereas returning `object`/`IResult` loses that static typing.

- id: dotnet-pipeline-filters-vs-middleware-04
  answer: |
    Use MIDDLEWARE for cross-cutting concerns that apply broadly and early — auth, logging, compression, CORS, exception handling — operating on the raw `HttpContext` before routing.
    Use an ENDPOINT FILTER / MVC action filter when you need the ENDPOINT CONTEXT: access to route values, action/endpoint metadata, model-binding/validation state, arguments, and the ability to run code before AND after a specific handler and short-circuit it. Filters can see the action's parameters and result and can target specific endpoints/groups, which middleware (which only sees HttpContext) cannot do. Prefer filters for per-endpoint authorization/validation/caching logic.

- id: dotnet-efcore-context-lifetime-01
  answer: |
    A `DbContext` should be registered with a SCOPED lifetime (the EF Core default in ASP.NET Core), so one context per request.
    A `DbContext` is NOT thread-safe and NOT safe to share across threads or concurrent async operations because its internal change tracker, connection, and async state are designed for single-threaded/single-operation use. Concurrent use causes race conditions, "A second operation started on this context" exceptions, corrupted tracking, and cross-talk between operations. Always await one async call before starting another on the same context, or use a separate context per operation.

- id: dotnet-efcore-notracking-02
  answer: |
    The change tracker keeps references to loaded entities and watches them for modifications so that `SaveChanges` can persist only what changed (and so that repeated queries return the same tracked instance — identity map).
    Add `AsNoTracking()` to queries that are READ-ONLY: it tells EF not to track returned entities, which (a) avoids the overhead of tracking, (b) prevents accidental unintended updates, and (c) improves performance at scale. Use it for queries where you won't be saving the results back.

- id: dotnet-efcore-nplus1-03
  answer: |
    An `IQueryable` is deferred: composing a query (`Where`, `Select`, etc.) builds an expression tree but does NOT execute against the database until it is enumerated (e.g. `ToListAsync()`, `foreach`, `FirstOrDefaultAsync()`).
    N+1 happens when you enumerate a parent list (1 query) and then, for each item in a loop, lazily load a related entity (N more queries) — N+1 total round-trips. Fixes: use eager loading with `Include()`/`ThenInclude()`, project only the needed columns with `Select` (which can flatten related data into one query), or use explicit/`.LoadAsync()` loading. Also ensure the related navigation isn't silently lazy-loaded when tracking is disabled.

- id: dotnet-efcore-savechanges-tx-04
  answer: |
    Yes — a single `SaveChanges`/`SaveChangesAsync` call is transactional: EF wraps all the inserts/updates/deletes it generates in one database transaction and commits them atomically (all-or-nothing). Multiple separate `SaveChanges` calls are separate transactions.
    A migration is EF Core's way of versioning the database schema as code: it captures an incremental set of schema changes (adding a table/column, etc.) and can be applied/rolled back with `dotnet ef database update` / `migrations add`. Migrations keep the database schema in sync with the model and form a history you can deploy.

- id: dotnet-gc-generations-loh-01
  answer: |
    The .NET GC is generational with three managed heaps: Gen 0 (new, short-lived objects — collected very frequently and cheaply), Gen 1 (a buffer/survivor space between 0 and 2), and Gen 2 (long-lived objects — collected rarely, in a full/expensive GC). Objects promote from 0→1→2 as they survive collections.
    The Large Object Heap (LOH) holds objects ≥ ~85,000 bytes (large arrays/strings). The LOH is collected only during Gen 2 / full GCs and historically was not compacted, so it fragments. Large, SHORT-LIVED allocations are especially costly because they skip the cheap Gen 0 path, land straight on the LOH, and force expensive full Gen 2 collections to reclaim them — hammering throughput. Reuse large buffers (e.g. ArrayPool) instead of allocating per request.

- id: dotnet-gc-dispose-finalizer-02
  answer: |
    `IDisposable.Dispose()` is DETERMINISTIC, programmer-controlled cleanup — you call it (via `using`) to release unmanaged resources and/or expensive managed ones (file handles, sockets, DB connections) promptly.
    A finalizer (`~MyType()`) is NON-deterministic backup cleanup run by the GC before an object with unmanaged resources is collected; it should only release truly unmanaged resources and is a safety net. You need a finalizer ONLY when your type directly holds unmanaged memory/handles and must guarantee release even if `Dispose` is never called (and you should implement the full `Dispose(bool)` pattern). `IAsyncDisposable`/`DisposeAsync` adds an ASYNC cleanup path for resources whose release is itself async (e.g. flushing/network streams), so you aren't blocking threads during teardown.

- id: dotnet-gc-span-arraypool-03
  answer: |
    - `Span<T>` / `ReadOnlySpan<T>`: a stack-only, ref-struct view over contiguous memory (array, stack, or native) that lets you slice/operate without copying or allocating — avoids heap allocations and array copies.
    - `stackalloc`: allocates a buffer on the call stack (not the heap) for short-lived spans, avoiding GC pressure (must be used inside a `Span<T>`).
    - `ArrayPool<T>`: a shared, thread-safe pool of large arrays you rent (`ArrayPool<T>.Shared.Rent(n)`) and return, instead of allocating a new large array each time — avoids LOH pressure and GC churn.
    All three reduce/avoid heap allocations and GC work on hot paths.

- id: dotnet-gc-struct-vs-class-04
  answer: |
    A `struct` is a VALUE type: it is typically allocated inline where it's declared (on the stack, or inside a containing object/array) and is COPIED by value on assignment/passing. A `class` is a REFERENCE type: the instance lives on the managed heap and you pass around a reference (4/8 bytes) to it.
    Value types avoid heap allocation/GC when used locally, but copying large structs is expensive. "Boxing" is wrapping a value type (e.g. an `int`) into an `object`/interface reference so it can live on the heap — this allocates and is a common hidden-allocation/perf pitfall (and unboxing copies it back).

- id: dotnet-json-stj-defaults-01
  answer: |
    System.Text.Json serializes property names by their .NET name by default, but in ASP.NET Core the default `JsonOptions` already uses camelCase (`JsonNamingPolicy.CamelCase`). To set it explicitly: `new JsonSerializerOptions { PropertyNamingPolicy = JsonNamingPolicy.CamelCase }` (and in ASP.NET Core: `builder.Services.Configure<JsonOptions>(o => o.JsonSerializerOptions.PropertyNamingPolicy = JsonNamingPolicy.CamelCase)`).
    To override a SINGLE property's serialized name, annotate it: `[JsonPropertyName("order_id")]` — that name is then used regardless of the global policy.

- id: dotnet-json-sourcegen-02
  answer: |
    System.Text.Json source generation uses a Roslyn source generator (annotate a class/record with `[JsonSerializable(typeof(MyType))]` inside a `[JsonSourceGenerationOptions(...)]` partial `JsonSerializerContext`) to emit serialization/deserialization code at COMPILE time instead of using runtime reflection.
    Why it matters: (1) performance — no runtime reflection, faster startup and throughput, especially in hot loops; (2) trim- and Native-AOT-safe — reflection-based serialization can't be statically analyzed/AOT-compiled, so source gen is required for trimmed and Native AOT apps; (3) smaller footprint because only configured types are included.

- id: dotnet-json-stj-vs-newtonsoft-03
  answer: |
    System.Text.Json (STJ) is the modern default: faster, lower-allocation, asynchronously stream-friendly, and works with trimming/Native AOT. It is stricter (no duplicate properties, no comments/quotation quirks by default, case-insensitive matching off unless set) and has a smaller feature surface.
    Newtonsoft.Json (Json.NET) is more flexible/forgiving: rich attribute set, `TypeNameHandling`/polymorphism, tolerant parsing (comments, trailing commas, relaxed quotes), deep LINQ-to-JSON (`JObject`) support, and many legacy behaviors. Trade-offs: slower, higher allocation, larger, not AOT-friendly. Migrate to STJ for new code; keep Newtonsoft for compatibility or when you need its specific leniencies.

- id: dotnet-json-options-reuse-04
  answer: |
    A `JsonSerializerOptions` instance is relatively expensive to construct (it caches metadata about types and converters). Creating a new one per call (e.g. per request) causes repeated allocation and metadata recomputation, hurting throughput. Create it ONCE (a static/shared instance) and reuse it everywhere.
    Polymorphic (base/derived) serialization: by default STJ serializes only the DECLARED/base type's properties and does NOT emit derived-type data. To support it, use attribute-based polymorphism — annotate the base type with `[JsonDerivedType(typeof(Derived), "discriminator")]` (introduced in .NET 7) so the serializer knows the derived types and writes a discriminator; without this, derived fields are silently dropped.

- id: dotnet-http-socket-exhaustion-01
  answer: |
    `new HttpClient()` per request causes socket exhaustion because each `HttpClient` holds underlying sockets (and connection pool state) that are NOT released when you dispose it — the sockets stay in `TIME_WAIT` until garbage collection, because HttpClient is meant to be reused (it's effectively a shared, pooled facade). Creating and dropping one per request floods the system with lingering sockets, exhausting the ephemeral port range ("Unable to connect because the socket is exhausted").
    `IHttpClientFactory` solves this by managing a pool of `HttpClient`/handler instances with a controlled lifetime and rotating handlers, so sockets are reused and disposed safely. You register with `AddHttpClient` and inject `IHttpClientFactory`, calling `CreateClient(...)` per use.

- id: dotnet-http-typed-clients-02
  answer: |
    - Named clients: `services.AddHttpClient("github")` and then `factory.CreateClient("github")` — you configure by name and must know the name everywhere.
    - Typed clients: `services.AddHttpClient<IGitHubClient, GitHubClient>()` where `GitHubClient` takes an `HttpClient` in its constructor and encapsulates the API calls. DI injects the typed client with its own pre-configured HttpClient.
    Prefer a typed client because it wraps the HttpClient logic in a strongly-typed, testable service (no magic name strings, centralized base address/headers/retry config, easy to mock/inject), and DI manages the HttpClient lifetime for you.

- id: dotnet-http-lifetime-dns-03
  answer: |
    Even with IHttpClientFactory, handlers (and their cached DNS/connection pools) are pooled and, by default, reused for a lifetime (e.g. 2 minutes in older defaults). If a handler lives too long, the pooled connections keep resolving to an OLD IP — so after a DNS change / service scale event, the client keeps hitting stale endpoints.
    The fix is to bound the pooled connection lifetime so DNS is re-resolved periodically: configure a `SocketsHttpHandler` with `PooledConnectionLifetime` (e.g. 2 minutes) via `AddHttpClient(...).ConfigurePrimaryHttpMessageHandler(() => new SocketsHttpHandler { PooledConnectionLifetime = TimeSpan.FromMinutes(2) })`. This rotates connections before they go stale without churning handlers too aggressively.

- id: dotnet-http-resilience-04
  answer: |
    In modern .NET use the resilience extensions: `Microsoft.Extensions.Http.Resilience` (built on the Polly-derived `Microsoft.Extensions.Resilience` pipeline) or Polly directly via `AddPolicyHandler` / `AddResilienceHandler`. You compose policies for Retry (with backoff), Timeout, and Circuit Breaker (stop calling a failing dependency, half-open to test recovery). These can be added per named/typed client.
    Cancellation tokens (`CancellationToken`) are threaded into every HTTP call: they let the caller cancel an in-flight request (e.g. user navigated away, upstream timeout) so the HttpClient aborts promptly instead of holding a socket, and they integrate with the Timeout policy.

- id: dotnet-log-templates-01
  answer: |
    `logger.LogInformation("Order {OrderId} shipped", orderId)` uses a message TEMPLATE with a named placeholder, so the value (`orderId`) is captured as STRUCTURED, strongly-typed data alongside the message — enabling filtering/aggregation ("show all shipped orders", "count by OrderId") in sinks like Seq/ELK/Application Insights.
    String interpolation (`$"Order {orderId} shipped"`) bakes the value into a single pre-rendered string, destroying the structure, forcing the value to a string, and preventing querying by field. Templates keep the raw value and also avoid unnecessary string formatting when the log level is suppressed.

- id: dotnet-log-levels-02
  answer: |
    The ILogger levels, from least to most severe (and `None` to disable): Trace → Debug → Information → Warning → Error → Critical → None.
    Per-category filtering is configured in `appsettings.json` under `"Logging": { "LogLevel": { "<Category>" : "<Level>" } }` and/or `"Logging": { "LogLevel": { "Default": "Information" } }`. To quiet EF Core: `"Microsoft.EntityFrameworkCore": "Warning"` (or `Microsoft.EntityFrameworkCore.Database.Command": "Warning"` to silence SQL logging while keeping other EF warnings). Higher-severity logs above the set level are dropped; providers can also filter.

- id: dotnet-log-highperf-03
  answer: |
    The `[LoggerMessage]` source generator (in `Microsoft.Extensions.Logging.Abstractions`) lets you define logging methods on a partial class/struct with attributes like `[LoggerMessage(EventId, LogLevel.Information, "Order {OrderId} shipped")]`. The compiler generates a highly optimized logging method.
    Why preferred on hot paths: it avoids runtime template parsing, avoids boxing of value-type arguments (the generated code uses strongly-typed parameters and only calls the underlying logger when the level is enabled), and avoids allocating the formatted string when the message is suppressed. This is substantially faster and allocates less than `LogInformation("...", arg)` in tight loops, plus it gives compile-time checking of the message template.

- id: dotnet-log-scopes-otel-04
  answer: |
    Logging scopes (`logger.BeginScope("Request {RequestId}", id)` or a `IEnumerable<KeyValuePair>` state) attach shared contextual properties to all log entries emitted within that scope — e.g. a correlation/request ID — so related logs from one operation are grouped and queryable without repeating fields on every line.
    At a high level .NET exposes distributed tracing and metrics through OpenTelemetry and `System.Diagnostics`: activities are created via `ActivitySource`/`Activity` (traces, with context propagated across service boundaries via W3C traceparent), and metrics via `Meter`/`Instrument` (counters, histograms). The OpenTelemetry .NET SDK exports this telemetry to backends (Jaeger, Prometheus, Azure Monitor, etc.). Logs, traces, and metrics share a common context (e.g. trace/span IDs) for end-to-end correlation.
