import { createRoot } from "react-dom/client";
import { boot } from "./boot";
import { errText, log } from "./log";
import { App } from "./ui/App";
import "./ui/app.css";

window.addEventListener("error", (e) => log("error", "window", `${e.message} (${e.filename}:${e.lineno})`));
window.addEventListener("unhandledrejection", (e) => log("error", "window", `unhandled rejection: ${errText(e.reason)}`));
const root = createRoot(document.getElementById("root")!);
boot().then(
  (deps) => root.render(<App deps={deps} />),
  (e: unknown) => { log("error", "boot", `start failed: ${errText(e)}`); root.render(<pre className="fatal">ocode could not start: {String((e as Error)?.message ?? e)}</pre>); },
);
