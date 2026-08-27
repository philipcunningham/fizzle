import { readdirSync, readFileSync } from "node:fs";
import { dirname, extname, join, normalize, relative, resolve } from "node:path";
import ts from "typescript";

const root = new URL("..", import.meta.url).pathname;
const sourceRoot = join(root, "src");
const stubPath = join(sourceRoot, "core", "stub.ts");
const allowedCoreImplementations = new Set([
  stubPath,
  join(sourceRoot, "core", "wasm.ts"),
  join(root, "tests", "worker.test.ts"),
]);
const protocol = JSON.parse(readFileSync(join(root, "..", "protocol", "methods.json"), "utf8"));
const coreMethods = new Set(protocol.methods.map((method) => method[0]));

function resolvedImport(from, specifier) {
  if (!specifier.startsWith(".")) return null;
  return normalize(resolve(dirname(from), specifier));
}

function isForbiddenScreenTarget(target) {
  if (!target) return false;
  return [join(sourceRoot, "core"), join(sourceRoot, "shell")].some(
    (boundary) => target === boundary || target.startsWith(`${boundary}/`),
  );
}

function objectMethodCount(node) {
  if (!ts.isObjectLiteralExpression(node)) return 0;
  return node.properties.filter((property) => {
    const name = property.name && ts.isIdentifier(property.name) ? property.name.text : "";
    return coreMethods.has(name);
  }).length;
}

function inspect(path, source) {
  const failures = [];
  const ast = ts.createSourceFile(path, source, ts.ScriptTarget.Latest, true);
  const screen = path.includes("/src/screens/");
  const visit = (node) => {
    if (screen && ts.isImportDeclaration(node) && ts.isStringLiteral(node.moduleSpecifier)) {
      if (isForbiddenScreenTarget(resolvedImport(path, node.moduleSpecifier.text))) {
        failures.push(`${relative(root, path)} crosses the presentational screen boundary`);
      }
    }
    if (
      screen &&
      ts.isCallExpression(node) &&
      node.expression.kind === ts.SyntaxKind.ImportKeyword &&
      node.arguments[0] &&
      ts.isStringLiteral(node.arguments[0]) &&
      isForbiddenScreenTarget(resolvedImport(path, node.arguments[0].text))
    ) {
      failures.push(
        `${relative(root, path)} dynamically crosses the presentational screen boundary`,
      );
    }
    if (
      !allowedCoreImplementations.has(path) &&
      ts.isFunctionLike(node) &&
      node.type?.getText(ast) === "Core"
    ) {
      failures.push(`${relative(root, path)} defines a second Core implementation`);
    }
    if (
      !allowedCoreImplementations.has(path) &&
      path.startsWith(`${sourceRoot}/`) &&
      ts.isVariableDeclaration(node) &&
      node.type?.getText(ast) === "Core"
    ) {
      failures.push(`${relative(root, path)} defines a second Core implementation`);
    }
    if (!allowedCoreImplementations.has(path) && objectMethodCount(node) >= 10) {
      failures.push(`${relative(root, path)} contains a behavioral Core object`);
    }
    ts.forEachChild(node, visit);
  };
  visit(ast);
  return failures;
}

const failures = [];
const walk = (directory) => {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    if (entry.name === "node_modules" || entry.name === "dist") continue;
    const path = join(directory, entry.name);
    if (entry.isDirectory()) {
      walk(path);
    } else if ([".ts", ".tsx"].includes(extname(path))) {
      failures.push(...inspect(path, readFileSync(path, "utf8")));
    }
  }
};

walk(root);

const adversarial = [
  [
    join(root, "tests", "support", "testCore.ts"),
    "function createTestCore(): Core { return {} as Core; }",
  ],
  [join(sourceRoot, "screens", "Bad.tsx"), 'import "../shell";'],
  [
    join(sourceRoot, "screens", "Bad.tsx"),
    'async function bad() { await import("../shell/EditorShell"); }',
  ],
  [join(sourceRoot, "core", "second.ts"), "const second: Core = { ...createCoreStub() };"],
];
for (const [path, source] of adversarial) {
  if (inspect(path, source).length === 0) {
    failures.push(`architecture guard accepted adversarial fixture: ${source}`);
  }
}

if (failures.length > 0) {
  console.error([...new Set(failures)].join("\n"));
  process.exit(1);
}
console.log("frontend architecture boundaries are intact");
