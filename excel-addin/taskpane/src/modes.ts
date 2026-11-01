/** The three choices in the pane's permission picker. */
export type ModeChoice = "ask" | "auto" | "yolo";

/**
 * What a choice means on the server: the session's permission mode, and the server-wide auto-permission switch.
 * permissionFor and choiceFor only ever see "normal" or "yolo": the pane sets those two, and savedModeView shows locked and sandbox read-only.
 */
export interface PermissionSetting { mode: "normal" | "yolo"; auto: boolean }

/** Ask = normal with auto-permission off; Auto = normal with it on; YOLO = yolo (auto-permission is not consulted, so `auto` is ignored). */
export function permissionFor(choice: ModeChoice): PermissionSetting {
  return { mode: choice === "yolo" ? "yolo" : "normal", auto: choice === "auto" };
}

/** The picker segment for a mode and auto-permission pair. yolo wins; otherwise auto decides. */
export function choiceFor(s: PermissionSetting): ModeChoice {
  if (s.mode === "yolo") return "yolo";
  return s.auto ? "auto" : "ask";
}

/**
 * What the pane shows for a session's saved permission mode (ocode's GET /api/sessions/{id}/status): a picker choice,
 * or a read-only chip for a mode the picker has no segment for. A normal chat reads Ask or Auto from the server-wide switch.
 */
export type SavedModeView = { choice: ModeChoice } | { chip: "locked" | "sandbox" };

/** Maps a saved mode from ocode. A value outside the four modes ocode reports throws: it is not shown as a guess. */
export function savedModeView(mode: string, autoOn: boolean): SavedModeView {
  switch (mode) {
    case "yolo": return { choice: "yolo" };
    case "normal": return { choice: choiceFor({ mode: "normal", auto: autoOn }) };
    case "locked": return { chip: "locked" };
    case "sandbox": return { chip: "sandbox" };
    default: throw new Error(`ocode reported an unknown permission mode: ${mode}`);
  }
}
