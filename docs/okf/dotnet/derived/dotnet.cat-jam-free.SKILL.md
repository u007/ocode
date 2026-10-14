---
name: dotnet-tuning-cat-jam-free
description: >
  Corrective .NET guidance for cat-jam-free on EF Core and HttpClient: what to
  do for parallel DbContext work, when an explicit transaction is needed beyond
  one SaveChanges, the three HttpClient lifetimes and the DNS flaw of the static
  client, and why caching a factory-created client defeats handler rotation.
when_to_use: >
  Load when the provider-stripped model id (see stack-detection.md) resolves
  to exactly `cat-jam-free` AND the repository is a .NET project
  (a `*.sln`, `global.json`, `*.csproj`, `*.vbproj`, or
  `Directory.Build.props` file — per meta.yaml detection). For any other
  model or non-.NET repo, do not load.
tuned_for: cat-jam-free
tuned_version: "alpha"
stack: dotnet
source_scorecard: ../scores/cat-jam-free.md
threshold: 0.85
revalidate_when: model_version changes
---
# .NET corrections for cat-jam-free

<!-- kaizen:digest -->
**EF Core. After stating a constraint, always give the remedy:**
1. DbContext is short-lived and scoped (one per request via `AddDbContext`) and NOT thread-safe. Never stop there. Say what to do instead: `await` every operation before starting the next on the same context (a second concurrent operation throws `InvalidOperationException`), and for parallel work use a separate context per task, each from its own scope or from `IDbContextFactory<T>.CreateDbContext()`.
2. One `SaveChanges`/`SaveChangesAsync` is atomic: all its statements commit or roll back together. To make several `SaveChanges` calls (or raw SQL plus SaveChanges) atomic you need an explicit transaction: `await using var tx = await db.Database.BeginTransactionAsync(); ... await tx.CommitAsync();`. Mention this whenever transactionality comes up.
3. When describing migrations, name the workflow: `dotnet ef migrations add <Name>` diffs the model against the snapshot and generates the migration; `dotnet ef database update` (or `Database.MigrateAsync()`) applies pending ones.

**.NET HttpClient. State the full mechanism, not just "use the factory":**
1. A socket-exhaustion answer names three stages and the flaw of each: (a) `new HttpClient()` + dispose per request leaves sockets in TIME_WAIT and exhausts ports; (b) one shared `static` HttpClient fixes exhaustion but its pooled connections never re-resolve DNS, so it keeps hitting stale IPs after a failover; (c) IHttpClientFactory pools `HttpMessageHandler`s (no exhaustion) AND rotates them on `HandlerLifetime` (default 2 min) so DNS changes are picked up. Never skip stage (b) or the DNS half of (c).
2. An HttpClient stays bound to the handler it was created with. Caching a factory-created client (or a transient typed client) in a singleton or static field pins that handler, so it never sees rotation and goes stale on DNS like the static client. Say this in words whenever stale DNS or handler lifetime comes up. Then give the alternative: a primary `SocketsHttpHandler` with `PooledConnectionLifetime`, optionally disabling factory rotation via `SetHandlerLifetime(Timeout.InfiniteTimeSpan)`. `HandlerLifetime` and `PooledConnectionLifetime` are separate settings.
3. For retries/timeouts/circuit breakers on .NET 8+, name the method: `.AddStandardResilienceHandler()` (or `.AddResilienceHandler(...)`) from `Microsoft.Extensions.Http.Resilience`, chained onto `AddHttpClient`. Always flow a `CancellationToken` into the HTTP call.
<!-- /kaizen:digest -->
