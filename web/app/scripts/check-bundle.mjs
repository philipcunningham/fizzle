// Payload budget check (spec Q3): the compressed payload must stay at
// or under 3 MB. Gzips everything in dist/ and fails the build when
// the sum crosses the budget.
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { gzipSync } from "node:zlib";

const BUDGET_BYTES = 3 * 1024 * 1024;
const dist = new URL("../dist", import.meta.url).pathname;

function* files(dir) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) yield* files(path);
    else yield path;
  }
}

let total = 0;
const rows = [];
for (const path of files(dist)) {
  const compressed = gzipSync(readFileSync(path)).length;
  total += compressed;
  rows.push(`${(compressed / 1024).toFixed(1).padStart(9)} KB  ${path.slice(dist.length + 1)}`);
}

console.log(rows.join("\n"));
console.log(
  `${(total / 1024).toFixed(1).padStart(9)} KB  total (budget ${BUDGET_BYTES / 1024} KB)`,
);

if (total > BUDGET_BYTES) {
  console.error(`payload budget exceeded: ${total} > ${BUDGET_BYTES} bytes compressed`);
  process.exit(1);
}
