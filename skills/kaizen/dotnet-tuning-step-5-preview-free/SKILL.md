---
name: dotnet-tuning-step-5-preview-free
description: >
  Corrective .NET HttpClient/resilience guidance for step-5-preview-free: the
  three HttpClient lifetimes (per-request, static, factory) and the flaw of
  each, the client-to-handler binding that makes caching a factory client go
  stale on DNS, HandlerLifetime vs PooledConnectionLifetime, and the exact
  Microsoft.Extensions.Http.Resilience / Polly method names.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `step-5-preview-free` AND the repository is a .NET project
  (a `*.sln`, `global.json`, `*.csproj`, `*.vbproj`, or
  `Directory.Build.props` file — per meta.yaml detection). For any other
  model or non-.NET repo, do not load.
tuned_for: step-5-preview-free
tuned_version: "5-preview"
stack: dotnet
source_scorecard: ../scores/step-5-preview-free.md
threshold: 0.85
revalidate_when: model_version changes
---
# .NET corrections for step-5-preview-free

<!-- kaizen:digest -->
**.NET HttpClient / resilience. Always state the full mechanism, not just the fix:**
1. Any explanation of socket exhaustion or why IHttpClientFactory exists names THREE stages, in order, and the flaw of each: (a) `new HttpClient()` + dispose per request leaves sockets in TIME_WAIT and exhausts ephemeral ports; (b) one shared `static` HttpClient fixes exhaustion but its pooled connections never re-resolve DNS, so it keeps hitting old IPs after a failover; (c) IHttpClientFactory pools `HttpMessageHandler`s AND rotates them on `HandlerLifetime` (default 2 min), fixing both. Stage (b) is the reason rotation exists; never skip it even when the question only asks about (a) and (c).
2. An HttpClient is bound to the handler it was created with. Caching a factory-created client (or a transient typed client) in a singleton or static field pins that handler, so it never sees rotation and goes stale on DNS exactly like the static client. Say this in words whenever handler lifetime or stale DNS comes up; then give the alternative: a primary `SocketsHttpHandler` with `PooledConnectionLifetime`, optionally with factory rotation disabled via `SetHandlerLifetime(Timeout.InfiniteTimeSpan)`. `HandlerLifetime` and `PooledConnectionLifetime` are separate settings.
3. Use real method names only: `AddStandardResilienceHandler()` / `AddResilienceHandler(...)` from `Microsoft.Extensions.Http.Resilience` (there is no `AddStandardHandler()`); legacy Polly is `AddTransientHttpErrorPolicy` / `AddPolicyHandler` with `HttpPolicyExtensions.HandleTransientHttpError()`. Always flow a `CancellationToken` into the HTTP call.
<!-- /kaizen:digest -->

## resilience-http: three HttpClient lifetimes, three flaws

When asked why `using (var client = new HttpClient())` per request is wrong
and how IHttpClientFactory helps, cover all three stages. Dropping the middle
one loses the point of the factory.

- **Per-request client + dispose.** Each `new HttpClient()` gets its own
  handler and connection pool; disposing it closes the sockets, which sit in
  TIME_WAIT. Under load the OS runs out of ephemeral ports.
- **One long-lived `static` HttpClient.** No exhaustion, but its pooled
  connections stay open and the hostname is never re-resolved, so after a
  deployment or failover it keeps talking to the old IP. State this flaw
  explicitly; "DNS" must not appear only as a side benefit of the factory.
- **IHttpClientFactory.** Hands out cheap clients over a pool of shared
  `HttpMessageHandler`s (no exhaustion) and rotates those handlers on
  `HandlerLifetime` (default 2 minutes), so fresh connections pick up DNS
  changes. Register with `AddHttpClient(...)`, inject `IHttpClientFactory` or
  a named/typed client.

## resilience-http: a client is bound to its handler; don't cache factory clients

The question "even factory clients go stale if held too long" is about the
client-to-handler binding, not about rotation in general.

- An HttpClient keeps the `HttpMessageHandler` it was constructed with for
  its whole life. Factory rotation only affects clients created AFTER the
  rotation. A factory client stored in a singleton, a static field, or a
  long-lived background service pins its original handler and never sees a
  new one, so it serves stale DNS exactly like the static client in the
  previous section. Say this sentence whenever handler lifetime comes up.
- Typed clients are registered transient. Injecting one into a singleton has
  the same pinning effect. Fix: inject `IHttpClientFactory` and call
  `CreateClient` near each use, or inject the typed client only into
  scoped/transient consumers.
- The modern alternative is a primary `SocketsHttpHandler` configured with
  `PooledConnectionLifetime` (e.g. 2 minutes): the handler itself retires and
  re-creates pooled connections, re-resolving DNS, so a long-lived
  client/handler is then safe to cache. You can disable factory rotation with
  `SetHandlerLifetime(Timeout.InfiniteTimeSpan)` and let the handler do it.
- `HandlerLifetime` (factory, `SetHandlerLifetime`) and
  `SocketsHttpHandler.PooledConnectionLifetime` (connection recycling inside
  one handler) are two different settings. Name the one you are configuring.

## resilience-http: exact API names

- .NET 8+: package `Microsoft.Extensions.Http.Resilience` (Polly v8). Chain
  `.AddStandardResilienceHandler()` onto `AddHttpClient` for the default
  retry / total timeout / per-attempt timeout / circuit-breaker pipeline, or
  `.AddResilienceHandler("name", builder => builder.AddRetry(...)
  .AddCircuitBreaker(...).AddTimeout(...))` for a custom one. There is no
  `AddStandardHandler()`.
- Legacy (`Microsoft.Extensions.Http.Polly`): `AddTransientHttpErrorPolicy(...)`
  / `AddPolicyHandler(...)` with `HttpPolicyExtensions.HandleTransientHttpError()`.
  Not `Handle<HttpRequestException>()` on `HttpPolicyExtensions`.
- Pass a `CancellationToken` (e.g. `HttpContext.RequestAborted` or a linked
  `CancellationTokenSource` with a timeout) into every `SendAsync`/`GetAsync`;
  resilience timeouts are enforced through linked tokens and surface as
  `OperationCanceledException`/`TaskCanceledException`.
