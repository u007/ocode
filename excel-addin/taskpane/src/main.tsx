import { createRoot } from "react-dom/client";
import { boot } from "./boot";
import { App } from "./ui/App";
import "./ui/app.css";

const root = createRoot(document.getElementById("root")!);
boot().then(
  (deps) => root.render(<App deps={deps} />),
  (e: unknown) => root.render(<pre className="fatal">ocode could not start: {String((e as Error)?.message ?? e)}</pre>),
);
