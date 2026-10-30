import { Rect, cellAddress, parseAddress } from "../a1";
import type { CellValue } from "../port";

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

  async record(s: Omit<Step, "id" | "at">): Promise<Step> {
    await this.ensure();
    const step: Step = { ...s, id: `s${++this.seq}`, at: this.now().toISOString() };
    this.steps.push(step);
    if (this.steps.length > MAX_STEPS) this.steps.splice(0, this.steps.length - MAX_STEPS);
    await this.store.save(this.steps);
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
    if (s) s.undone = true;
    await this.store.save(this.steps);
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
