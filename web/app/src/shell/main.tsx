import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { createWasmCore } from "../core/wasm";
import { App } from "./App";
import "./tokens.css";
import "./mockup.css";

const root = document.getElementById("root");
if (!root) throw new Error("missing #root");
createRoot(root).render(
  <StrictMode>
    <App core={createWasmCore()} />
  </StrictMode>,
);
