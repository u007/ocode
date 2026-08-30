import React from "react";
import ReactDOM from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import App from "./App";
import { _basePath, initBackendBase } from "./api/client";
import "./debug";
import "./index.css";

// Bootstrap backend origin before first render so initial API/SSE calls
// respect the configured backend_url (same-origin vs hub). Gate the initial
// render on this promise so the first eventBus connection and initial
// fetches use the correct origin; failure falls back to same-origin.
function start() {
  ReactDOM.createRoot(document.getElementById("root")!).render(
    <React.StrictMode>
      <BrowserRouter basename={_basePath || undefined}>
        <App />
      </BrowserRouter>
    </React.StrictMode>,
  );
}
initBackendBase().then(start).catch(start);
