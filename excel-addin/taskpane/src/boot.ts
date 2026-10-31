import { BridgeClient, type BridgeStatus } from "./bridgeClient";
import { BridgeJournalStore } from "./bridgeStore";
import { FakePort } from "./fakePort";
import { SessionIndex, workbookId } from "./identity";
import { OcodeClient } from "./ocodeClient";
import { OfficePort } from "./officePort";
import type { ExcelPort } from "./port";
import { Runner, type Contract } from "./runner";
import { Journal } from "./tools/journal";

export interface Deps {
  port: ExcelPort;
  journal: Journal;
  ocode: OcodeClient;
  index: SessionIndex;
  workbook: string;
  fileName: string;
  workspace: string;
  fake: boolean;
  /** The Runner that executes bridge tool calls. The UI uses it too (Undo), so writes never interleave. */
  runner: Runner;
  /** One-line notice to show once, e.g. when this workbook was copied and got a fresh id. */
  notice?: string;
  onBridgeStatus(cb: (s: BridgeStatus) => void): void;
}

/** The bridge key arrives once in the task pane URL; keep it for the tab and drop it from the address bar. */
function paneKey(): string {
  const url = new URL(location.href);
  const k = url.searchParams.get("key");
  if (k) {
    sessionStorage.setItem("ocode.bridge_key", k);
    url.searchParams.delete("key");
    history.replaceState(null, "", url.toString());
  }
  return sessionStorage.getItem("ocode.bridge_key") ?? "";
}

export async function boot(): Promise<Deps> {
  const fake = new URL(location.href).searchParams.has("fake");
  if (!fake) await Office.onReady();
  const key = paneKey();
  const base = "";
  const port: ExcelPort = fake
    ? new FakePort().addSheet("Summary", [["Category", "Spent", "Budget"], ["Travel", 841.5, 800], ["Meals", 451.8, 450], ["Total", 1293.3, 1250]])
    : new OfficePort();
  const fileUrl = fake ? "" : (Office.context.document.url ?? "");
  const { id: wb, forked } = await workbookId(port, fileUrl);
  const notice = forked ? "This workbook was copied or moved, so ocode started a fresh history for it." : undefined;
  const fileName = fake ? "Sample.xlsx" : decodeURIComponent(fileUrl.split(/[\\/]/).pop() || "Workbook.xlsx");
  // One id per page load: a second pane on the same workbook is told apart from a reconnect of this one.
  const instance = crypto.randomUUID();

  const contract = (await (await fetch(`${base}/bridge/tools`, { headers: { "X-Bridge-Key": key } })).json()) as Contract;
  const journal = new Journal(new BridgeJournalStore(base, key, wb));
  const runner = new Runner(port, journal, contract);
  // The bridge connects during boot, before the UI exists: remember the latest status and replay it on subscribe.
  let cb: (s: BridgeStatus) => void = () => undefined;
  let last: BridgeStatus = "connecting";
  const bridge = new BridgeClient({ base, key, workbook: wb, instance, runner, onStatus: (s) => { last = s; cb(s); } });
  bridge.start();

  const ws = (await (await fetch(`${base}/bridge/workspace/${encodeURIComponent(wb)}`, { method: "POST", headers: { "X-Bridge-Key": key } })).json()) as { path: string };
  const ocode = new OcodeClient({ base, key });
  // Register the workspace as a project so ocode treats it as one (and shows these sessions under it).
  // Not fatal: chat can still start, but directory-bound features may refuse the path. See RESUME.md (U6).
  await ocode.addProject(ws.path).catch((e) => console.warn("could not register the workspace as an ocode project:", e));
  return {
    port, journal, runner, notice, workbook: wb, fileName, fake, workspace: ws.path,
    ocode, index: new SessionIndex(base, key, wb),
    onBridgeStatus: (f) => { cb = f; f(last); },
  };
}
