import { readFile, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";

const manifestUrl = new URL("../../protocol/methods.json", import.meta.url);
const generatedUrl = new URL("../src/boundary/protocol.generated.ts", import.meta.url);
const manifest = JSON.parse(await readFile(manifestUrl, "utf8"));
if (manifest.methodFields.join(",") !== "name,request,response,transfer") {
  throw new Error("unexpected protocol method tuple schema");
}
for (const method of manifest.methods) {
  if (method.length !== manifest.methodFields.length || method.some((field) => !field)) {
    throw new Error(`incomplete protocol method: ${JSON.stringify(method)}`);
  }
}
const names = manifest.methods.map(([name]) => name);
const duplicates = names.filter((name, index) => names.indexOf(name) !== index);
if (duplicates.length > 0) throw new Error(`duplicate protocol methods: ${duplicates.join(", ")}`);

const source = `// Generated from web/protocol/methods.json by scripts/check-protocol.mjs.
// Edit the manifest, then run the checker with --write.
export const coreMethods = [
${names.map((name) => `  ${JSON.stringify(name)},`).join("\n")}
] as const;
`;

if (process.argv.includes("--write")) {
  await writeFile(generatedUrl, source);
} else if ((await readFile(generatedUrl, "utf8")) !== source) {
  throw new Error(`protocol.generated.ts is stale; run ${fileURLToPath(import.meta.url)} --write`);
}
