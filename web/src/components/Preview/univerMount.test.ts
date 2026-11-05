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
  const workbook = {
    setEditable: vi.fn(),
    setActiveSheet: vi.fn(),
    getSheetBySheetId: vi.fn(() => ({})),
    save: vi.fn(() => ({}) as IWorkbookData),
  };
  const univerAPI = {
    Event: {
      LifeCycleChanged: "LifeCycleChanged",
      BeforeCommandExecute: "BeforeCommandExecute",
      CommandExecuted: "CommandExecuted",
    },
    addEvent: vi.fn(addEvent),
    createWorkbook: vi.fn(() => workbook),
    toggleDarkMode: vi.fn(),
  };
  const univer = { dispose: vi.fn() };
  return { listeners, state, fire, workbook, univerAPI, univer };
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
});

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
