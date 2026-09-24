- id: dotnet-di-lifetimes-01
  answer: |
    Transient services create a new instance every time they are resolved. Scoped services create one instance per dependency-injection scope; an ASP.NET Core request normally has its own scope, so all resolutions within that request share the scoped instance. Singleton services create one instance per root service provider, usually one per application, and reuse it for the application’s lifetime. Disposal normally occurs when the relevant scope or root provider is disposed.

- id: dotnet-di-captive-02
  answer: |
    A captive dependency occurs when a longer-lived service holds a reference to a shorter-lived service. If a singleton injects a scoped service, the singleton effectively captures that scoped instance for the application’s lifetime. After the original request scope ends, the captured service may be disposed while the singleton still uses it, causing stale data, ObjectDisposedException, or unsafe concurrent access. The lifetime mismatch is the bug; either make the consumer scoped or create a new scope where the scoped service is needed.

- id: dotnet-di-scope-in-singleton-03
  answer: |
    Inject IServiceScopeFactory into the singleton background service. On each iteration, create an async scope, resolve the scoped service or DbContext from that scope, perform the work, and dispose the scope afterward. For example, the loop can use await using (var scope = scopeFactory.CreateAsyncScope()), then scope.ServiceProvider.GetRequiredService<MyScopedService>() and await its work. This prevents a DbContext or other scoped service from being retained across iterations or concurrent operations.

- id: dotnet-di-keyed-04
  answer: |
    Keyed services were introduced in .NET 8. Register implementations with AddKeyedSingleton, AddKeyedScoped, or AddKeyedTransient, supplying a key, such as "primary" and "secondary". Consume them through IKeyedServiceProvider with GetRequiredKeyedService<T>("primary") or GetKeyedService<T>("secondary"). Where the hosting integration supports it, a keyed constructor parameter can also be declared with FromKeyedServices("primary"). Unkeyed registrations can coexist with keyed ones.

- id: dotnet-config-precedence-01
  answer: |
    A typical WebApplication or generic host composes configuration in this approximate order: appsettings.json, appsettings.{Environment}.json, user secrets during development, environment variables, and command-line arguments. Later providers override earlier providers when the same key occurs, so a command-line value normally wins over an environment variable, which wins over an appsettings value. The exact set of providers can be changed by the application.

- id: dotnet-config-options-interfaces-02
  answer: |
    IOptions<T> is singleton-scoped and returns a configuration snapshot that is cached after first resolution; it does not update when configuration reloads. IOptionsSnapshot<T> is scoped and produces a consistent value for each scope, with reloads visible in subsequently created scopes. IOptionsMonitor<T> is singleton-scoped, exposes the current value across reloads, and provides an OnChange callback. IOptions and IOptionsMonitor can be injected into singleton, scoped, or transient services; IOptionsSnapshot should be injected into scoped or transient services because it is scoped.

- id: dotnet-config-secrets-03
  answer: |
    For local development, use .NET user secrets, initialized with dotnet user-secrets, or another untracked local secret mechanism. User secrets are stored outside the project rather than in a committed appsettings file. In production, use environment variables, a deployment secret, or a managed secret store such as a cloud secret manager or Azure Key Vault, and inject them through configuration. Secrets should never be committed, should have controlled access and rotation, and should be treated differently from ordinary application settings.

- id: dotnet-config-options-binding-04
  answer: |
    Read a configuration section and bind it to an options type, for example builder.Services.AddOptions<MyOptions>().Bind(builder.Configuration.GetSection("MyOptions")). Consumers then receive IOptions<MyOptions>, MyOptions, or a validated equivalent. Add data-annotation and custom validation, then call ValidateOnStart() so validation occurs when the host starts rather than when the options are first used. An invalid section or value can therefore stop startup with a clear configuration error.

- id: dotnet-pipeline-order-01
  answer: |
    The request travels through middleware in the order the middleware was registered, and each middleware normally calls the next middleware. UseRouting should run before middleware that needs endpoint metadata. UseAuthentication should run before UseAuthorization so authentication can populate the user and claims, and UseAuthorization should run after routing and before the endpoint executes so endpoint authorization metadata and policies are available. The endpoint middleware is the terminal part of the relevant request path.

- id: dotnet-pipeline-use-vs-map-02
  answer: |
    Use adds a middleware component to the current pipeline; it can inspect or modify the request, perform work, and call next. Run adds a terminal delegate that handles the request without automatically calling another delegate, so it normally ends that pipeline. Map creates a branch selected by a request path, such as Map("/api", ...), and executes the branch pipeline for a matching request. A middleware short-circuits when it deliberately does not call next and returns or writes the response itself; downstream middleware and the endpoint are then skipped.

- id: dotnet-pipeline-minimal-results-03
  answer: |
    A minimal-API handler can return an IResult, one of the helper results from Results or TypedResults, or an ordinary value. An ordinary object is normally serialized as JSON, with a default success response, but it gives less explicit control over status codes, headers, content types, and response shape. Results.Ok, Results.NotFound, Results.Created, Results.ValidationProblem, and similar helpers construct HTTP-specific results. TypedResults returns concrete, compile-time-known result types, improving type inference and API metadata generation and making it easier to use source generation and Native AOT.

- id: dotnet-pipeline-filters-vs-middleware-04
  answer: |
    Use middleware for broad, endpoint-independent concerns such as request logging, authentication infrastructure, compression, response headers, and exception handling around the entire pipeline. Use an endpoint or MVC filter for concerns tied to a particular endpoint, action, or resource. A filter can participate in endpoint-specific ordering, inspect endpoint metadata, access model-bound arguments, short-circuit before the action, and transform or replace the action result. Middleware can also write or change the response, but it does not naturally receive the action’s typed arguments and result pipeline.

- id: dotnet-efcore-context-lifetime-01
  answer: |
    Register a DbContext as scoped. In a web application that normally means one context per request; in a background worker, create one scope and one context for each unit of work. A DbContext is not thread-safe because it contains mutable change-tracking state, identity maps, cached metadata, and connection or transaction state. Concurrent queries or mutations can race and corrupt tracking state or produce inconsistent results. Asynchronous methods do not make a DbContext safe to use concurrently; keep each context within one logical operation or async flow.

- id: dotnet-efcore-notracking-02
  answer: |
    EF Core’s change tracker records entities returned by queries, their keys and relationships, original and current values, and changes made to them. It can detect Added, Modified, Deleted, and Unmodified states and persist those changes during SaveChanges. AsNoTracking is useful for read-only queries when the returned entities do not need change tracking, identity resolution, or update detection, because it reduces tracking overhead, memory use, and change-detection work. For an update, attach the entity and explicitly mark it as modified, or query it with tracking enabled.

- id: dotnet-efcore-nplus1-03
  answer: |
    An IQueryable represents a composable query expression. It is normally deferred until the query is enumerated, so the database command is not necessarily sent when the queryable is constructed. ToListAsync materializes the results and executes the query at that point. With lazy loading, materializing a collection of parent entities and then accessing a navigation property can issue one additional query per parent, producing N+1 queries. Fix it with eager loading using Include and ThenInclude, a projection that fetches the required data in one query, a join, or an appropriate split-query strategy, while avoiding unnecessary lazy loading.

- id: dotnet-efcore-savechanges-tx-04
  answer: |
    Normally, one SaveChangesAsync call is transactional: EF Core executes the tracked inserts, updates, and deletes as a unit, commits them when successful, and rolls them back if an error occurs, subject to the provider and configuration. If the caller already owns a transaction, multiple SaveChanges calls can participate in that transaction. A migration is different: it is a versioned, usually code-based description of schema changes, such as creating a table or adding a column. Migrations update database structure and are recorded in the migrations history table; they are not the data-saving operation performed by SaveChangesAsync.

- id: dotnet-gc-generations-loh-01
  answer: |
    .NET uses a generational garbage collector based on the observation that most objects die young. Gen 0 collects new allocations frequently, objects that survive can move to Gen 1, and longer-lived survivors can move to Gen 2, which is collected less often. The Large Object Heap holds large allocations, generally payloads above roughly 85,000 bytes, and is not compacted by the normal compacting gen-2 collection. Large, short-lived allocations are costly because they consume a sizable LOH allocation, can cause memory pressure and fragmentation, and remain until a later LOH collection even though they become unreachable quickly.

- id: dotnet-gc-dispose-finalizer-02
  answer: |
    IDisposable provides deterministic release through Dispose, usually from a using statement, and should be implemented for managed and unmanaged resources whose lifetime the caller controls. A finalizer runs nondeterministily after an object becomes unreachable and at the discretion of the garbage collector; it is a last-resort safety mechanism, normally for unmanaged resources, not a replacement for Dispose. The standard dispose pattern protects Dispose(bool) and coordinates it with the finalizer. IAsyncDisposable adds DisposeAsync for resources that require asynchronous cleanup, and callers can use await using; it does not make finalization asynchronous.

- id: dotnet-gc-span-arraypool-03
  answer: |
    Span<T> is a view over existing memory and can avoid allocating a new array or copying data when a short-lived view is sufficient. stackalloc allocates unmanaged storage on the stack for a limited, usually short-lived scope, avoiding a garbage-collected heap object; it is unsuitable for large or long-lived buffers and has restrictions on managed types and async use. ArrayPool<T>.Shared.Rent reuses previously allocated arrays, avoiding a new array allocation for each operation; the caller should return the array when finished and must not assume it is zeroed.

- id: dotnet-gc-struct-vs-class-04
  answer: |
    A struct is a value type whose data is stored directly in a variable, field, collection element, or containing value, so ordinary local use generally does not require a separate heap allocation. Assignments and many parameter passing operations copy the struct. A class is a reference type; each instance is normally allocated on the managed heap, and multiple references can share the same object. Boxing is converting a value type to object, an interface, or another reference representation; it creates a separate heap object containing a copy of the value, adding allocation and potentially losing direct access to the original value.

- id: dotnet-json-stj-defaults-01
  answer: |
    Set JsonSerializerOptions.PropertyNamingPolicy to JsonNamingPolicy.CamelCase, or use JsonSerializerDefaults.Web for the common web defaults, to serialize CLR property names as camelCase. Override an individual property with the JsonPropertyName attribute, for example [JsonPropertyName("order_id")]. Property naming policy changes CLR property names; dictionary keys are controlled separately by DictionaryKeyPolicy if a dictionary-key policy is desired.

- id: dotnet-json-sourcegen-02
  answer: |
    System.Text.Json source generation uses a JsonSerializerContext marked with JsonSerializable attributes. The compiler generates serialization metadata and serializers for the specified types at build time, so the runtime does not need to discover and reflect over those types. This can improve startup time, throughput, allocations, and memory use for supported scenarios. It also preserves the type metadata needed when trimming or publishing Native AOT, where runtime reflection may be unavailable or incomplete. Applications then serialize through the generated context or use it as the options type-info resolver.

- id: dotnet-json-stj-vs-newtonsoft-03
  answer: |
    System.Text.Json is built into modern .NET, is the default for ASP.NET Core, avoids a separate package dependency, and generally offers strong throughput and startup performance for common object graphs. It also supports source generation, trimming, and Native AOT. Newtonsoft.Json is an external package with a long-established ecosystem, extensive third-party converters and conventions, and mature dynamic APIs such as JObject and JToken, so existing systems may depend on its behavior. Both libraries support custom converters and advanced scenarios, but their defaults, converter behavior, and edge-case compatibility differ; performance should be measured for the actual workload rather than assumed.

- id: dotnet-json-options-reuse-04
  answer: |
    JsonSerializerOptions caches serializer metadata and related state, and using one instance repeatedly avoids rebuilding that work and allocating options for each request. Configure it during application startup, share it safely once configured, and do not mutate it while it is being used. For polymorphic serialization, modern System.Text.Json supports attributes such as JsonPolymorphic and JsonDerivedType to declare a base type’s derived types and discriminator. The serializer writes type information and uses it to select the appropriate derived type during deserialization. Custom polymorphism or unusual discriminator formats can also be implemented with a custom converter or resolver configuration.

- id: dotnet-http-socket-exhaustion-01
  answer: |
    Constructing and disposing a new HttpClient for every request generally creates a new handler and connection pool. The request pays for fresh DNS, TCP, and TLS setup, and rapid connection teardown can leave many sockets in TIME_WAIT, exhaust ephemeral ports, or hit connection limits. IHttpClientFactory manages and pools handlers, reuses connections, rotates handler resources, and avoids creating a new socket stack for every logical request. Clients created by the factory should be short-lived or injected, while the factory manages the underlying handler lifetime.

- id: dotnet-http-typed-clients-02
  answer: |
    A named client is registered with AddHttpClient("name", options) and obtained by calling IHttpClientFactory.CreateClient("name"), allowing different base addresses, headers, and policies to be selected by name. A typed client is a class that receives an injected HttpClient and is registered with AddHttpClient<MyApiClient>; the factory supplies the configured client while DI manages the typed service. A typed client is usually preferable for a dedicated external API because it gives compile-time type safety and centralizes the base address, headers, serialization, and API calls, while named clients are useful when configurations are selected dynamically.

- id: dotnet-http-lifetime-dns-03
  answer: |
    IHttpClientFactory rotates its managed handlers after a default lifetime, commonly two minutes, which eventually allows new DNS resolutions, but an existing connection can continue using an old address during that interval. The lifetime of HttpClient itself is not the DNS-refresh mechanism. Configure a SocketsHttpHandler as the primary handler and set a finite PooledConnectionLifetime, causing old pooled connections to be retired and subsequent requests to establish fresh connections. Handler lifetime and connection-pool lifetime can be tuned together, with the values chosen according to expected traffic and DNS-change frequency.

- id: dotnet-http-resilience-04
  answer: |
    Use the Microsoft.Extensions.Http.Resilience package and AddStandardResilienceHandler for a standard retry, timeout, and circuit-breaker policy, or compose a custom resilience handler with the newer resilience APIs and Polly. Retries should generally target transient transport failures and selected statuses such as 408, 429, and appropriate 5xx responses, use backoff with jitter, and be limited to operations that are safe to repeat. A total or per-attempt timeout bounds work, and a circuit breaker stops calls after repeated failures and later permits probe requests. Pass the caller’s CancellationToken to SendAsync and link it with timeout and policy tokens; preserve caller cancellation and generally do not retry an operation canceled by the caller.

- id: dotnet-log-templates-01
  answer: |
    A logging template keeps the message and named values as structured data, so the value is captured as a property such as OrderId rather than being permanently embedded in text. Logging providers can filter, query, aggregate, and render these properties independently, including in JSON output. The template also lets the logger avoid formatting and string allocation entirely when the log level is disabled. Interpolating the value constructs a string even when the message will be discarded and makes structured analysis harder.

- id: dotnet-log-levels-02
  answer: |
    The standard ILogger levels, from least to most severe, are Trace, Debug, Information, Warning, Error, Critical, and None. None is normally used to disable logging for a category rather than as a normal severity. Configure a default threshold and per-category overrides in appsettings.json under Logging:LogLevel, such as setting Microsoft.EntityFrameworkCore to Warning. Equivalent code configuration is builder.Logging.AddFilter("Microsoft.EntityFrameworkCore", LogLevel.Warning), and configuration can be supplied through environment variables such as Logging__LogLevel__Microsoft.EntityFrameworkCore=Warning.

- id: dotnet-log-highperf-03
  answer: |
    LoggerMessage.Define and the newer LoggerMessage source generator let you declare a partial logging method with an attribute such as [LoggerMessage], specifying its level, event ID, and message template. The compiler generates optimized logging code with a strongly typed delegate and precomputed message handling, avoiding repeated generic log-method parsing, metadata lookup, and avoidable allocations. It is especially useful for hot paths, although ordinary ILogger calls are appropriate when the extra definition is not justified.

- id: dotnet-log-scopes-otel-04
  answer: |
    BeginScope creates a structured, nested logging scope containing values such as a request ID, user ID, order ID, or operation name. Those values can be included in every log written inside the scope, and the scope is normally disposed with using; scope state generally flows across asynchronous calls. OpenTelemetry uses System.Diagnostics.Activity and ActivitySource for distributed traces and spans, with W3C trace-context propagation, and System.Diagnostics.Metrics with Meter for counters, histograms, and other instruments. The OpenTelemetry SDK and instrumentation packages collect these signals and export them to configured backends; scopes and Activity IDs are often connected for log-to-trace correlation.
