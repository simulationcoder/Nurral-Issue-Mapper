import React from "react";
import ReactDOM from "react-dom/client";
import "./index.css";
import { IssueMapTab } from "./IssueMapTab";
import { applySystemTheme } from "./theme";

applySystemTheme();

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <div className="h-full p-3">
      <IssueMapTab />
    </div>
  </React.StrictMode>,
);
