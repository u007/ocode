# Account Login + Encrypted Config Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `ocode /login` (device-code auth against kakiit) plus
encrypted, deferred, auto-merging config sync of `ocodeconfig.json` and
`auth.json` between machines, backed by new kakiit API routes with
server-side envelope encryption and Postgres advisory-lock concurrency
control.

**Architecture:** kakiit gains a new `/api/ocode/*` Hono route group (device
code flow + versioned blob sync) reusing its existing better-auth user
system, with per-user envelope encryption (AES-256-GCM, DEK wrapped by a
master key from env) and `pg_advisory_xact_lock` serializing writes. ocode
gains a new `internal/sync` Go package that drives the device-code login,
hooks the two existing config-save choke points to debounce-push changes in
the background, and does a bounded-retry 3-way JSON merge on version
conflicts so sync never surfaces a failure to the user.

**Tech Stack:** kakiit: Next.js, Hono, better-auth, Drizzle ORM, Postgres,
Zod, `bun:test`. ocode: Go, existing `internal/config`, `internal/auth`,
`internal/tui` packages.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-28-account-login-config-sync-design.md`
- All new kakiit DB objects follow project naming conventions: `snake_case`
  tables/columns, explicit index names (`<table>_<col>_idx` /
  `_uniq`), explicit FK constraint names (`<table>_<column>_fk`).
- All migration DDL must be idempotent (existence guards) and
  transaction-safe (no `CREATE INDEX CONCURRENTLY` etc.) per project rules.
- Migrations are generated via `drizzle-kit generate`, never hand-written,
  then guards are added before commit.
- ocode config file writes go only through the existing
  `withOcodeConfigLock` (`internal/config/ocodeconfig.go`) and
  `persistLocked` (`internal/auth/store.go`) choke points — no new direct
  file-write paths.
- Conflict policy (from spec §5): `ocodeconfig` blob is local-wins on a
  same-key conflict; `authsecrets` blob is remote-wins.
- Bounded retry: 5 attempts, 250ms exponential backoff base, on merge
  conflict during push; silent give-up (logged only) after exhaustion.

---

## Part A — kakiit backend

### Task 1: Schema — device codes, sync keys, sync tokens, config blobs

**Files:**
- Modify: `src/server/db/schema.ts` (append new tables at end of file,
  following the existing `createTable`/`d.varchar`/`uuidv7` conventions
  used by `users`/`accounts` around line 167)
- Create: `drizzle/<generated-name>.sql` (via `drizzle-kit generate`, then
  hand-add idempotency guards per project migration rules)
- Test: `src/server/db/ocode-sync-schema.test.ts`

**Interfaces:**
- Produces: exported Drizzle tables `ocodeDeviceCodes`, `ocodeSyncKeys`,
  `ocodeSyncTokens`, `ocodeConfigSyncs` — consumed by every later kakiit
  task in this plan.

- [ ] **Step 1: Add the four tables to `schema.ts`**

```typescript
export const ocodeBlobTypeEnum = pgEnum('ocode_blob_type_enum', [
  'ocodeconfig',
  'authsecrets',
]);

export const ocodeDeviceCodes = createTable(
  'ocode_device_code',
  d => ({
    id: d
      .varchar({ length: 255 })
      .notNull()
      .primaryKey()
      .$defaultFn(() => uuidv7()),
    deviceCode: d.varchar({ length: 255 }).notNull().unique(),
    userCode: d.varchar({ length: 16 }).notNull().unique(),
    userId: d.varchar({ length: 255 }).references(() => users.id),
    approved: d.boolean().notNull().default(false),
    expiresAt: d.timestamp({ withTimezone: true }).notNull(),
    createdAt: d
      .timestamp({ withTimezone: true })
      .default(sql`CURRENT_TIMESTAMP`)
      .notNull(),
  }),
  t => [
    index('ocode_device_code_device_code_idx').on(t.deviceCode),
    index('ocode_device_code_user_code_idx').on(t.userCode),
  ]
);

export const ocodeSyncKeys = createTable(
  'ocode_sync_key',
  d => ({
    id: d
      .varchar({ length: 255 })
      .notNull()
      .primaryKey()
      .$defaultFn(() => uuidv7()),
    userId: d
      .varchar({ length: 255 })
      .notNull()
      .unique()
      .references(() => users.id, { onDelete: 'cascade' }),
    wrappedDek: d.text().notNull(),
    createdAt: d
      .timestamp({ withTimezone: true })
      .default(sql`CURRENT_TIMESTAMP`)
      .notNull(),
  })
);

export const ocodeSyncTokens = createTable(
  'ocode_sync_token',
  d => ({
    id: d
      .varchar({ length: 255 })
      .notNull()
      .primaryKey()
      .$defaultFn(() => uuidv7()),
    userId: d
      .varchar({ length: 255 })
      .notNull()
      .references(() => users.id, { onDelete: 'cascade' }),
    tokenHash: d.varchar({ length: 255 }).notNull().unique(),
    createdAt: d
      .timestamp({ withTimezone: true })
      .default(sql`CURRENT_TIMESTAMP`)
      .notNull(),
    revokedAt: d.timestamp({ withTimezone: true }),
  }),
  t => [index('ocode_sync_token_user_id_idx').on(t.userId)]
);

export const ocodeConfigSyncs = createTable(
  'ocode_config_sync',
  d => ({
    id: d
      .varchar({ length: 255 })
      .notNull()
      .primaryKey()
      .$defaultFn(() => uuidv7()),
    userId: d
      .varchar({ length: 255 })
      .notNull()
      .references(() => users.id, { onDelete: 'cascade' }),
    blobType: ocodeBlobTypeEnum().notNull(),
    ciphertext: d.text().notNull(),
    version: d.integer().notNull().default(0),
    updatedAt: d
      .timestamp({ withTimezone: true })
      .default(sql`CURRENT_TIMESTAMP`)
      .$onUpdate(() => new Date())
      .notNull(),
  }),
  t => [
    uniqueIndex('ocode_config_sync_user_id_blob_type_uniq').on(
      t.userId,
      t.blobType
    ),
  ]
);
```

(Add `pgEnum`, `uniqueIndex` to the existing top-of-file `drizzle-orm/pg-core`
import if not already imported.)

- [ ] **Step 2: Generate the migration**

Run: `bun run db:generate`

- [ ] **Step 3: Add idempotency guards to the generated SQL**

Open the new file under `drizzle/`. Wrap each `CREATE TABLE` as
`CREATE TABLE IF NOT EXISTS`, each `CREATE INDEX`/`CREATE UNIQUE INDEX` as
`... IF NOT EXISTS`, and each `ALTER TABLE ... ADD CONSTRAINT` in the
project's standard `DO $$ BEGIN ... EXCEPTION WHEN duplicate_object THEN
NULL; END $$;` guard (see any recent migration under `drizzle/` for the
exact wrapping style used in this repo).

- [ ] **Step 4: Write the schema smoke test**

```typescript
import { describe, it, expect } from 'bun:test';
import { db } from '@/server/db';
import {
  ocodeDeviceCodes,
  ocodeSyncKeys,
  ocodeSyncTokens,
  ocodeConfigSyncs,
} from '@/server/db/schema';

describe('ocode sync schema', () => {
  it('tables are queryable (migration applied)', async () => {
    await expect(
      db.select().from(ocodeDeviceCodes).limit(1)
    ).resolves.toBeArray();
    await expect(db.select().from(ocodeSyncKeys).limit(1)).resolves.toBeArray();
    await expect(
      db.select().from(ocodeSyncTokens).limit(1)
    ).resolves.toBeArray();
    await expect(
      db.select().from(ocodeConfigSyncs).limit(1)
    ).resolves.toBeArray();
  });
});
```

- [ ] **Step 5: Apply the migration to the dev DB and run the test**

Run: `bun run migrate` (or this project's existing migration-apply script —
check `package.json` `db:*` scripts) then `bun test --serial --isolate
./src/server/db/ocode-sync-schema.test.ts`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add src/server/db/schema.ts drizzle/ src/server/db/ocode-sync-schema.test.ts
git commit -m "feat(db): add ocode sync tables (device codes, keys, tokens, config blobs)"
```

---

### Task 2: Envelope encryption service

**Files:**
- Create: `src/server/services/ocode-sync/crypto.ts`
- Test: `src/server/services/ocode-sync/crypto.test.ts`
- Modify: `src/env-schema.ts` (add `KAKIIT_OCODE_SYNC_MASTER_KEY`)

**Interfaces:**
- Consumes: `env.KAKIIT_OCODE_SYNC_MASTER_KEY` (base64, 32 bytes)
- Produces: `generateWrappedDek(): string`, `unwrapDek(wrapped: string):
  Buffer`, `encryptBlob(dek: Buffer, plaintext: string): string`,
  `decryptBlob(dek: Buffer, ciphertext: string): string` — consumed by
  Task 5 (sync routes).

- [ ] **Step 1: Add the env var to `env-schema.ts`**

```typescript
KAKIIT_OCODE_SYNC_MASTER_KEY: z.string().min(44), // base64-encoded 32 bytes
```

- [ ] **Step 2: Write the failing test**

```typescript
import { describe, it, expect } from 'bun:test';
import {
  generateWrappedDek,
  unwrapDek,
  encryptBlob,
  decryptBlob,
} from './crypto';

describe('ocode sync crypto', () => {
  it('round-trips a DEK through wrap/unwrap', () => {
    const wrapped = generateWrappedDek();
    const dek = unwrapDek(wrapped);
    expect(dek).toHaveLength(32);
  });

  it('round-trips a blob through encrypt/decrypt', () => {
    const dek = unwrapDek(generateWrappedDek());
    const plaintext = JSON.stringify({ theme: 'dark' });
    const ciphertext = encryptBlob(dek, plaintext);
    expect(ciphertext).not.toContain('dark');
    expect(decryptBlob(dek, ciphertext)).toBe(plaintext);
  });

  it('two DEKs produce different ciphertexts for the same plaintext', () => {
    const dekA = unwrapDek(generateWrappedDek());
    const dekB = unwrapDek(generateWrappedDek());
    const plaintext = 'same-input';
    expect(encryptBlob(dekA, plaintext)).not.toBe(encryptBlob(dekB, plaintext));
  });
});
```

- [ ] **Step 3: Run test to verify it fails**

Run: `bun test --serial --isolate ./src/server/services/ocode-sync/crypto.test.ts`
Expected: FAIL — `crypto.ts` does not exist yet

- [ ] **Step 4: Implement `crypto.ts`**

```typescript
import { randomBytes, createCipheriv, createDecipheriv } from 'node:crypto';
import { env } from '@/env';

const ALGO = 'aes-256-gcm';

function masterKey(): Buffer {
  return Buffer.from(env.KAKIIT_OCODE_SYNC_MASTER_KEY, 'base64');
}

function seal(key: Buffer, plaintext: Buffer): string {
  const iv = randomBytes(12);
  const cipher = createCipheriv(ALGO, key, iv);
  const ciphertext = Buffer.concat([cipher.update(plaintext), cipher.final()]);
  const tag = cipher.getAuthTag();
  return Buffer.concat([iv, tag, ciphertext]).toString('base64');
}

function open(key: Buffer, sealed: string): Buffer {
  const raw = Buffer.from(sealed, 'base64');
  const iv = raw.subarray(0, 12);
  const tag = raw.subarray(12, 28);
  const ciphertext = raw.subarray(28);
  const decipher = createDecipheriv(ALGO, key, iv);
  decipher.setAuthTag(tag);
  return Buffer.concat([decipher.update(ciphertext), decipher.final()]);
}

/** Generates a fresh random DEK, wrapped (encrypted) with the master key. */
export function generateWrappedDek(): string {
  const dek = randomBytes(32);
  return seal(masterKey(), dek);
}

/** Unwraps a stored DEK using the master key. */
export function unwrapDek(wrapped: string): Buffer {
  return open(masterKey(), wrapped);
}

export function encryptBlob(dek: Buffer, plaintext: string): string {
  return seal(dek, Buffer.from(plaintext, 'utf8'));
}

export function decryptBlob(dek: Buffer, ciphertext: string): string {
  return open(dek, ciphertext).toString('utf8');
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `bun test --serial --isolate ./src/server/services/ocode-sync/crypto.test.ts`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add src/env-schema.ts src/server/services/ocode-sync/crypto.ts src/server/services/ocode-sync/crypto.test.ts
git commit -m "feat(ocode-sync): add envelope encryption for sync blobs"
```

---

### Task 3: Device-code service (start/approve/poll/revoke)

**Files:**
- Create: `src/server/services/ocode-sync/device-code-service.ts`
- Test: `src/server/services/ocode-sync/device-code-service.test.ts`

**Interfaces:**
- Consumes: `db`, `ocodeDeviceCodes`, `ocodeSyncTokens` (Task 1)
- Produces: `startDeviceFlow(): Promise<{deviceCode, userCode, verifyUrl,
  expiresIn}>`, `approveDevice(userCode: string, userId: string):
  Promise<void>`, `pollDeviceToken(deviceCode: string): Promise<{status:
  'pending'|'approved'|'expired', token?: string}>`, `revokeSyncToken(token:
  string): Promise<void>` — consumed by Task 4 (routes).

- [ ] **Step 1: Write the failing test**

```typescript
import { describe, it, expect } from 'bun:test';
import { randomUUID } from 'node:crypto';
import {
  startDeviceFlow,
  approveDevice,
  pollDeviceToken,
  revokeSyncToken,
} from './device-code-service';
import { db } from '@/server/db';
import { users, ocodeSyncTokens } from '@/server/db/schema';

async function makeUser() {
  const [user] = await db
    .insert(users)
    .values({ email: `${randomUUID()}@test.local`, emailVerified: true })
    .returning();
  return user!;
}

describe('device-code-service', () => {
  it('start -> poll (pending) -> approve -> poll (approved with token)', async () => {
    const user = await makeUser();
    const { deviceCode, userCode } = await startDeviceFlow();

    const beforeApproval = await pollDeviceToken(deviceCode);
    expect(beforeApproval.status).toBe('pending');

    await approveDevice(userCode, user.id);

    const afterApproval = await pollDeviceToken(deviceCode);
    expect(afterApproval.status).toBe('approved');
    expect(afterApproval.token).toBeTruthy();

    const rows = await db
      .select()
      .from(ocodeSyncTokens)
      .where(eq(ocodeSyncTokens.userId, user.id));
    expect(rows).toHaveLength(1);
  });

  it('revoked token stops appearing as active', async () => {
    const user = await makeUser();
    const { deviceCode, userCode } = await startDeviceFlow();
    await approveDevice(userCode, user.id);
    const { token } = await pollDeviceToken(deviceCode);

    await revokeSyncToken(token!);

    const rows = await db
      .select()
      .from(ocodeSyncTokens)
      .where(eq(ocodeSyncTokens.userId, user.id));
    expect(rows[0]!.revokedAt).not.toBeNull();
  });
});
```

(Add `import { eq } from 'drizzle-orm';` alongside the other imports.)

- [ ] **Step 2: Run test to verify it fails**

Run: `bun test --serial --isolate ./src/server/services/ocode-sync/device-code-service.test.ts`
Expected: FAIL — module does not exist

- [ ] **Step 3: Implement `device-code-service.ts`**

```typescript
import { randomBytes, randomUUID, createHash } from 'node:crypto';
import { eq, and, isNull } from 'drizzle-orm';
import { db } from '@/server/db';
import { ocodeDeviceCodes, ocodeSyncTokens } from '@/server/db/schema';
import { env } from '@/env';

const DEVICE_CODE_TTL_MS = 10 * 60 * 1000; // 10 minutes

function generateUserCode(): string {
  // 8-char, human-typeable code, e.g. "K3F9-QZ2R"
  const alphabet = 'ABCDEFGHJKLMNPQRSTUVWXYZ23456789';
  const part = () =>
    Array.from({ length: 4 }, () => alphabet[randomBytes(1)[0]! % alphabet.length]).join('');
  return `${part()}-${part()}`;
}

function hashToken(token: string): string {
  return createHash('sha256').update(token).digest('hex');
}

export async function startDeviceFlow() {
  const deviceCode = randomUUID();
  const userCode = generateUserCode();
  const expiresAt = new Date(Date.now() + DEVICE_CODE_TTL_MS);

  await db.insert(ocodeDeviceCodes).values({ deviceCode, userCode, expiresAt });

  return {
    deviceCode,
    userCode,
    verifyUrl: `${env.APP_URL}/device`,
    expiresIn: DEVICE_CODE_TTL_MS / 1000,
  };
}

export async function approveDevice(userCode: string, userId: string) {
  const [row] = await db
    .update(ocodeDeviceCodes)
    .set({ approved: true, userId })
    .where(
      and(
        eq(ocodeDeviceCodes.userCode, userCode),
        eq(ocodeDeviceCodes.approved, false)
      )
    )
    .returning();

  if (!row) throw new Error('Invalid or already-used device code');
}

export async function pollDeviceToken(
  deviceCode: string
): Promise<{ status: 'pending' | 'approved' | 'expired'; token?: string }> {
  const [row] = await db
    .select()
    .from(ocodeDeviceCodes)
    .where(eq(ocodeDeviceCodes.deviceCode, deviceCode));

  if (!row) return { status: 'expired' };
  if (row.expiresAt < new Date()) return { status: 'expired' };
  if (!row.approved || !row.userId) return { status: 'pending' };

  const token = randomBytes(32).toString('base64url');
  await db.insert(ocodeSyncTokens).values({
    userId: row.userId,
    tokenHash: hashToken(token),
  });
  await db.delete(ocodeDeviceCodes).where(eq(ocodeDeviceCodes.id, row.id));

  return { status: 'approved', token };
}

export async function revokeSyncToken(token: string) {
  await db
    .update(ocodeSyncTokens)
    .set({ revokedAt: new Date() })
    .where(
      and(
        eq(ocodeSyncTokens.tokenHash, hashToken(token)),
        isNull(ocodeSyncTokens.revokedAt)
      )
    );
}

/** Looks up the active user for a bearer token; used by the sync auth middleware (Task 4). */
export async function resolveSyncToken(token: string): Promise<string | null> {
  const [row] = await db
    .select()
    .from(ocodeSyncTokens)
    .where(
      and(
        eq(ocodeSyncTokens.tokenHash, hashToken(token)),
        isNull(ocodeSyncTokens.revokedAt)
      )
    );
  return row?.userId ?? null;
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bun test --serial --isolate ./src/server/services/ocode-sync/device-code-service.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add src/server/services/ocode-sync/device-code-service.ts src/server/services/ocode-sync/device-code-service.test.ts
git commit -m "feat(ocode-sync): device-code login service (start/approve/poll/revoke)"
```

---

### Task 4: Device routes + `/device` approval page + sync-token auth middleware

**Files:**
- Create: `src/server/api/routes/ocode.device.ts`
- Create: `src/server/api/lib/ocode-sync-auth.ts`
- Modify: `src/server/api/index.ts` (register the new route)
- Create: `src/app/device/page.tsx`
- Test: `src/server/api/routes/ocode.device.test.ts`

**Interfaces:**
- Consumes: `startDeviceFlow`, `approveDevice`, `pollDeviceToken`,
  `revokeSyncToken`, `resolveSyncToken` (Task 3); `requireAuth`,
  `getAuthUser` (existing `../lib/auth`)
- Produces: `requireSyncToken` Hono middleware (sets `c.set('syncUserId',
  ...)`) — consumed by Task 5.

- [ ] **Step 1: Write the failing route test**

```typescript
import { describe, it, expect } from 'bun:test';
import app from '../index';

describe('POST /api/ocode/device/start', () => {
  it('returns a device code, user code, and verify url', async () => {
    const res = await app.request('/api/ocode/device/start', { method: 'POST' });
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.deviceCode).toBeTruthy();
    expect(body.userCode).toMatch(/^[A-Z0-9]{4}-[A-Z0-9]{4}$/);
    expect(body.verifyUrl).toContain('/device');
  });
});

describe('POST /api/ocode/device/token', () => {
  it('reports pending for an unapproved device code', async () => {
    const start = await app.request('/api/ocode/device/start', { method: 'POST' });
    const { deviceCode } = await start.json();

    const res = await app.request('/api/ocode/device/token', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ deviceCode }),
    });
    expect(res.status).toBe(200);
    expect((await res.json()).status).toBe('pending');
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bun test --serial --isolate ./src/server/api/routes/ocode.device.test.ts`
Expected: FAIL — route not mounted

- [ ] **Step 3: Implement `ocode-sync-auth.ts` middleware**

```typescript
import type { Context, Next } from 'hono';
import { resolveSyncToken } from '@/server/services/ocode-sync/device-code-service';

export interface SyncEnv {
  Variables: { syncUserId: string };
}

export async function requireSyncToken(c: Context<SyncEnv>, next: Next) {
  const header = c.req.header('authorization') ?? '';
  const token = header.startsWith('Bearer ') ? header.slice(7) : null;
  if (!token) return c.json({ error: 'Missing bearer token' }, 401);

  const userId = await resolveSyncToken(token);
  if (!userId) return c.json({ error: 'Invalid or revoked token' }, 401);

  c.set('syncUserId', userId);
  await next();
}
```

- [ ] **Step 4: Implement `ocode.device.ts` routes**

```typescript
import { Hono } from 'hono';
import { zValidator } from '@hono/zod-validator';
import { z } from 'zod';
import {
  startDeviceFlow,
  approveDevice,
  pollDeviceToken,
  revokeSyncToken,
} from '@/server/services/ocode-sync/device-code-service';
import { requireAuth, getAuthUser, type Env } from '../lib/auth';

const app = new Hono<Env>();

app.post('/start', async c => {
  const result = await startDeviceFlow();
  return c.json(result);
});

app.post(
  '/approve',
  zValidator('json', z.object({ userCode: z.string() })),
  requireAuth,
  async c => {
    const user = getAuthUser(c);
    const { userCode } = c.req.valid('json');
    try {
      await approveDevice(userCode, user.id);
      return c.json({ ok: true });
    } catch {
      return c.json({ error: 'Invalid or already-used code' }, 400);
    }
  }
);

app.post(
  '/token',
  zValidator('json', z.object({ deviceCode: z.string() })),
  async c => {
    const { deviceCode } = c.req.valid('json');
    const result = await pollDeviceToken(deviceCode);
    return c.json(result);
  }
);

app.post(
  '/revoke',
  zValidator('json', z.object({ token: z.string() })),
  async c => {
    const { token } = c.req.valid('json');
    await revokeSyncToken(token);
    return c.json({ ok: true });
  }
);

export default app;
```

- [ ] **Step 5: Mount the route in `src/server/api/index.ts`**

Add near the other route imports/mounts:

```typescript
import ocodeDeviceRoutes from './routes/ocode.device';
// ...
apiApp.route('/ocode/device', ocodeDeviceRoutes);
```

(Match the exact `apiApp.route(...)` call style already used for
`profileRoutes` etc. in this file.)

- [ ] **Step 6: Add the `/device` approval page**

```tsx
'use client';

import { useState } from 'react';
import { authClient } from '@/lib/auth-client';

export default function DeviceApprovalPage() {
  const [code, setCode] = useState('');
  const [status, setStatus] = useState<'idle' | 'approved' | 'error'>('idle');
  const { data: session } = authClient.useSession();

  async function approve() {
    if (!session) {
      window.location.href = `/login?redirect=/device`;
      return;
    }
    const res = await fetch('/api/ocode/device/approve', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ userCode: code }),
    });
    setStatus(res.ok ? 'approved' : 'error');
  }

  return (
    <div>
      <h1>Approve ocode device</h1>
      <input
        value={code}
        onChange={e => setCode(e.target.value.toUpperCase())}
        placeholder="XXXX-XXXX"
      />
      <button onClick={approve}>Approve</button>
      {status === 'approved' && <p>Device approved — return to your terminal.</p>}
      {status === 'error' && <p>Invalid or expired code.</p>}
    </div>
  );
}
```

- [ ] **Step 7: Run test to verify it passes**

Run: `bun test --serial --isolate ./src/server/api/routes/ocode.device.test.ts`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add src/server/api/routes/ocode.device.ts src/server/api/routes/ocode.device.test.ts src/server/api/lib/ocode-sync-auth.ts src/server/api/index.ts src/app/device/page.tsx
git commit -m "feat(ocode-sync): device-code routes, approval page, sync-token middleware"
```

---

### Task 5: Sync blob routes (GET/PUT with advisory lock + version check)

**Files:**
- Create: `src/server/services/ocode-sync/sync-service.ts`
- Create: `src/server/api/routes/ocode.sync.ts`
- Modify: `src/server/api/index.ts` (register the new route)
- Test: `src/server/services/ocode-sync/sync-service.test.ts`
- Test: `src/server/api/routes/ocode.sync.test.ts`

**Interfaces:**
- Consumes: `unwrapDek`, `encryptBlob`, `decryptBlob`, `generateWrappedDek`
  (Task 2); `ocodeSyncKeys`, `ocodeConfigSyncs` (Task 1); `requireSyncToken`
  (Task 4)
- Produces: `pushBlob(userId, blobType, version, plaintext): Promise<{ok:
  true, version: number} | {ok: false, currentVersion: number,
  currentBlob: string}>`, `pullBlob(userId, blobType): Promise<{version:
  number, blob: string} | null>` — this is the full server-side contract
  the ocode client (Part B) talks to.

- [ ] **Step 1: Write the failing service test**

```typescript
import { describe, it, expect } from 'bun:test';
import { randomUUID } from 'node:crypto';
import { pushBlob, pullBlob } from './sync-service';
import { db } from '@/server/db';
import { users } from '@/server/db/schema';

async function makeUser() {
  const [user] = await db
    .insert(users)
    .values({ email: `${randomUUID()}@test.local`, emailVerified: true })
    .returning();
  return user!;
}

describe('sync-service', () => {
  it('first push for a user creates the row at version 1', async () => {
    const user = await makeUser();
    const result = await pushBlob(user.id, 'ocodeconfig', 0, '{"theme":"dark"}');
    expect(result).toEqual({ ok: true, version: 1 });

    const pulled = await pullBlob(user.id, 'ocodeconfig');
    expect(pulled?.version).toBe(1);
    expect(pulled?.blob).toBe('{"theme":"dark"}');
  });

  it('rejects a stale version and returns the current blob', async () => {
    const user = await makeUser();
    await pushBlob(user.id, 'ocodeconfig', 0, '{"theme":"dark"}');

    const stale = await pushBlob(user.id, 'ocodeconfig', 0, '{"theme":"light"}');
    expect(stale.ok).toBe(false);
    if (!stale.ok) {
      expect(stale.currentVersion).toBe(1);
      expect(stale.currentBlob).toBe('{"theme":"dark"}');
    }
  });

  it('accepts a push at the correct next version', async () => {
    const user = await makeUser();
    await pushBlob(user.id, 'ocodeconfig', 0, '{"theme":"dark"}');
    const second = await pushBlob(user.id, 'ocodeconfig', 1, '{"theme":"light"}');
    expect(second).toEqual({ ok: true, version: 2 });
  });

  it('serializes concurrent pushes for the same user', async () => {
    const user = await makeUser();
    await pushBlob(user.id, 'ocodeconfig', 0, '{"n":0}');

    const [a, b] = await Promise.all([
      pushBlob(user.id, 'ocodeconfig', 1, '{"n":1}'),
      pushBlob(user.id, 'ocodeconfig', 1, '{"n":2}'),
    ]);
    // Exactly one of the two racing pushes succeeds at version 2; the
    // other observes the lock and sees the now-current version and loses.
    const outcomes = [a, b];
    const succeeded = outcomes.filter(o => o.ok);
    expect(succeeded).toHaveLength(1);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bun test --serial --isolate ./src/server/services/ocode-sync/sync-service.test.ts`
Expected: FAIL — module does not exist

- [ ] **Step 3: Implement `sync-service.ts`**

```typescript
import { eq, and, sql } from 'drizzle-orm';
import { db } from '@/server/db';
import {
  ocodeSyncKeys,
  ocodeConfigSyncs,
  type ocodeBlobTypeEnum,
} from '@/server/db/schema';
import { generateWrappedDek, unwrapDek, encryptBlob, decryptBlob } from './crypto';

type BlobType = (typeof ocodeBlobTypeEnum.enumValues)[number];

async function getOrCreateDek(userId: string): Promise<Buffer> {
  const [existing] = await db
    .select()
    .from(ocodeSyncKeys)
    .where(eq(ocodeSyncKeys.userId, userId));
  if (existing) return unwrapDek(existing.wrappedDek);

  const wrapped = generateWrappedDek();
  await db.insert(ocodeSyncKeys).values({ userId, wrappedDek: wrapped });
  return unwrapDek(wrapped);
}

export async function pushBlob(
  userId: string,
  blobType: BlobType,
  clientVersion: number,
  plaintext: string
): Promise<
  | { ok: true; version: number }
  | { ok: false; currentVersion: number; currentBlob: string }
> {
  return db.transaction(async tx => {
    await tx.execute(sql`select pg_advisory_xact_lock(hashtext(${userId})::bigint)`);

    const dek = await getOrCreateDek(userId);

    const [existing] = await tx
      .select()
      .from(ocodeConfigSyncs)
      .where(
        and(
          eq(ocodeConfigSyncs.userId, userId),
          eq(ocodeConfigSyncs.blobType, blobType)
        )
      );

    if (!existing) {
      if (clientVersion !== 0) {
        return { ok: false as const, currentVersion: 0, currentBlob: '' };
      }
      await tx.insert(ocodeConfigSyncs).values({
        userId,
        blobType,
        ciphertext: encryptBlob(dek, plaintext),
        version: 1,
      });
      return { ok: true as const, version: 1 };
    }

    if (existing.version !== clientVersion) {
      return {
        ok: false as const,
        currentVersion: existing.version,
        currentBlob: decryptBlob(dek, existing.ciphertext),
      };
    }

    const nextVersion = existing.version + 1;
    await tx
      .update(ocodeConfigSyncs)
      .set({ ciphertext: encryptBlob(dek, plaintext), version: nextVersion })
      .where(eq(ocodeConfigSyncs.id, existing.id));

    return { ok: true as const, version: nextVersion };
  });
}

export async function pullBlob(
  userId: string,
  blobType: BlobType
): Promise<{ version: number; blob: string } | null> {
  const [row] = await db
    .select()
    .from(ocodeConfigSyncs)
    .where(
      and(
        eq(ocodeConfigSyncs.userId, userId),
        eq(ocodeConfigSyncs.blobType, blobType)
      )
    );
  if (!row) return null;

  const dek = await getOrCreateDek(userId);
  return { version: row.version, blob: decryptBlob(dek, row.ciphertext) };
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bun test --serial --isolate ./src/server/services/ocode-sync/sync-service.test.ts`
Expected: PASS

- [ ] **Step 5: Write and implement the route (test + implementation together, same shape as Task 4)**

```typescript
// src/server/api/routes/ocode.sync.test.ts
import { describe, it, expect } from 'bun:test';
import app from '../index';
import { db } from '@/server/db';
import { users, ocodeSyncTokens } from '@/server/db/schema';
import { randomUUID, randomBytes, createHash } from 'node:crypto';

async function makeAuthedUser() {
  const [user] = await db
    .insert(users)
    .values({ email: `${randomUUID()}@test.local`, emailVerified: true })
    .returning();
  const token = randomBytes(32).toString('base64url');
  await db.insert(ocodeSyncTokens).values({
    userId: user!.id,
    tokenHash: createHash('sha256').update(token).digest('hex'),
  });
  return { user: user!, token };
}

describe('PUT /api/ocode/sync/:blobType', () => {
  it('rejects requests without a bearer token', async () => {
    const res = await app.request('/api/ocode/sync/ocodeconfig', {
      method: 'PUT',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ version: 0, blob: '{}' }),
    });
    expect(res.status).toBe(401);
  });

  it('pushes then pulls a blob for an authed user', async () => {
    const { token } = await makeAuthedUser();
    const push = await app.request('/api/ocode/sync/ocodeconfig', {
      method: 'PUT',
      headers: {
        'content-type': 'application/json',
        authorization: `Bearer ${token}`,
      },
      body: JSON.stringify({ version: 0, blob: '{"theme":"dark"}' }),
    });
    expect(push.status).toBe(200);
    expect((await push.json())).toEqual({ ok: true, version: 1 });

    const pull = await app.request('/api/ocode/sync/ocodeconfig', {
      headers: { authorization: `Bearer ${token}` },
    });
    expect(await pull.json()).toEqual({ version: 1, blob: '{"theme":"dark"}' });
  });
});
```

```typescript
// src/server/api/routes/ocode.sync.ts
import { Hono } from 'hono';
import { zValidator } from '@hono/zod-validator';
import { z } from 'zod';
import { pushBlob, pullBlob } from '@/server/services/ocode-sync/sync-service';
import { requireSyncToken, type SyncEnv } from '../lib/ocode-sync-auth';

const app = new Hono<SyncEnv>();

const blobTypeParam = z.enum(['ocodeconfig', 'authsecrets']);

app.get('/:blobType', requireSyncToken, async c => {
  const blobType = blobTypeParam.parse(c.req.param('blobType'));
  const userId = c.get('syncUserId');
  const result = await pullBlob(userId, blobType);
  if (!result) return c.json({ version: 0, blob: null });
  return c.json(result);
});

app.put(
  '/:blobType',
  zValidator('json', z.object({ version: z.number().int(), blob: z.string() })),
  requireSyncToken,
  async c => {
    const blobType = blobTypeParam.parse(c.req.param('blobType'));
    const userId = c.get('syncUserId');
    const { version, blob } = c.req.valid('json');
    const result = await pushBlob(userId, blobType, version, blob);
    return c.json(result);
  }
);

export default app;
```

Register in `src/server/api/index.ts`:

```typescript
import ocodeSyncRoutes from './routes/ocode.sync';
// ...
apiApp.route('/ocode/sync', ocodeSyncRoutes);
```

- [ ] **Step 6: Run both test files to verify they pass**

Run: `bun test --serial --isolate ./src/server/api/routes/ocode.sync.test.ts ./src/server/services/ocode-sync/sync-service.test.ts`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add src/server/services/ocode-sync/sync-service.ts src/server/services/ocode-sync/sync-service.test.ts src/server/api/routes/ocode.sync.ts src/server/api/routes/ocode.sync.test.ts src/server/api/index.ts
git commit -m "feat(ocode-sync): versioned blob push/pull routes with advisory-lock concurrency"
```

---

## Part B — ocode client (Go) ✅ Implemented in commit `1b4e252`

### Task 6: `internal/sync` package skeleton — types and local paths

**Files:**
- Create: `internal/sync/sync.go`
- Test: `internal/sync/sync_test.go`

**Interfaces:**
- Produces: `type BlobType string` (`BlobTypeConfig = "ocodeconfig"`,
  `BlobTypeAuth = "authsecrets"`), `SnapshotPath(blob BlobType) (string,
  error)`, `TokenPath() (string, error)` — consumed by every later Go task.

- [ ] **Step 1: Write the failing test**

```go
package sync

import "testing"

func TestSnapshotPathDiffersPerBlobType(t *testing.T) {
	configPath, err := SnapshotPath(BlobTypeConfig)
	if err != nil {
		t.Fatalf("SnapshotPath(config): %v", err)
	}
	authPath, err := SnapshotPath(BlobTypeAuth)
	if err != nil {
		t.Fatalf("SnapshotPath(auth): %v", err)
	}
	if configPath == authPath {
		t.Fatalf("expected distinct paths, got %q for both", configPath)
	}
}

func TestTokenPathIsNonEmpty(t *testing.T) {
	path, err := TokenPath()
	if err != nil {
		t.Fatalf("TokenPath: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty token path")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sync/...`
Expected: FAIL — package does not exist

- [ ] **Step 3: Implement `sync.go`**

```go
package sync

import (
	"path/filepath"

	"github.com/u007/ocode/internal/paths"
)

type BlobType string

const (
	BlobTypeConfig BlobType = "ocodeconfig"
	BlobTypeAuth   BlobType = "authsecrets"
)

// SnapshotPath returns the path of the last-synced JSON snapshot for a
// blob type — the "base" side of the 3-way merge in Task 7.
func SnapshotPath(blob BlobType) (string, error) {
	dir, err := paths.GlobalDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sync", string(blob)+".snapshot.json"), nil
}

// TokenPath returns the path of the stored sync bearer token.
func TokenPath() (string, error) {
	dir, err := paths.GlobalDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sync", "token"), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/sync/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/sync/sync.go internal/sync/sync_test.go
git commit -m "feat(sync): internal/sync package skeleton (blob types, local paths)"
```

---

### Task 7: 3-way JSON merge with per-blob-type conflict policy

**Files:**
- Create: `internal/sync/merge.go`
- Test: `internal/sync/merge_test.go`

**Interfaces:**
- Consumes: `BlobType` (Task 6)
- Produces: `Merge(blob BlobType, base, local, remote json.RawMessage)
  (json.RawMessage, error)` — consumed by Task 9 (push flow).

- [ ] **Step 1: Write the failing tests**

```go
package sync

import (
	"encoding/json"
	"testing"
)

func mustJSON(t *testing.T, s string) json.RawMessage {
	t.Helper()
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("invalid test fixture JSON %q: %v", s, err)
	}
	return json.RawMessage(s)
}

func TestMergeNonConflictingKeysUnion(t *testing.T) {
	base := mustJSON(t, `{"a":1,"b":2}`)
	local := mustJSON(t, `{"a":1,"b":2,"c":3}`)
	remote := mustJSON(t, `{"a":1,"b":9}`)

	merged, err := Merge(BlobTypeConfig, base, local, remote)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(merged, &out); err != nil {
		t.Fatalf("unmarshal merged: %v", err)
	}
	if out["b"].(float64) != 9 {
		t.Errorf("expected remote's change to 'b' to survive, got %v", out["b"])
	}
	if out["c"].(float64) != 3 {
		t.Errorf("expected local-only key 'c' to survive, got %v", out["c"])
	}
}

func TestMergeConfigConflictLocalWins(t *testing.T) {
	base := mustJSON(t, `{"theme":"light"}`)
	local := mustJSON(t, `{"theme":"dark"}`)
	remote := mustJSON(t, `{"theme":"solarized"}`)

	merged, err := Merge(BlobTypeConfig, base, local, remote)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	var out map[string]string
	json.Unmarshal(merged, &out)
	if out["theme"] != "dark" {
		t.Errorf("expected local-wins for ocodeconfig, got %q", out["theme"])
	}
}

func TestMergeAuthConflictRemoteWins(t *testing.T) {
	base := mustJSON(t, `{"anthropic":{"token":"old"}}`)
	local := mustJSON(t, `{"anthropic":{"token":"stale-local"}}`)
	remote := mustJSON(t, `{"anthropic":{"token":"rotated-remote"}}`)

	merged, err := Merge(BlobTypeAuth, base, local, remote)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	var out map[string]map[string]string
	json.Unmarshal(merged, &out)
	if out["anthropic"]["token"] != "rotated-remote" {
		t.Errorf("expected remote-wins for authsecrets, got %q", out["anthropic"]["token"])
	}
}

func TestMergeEmptyBaseTreatsAllLocalKeysAsNew(t *testing.T) {
	base := mustJSON(t, `{}`)
	local := mustJSON(t, `{"theme":"dark"}`)
	remote := mustJSON(t, `{}`)

	merged, err := Merge(BlobTypeConfig, base, local, remote)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	var out map[string]string
	json.Unmarshal(merged, &out)
	if out["theme"] != "dark" {
		t.Errorf("expected local-only key to survive an empty base, got %v", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sync/... -run TestMerge`
Expected: FAIL — `Merge` not defined

- [ ] **Step 3: Implement `merge.go`**

```go
package sync

import (
	"encoding/json"
	"fmt"
)

// Merge performs a top-level-key 3-way merge: keys changed only on one
// side since base are taken as-is; keys changed on both sides resolve per
// blobType's conflict policy (local-wins for config, remote-wins for
// secrets — see spec §5).
func Merge(blobType BlobType, base, local, remote json.RawMessage) (json.RawMessage, error) {
	baseMap, err := toMap(base)
	if err != nil {
		return nil, fmt.Errorf("decode base: %w", err)
	}
	localMap, err := toMap(local)
	if err != nil {
		return nil, fmt.Errorf("decode local: %w", err)
	}
	remoteMap, err := toMap(remote)
	if err != nil {
		return nil, fmt.Errorf("decode remote: %w", err)
	}

	merged := make(map[string]json.RawMessage, len(remoteMap))
	for k, v := range remoteMap {
		merged[k] = v
	}

	for k, localVal := range localMap {
		baseVal, hadBase := baseMap[k]
		remoteVal, hasRemote := remoteMap[k]

		localChanged := !hadBase || !equalRaw(baseVal, localVal)
		if !localChanged {
			continue // local matches base — nothing to bring forward
		}

		remoteChanged := hasRemote && (!hadBase || !equalRaw(baseVal, remoteVal))
		if hasRemote && remoteChanged {
			// Changed on both sides since base: resolve per policy.
			if blobType == BlobTypeConfig {
				merged[k] = localVal
			}
			// else (BlobTypeAuth): keep remote's value, already in `merged`.
			continue
		}

		// Only local changed (or key is local-only): bring it forward.
		merged[k] = localVal
	}

	out, err := json.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("encode merged: %w", err)
	}
	return out, nil
}

func toMap(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]json.RawMessage{}
	}
	return m, nil
}

func equalRaw(a, b json.RawMessage) bool {
	var av, bv interface{}
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		return string(a) == string(b)
	}
	aj, _ := json.Marshal(av)
	bj, _ := json.Marshal(bv)
	return string(aj) == string(bj)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/sync/... -run TestMerge -v`
Expected: PASS (all 4 cases)

- [ ] **Step 5: Commit**

```bash
git add internal/sync/merge.go internal/sync/merge_test.go
git commit -m "feat(sync): 3-way JSON merge with per-blob-type conflict policy"
```

---

### Task 8: HTTP client — device login (start/poll) and token storage

**Files:**
- Create: `internal/sync/client.go`
- Create: `internal/sync/device_login.go`
- Test: `internal/sync/device_login_test.go`

**Interfaces:**
- Consumes: `TokenPath` (Task 6)
- Produces: `type Client struct { BaseURL string; HTTPClient *http.Client
  }`, `func (c *Client) Login(ctx context.Context, openBrowser
  func(url string) error, print func(format string, args ...interface{}))
  error`, `func SaveToken(token string) error`, `func LoadToken() (string,
  bool, error)` — consumed by Task 13 (TUI command).

- [ ] **Step 1: Write the failing test (using an `httptest.Server` fake of
  the two device endpoints)**

```go
package sync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoginPollsUntilApprovedAndSavesToken(t *testing.T) {
	pollCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ocode/device/start":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"deviceCode": "dc-1", "userCode": "AAAA-BBBB",
				"verifyUrl": "http://example.invalid/device", "expiresIn": 600,
			})
		case "/api/ocode/device/token":
			pollCount++
			if pollCount < 2 {
				json.NewEncoder(w).Encode(map[string]string{"status": "pending"})
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"status": "approved", "token": "tok-123"})
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	t.Setenv("XDG_DATA_HOME", t.TempDir())

	client := &Client{BaseURL: srv.URL, HTTPClient: srv.Client(), pollInterval: 0}
	var openedURL string
	err := client.Login(context.Background(), func(url string) error {
		openedURL = url
		return nil
	}, func(string, ...interface{}) {})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if openedURL != "http://example.invalid/device" {
		t.Errorf("expected browser opened to verify url, got %q", openedURL)
	}

	token, ok, err := LoadToken()
	if err != nil {
		t.Fatalf("LoadToken: %v", err)
	}
	if !ok || token != "tok-123" {
		t.Errorf("expected saved token %q, ok=%v", token, ok)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sync/... -run TestLogin`
Expected: FAIL — `Client`/`Login` not defined

- [ ] **Step 3: Implement `client.go`**

```go
package sync

import "net/http"

// Client talks to the kakiit /api/ocode/* endpoints.
type Client struct {
	BaseURL      string
	HTTPClient   *http.Client
	pollInterval time.Duration // exposed for tests; zero-value = production default
}

func NewClient(baseURL string) *Client {
	return &Client{BaseURL: baseURL, HTTPClient: http.DefaultClient, pollInterval: 2 * time.Second}
}

func (c *Client) interval() time.Duration {
	if c.pollInterval == 0 {
		return 2 * time.Second
	}
	return c.pollInterval
}
```

(Add `"time"` to the import block.)

- [ ] **Step 4: Implement `device_login.go`**

```go
package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type deviceStartResponse struct {
	DeviceCode string `json:"deviceCode"`
	UserCode   string `json:"userCode"`
	VerifyURL  string `json:"verifyUrl"`
	ExpiresIn  int    `json:"expiresIn"`
}

type deviceTokenResponse struct {
	Status string `json:"status"`
	Token  string `json:"token"`
}

// Login runs the device-code flow: starts it, opens the verify URL via
// openBrowser, polls until approved or expired, and saves the resulting
// bearer token locally.
func (c *Client) Login(ctx context.Context, openBrowser func(url string) error, print func(format string, args ...interface{})) error {
	start, err := c.deviceStart(ctx)
	if err != nil {
		return fmt.Errorf("start device flow: %w", err)
	}

	print("Enter code %s at %s\n", start.UserCode, start.VerifyURL)
	if err := openBrowser(start.VerifyURL); err != nil {
		print("Could not open browser automatically: %v\n", err)
	}

	deadline := time.Now().Add(time.Duration(start.ExpiresIn) * time.Second)
	for time.Now().Before(deadline) {
		resp, err := c.deviceToken(ctx, start.DeviceCode)
		if err != nil {
			return fmt.Errorf("poll device token: %w", err)
		}
		switch resp.Status {
		case "approved":
			return SaveToken(resp.Token)
		case "expired":
			return fmt.Errorf("device code expired before approval")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(c.interval()):
		}
	}
	return fmt.Errorf("device code expired before approval")
}

func (c *Client) deviceStart(ctx context.Context) (*deviceStartResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/ocode/device/start", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var out deviceStartResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) deviceToken(ctx context.Context, deviceCode string) (*deviceTokenResponse, error) {
	body, _ := json.Marshal(map[string]string{"deviceCode": deviceCode})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/ocode/device/token", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var out deviceTokenResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SaveToken persists the sync bearer token at 0600, mirroring auth.json's
// existing local-file permission convention.
func SaveToken(token string) error {
	path, err := TokenPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(token), 0o600)
}

// LoadToken reads the stored sync bearer token. A missing file is not an
// error — it means the user hasn't logged in.
func LoadToken() (string, bool, error) {
	path, err := TokenPath()
	if err != nil {
		return "", false, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(data), true, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/sync/... -run TestLogin -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/sync/client.go internal/sync/device_login.go internal/sync/device_login_test.go
git commit -m "feat(sync): device-code login client (start/poll/save token)"
```

---

### Task 9: Push flow — bounded-retry merge, never fails to the user

**Files:**
- Create: `internal/sync/push.go`
- Test: `internal/sync/push_test.go`

**Interfaces:**
- Consumes: `Client` (Task 8), `Merge` (Task 7), `SnapshotPath` (Task 6)
- Produces: `func (c *Client) Push(ctx context.Context, blob BlobType,
  local json.RawMessage) error` — consumed by Task 11 (debounce watcher).

- [ ] **Step 1: Write the failing tests**

```go
package sync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestPushSucceedsOnFirstTry(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	pushCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pushCount++
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "version": 1})
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	err := c.Push(context.Background(), BlobTypeConfig, json.RawMessage(`{"theme":"dark"}`))
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if pushCount != 1 {
		t.Errorf("expected exactly one push attempt, got %d", pushCount)
	}
}

func TestPushRetriesOnConflictThenSucceeds(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	snapshotPath, _ := SnapshotPath(BlobTypeConfig)
	os.MkdirAll(fileDir(snapshotPath), 0o700)
	os.WriteFile(snapshotPath, []byte(`{"theme":"light"}`), 0o600)

	attempt := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt == 1 {
			// currentBlob is the JSON blob encoded as a *string* field, matching
			// sync-service.ts's pushBlob() failure shape from Task 5
			// ({ok:false, currentVersion, currentBlob: string}).
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": false, "currentVersion": 5,
				"currentBlob": `{"theme":"light","extra":"remote"}`,
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "version": 6})
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	err := c.Push(context.Background(), BlobTypeConfig, json.RawMessage(`{"theme":"dark"}`))
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if attempt != 2 {
		t.Errorf("expected retry after conflict, got %d attempts", attempt)
	}
}

func TestPushGivesUpSilentlyAfterMaxRetries(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": false, "currentVersion": 1, "currentBlob": `{"theme":"remote"}`,
		})
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTPClient: srv.Client(), retryBackoff: 0}
	err := c.Push(context.Background(), BlobTypeConfig, json.RawMessage(`{"theme":"local"}`))
	if err != nil {
		t.Fatalf("Push must not return an error to the caller even after exhausting retries, got: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sync/... -run TestPush`
Expected: FAIL — `Push` not defined

- [ ] **Step 3: Implement `push.go`**

```go
package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const maxPushRetries = 5

func fileDir(path string) string { return filepath.Dir(path) }

type pushResponse struct {
	OK             bool   `json:"ok"`
	Version        int    `json:"version"`
	CurrentVersion int    `json:"currentVersion"`
	CurrentBlob    string `json:"currentBlob"` // JSON blob encoded as a string, per sync-service.ts's pushBlob()
}

// Push sends the local blob to the server, auto-merging and retrying on a
// version conflict. It never returns an error to the caller for a
// resolvable conflict — see spec §5. Retries are bounded; after
// exhausting them it logs and gives up for this cycle only.
func (c *Client) Push(ctx context.Context, blob BlobType, local json.RawMessage) error {
	token, ok, err := LoadToken()
	if err != nil {
		return fmt.Errorf("load sync token: %w", err)
	}
	if !ok {
		return nil // not logged in — nothing to do, not an error
	}

	snapshotPath, err := SnapshotPath(blob)
	if err != nil {
		return err
	}
	base := readSnapshot(snapshotPath)
	version := lastKnownVersion(snapshotPath)
	payload := local

	for attempt := 0; attempt < maxPushRetries; attempt++ {
		resp, err := c.pushOnce(ctx, token, blob, version, payload)
		if err != nil {
			return fmt.Errorf("push %s: %w", blob, err)
		}
		if resp.OK {
			writeSnapshot(snapshotPath, payload, resp.Version)
			return nil
		}

		merged, err := Merge(blob, base, payload, json.RawMessage(resp.CurrentBlob))
		if err != nil {
			log.Printf("sync: merge failed for %s, will retry next cycle: %v", blob, err)
			return nil
		}
		if err := os.WriteFile(localConfigPathFor(blob), merged, 0o600); err != nil {
			log.Printf("sync: writing merged %s to disk failed, will retry next cycle: %v", blob, err)
			return nil
		}
		base = json.RawMessage(resp.CurrentBlob)
		version = resp.CurrentVersion
		payload = merged

		time.Sleep(retryBackoffDuration(c, attempt))
	}

	log.Printf("sync: gave up pushing %s after %d attempts (persistent conflict), will retry next cycle", blob, maxPushRetries)
	return nil
}

func (c *Client) pushOnce(ctx context.Context, token string, blob BlobType, version int, payload json.RawMessage) (*pushResponse, error) {
	body, _ := json.Marshal(map[string]interface{}{"version": version, "blob": string(payload)})
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.BaseURL+"/api/ocode/sync/"+string(blob), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var out pushResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func retryBackoffDuration(c *Client, attempt int) time.Duration {
	if c.retryBackoff == 0 && c.retryBackoffSet {
		return 0 // explicitly disabled in tests
	}
	base := c.retryBackoff
	if base == 0 {
		base = 250 * time.Millisecond
	}
	return base << attempt
}
```

Add `retryBackoff time.Duration` and `retryBackoffSet bool` fields to the
`Client` struct in `client.go`, and implement `readSnapshot`,
`lastKnownVersion`, `writeSnapshot`, `localConfigPathFor` as small helpers
in `push.go` (read/write the snapshot JSON file at `SnapshotPath(blob)`,
storing `{"version": N, "blob": <raw>}`; `localConfigPathFor` maps
`BlobTypeConfig`/`BlobTypeAuth` to the real config paths via
`config.ActiveOcodeConfigPath()` / the equivalent auth store path helper).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/sync/... -run TestPush -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/sync/push.go internal/sync/push_test.go internal/sync/client.go
git commit -m "feat(sync): push flow with bounded-retry auto-merge on conflict"
```

---

### Task 10: Pull flow (startup opportunistic pull-merge)

**Files:**
- Create: `internal/sync/pull.go`
- Test: `internal/sync/pull_test.go`

**Interfaces:**
- Consumes: `Client.pushOnce`-style HTTP plumbing (Task 8/9), `Merge`
  (Task 7)
- Produces: `func (c *Client) Pull(ctx context.Context, blob BlobType)
  error` — consumed by Task 13 (startup hook).

- [ ] **Step 1: Write the failing test**

```go
package sync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestPullBootstrapsEmptyLocalFile(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"version": 3, "blob": `{"theme":"dark"}`})
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	if err := SaveToken("tok"); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}
	if err := c.Pull(context.Background(), BlobTypeConfig); err != nil {
		t.Fatalf("Pull: %v", err)
	}

	localPath := localConfigPathFor(BlobTypeConfig)
	data, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("expected local file written by Pull: %v", err)
	}
	if string(data) != `{"theme":"dark"}` {
		t.Errorf("unexpected local content: %s", data)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sync/... -run TestPull`
Expected: FAIL — `Pull` not defined

- [ ] **Step 3: Implement `pull.go`**

```go
package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

type pullResponse struct {
	Version int    `json:"version"`
	Blob    string `json:"blob"`
}

// Pull fetches the server's current blob and merges it with whatever's
// on disk locally (3-way, using the last-synced snapshot as base — an
// empty snapshot on a brand-new machine means every local key is "new").
func (c *Client) Pull(ctx context.Context, blob BlobType) error {
	token, ok, err := LoadToken()
	if err != nil {
		return fmt.Errorf("load sync token: %w", err)
	}
	if !ok {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/ocode/sync/"+string(blob), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	var out pullResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return err
	}
	if out.Blob == "" {
		return nil // nothing synced yet for this user
	}

	snapshotPath, err := SnapshotPath(blob)
	if err != nil {
		return err
	}
	base := readSnapshot(snapshotPath)
	localPath := localConfigPathFor(blob)
	local, _ := os.ReadFile(localPath) // missing local file == empty JSON object

	merged, err := Merge(blob, base, jsonOrEmptyObject(local), json.RawMessage(out.Blob))
	if err != nil {
		return fmt.Errorf("merge on pull: %w", err)
	}
	if err := os.WriteFile(localPath, merged, 0o600); err != nil {
		return err
	}
	writeSnapshot(snapshotPath, merged, out.Version)
	return nil
}

func jsonOrEmptyObject(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(raw)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/sync/... -run TestPull -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/sync/pull.go internal/sync/pull_test.go
git commit -m "feat(sync): startup opportunistic pull-merge"
```

---

### Task 11: Hook config-save choke points + debounced background push

**Files:**
- Modify: `internal/config/ocodeconfig.go` (`withOcodeConfigLock`, ~line
  1062-1077)
- Modify: `internal/auth/store.go` (`persistLocked`, ~line 394-418)
- Create: `internal/sync/watcher.go`
- Test: `internal/sync/watcher_test.go`

**Interfaces:**
- Consumes: `Client.Push` (Task 9)
- Produces: `var OnConfigSaved func()` (in `internal/config`), `var
  OnCredentialsSaved func()` (in `internal/auth`), `func
  StartWatcher(client *Client) (stop func())` in `internal/sync` — consumed
  by Task 13 (serve/CLI startup wiring).

- [ ] **Step 1: Add optional hook variables to the two existing choke points**

In `internal/config/ocodeconfig.go`, right after the existing
`withOcodeConfigLock` function:

```go
// OnConfigSaved, if set, is invoked after every successful ocodeconfig.json
// write. Used by internal/sync to debounce a background push; nil by
// default so this package has no dependency on sync.
var OnConfigSaved func()

func withOcodeConfigLock(fn func(cfg *OcodeConfig) error) error {
	unlock, err := lockOcodeConfig()
	if err != nil {
		return err
	}
	defer unlock()

	cfg, err := loadFullOcodeConfig()
	if err != nil {
		return fmt.Errorf("load ocode config: %w", err)
	}
	if err := fn(cfg); err != nil {
		return err
	}
	if err := SaveOcodeConfig(cfg); err != nil {
		return err
	}
	if OnConfigSaved != nil {
		OnConfigSaved()
	}
	return nil
}
```

In `internal/auth/store.go`, add the same pattern around `persistLocked`:

```go
// OnCredentialsSaved, if set, is invoked after every successful auth.json
// write. Used by internal/sync to debounce a background push.
var OnCredentialsSaved func()

func persistLocked() error {
	// ...existing body unchanged up to the final os.Chmod call...
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod auth file: %w", err)
	}
	if OnCredentialsSaved != nil {
		OnCredentialsSaved()
	}
	return nil
}
```

- [ ] **Step 2: Write the failing watcher test**

```go
package sync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/u007/ocode/internal/config"
)

func TestWatcherDebouncesRapidSaves(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	var pushCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&pushCount, 1)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "version": 1})
	}))
	defer srv.Close()
	SaveToken("tok")

	c := &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	stop := StartWatcher(c, WithDebounce(20*time.Millisecond))
	defer stop()

	for i := 0; i < 5; i++ {
		config.OnConfigSaved()
		time.Sleep(5 * time.Millisecond)
	}

	time.Sleep(60 * time.Millisecond)
	if got := atomic.LoadInt32(&pushCount); got != 1 {
		t.Errorf("expected exactly one debounced push for 5 rapid saves, got %d", got)
	}
	_ = context.Background()
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/sync/... -run TestWatcher`
Expected: FAIL — `StartWatcher`/`WithDebounce` not defined

- [ ] **Step 4: Implement `watcher.go`**

```go
package sync

import (
	"context"
	"log"
	"time"

	"github.com/u007/ocode/internal/auth"
	"github.com/u007/ocode/internal/config"
)

const defaultDebounce = 5 * time.Second

type watcherOptions struct {
	debounce time.Duration
}

type WatcherOption func(*watcherOptions)

// WithDebounce overrides the default 5s idle debounce; exposed for tests.
func WithDebounce(d time.Duration) WatcherOption {
	return func(o *watcherOptions) { o.debounce = d }
}

// StartWatcher hooks config.OnConfigSaved / auth.OnCredentialsSaved and
// pushes each blob to the server after a debounce period of inactivity.
// Returns a stop func that unhooks and stops the timers.
func StartWatcher(c *Client, opts ...WatcherOption) (stop func()) {
	o := watcherOptions{debounce: defaultDebounce}
	for _, apply := range opts {
		apply(&o)
	}

	configCh := make(chan struct{}, 1)
	authCh := make(chan struct{}, 1)

	config.OnConfigSaved = func() { nonBlockingSignal(configCh) }
	auth.OnCredentialsSaved = func() { nonBlockingSignal(authCh) }

	done := make(chan struct{})
	go debounceLoop(configCh, done, o.debounce, func() { pushBlobFromDisk(c, BlobTypeConfig) })
	go debounceLoop(authCh, done, o.debounce, func() { pushBlobFromDisk(c, BlobTypeAuth) })

	return func() {
		close(done)
		config.OnConfigSaved = nil
		auth.OnCredentialsSaved = nil
	}
}

func nonBlockingSignal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func debounceLoop(signal <-chan struct{}, done <-chan struct{}, debounce time.Duration, fire func()) {
	var timer *time.Timer
	var timerCh <-chan time.Time
	for {
		select {
		case <-done:
			if timer != nil {
				timer.Stop()
			}
			return
		case <-signal:
			if timer != nil {
				timer.Stop()
			}
			timer = time.NewTimer(debounce)
			timerCh = timer.C
		case <-timerCh:
			fire()
			timerCh = nil
		}
	}
}

func pushBlobFromDisk(c *Client, blob BlobType) {
	path := localConfigPathFor(blob)
	data, err := readLocalConfigFile(path)
	if err != nil {
		log.Printf("sync: reading %s for background push failed: %v", blob, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.Push(ctx, blob, data); err != nil {
		log.Printf("sync: background push of %s failed: %v", blob, err)
	}
}
```

(`readLocalConfigFile` is a small helper added alongside `localConfigPathFor`
from Task 9 — reads the file and returns `json.RawMessage`, wrapping a
missing file as `{}` via the existing `jsonOrEmptyObject` from Task 10.)

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/sync/... -run TestWatcher -v`
Expected: PASS

- [ ] **Step 6: Run the full existing config/auth test suites to confirm no regression**

Run: `go test ./internal/config/... ./internal/auth/...`
Expected: PASS (unchanged behavior — the new hook vars default to `nil`
and are no-ops until `StartWatcher` sets them)

- [ ] **Step 7: Commit**

```bash
git add internal/config/ocodeconfig.go internal/auth/store.go internal/sync/watcher.go internal/sync/watcher_test.go
git commit -m "feat(sync): debounced background push hooked into config/auth save choke points"
```

---

### Task 12: `/login` and `/logout` TUI commands

**Files:**
- Create: `internal/tui/command_login.go`
- Modify: `internal/tui/commands.go` (register `/login`, `/logout` in
  `commandSpecs`, following the existing `/cron` entry at line 165)
- Test: `internal/tui/command_login_test.go`

**Interfaces:**
- Consumes: `Client.Login`, `sync.LoadToken` (Task 8), `sync.Client.Pull`
  (Task 10), `sync.StartWatcher` (Task 11)
- Produces: nothing further consumed elsewhere — this is the user-facing
  entry point.

- [ ] **Step 1: Write the failing test** (test the pure argument-parsing/
  dispatch logic — the actual network call is exercised by Task 8's tests)

```go
package tui

import "testing"

func TestLoginCommandRegistered(t *testing.T) {
	initCommands() // or whatever existing init hook populates commandLookup
	if _, ok := commandLookup["/login"]; !ok {
		t.Fatal("expected /login to be registered")
	}
	if _, ok := commandLookup["/logout"]; !ok {
		t.Fatal("expected /logout to be registered")
	}
}
```

(Match whatever the existing `commands_test.go` — if one exists — uses to
trigger command registration; if `commandSpecs` is populated by an
`init()` func rather than an explicit call, drop the `initCommands()` line
and rely on package init.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/... -run TestLoginCommandRegistered`
Expected: FAIL — `/login` not in `commandLookup`

- [ ] **Step 3: Implement `command_login.go`**

```go
package tui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/u007/ocode/internal/sync"
)

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

func runLoginCmd(m *model, args []string) tea.Cmd {
	return func() tea.Msg {
		client := sync.NewClient(kakiitBaseURL())
		err := client.Login(context.Background(), openBrowser, func(format string, a ...interface{}) {
			m.appendSystemLine(fmt.Sprintf(format, a...))
		})
		if err != nil {
			return statusMsg{text: fmt.Sprintf("Login failed: %v", err)}
		}
		if err := client.Pull(context.Background(), sync.BlobTypeConfig); err != nil {
			m.appendSystemLine(fmt.Sprintf("Warning: initial config pull failed: %v", err))
		}
		if err := client.Pull(context.Background(), sync.BlobTypeAuth); err != nil {
			m.appendSystemLine(fmt.Sprintf("Warning: initial credentials pull failed: %v", err))
		}
		sync.StartWatcher(client)
		return statusMsg{text: "Logged in — config sync active."}
	}
}

func runLogoutCmd(m *model, args []string) tea.Cmd {
	return func() tea.Msg {
		token, ok, err := sync.LoadToken()
		if err != nil || !ok {
			return statusMsg{text: "Not logged in."}
		}
		client := sync.NewClient(kakiitBaseURL())
		if err := client.Revoke(context.Background(), token); err != nil {
			m.appendSystemLine(fmt.Sprintf("Warning: server-side revoke failed: %v", err))
		}
		if err := sync.ClearToken(); err != nil {
			return statusMsg{text: fmt.Sprintf("Logout failed: %v", err)}
		}
		return statusMsg{text: "Logged out — sync stopped."}
	}
}
```

Add `kakiitBaseURL()` to `internal/tui/command_login.go`:

```go
import "os"

// kakiitBaseURL resolves the sync server URL. Defaults to the production
// kakiit instance; overridable for local development against a
// `bun run dev` kakiit checkout.
func kakiitBaseURL() string {
	if v := os.Getenv("OCODE_SYNC_URL"); v != "" {
		return v
	}
	return "https://kakiit.com"
}
```

Add `Revoke` to `internal/sync/device_login.go`:

```go
// Revoke tells the server to invalidate a sync bearer token.
func (c *Client) Revoke(ctx context.Context, token string) error {
	body, _ := json.Marshal(map[string]string{"token": token})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/ocode/device/revoke", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("revoke request failed: status %d", res.StatusCode)
	}
	return nil
}
```

Add `ClearToken` to `internal/sync/device_login.go`:

```go
// ClearToken deletes the locally stored sync bearer token. Missing file
// is not an error — logout is idempotent.
func ClearToken() error {
	path, err := TokenPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
```

`m.appendSystemLine` and `statusMsg` should match whatever existing
system-message mechanism `runCronCmd` (in `command_cron.go`) already uses
— reuse that exact call, don't invent a new one.

- [ ] **Step 4: Register the commands in `commands.go`**

```go
{name: "/login", help: "Log in and enable encrypted config sync", handler: runLoginCmd},
{name: "/logout", help: "Log out and stop config sync", handler: runLogoutCmd},
```

(Insert alongside the other entries in the `commandSpecs` slice, e.g. right
before the `/cron` line.)

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/tui/... -run TestLoginCommandRegistered -v`
Expected: PASS

- [ ] **Step 6: Full build check**

Run: `go build ./...`
Expected: builds cleanly

- [ ] **Step 7: Commit**

```bash
git add internal/tui/command_login.go internal/tui/commands.go internal/tui/command_login_test.go
git commit -m "feat(tui): add /login and /logout commands wiring device auth + sync"
```

---

## Self-review notes (carried over from plan authoring)

- `pushResponse.CurrentBlob` / `pullResponse.Blob` are both typed as Go
  `string` (not `json.RawMessage`), matching the wire shape kakiit's
  `sync-service.ts` actually returns (`currentBlob`/`blob` as JSON-encoded
  strings, per Task 5) — converted to `json.RawMessage` only at the call
  site immediately before `Merge`.
- `kakiitBaseURL()`, `Client.Revoke`, `sync.ClearToken` are folded into
  Task 12 rather than split into separate tasks — each is a small function
  with no independent test value beyond what Task 12's own test already
  covers end-to-end.
