/**
 * The Pulse assistant's session id prefix, fixed by the server contract
 * (`GET /api/pulse/assistant`): every assistant session id starts with it, and
 * such a session belongs to no project and no host.
 */
const PULSE_ASSISTANT_PREFIX = "pulse_";

export function isPulseAssistantSession(sessionId: string | null | undefined): boolean {
  return !!sessionId && sessionId.startsWith(PULSE_ASSISTANT_PREFIX);
}

/** The segment after the last `/`: provider and router prefixes are noise at
 *  drawer width (`openrouter/vendor/model:free` shows as `model:free`); the full
 *  id stays in the button's title. */
export function shortModelName(model: string): string {
  if (model === "") return "default model";
  return model.slice(model.lastIndexOf("/") + 1);
}

/** Window event asking the app to open Settings on the "Pulse assistant"
 *  section. App switches the view; the force-mounted SettingsPanel selects the
 *  section. Same shape as `ocode:open-settings-profiles`. */
export const OPEN_PULSE_ASSISTANT_SETTINGS_EVENT = "ocode:open-settings-pulse-assistant";
