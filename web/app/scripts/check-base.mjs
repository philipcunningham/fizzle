// A GitHub Pages project site serves from a sub-path, so the bundle has
// to address its own assets relative to that base. The core is fetched
// by the worker at runtime rather than imported, so vite cannot rewrite
// it: an absolute "/fizzle.wasm" loads the shell and then 404s the core,
// which looks like the app booting and then doing nothing.
//
// Builds into a temporary directory under a sub-path base and asserts
// every reference carries it.
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, readdirSync, rmSync, statSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const BASE = "/fizzle-base-check/";
const out = mkdtempSync(join(tmpdir(), "fizzle-base-"));

try {
  execFileSync("npx", ["vite", "build", "--outDir", out, "--emptyOutDir"], {
    env: { ...process.env, VITE_BASE: BASE },
    stdio: "pipe",
  });

  const files = [];
  const walk = (dir) => {
    for (const name of readdirSync(dir)) {
      const path = join(dir, name);
      if (statSync(path).isDirectory()) walk(path);
      else files.push(path);
    }
  };
  walk(out);

  const problems = [];

  const html = readFileSync(join(out, "index.html"), "utf8");
  for (const match of html.matchAll(/(?:src|href)="([^"]+)"/g)) {
    const url = match[1];
    if (url.startsWith("/") && !url.startsWith(BASE)) {
      problems.push(`index.html references ${url}, which ignores the base`);
    }
  }

  // The worker fetches the core by URL, so the base has to be in the
  // emitted string rather than resolved by the bundler.
  const workers = files.filter((f) => /worker.*\.js$/.test(f));
  if (workers.length === 0) problems.push("no worker bundle was emitted");
  for (const worker of workers) {
    const js = readFileSync(worker, "utf8");
    if (js.includes('"/fizzle.wasm"') || js.includes("'/fizzle.wasm'")) {
      problems.push(`${worker} fetches an absolute /fizzle.wasm, so a sub-path host cannot boot`);
    }
    if (!js.includes(BASE)) {
      problems.push(`${worker} carries no reference to the base ${BASE}`);
    }
  }

  if (problems.length > 0) {
    console.error("the bundle is not sub-path safe:");
    for (const p of problems) console.error(`  ${p}`);
    process.exit(1);
  }
  console.log(`sub-path safe: every reference carries the base ${BASE}`);
} finally {
  rmSync(out, { recursive: true, force: true });
}
