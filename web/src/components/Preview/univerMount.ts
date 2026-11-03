import type { IWorkbookData } from "@univerjs/core";

/**
 * Runtime half of the spreadsheet viewer: mounts a read-only Univer sheet
 * (open-source @univerjs packages only, Apache-2.0). Loaded with a dynamic
 * import from ExcelViewer so Univer stays out of the main bundle.
 */

export interface UniverSheetHandle {
  /** Shows the sheet with this id. Throws when the workbook has no such sheet. */
  activateSheet(sheetId: string): void;
  setDarkMode(darkMode: boolean): void;
  /** Tears down every Univer instance this handle created. */
  dispose(): void;
}

/**
 * Mounts `data` into `container`. The workbook is locked against editing and
 * only the grid and sheet bar are shown (no ribbon, formula bar, header or
 * context menu).
 */
export async function mountUniverSheet(
  container: HTMLElement,
  data: IWorkbookData,
  darkMode: boolean,
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

  const workbook = univerAPI.createWorkbook(data);
  workbook.setEditable(false);

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
    dispose() {
      univer.dispose();
    },
  };
}
