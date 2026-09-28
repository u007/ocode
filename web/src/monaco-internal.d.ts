// Monaco ships type declarations only for its public API (`monaco.d.ts`).
// ocode reaches into one internal module to neutralize Monaco's WebKit
// clipboard workaround — see `lib/monacoClipboardPatch.ts` for why. Declaring
// just the surface we touch keeps that reach explicit instead of falling back
// to `any` from an untyped deep import.
declare module "monaco-editor/esm/vs/platform/clipboard/browser/clipboardService.js" {
  export const BrowserClipboardService: {
    prototype: {
      installWebKitWriteTextWorkaround?: () => void;
    };
  };
}
