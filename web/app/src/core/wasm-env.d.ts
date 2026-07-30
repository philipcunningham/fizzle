// Types for the generated Go runtime shim (make wasm copies
// wasm_exec.js into src/core/generated/). The shim defines Go on the
// global scope; the WASM module adds fizzleCore once it has started.
declare module "*/wasm_exec.js";

declare class Go {
  importObject: WebAssembly.Imports;
  run(instance: WebAssembly.Instance): Promise<void>;
}
