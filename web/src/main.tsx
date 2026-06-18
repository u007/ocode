import React from "react";
import ReactDOM from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import App from "./App";
import { _basePath } from "./api/client";
import "./index.css";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <BrowserRouter basename={_basePath || undefined}>
      <App />
    </BrowserRouter>
  </React.StrictMode>,
);
