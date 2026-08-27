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

function behavioralMethodCount(node) {
  let members;
  if (ts.isObjectLiteralExpression(node)) members = node.properties;
  if (ts.isClassDeclaration(node) || ts.isClassExpression(node)) members = node.members;
  if (!members) return 0;
  return members.filter((member) => {
    const name = member.name && ts.isIdentifier(member.name) ? member.name.text : "";
    return coreMethods.has(name);
  }).length;
}

function inspect(path, source) {
  const failures = [];
  const ast = ts.createSourceFile(path, source, ts.ScriptTarget.Latest, true);
  const screen = path.includes("/src/screens/");
  const coreTypeNames = new Set(["Core"]);
  for (const statement of ast.statements) {
    if (!ts.isImportDeclaration(statement)) continue;
    const bindings = statement.importClause?.namedBindings;
    if (!bindings || !ts.isNamedImports(bindings)) continue;
    for (const element of bindings.elements) {
      if ((element.propertyName ?? element.name).text === "Core") {
        coreTypeNames.add(element.name.text);
      }
    }
  }
  const isCoreType = (type) =>
    type && ts.isTypeReferenceNode(type) && ts.isIdentifier(type.typeName)
      ? coreTypeNames.has(type.typeName.text)
      : false;
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
    if (!allowedCoreImplementations.has(path) && ts.isFunctionLike(node) && isCoreType(node.type)) {
      failures.push(`${relative(root, path)} defines a second Core implementation`);
    }
    if (
      !allowedCoreImplementations.has(path) &&
      path.startsWith(`${sourceRoot}/`) &&
      ts.isVariableDeclaration(node) &&
      isCoreType(node.type)
    ) {
      failures.push(`${relative(root, path)} defines a second Core implementation`);
    }
    if (
      !allowedCoreImplementations.has(path) &&
      path.startsWith(`${sourceRoot}/`) &&
      (ts.isAsExpression(node) || ts.isTypeAssertionExpression(node)) &&
      isCoreType(node.type)
    ) {
      failures.push(`${relative(root, path)} defines a second Core implementation`);
    }
    if (
      !allowedCoreImplementations.has(path) &&
      path.startsWith(`${sourceRoot}/`) &&
      ts.isSatisfiesExpression(node) &&
      isCoreType(node.type)
    ) {
      failures.push(`${relative(root, path)} defines a second Core implementation`);
    }
    if (!allowedCoreImplementations.has(path) && behavioralMethodCount(node) >= 10) {
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
  [join(sourceRoot, "core", "second.ts"), "const second = { ...createCoreStub() } as Core;"],
  [
    join(sourceRoot, "core", "second.ts"),
    'import type { Core as C } from "../boundary/contract"; const second: C = { ...createCoreStub() };',
  ],
  [
    join(sourceRoot, "core", "second.ts"),
    "class Second { snapshot() {} newDisk() {} openImage() {} schema() {} undo() {} redo() {} beginGesture() {} commitGesture() {} setAreaField() {} renameBank() {} }",
  ],
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
