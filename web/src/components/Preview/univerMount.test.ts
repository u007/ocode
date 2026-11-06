import { describe, it, expect, vi, beforeEach } from "vitest";
import { CommandType, LifecycleStages, type IWorkbookData } from "@univerjs/core";
import { mountUniverSheet } from "./univerMount";

// Univer needs canvas and a full UI plugin stack, neither of which jsdom has.
// The facade is replaced by a fake that keeps the two behaviours the dirty
// tracking depends on: lifecycle events replay the current stage to a new
// subscriber (as the facade's BehaviorSubject does), and command events fire
// synchronously in execution order.
const fake = vi.hoisted(() => {
  type Listener = (params: Record<string, unknown>) => void;
  const listeners = new Map<string, Set<Listener>>();
  const state = { stage: 0 };
  const fire = (name: string, params: Record<string, unknown>) => {
    for (const l of [...(listeners.get(name) ?? [])]) l(params);
  };
  const addEvent = (name: string, cb: Listener) => {
    let set = listeners.get(name);
    if (!set) {
      set = new Set();
      listeners.set(name, set);
    }
    set.add(cb);
    if (name === "LifeCycleChanged") cb({ stage: state.stage });
    return {
      dispose: () => {
        listeners.get(name)?.delete(cb);
      },
    };
  };
  // Cell (r, c) of the fake sheet: blank at A1, 42 at B1, "r:c" elsewhere.
  const cell = (r: number, c: number) => (r === 0 && c === 0 ? null : r === 0 && c === 1 ? 42 : `${r}:${c}`);
  const grid = (rows: number, cols: number) =>
    Array.from({ length: rows }, (_, r) => Array.from({ length: cols }, (_, c) => cell(r, c)));
  const activeRange: { current: Record<string, unknown> | null } = { current: null };
  const sheet = {
    getSheetName: vi.fn(() => "Stock"),
    getActiveRange: vi.fn(() => activeRange.current),
    getRange: vi.fn((_row: number, _col: number, rows: number, cols: number) => ({
      getValues: () => grid(rows, cols),
    })),
  };
  const workbook = {
    setEditable: vi.fn(),
    setActiveSheet: vi.fn(),
    getSheetBySheetId: vi.fn(() => ({})),
    getActiveSheet: vi.fn(() => sheet),
    save: vi.fn(() => ({}) as IWorkbookData),
  };
  const univerAPI = {
    Event: {
      LifeCycleChanged: "LifeCycleChanged",
      BeforeCommandExecute: "BeforeCommandExecute",
      CommandExecuted: "CommandExecuted",
      SelectionChanged: "SelectionChanged",
      SelectionMoveEnd: "SelectionMoveEnd",
      ActiveSheetChanged: "ActiveSheetChanged",
    },
    addEvent: vi.fn(addEvent),
    createWorkbook: vi.fn(() => workbook),
    toggleDarkMode: vi.fn(),
  };
  const univer = { dispose: vi.fn() };
  return { listeners, state, fire, sheet, activeRange, workbook, univerAPI, univer };
});

vi.mock("@univerjs/presets", () => ({
  LocaleType: { EN_US: "en-US" },
  mergeLocales: (x: unknown) => x,
  createUniver: () => ({ univer: fake.univer, univerAPI: fake.univerAPI }),
}));
vi.mock("@univerjs/preset-sheets-core", () => ({
  UniverSheetsCorePreset: () => ({}),
}));
vi.mock("@univerjs/preset-sheets-core/locales/en-US", () => ({ default: {} }));
vi.mock("@univerjs/preset-sheets-core/lib/index.css", () => ({}));

const DATA = {} as IWorkbookData;

/** A user keystroke: a COMMAND that dispatches one MUTATION inside it. */
function userEdit() {
  fake.fire("BeforeCommandExecute", { id: "sheet.command.set-range-values", type: CommandType.COMMAND });
  fake.fire("BeforeCommandExecute", { id: "sheet.mutation.set-range-values", type: CommandType.MUTATION });
  fake.fire("CommandExecuted", { id: "sheet.mutation.set-range-values", type: CommandType.MUTATION, options: undefined });
  fake.fire("CommandExecuted", { id: "sheet.command.set-range-values", type: CommandType.COMMAND, options: undefined });
}

/** A mutation Univer dispatches on its own, outside any command. */
function ownMutation(options?: Record<string, unknown>) {
  fake.fire("CommandExecuted", { id: "sheet.mutation.set-worksheet-col-width", type: CommandType.MUTATION, options });
}

async function mountEditable(onDirty: () => void) {
  return mountUniverSheet(document.createElement("div"), DATA, false, { editable: true, onDirty });
}

beforeEach(() => {
  fake.listeners.clear();
  fake.state.stage = LifecycleStages.Ready;
  fake.univerAPI.addEvent.mockClear();
  fake.univer.dispose.mockClear();
  fake.workbook.setEditable.mockClear();
  fake.sheet.getRange.mockClear();
  fake.activeRange.current = null;
});

/** A selection starting at `row`/`col` of the given size, in the fake sheet. */
function selectRange(row: number, col: number, height: number, width: number, a1: string) {
  fake.activeRange.current = {
    getRow: () => row,
    getColumn: () => col,
    getHeight: () => height,
    getWidth: () => width,
    getA1Notation: () => a1,
  };
}

describe("mountUniverSheet user-edit tracking", () => {
  it("ignores every mutation until the grid has Rendered", async () => {
    const onDirty = vi.fn();
    await mountEditable(onDirty);
    // Still at Ready: the mount-time layout mutations and a stray user-looking
    // command must not dirty the workbook.
    ownMutation();
    userEdit();
    expect(onDirty).not.toHaveBeenCalled();

    fake.fire("LifeCycleChanged", { stage: LifecycleStages.Rendered });
    ownMutation();
    expect(onDirty).not.toHaveBeenCalled();
  });

  it("counts a mutation dispatched inside a command once the grid has Rendered", async () => {
    const onDirty = vi.fn();
    await mountEditable(onDirty);
    fake.fire("LifeCycleChanged", { stage: LifecycleStages.Rendered });

    userEdit();
    expect(onDirty).toHaveBeenCalledTimes(1);
  });

  it("does not count a mutation Univer dispatches outside any command", async () => {
    const onDirty = vi.fn();
    await mountEditable(onDirty);
    fake.fire("LifeCycleChanged", { stage: LifecycleStages.Rendered });

    ownMutation();
    expect(onDirty).not.toHaveBeenCalled();
  });

  it("does not leave a syncOnly command counted as in flight", async () => {
    const onDirty = vi.fn();
    await mountEditable(onDirty);
    fake.fire("LifeCycleChanged", { stage: LifecycleStages.Rendered });

    // CommandService runs a syncOnly command without its handler and emits no
    // CommandExecuted for it, so it must not leave the depth above zero.
    fake.fire("BeforeCommandExecute", { id: "sync", type: CommandType.COMMAND, options: { syncOnly: true } });
    ownMutation();
    expect(onDirty).not.toHaveBeenCalled();
  });

  it("drops mutations flagged as local, collaboration or changeset loads", async () => {
    const onDirty = vi.fn();
    await mountEditable(onDirty);
    fake.fire("LifeCycleChanged", { stage: LifecycleStages.Rendered });

    fake.fire("BeforeCommandExecute", { id: "outer", type: CommandType.COMMAND });
    for (const options of [{ onlyLocal: true }, { fromCollab: true }, { fromChangeset: true }, { syncOnly: true }]) {
      fake.fire("CommandExecuted", { id: "m", type: CommandType.MUTATION, options });
    }
    fake.fire("CommandExecuted", { id: "outer", type: CommandType.COMMAND, options: undefined });
    expect(onDirty).not.toHaveBeenCalled();
  });

  it("stays inside a command across nesting until the outermost one ends", async () => {
    const onDirty = vi.fn();
    await mountEditable(onDirty);
    fake.fire("LifeCycleChanged", { stage: LifecycleStages.Rendered });

    fake.fire("BeforeCommandExecute", { id: "outer", type: CommandType.COMMAND });
    fake.fire("BeforeCommandExecute", { id: "inner", type: CommandType.COMMAND });
    fake.fire("CommandExecuted", { id: "inner", type: CommandType.COMMAND, options: undefined });
    fake.fire("CommandExecuted", { id: "m", type: CommandType.MUTATION, options: undefined });
    expect(onDirty).toHaveBeenCalledTimes(1);

    fake.fire("CommandExecuted", { id: "outer", type: CommandType.COMMAND, options: undefined });
    ownMutation();
    expect(onDirty).toHaveBeenCalledTimes(1);
  });

  it("counts edits from the start when the stage was already Rendered at mount", async () => {
    fake.state.stage = LifecycleStages.Steady;
    const onDirty = vi.fn();
    await mountEditable(onDirty);
    userEdit();
    expect(onDirty).toHaveBeenCalledTimes(1);
  });

  it("subscribes to nothing for a read-only mount", async () => {
    fake.state.stage = LifecycleStages.Steady;
    const onDirty = vi.fn();
    await mountUniverSheet(document.createElement("div"), DATA, false, { editable: false, onDirty });
    expect(fake.workbook.setEditable).toHaveBeenCalledWith(false);
    expect(fake.univerAPI.addEvent).not.toHaveBeenCalled();
    userEdit();
    expect(onDirty).not.toHaveBeenCalled();
  });

  it("disposes its subscriptions and the Univer instance on dispose", async () => {
    const onDirty = vi.fn();
    const handle = await mountEditable(onDirty);
    fake.fire("LifeCycleChanged", { stage: LifecycleStages.Rendered });

    handle.dispose();
    expect(fake.univer.dispose).toHaveBeenCalledTimes(1);
    userEdit();
    expect(onDirty).not.toHaveBeenCalled();
  });
});

describe("mountUniverSheet selection report", () => {
  it("names the active sheet", async () => {
    const handle = await mountUniverSheet(document.createElement("div"), DATA, false);
    expect(handle.activeSheetName()).toBe("Stock");
  });

  it("reports null when no range is active", async () => {
    const handle = await mountUniverSheet(document.createElement("div"), DATA, false);
    expect(handle.selection()).toBeNull();
  });

  it("reports the sheet, A1 range and cell values as strings", async () => {
    const handle = await mountUniverSheet(document.createElement("div"), DATA, false);
    selectRange(0, 0, 2, 2, "A1:B2");
    expect(handle.selection()).toEqual({
      sheet: "Stock",
      range: "A1:B2",
      values: [
        ["", "42"],
        ["1:0", "1:1"],
      ],
    });
  });

  it("reads at most 20 rows and 10 columns of a large selection", async () => {
    const handle = await mountUniverSheet(document.createElement("div"), DATA, false);
    selectRange(3, 2, 1000, 500, "C4:BX1003");
    const sel = handle.selection();
    expect(fake.sheet.getRange).toHaveBeenCalledWith(3, 2, 20, 10);
    expect(sel?.range).toBe("C4:BX1003");
    expect(sel?.values).toHaveLength(20);
    expect(sel?.values.every((row) => row.length === 10)).toBe(true);
  });

  it("calls back on selection and active-sheet changes until unsubscribed", async () => {
    const handle = await mountUniverSheet(document.createElement("div"), DATA, false);
    const cb = vi.fn();
    const unsubscribe = handle.onSelectionChange(cb);

    fake.fire("SelectionChanged", {});
    fake.fire("SelectionMoveEnd", {});
    fake.fire("ActiveSheetChanged", {});
    expect(cb).toHaveBeenCalledTimes(3);

    unsubscribe();
    fake.fire("SelectionChanged", {});
    expect(cb).toHaveBeenCalledTimes(3);
  });

  it("stops selection callbacks on dispose", async () => {
    const handle = await mountUniverSheet(document.createElement("div"), DATA, false);
    const cb = vi.fn();
    handle.onSelectionChange(cb);

    handle.dispose();
    fake.fire("SelectionChanged", {});
    expect(cb).not.toHaveBeenCalled();
  });
});
