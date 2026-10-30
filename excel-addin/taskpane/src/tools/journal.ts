import { Rect, cellAddress, parseAddress } from "../a1";
import type { CellValue } from "../port";
import { ToolError } from "./util";

export interface Step {
  id: string;
  tool: string;
  sheet: string;
  address: string;
  at: string;
  /** state before the write, as written back on undo (formula text or typed value) */
  before: CellValue[][];
  /** state right after the write, used to detect later edits by someone else */
  after: CellValue[][];
  /** number formats before the write; restored on undo BEFORE the values. Absent in steps recorded before formats were kept. */
  beforeFormats?: string[][];
  /** number formats right after the write */
  afterFormats?: string[][];
  undone?: boolean;
}

export interface JournalStore {
  load(): Promise<Step[]>;
  save(steps: Step[]): Promise<void>;
}

export class MemoryJournalStore implements JournalStore {
  steps: Step[] = [];
  async load() { return structuredClone(this.steps); }
  async save(steps: Step[]) { this.steps = structuredClone(steps); }
}

const MAX_STEPS = 100;
export const MAX_CHECKPOINT_CELLS = 20000;
/** A single step larger than this is refused before any cell changes. */
export const MAX_STEP_BYTES = 2 * 1024 * 1024;
/** Byte budget for the persisted journal (the bridge accepts 8 MiB per PUT). The newest step is always kept. */
export const MAX_JOURNAL_BYTES = 4 * 1024 * 1024;

const encoder = new TextEncoder();
/** UTF-8 size of the JSON encoding. The bridge limit is in bytes; JS string length undercounts non-ASCII text. */
export const jsonBytes = (v: unknown): number => encoder.encode(JSON.stringify(v)).length;

/** Drop the oldest steps until the persisted array fits MAX_JOURNAL_BYTES. Never drops the newest step. */
function trimToBytes(steps: Step[]): Step[] {
  // JSON array size = 1 + sum(step bytes + 1 separating comma)
  const sizes = steps.map((s) => jsonBytes(s) + 1);
  let total = 1 + sizes.reduce((a, b) => a + b, 0);
  let first = 0;
  while (first < steps.length - 1 && total > MAX_JOURNAL_BYTES) total -= sizes[first++];
  return first ? steps.slice(first) : steps;
}

export interface Conflict { address: string; expected: CellValue; actual: CellValue }

export class Journal {
  private steps: Step[] = [];
  private loaded = false;
  private seq = 0;

  constructor(private store: JournalStore, private now: () => Date = () => new Date()) {}

  private async ensure() {
    if (this.loaded) return;
    this.steps = await this.store.load();
    this.seq = this.steps.reduce((m, s) => Math.max(m, Number(s.id.replace(/^s/, "")) || 0), 0);
    this.loaded = true;
  }

  /**
   * Called BEFORE any cell is written: loads the journal so an unreachable bridge fails here, and refuses a step
   * whose estimated persisted size is too large to take.
   */
  async prepare(estimatedBytes: number): Promise<void> {
    await this.ensure();
    if (estimatedBytes > MAX_STEP_BYTES) {
      throw new ToolError(`This write needs about ${(estimatedBytes / 1048576).toFixed(1)} MiB of undo history, over the ${MAX_STEP_BYTES / 1048576} MiB limit for one step. Write in smaller blocks so every change can be undone.`);
    }
  }

  async record(s: Omit<Step, "id" | "at">): Promise<Step> {
    await this.ensure();
    const prevSteps = this.steps;
    const prevSeq = this.seq;
    const step: Step = { ...s, id: `s${this.seq + 1}`, at: this.now().toISOString() };
    let next = [...this.steps, step];
    if (next.length > MAX_STEPS) next = next.slice(next.length - MAX_STEPS);
    next = trimToBytes(next);
    this.seq += 1;
    this.steps = next;
    try {
      await this.store.save(this.steps);
    } catch (e) {
      // The step was not persisted: forget it and restore what was dropped to make room, so later writes are not poisoned.
      this.steps = prevSteps;
      this.seq = prevSeq;
      throw e;
    }
    return step;
  }

  /** Newest first. */
  async list(limit = 10): Promise<Step[]> {
    await this.ensure();
    return this.steps.slice(-limit).reverse();
  }

  async get(id: string): Promise<Step | undefined> {
    await this.ensure();
    return this.steps.find((s) => s.id === id);
  }

  async markUndone(id: string) {
    await this.ensure();
    const s = this.steps.find((x) => x.id === id);
    if (!s) return;
    s.undone = true;
    try {
      await this.store.save(this.steps);
    } catch (e) {
      s.undone = false; // memory must not claim an undo that was not persisted
      throw e;
    }
  }

  /** Cells whose current state differs from what the step left behind. */
  static conflicts(step: Step, rect: Rect, current: CellValue[][]): Conflict[] {
    const out: Conflict[] = [];
    step.after.forEach((row, i) => row.forEach((exp, j) => {
      const act = current[i]?.[j] ?? "";
      if (!same(exp, act)) out.push({ address: cellAddress(rect.c1 + j, rect.r1 + i), expected: exp, actual: act });
    }));
    return out;
  }
}

const same = (a: CellValue, b: CellValue) =>
  a === b || (a === null && b === "") || (a === "" && b === null) || (typeof a === "number" && typeof b === "number" && Math.abs(a - b) < 1e-9);

export function stepRect(step: Step): Rect {
  return { ...parseAddress(step.address)!, sheet: step.sheet };
}
