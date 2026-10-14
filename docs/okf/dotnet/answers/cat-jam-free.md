# cat-jam-free — closed-book answers for dotnet (opencode-go, 2026-10-14); answerer saw only _prompts/dotnet.md
# Delivery: one `ocode run -m opencode-go/cat-jam-free -effort med` session, sheet attached via -f, empty scratch cwd;
# session audited in sqlite: 1 user + 1 assistant message, 0 tool calls.

- id: dotnet-di-lifetimes-01
  answer: |
    Transient creates a new instance every time it is resolved; scoped creates one instance per DI scope (same within that scope, new across scopes); singleton creates one instance for the application/root provider and reuses it for all resolutions.

- id: dotnet-di-captive-02
  answer: |
    A captive dependency is a longer-lived service capturing a shorter-lived one, e.g. a singleton holding a scoped service. The singleton keeps the first scoped instance forever, so state leaks across requests and thread-safety rules for the scoped service (such as DbContext) are violated.

- id: dotnet-di-scope-in-singleton-03
  answer: |
    Inject IServiceScopeFactory into the singleton, then create a scope per iteration (await using var scope = scopeFactory.CreateAsyncScope()), resolve the scoped service inside it, use it, and let the scope dispose. Never inject the scoped service directly into the singleton.

- id: dotnet-di-keyed-04
  answer: |
    Keyed services, introduced in .NET 8. Register with AddKeyedTransient/Scoped/Singleton, consume via constructor parameter attribute [FromKeyedServices("key")] or provider.GetKeyedService<T>("key").

- id: dotnet-config-precedence-01
  answer: |
    In a typical host, later sources override earlier ones: appsettings.json, then appsettings.{Environment}.json, then user secrets in Development, then environment variables, then command-line arguments. Thus command-line wins, followed by environment variables.

- id: dotnet-config-options-interfaces-02
  answer: |
    IOptions<T> is effectively singleton, computed once and not reload-aware; IOptionsSnapshot<T> is scoped, reads current bound config when the scope is created; IOptionsMonitor<T> is singleton, exposes CurrentValue and change notifications. Inject Snapshot only into scoped/transient, not singletons; Monitor and Options can be injected anywhere.

- id: dotnet-config-secrets-03
  answer: |
    Local development secrets should live in User Secrets (outside source control, under the user profile) during Development. Production should use environment variables, a secret manager/vault such as Azure Key Vault, or platform-provided secrets, with appropriate access control and rotation.

- id: dotnet-config-options-binding-04
  answer: |
    Bind with services.Configure<MyOptions>(config.GetSection("MyOptions")) or AddOptions<MyOptions>().BindConfiguration("MyOptions"). For fail-fast validation, call ValidateDataAnnotations and ValidateOnStart so invalid options throw at startup.

- id: dotnet-pipeline-order-01
  answer: |
    Middleware runs as an onion: requests flow through middleware in registration order, responses flow back in reverse. UseRouting must run before UseAuthentication/UseAuthorization because auth needs endpoint metadata and route information; auth must run before endpoints execute.

- id: dotnet-pipeline-use-vs-map-02
  answer: |
    Use adds middleware that calls next and can short-circuit by not calling next; Run adds terminal middleware that ends the pipeline; Map branches the pipeline by path. Short-circuiting means writing a response without invoking subsequent middleware.

- id: dotnet-pipeline-minimal-results-03
  answer: |
    Non-IResult return values are mapped by the framework, such as strings to text/plain and objects to JSON with 200. Returning IResult executes it explicitly. Results/TypedResults provide explicit status codes, content types, streaming, and better testability; TypedResults also preserves static typing and richer OpenAPI metadata.

- id: dotnet-pipeline-filters-vs-middleware-04
  answer: |
    Use middleware for global HTTP cross-cutting concerns; use endpoint/action filters for handler-specific concerns. Filters can inspect and modify route values, bound arguments, and results, and integrate with model binding in ways generic middleware cannot.

- id: dotnet-efcore-context-lifetime-01
  answer: |
    DbContext should be scoped, typically one per request. It is not thread-safe and must not be shared across threads or concurrent async operations because its change tracker and underlying connection are not safe for concurrent use.

- id: dotnet-efcore-notracking-02
  answer: |
    The change tracker records entity state and detects changes for persistence. Use AsNoTracking for read-only queries to reduce memory and CPU overhead and to avoid accidentally updating queried entities.

- id: dotnet-efcore-nplus1-03
  answer: |
    IQueryable is deferred and composes SQL until materialized; ToListAsync (or similar) executes it once. N+1 arises when a parent query is followed by one child query per item, often in a loop; fix it with Include, eager loading, or projection in a single query.

- id: dotnet-efcore-savechanges-tx-04
  answer: |
    A single SaveChangesAsync call is wrapped in a transaction by most providers, so its changes succeed or fail together. A migration is a versioned, up/down schema change used to evolve the database schema from code.

- id: dotnet-gc-generations-loh-01
  answer: |
    Gen 0 collects short-lived objects, gen 1 buffers survivors, and gen 2 holds long-lived objects. The Large Object Heap holds objects around 85,000 bytes or more and is collected less efficiently. Large short-lived allocations are costly because they pressure the LOH and can trigger expensive gen 2 collections and fragmentation.

- id: dotnet-gc-dispose-finalizer-02
  answer: |
    IDisposable provides deterministic cleanup of unmanaged resources; a finalizer is a nondeterministic safety net. Use IDisposable when cleanup should be prompt, and a finalizer only when the class directly owns unmanaged resources that might be leaked. IAsyncDisposable adds asynchronous cleanup for resources requiring async disposal.

- id: dotnet-gc-span-arraypool-03
  answer: |
    Span<T> provides safe stack/heap memory views without allocating; stackalloc allocates small buffers on the stack; ArrayPool<T> rents reusable arrays to avoid repeated allocations. Together they reduce heap allocations and GC pressure.

- id: dotnet-gc-struct-vs-class-04
  answer: |
    A class is a reference type allocated on the heap; a struct is a value type usually stored inline and not heap-allocated. Boxing occurs when a value type is converted to object or an interface, causing a heap allocation and copy.

- id: dotnet-json-stj-defaults-01
  answer: |
    Set JsonSerializerOptions.PropertyNamingPolicy to JsonNamingPolicy.CamelCase. Override one property with the [JsonPropertyName("customName")] attribute.

- id: dotnet-json-sourcegen-02
  answer: |
    Source generation generates serialization code at compile time from a JsonSerializerContext. It avoids runtime reflection, improving startup and throughput, and supports trimming and Native AOT where reflection is limited.

- id: dotnet-json-stj-vs-newtonsoft-03
  answer: |
    System.Text.Json is faster and more allocation-efficient and supports modern .NET features and AOT. Newtonsoft.Json is more flexible, with broader converters, dynamic JSON, reference preservation, and older compatibility, but has more runtime reflection and allocation cost.

- id: dotnet-json-options-reuse-04
  answer: |
    Reuse JsonSerializerOptions because repeated options create and cache metadata repeatedly, harming performance. For polymorphism, use attributes such as [JsonPolymorphic] and [JsonDerivedType], or configure PolymorphismOptions, to emit and read a type discriminator.

- id: dotnet-http-socket-exhaustion-01
  answer: |
    Creating and disposing HttpClient per request disposes the underlying handler but can leave sockets in TIME_WAIT, exhausting ports. IHttpClientFactory pools and reuses HttpMessageHandlers, centralizes configuration, and manages handler lifetime to avoid socket exhaustion.

- id: dotnet-http-typed-clients-02
  answer: |
    Named clients are created by name through the factory; typed clients are classes injected with an HttpClient and registered via AddHttpClient<T>. Typed clients are preferred for encapsulation, testability, and strongly typed APIs.

- id: dotnet-http-lifetime-dns-03
  answer: |
    Pooled handlers live beyond the HttpClient, so DNS changes may not be seen until the handler lifetime expires. Set a shorter handler lifetime, or use SocketsHttpHandler with a PooledConnectionLifetime so connections and DNS are refreshed periodically.

- id: dotnet-http-resilience-04
  answer: |
    Use Microsoft.Extensions.Http.Resilience or Polly pipelines to add retry, timeout, and circuit-breaker policies to HttpClient. Pass a CancellationToken through requests so cancellation propagates to the underlying network operation.

- id: dotnet-log-templates-01
  answer: |
    Message templates preserve named placeholders as structured properties, improving log querying and avoiding eager string interpolation/allocation. Interpolating first loses that structure.

- id: dotnet-log-levels-02
  answer: |
    Levels in order are Trace, Debug, Information, Warning, Error, Critical, None. Configure per-category levels in appsettings or with AddFilter, such as setting Microsoft.EntityFrameworkCore to Warning.

- id: dotnet-log-highperf-03
  answer: |
    The LoggerMessage source generator turns a partial logging method into compile-generated code that avoids template parsing and boxing. It is preferred on hot paths for lower allocation and better throughput.

- id: dotnet-log-scopes-otel-04
  answer: |
    BeginScope adds contextual properties to logs produced within that scope. Distributed tracing and metrics are exposed through OpenTelemetry, using ActivitySource for traces and Meter for metrics, often configured via the OpenTelemetry SDK and exporters.
