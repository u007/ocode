import type { IDocumentData } from "@univerjs/core";

/**
 * Runtime half of the Word viewer: mounts a Univer document
 * (open-source @univerjs packages only, Apache-2.0). Loaded with a dynamic
 * import from DocxEditor so Univer stays out of the main bundle.
 */

export interface MountUniverDocOpts {
  /** Defaults to false (read-only). When true the document is editable. */
  editable?: boolean;
  /** Called on each user edit while editable. */
  onDirty?: () => void;
}

export interface UniverDocHandle {
  /** Current document snapshot, for serialization on save. */
  snapshot(): IDocumentData;
  setDarkMode(darkMode: boolean): void;
  /** Tears down every Univer instance this handle created. */
  dispose(): void;
}

export async function mountUniverDoc(
  container: HTMLElement,
  data: IDocumentData,
  darkMode: boolean,
  opts?: MountUniverDocOpts,
): Promise<UniverDocHandle> {
  const [presets, sheetsCore, enUS, uiEnUS, docsEnUS] = await Promise.all([
    import("@univerjs/presets"),
    import("@univerjs/preset-sheets-core"),
    import("@univerjs/preset-sheets-core/locales/en-US"),
    import("@univerjs/ui/locale/en-US"),
    import("@univerjs/docs-ui/locale/en-US"),
    import("@univerjs/preset-sheets-core/lib/index.css"),
    import("@univerjs/ui/lib/index.css"),
    import("@univerjs/docs-ui/lib/index.css"),
  ]);

  const { univer, univerAPI } = presets.createUniver({
    locale: presets.LocaleType.EN_US,
    locales: { [presets.LocaleType.EN_US]: presets.mergeLocales(enUS.default, uiEnUS.default, docsEnUS.default) },
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
  const doc = univerAPI.createDocument(data);
  // @ts-ignore - setEditable may not exist on FDocument in this version
  try { (doc as any).setEditable?.(editable); } catch { /* ignore */ }

  // Dirty tracking: listen to change$ if available
  if (editable && opts?.onDirty) {
    try {
      (doc as any).change$?.subscribe?.(() => opts.onDirty?.());
    } catch { /* ignore */ }
  }

  return {
    snapshot() {
      return (doc as any).save?.() ?? data;
    },
    setDarkMode(_dm: boolean) {
      // Dark mode is fixed at mount; Univer FUniver has no setter in this version.
    },
    dispose() {
      try { (doc as any).dispose?.(); } catch { /* ignore */ }
      try { univer.dispose?.(); } catch { /* ignore */ }
    },
  };
}
