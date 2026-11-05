import { CommandType, LifecycleStages, type IDisposable, type IWorkbookData } from "@univerjs/core";
import type { FUniver } from "@univerjs/core/facade";

/**
 * Runtime half of the spreadsheet viewer: mounts a Univer sheet
 * (open-source @univerjs packages only, Apache-2.0). Loaded with a dynamic
 * import from ExcelViewer so Univer stays out of the main bundle.
 */

export interface MountUniverOpts {
  /** Defaults to false (read-only). When true the grid is editable. */
  editable?: boolean;
  /**
   * Called on each user edit (cell edits, structural changes) while editable.
   * Univer's own mount-time and layout mutations do not count; see
   * `watchUserEdits`.
   */
  onDirty?: () => void;
}

export interface UniverSheetHandle {
  /** Shows the sheet with this id. Throws when the workbook has no such sheet. */
  activateSheet(sheetId: string): void;
  setDarkMode(darkMode: boolean): void;
  /** Current workbook snapshot, for serialization on save. */
  snapshot(): IWorkbookData;
  /** Tears down every Univer instance this handle created. */
  dispose(): void;
}

/**
 * Mounts `data` into `container`. By default the workbook is locked against
 * editing and only the grid and sheet bar are shown (no ribbon, formula bar,
 * header or context menu). With `opts.editable` the grid accepts edits and
 * `opts.onDirty` fires on each local mutation.
 */
export async function mountUniverSheet(
  container: HTMLElement,
  data: IWorkbookData,
  darkMode: boolean,
  opts?: MountUniverOpts,
): Promise<UniverSheetHandle> {
  const [presets, sheetsCore, enUS] = await Promise.all([
    import("@univerjs/presets"),
    import("@univerjs/preset-sheets-core"),
    import("@univerjs/preset-sheets-core/locales/en-US"),
    import("@univerjs/preset-sheets-core/lib/index.css"),
  ]);

  const { univer, univerAPI } = presets.createUniver({
    locale: presets.LocaleType.EN_US,
    locales: { [presets.LocaleType.EN_US]: presets.mergeLocales(enUS.default) },
    darkMode,
    presets: [
      sheetsCore.UniverSheetsCorePreset({
        container,
        header: false,
        toolbar: false,
        formulaBar: false,
        footer: false,
        contextMenu: false,
        disableAutoFocus: true,
      }),
    ],
  });

  const editable = opts?.editable === true;
  // Subscribed before createWorkbook so every command that follows is paired.
  const userEdits = editable && opts?.onDirty ? watchUserEdits(univerAPI, opts.onDirty) : [];
  const workbook = univerAPI.createWorkbook(data);
  workbook.setEditable(editable);

  return {
    activateSheet(sheetId) {
      if (workbook.getSheetBySheetId(sheetId) === null) {
        throw new Error(`Univer has no sheet "${sheetId}"`);
      }
      workbook.setActiveSheet(sheetId);
    },
    setDarkMode(next) {
      univerAPI.toggleDarkMode(next);
    },
    snapshot() {
      return workbook.save();
    },
    dispose() {
      for (const sub of userEdits) sub.dispose();
      univer.dispose();
    },
  };
}

/**
 * Reports user edits and nothing else. Univer also runs mutations of its own
 * (mount layout, selection, auto-height), and a dirty flag set by those would
 * show Save on a file nobody touched. A mutation counts only when:
 *  - the grid has Rendered. The UI plugin sets Rendered about 300 ms after
 *    Ready, so everything Univer does while mounting comes before it. A late
 *    subscriber gets the current stage replayed, so this holds either way.
 *  - it runs inside a COMMAND. Keyboard and pointer input reach the model
 *    through commands; controllers dispatch mutations directly.
 *  - it is not flagged onlyLocal, fromCollab, fromChangeset or syncOnly.
 */
function watchUserEdits(univerAPI: FUniver, onDirty: () => void): IDisposable[] {
  let rendered = false;
  let commandDepth = 0;
  return [
    univerAPI.addEvent(univerAPI.Event.LifeCycleChanged, ({ stage }) => {
      if (stage >= LifecycleStages.Rendered) rendered = true;
    }),
    // A syncOnly command never emits CommandExecuted (CommandService.executeCommand
    // returns before it), so counting it would leave the depth above zero.
    // Known gap, not fixable from these events: a COMMAND whose handler throws, or
    // that a before-listener cancels, also never emits CommandExecuted.
    univerAPI.addEvent(univerAPI.Event.BeforeCommandExecute, ({ type, options }) => {
      if (type === CommandType.COMMAND && !options?.syncOnly) commandDepth += 1;
    }),
    univerAPI.addEvent(univerAPI.Event.CommandExecuted, ({ type, options }) => {
      if (type === CommandType.COMMAND) {
        commandDepth -= 1;
        return;
      }
      if (type !== CommandType.MUTATION || !rendered || commandDepth === 0) return;
      if (options?.onlyLocal || options?.fromCollab || options?.fromChangeset || options?.syncOnly) return;
      onDirty();
    }),
  ];
}
