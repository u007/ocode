---
name: dotnet-tuning-longcat-2.5-preview-free
description: >
  Corrective .NET HttpClient/resilience guidance for longcat-2.5-preview-free:
  why a single static HttpClient goes stale on DNS, why caching a
  factory-created client defeats handler rotation, HandlerLifetime vs
  PooledConnectionLifetime, and the current Microsoft.Extensions.Http.Resilience
  API (AddStandardResilienceHandler) instead of invented Polly helpers.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `longcat-2.5-preview-free` AND the repository is a .NET project
  (a `*.sln`, `global.json`, `*.csproj`, `*.vbproj`, or
  `Directory.Build.props` file — per meta.yaml detection). For any other
  model or non-.NET repo, do not load.
tuned_for: longcat-2.5-preview-free
tuned_version: "2.5-preview"
stack: dotnet
source_scorecard: ../scores/longcat-2.5-preview-free.md
threshold: 0.75
revalidate_when: model_version changes
---
# .NET corrections for longcat-2.5-preview-free

<!-- kaizen:digest -->
**.NET HttpClient / resilience. State the mechanism and use real APIs only:**
1. Any answer about socket exhaustion, `new HttpClient()` per request, or how IHttpClientFactory helps is INCOMPLETE unless it contains all three of these sentences (in your own words is fine; none may be dropped):
   - "Creating and disposing an HttpClient per request leaves its sockets in TIME_WAIT, so under load the machine runs out of ports."
   - "The obvious fix, one shared static HttpClient, avoids exhaustion but never re-resolves DNS, so it keeps talking to stale IPs after a failover."
   - "IHttpClientFactory solves BOTH: it pools HttpMessageHandlers (no exhaustion) and rotates them on `HandlerLifetime` (default 2 min), so DNS changes are picked up."
   The question may only ask about per-request vs factory. Still state the static-client middle step, because it is the reason the factory rotates handlers.
2. Whenever you explain stale DNS, handler rotation or caching a client, state the binding in words: "an HttpClient is bound to the handler it was created with; the factory rotates handlers on `HandlerLifetime`, so caching a factory client in a singleton/static field pins the old handler and defeats rotation." Then give the alternative: `SocketsHttpHandler.PooledConnectionLifetime` recycles connections inside one handler. Factory rotation can then be disabled with `SetHandlerLifetime(Timeout.InfiniteTimeSpan)`. `HandlerLifetime` and `PooledConnectionLifetime` are separate settings.
3. For retries, timeouts and circuit breakers on .NET 8+, use `Microsoft.Extensions.Http.Resilience` with `.AddStandardResilienceHandler()` (or `.AddResilienceHandler(...)`). `AddRetryPolicy`, `AddTimeoutPolicy` and `AddCircuitBreakerPolicy` do not exist. The legacy Polly API is `AddTransientHttpErrorPolicy` / `AddPolicyHandler`. Always flow a `CancellationToken` into the HTTP call.
<!-- /kaizen:digest -->

## resilience-http: the three HttpClient lifetimes and what each gets wrong

- `new HttpClient()` per request + dispose: sockets linger in TIME_WAIT, so
  under load you exhaust ports.
- One long-lived `static` HttpClient: fixes exhaustion, but its pooled
  connections never re-resolve DNS, so it keeps hitting old IPs after a
  failover or deployment. Always state this middle ground and its DNS flaw
  when explaining why IHttpClientFactory exists.
- IHttpClientFactory: hands out cheap clients that share pooled
  `HttpMessageHandler`s and rotates those handlers on `HandlerLifetime`
  (default 2 minutes). You get no exhaustion and DNS changes are still picked up.

## resilience-http: don't cache factory clients; keep the two lifetimes apart

- An HttpClient stays bound to the handler it was created with. If you store
  a factory-created client in a singleton or static field, it never sees
  handler rotation and goes stale on DNS exactly like the static client.
  Resolve a client from the factory, or inject a typed client, near each use.
- `HandlerLifetime` (factory, set via `SetHandlerLifetime`) and
  `SocketsHttpHandler.PooledConnectionLifetime` (connection recycling inside
  one handler) are separate settings. Name the one you are actually
  configuring, and don't describe one as the other.
- The modern alternative is to configure a `SocketsHttpHandler` with
  `PooledConnectionLifetime` (e.g. 2 min) as the primary handler. Optionally
  disable factory rotation with `SetHandlerLifetime(Timeout.InfiniteTimeSpan)`
  and let the handler recycle its own connections. That long-lived
  handler/client is safe to cache.

## resilience-http: use Microsoft.Extensions.Http.Resilience, not invented Polly methods

- On .NET 8+, add the `Microsoft.Extensions.Http.Resilience` package (built on
  Polly v8) and chain `.AddStandardResilienceHandler()` onto `AddHttpClient`.
  It gives you retry with backoff, a total timeout, a per-attempt timeout and a
  circuit breaker. Use `.AddResilienceHandler("name", builder => ...)` for a
  custom pipeline.
- `AddRetryPolicy`, `AddTimeoutPolicy` and `AddCircuitBreakerPolicy` do not
  exist. The legacy `Microsoft.Extensions.Http.Polly` API is
  `AddTransientHttpErrorPolicy(...)` / `AddPolicyHandler(...)`. Mention it only
  as the older approach.
- Pass a `CancellationToken` (for example `HttpContext.RequestAborted` or a
  linked `CancellationTokenSource` with a timeout) into every
  `GetAsync`/`SendAsync` call so callers can cancel in-flight work.
