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

/** Largest selection the viewer reports to the chat, in cells. */
const MAX_SELECTION_ROWS = 20;
const MAX_SELECTION_COLS = 10;
/**
 * Id of the operation every selection change runs (mouse, keyboard, programmatic
 * activate). Defined locally because `@univerjs/sheets` is only a transitive dependency.
 */
const SET_SELECTIONS_OPERATION = "sheet.operation.set-selections";

export interface UniverSheetHandle {
  /** Shows the sheet with this id. Throws when the workbook has no such sheet. */
  activateSheet(sheetId: string): void;
  /** Activates `a1` on the named sheet, or on the active sheet when `sheet` is null. Throws on an unknown sheet or a bad range. */
  activateRange(sheet: string | null, a1: string): void;
  /** Names of every sheet, in workbook order. */
  sheetNames(): string[];
  /** Name of the active sheet. */
  activeSheetName(): string;
  /** Active selection: sheet name, A1 range and the cell values (strings, max 20 rows x 10 cols); null when nothing is selected. */
  selection(): { sheet: string; range: string; values: string[][] } | null;
  /**
   * Subscribe to selection / active-sheet changes; returns an unsubscribe.
   * Selection is observed through the set-selections command, not the facade's
   * Selection* events: in this mount the facade never registers those, because
   * its render-unit lookup of the selections service returns null.
   */
  onSelectionChange(cb: () => void): () => void;
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
  // Selection subscriptions still open. dispose() ends any the caller kept.
  const selectionSubs = new Set<IDisposable>();

  return {
    activateSheet(sheetId) {
      if (workbook.getSheetBySheetId(sheetId) === null) {
        throw new Error(`Univer has no sheet "${sheetId}"`);
      }
      workbook.setActiveSheet(sheetId);
    },
    activateRange(sheetName, a1) {
      const target = sheetName === null ? workbook.getActiveSheet() : workbook.getSheetByName(sheetName);
      if (target === null) throw new Error(`Univer has no sheet "${sheetName}"`);
      // Resolve and check the range before switching sheets, so a bad A1 leaves the view alone.
      const range = target.getRange(a1);
      assertRangeInSheet(target, range, a1);
      if (sheetName !== null) workbook.setActiveSheet(target.getSheetId());
      range.activate();
    },
    sheetNames() {
      return workbook.getSheets().map((s) => s.getSheetName());
    },
    activeSheetName() {
      return workbook.getActiveSheet().getSheetName();
    },
    selection() {
      const sheet = workbook.getActiveSheet();
      const range = sheet.getActiveRange();
      if (range === null) return null;
      // Read only the capped corner: a full-column selection would otherwise
      // materialise every cell of the column before the slice.
      const rows = Math.min(range.getHeight(), MAX_SELECTION_ROWS);
      const cols = Math.min(range.getWidth(), MAX_SELECTION_COLS);
      const visible = sheet.getRange(range.getRow(), range.getColumn(), rows, cols);
      return {
        sheet: sheet.getSheetName(),
        range: range.getA1Notation(),
        values: visible.getValues().map((row) => row.map(cellText)),
      };
    },
    onSelectionChange(cb) {
      const subs = [
        univerAPI.addEvent(univerAPI.Event.CommandExecuted, ({ id }) => {
          if (id === SET_SELECTIONS_OPERATION) cb();
        }),
        univerAPI.addEvent(univerAPI.Event.ActiveSheetChanged, cb),
      ];
      for (const sub of subs) selectionSubs.add(sub);
      return () => {
        for (const sub of subs) {
          sub.dispose();
          selectionSubs.delete(sub);
        }
      };
    },
    setDarkMode(next) {
      univerAPI.toggleDarkMode(next);
    },
    snapshot() {
      return workbook.save();
    },
    dispose() {
      for (const sub of [...userEdits, ...selectionSubs]) sub.dispose();
      univer.dispose();
    },
  };
}

/**
 * Throws unless `range` is a non-empty rectangle inside `sheet`. Univer does not
 * throw on malformed A1: it returns NaN coordinates, so the check has to run on
 * the range itself.
 */
function assertRangeInSheet(
  sheet: { getMaxRows(): number; getMaxColumns(): number },
  range: { getRow(): number; getColumn(): number; getHeight(): number; getWidth(): number },
  a1: string,
): void {
  const row = range.getRow();
  const col = range.getColumn();
  const height = range.getHeight();
  const width = range.getWidth();
  const integers = [row, col, height, width].every((n) => Number.isInteger(n));
  const fits =
    integers &&
    row >= 0 &&
    col >= 0 &&
    height >= 1 &&
    width >= 1 &&
    row + height <= sheet.getMaxRows() &&
    col + width <= sheet.getMaxColumns();
  if (!fits) throw new Error(`invalid range: ${a1}`);
}

/** A cell value as the text the chat shows: blank cells are "", objects are JSON. */
function cellText(value: unknown): string {
  if (value === null || value === undefined) return "";
  return typeof value === "object" ? JSON.stringify(value) : String(value);
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
