# Dotnet — Kaizen blind answer sheet (questions only)

> **CLOSED-BOOK.** Answer every question from your own knowledge alone. You MUST
> NOT open, search, or otherwise access the Kaizen corpus — `questions.yaml`,
> `questions.md`, `scores/`, `derived/`, `meta.yaml`, or any file in this repo —
> nor look the answers up online. Doing so invalidates the evaluation.
>
> Answer each question **independently** (treat every item as a fresh context —
> no memory of earlier answers). If you are unsure, say so; do not guess to look
> complete. This measures what you actually know, not what you can retrieve.
>
> **Return format** — one YAML record per question so the grader can map answers
> back by id:
>
> ```yaml
> - id: <question-id>
>   answer: |
>     <your answer>
> ```

Total questions: 32

---

### dotnet-di-lifetimes-01

Explain the three built-in DI service lifetimes — transient, scoped, and singleton — and when each is resolved to a new instance.

### dotnet-di-captive-02

What is a "captive dependency" in .NET DI, and why is injecting a scoped service into a singleton a bug?

### dotnet-di-scope-in-singleton-03

A singleton background service needs to use a scoped service (e.g. a DbContext) on each iteration. How do you do it correctly?

### dotnet-di-keyed-04

You need two different implementations of the same interface resolvable by name. What DI feature (and which .NET version) supports this, and how do you consume it?

### dotnet-config-precedence-01

IConfiguration composes multiple sources. What is the default precedence in a typical host, and what wins when the same key exists in several sources?

### dotnet-config-options-interfaces-02

Contrast IOptions<T>, IOptionsSnapshot<T>, and IOptionsMonitor<T> — their lifetimes, whether they see config reloads, and where each can be injected.

### dotnet-config-secrets-03

Where should local development secrets (e.g. a connection string with a password) live, and how does that differ from production?

### dotnet-config-options-binding-04

How do you bind a configuration section to a strongly-typed options class, and how do you make the app fail fast on invalid options?

### dotnet-pipeline-order-01

Middleware in ASP.NET Core is order-sensitive. Explain how the pipeline executes and why, for example, UseAuthentication/UseAuthorization must sit in a specific place relative to UseRouting and the endpoints.

### dotnet-pipeline-use-vs-map-02

In configuring the pipeline, what's the difference between `Use`, `Run`, and `Map`, and what does it mean for a middleware to short-circuit?

### dotnet-pipeline-minimal-results-03

In a minimal API, how does a handler's return value become an HTTP response, and what do Results/TypedResults give you over returning a raw object?

### dotnet-pipeline-filters-vs-middleware-04

When should you use an endpoint filter (or MVC action filter) versus a piece of middleware? What can a filter do that middleware can't?

### dotnet-efcore-context-lifetime-01

What is the correct lifetime for a DbContext, and why is a DbContext instance not safe to share across threads or concurrent async operations?

### dotnet-efcore-notracking-02

What does EF Core's change tracker do, and when and why would you add AsNoTracking() to a query?

### dotnet-efcore-nplus1-03

Explain deferred execution of an IQueryable versus ToListAsync(), and how the N+1 query problem arises and is fixed.

### dotnet-efcore-savechanges-tx-04

Is a single SaveChangesAsync() call transactional? And what is a migration's role in EF Core?

### dotnet-gc-generations-loh-01

Describe the generational GC (gen 0/1/2) and the Large Object Heap. Why are large, short-lived allocations especially costly?

### dotnet-gc-dispose-finalizer-02

Contrast IDisposable/Dispose with a finalizer. When do you need each, and what does IAsyncDisposable add?

### dotnet-gc-span-arraypool-03

Name allocation-reducing tools in modern .NET — Span<T>, stackalloc, and ArrayPool<T> — and what each avoids.

### dotnet-gc-struct-vs-class-04

From an allocation standpoint, how do a struct (value type) and a class (reference type) differ, and what is boxing?

### dotnet-json-stj-defaults-01

Using System.Text.Json, how do you get camelCase JSON property names, and how do you override the serialized name of a single property?

### dotnet-json-sourcegen-02

What is System.Text.Json source generation, and why does it matter for performance and for trimming / Native AOT?

### dotnet-json-stj-vs-newtonsoft-03

System.Text.Json is the default in modern .NET, but people still reach for Newtonsoft.Json. What are the trade-offs?

### dotnet-json-options-reuse-04

Why should a JsonSerializerOptions instance be created once and reused, and how does System.Text.Json handle polymorphic (base/derived) serialization?

### dotnet-http-socket-exhaustion-01

Why does `using (var client = new HttpClient())` per request cause socket exhaustion, and how does IHttpClientFactory solve it?

### dotnet-http-typed-clients-02

What are named clients and typed clients with IHttpClientFactory, and why prefer a typed client?

### dotnet-http-lifetime-dns-03

Even factory-created HttpClients can serve stale DNS if held too long. Explain the handler-lifetime issue and the SocketsHttpHandler alternative.

### dotnet-http-resilience-04

How do you add retries, timeouts, and a circuit breaker to an HttpClient in modern .NET, and how do cancellation tokens fit in?

### dotnet-log-templates-01

Why should you write `logger.LogInformation("Order {OrderId} shipped", orderId)` instead of interpolating the value into the string?

### dotnet-log-levels-02

List the ILogger log levels in order and explain how per-category level filtering (e.g. quieting EF Core logs) is configured.

### dotnet-log-highperf-03

What is the LoggerMessage source generator (`[LoggerMessage]`), and why is it preferred over calling logger.LogInformation directly on hot paths?

### dotnet-log-scopes-otel-04

What are logging scopes (BeginScope) for, and at a high level how does .NET expose distributed tracing and metrics (OpenTelemetry)?
