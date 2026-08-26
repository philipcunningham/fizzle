import { readdirSync, readFileSync } from "node:fs";
import { join, relative } from "node:path";

const root = new URL("..", import.meta.url).pathname;
const forbiddenNames = new Set(["fake.ts", "scenarioCore.ts"]);
const failures = [];

const walk = (directory) => {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    if (entry.name === "node_modules" || entry.name === "dist") continue;
    const path = join(directory, entry.name);
    if (entry.isDirectory()) {
      walk(path);
      continue;
    }
    if (forbiddenNames.has(entry.name)) {
      failures.push(`${relative(root, path)} restores a behavioral editor fake`);
    }
    if (!path.includes("/src/screens/") || !/\.[jt]sx?$/.test(path)) continue;
    const source = readFileSync(path, "utf8");
    if (/from\s+["'][^"']*\/(?:core|shell)\//.test(source)) {
      failures.push(`${relative(root, path)} crosses the presentational screen boundary`);
    }
  }
};

walk(root);
if (failures.length > 0) {
  console.error(failures.join("\n"));
  process.exit(1);
}
console.log("frontend architecture boundaries are intact");
