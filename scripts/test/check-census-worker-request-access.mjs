import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { API } from "typescript/unstable/sync";
import * as ast from "typescript/unstable/ast";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const workerRoot = resolve(repoRoot, "census-worker");
const api = new API({ cwd: repoRoot });

function findFunction(sourceFile, predicate) {
  let found;
  function visit(node) {
    if (found) {
      return;
    }
    if (ast.isFunctionLikeDeclaration(node) && node.body && predicate(node)) {
      found = node;
      return;
    }
    node.forEachChild(visit);
  }
  visit(sourceFile);
  return found;
}

function isIncomingRequestParameter(node) {
  return (
    node.parameters.length === 2 &&
    ast.isIdentifier(node.parameters[0].name) &&
    node.parameters[0].name.text === "incomingRequest"
  );
}

function isWorkerFetch(node) {
  const object = node.parent;
  const declaration = object?.parent;
  return (
    isIncomingRequestParameter(node) &&
    ast.isMethodDeclaration(node) &&
    ast.isIdentifier(node.name) &&
    node.name.text === "fetch" &&
    ast.isObjectLiteralExpression(object) &&
    ast.isVariableDeclaration(declaration) &&
    ast.isIdentifier(declaration.name) &&
    declaration.name.text === "worker"
  );
}

function isRequestAdapter(node) {
  return (
    node.parameters.length === 1 &&
    ast.isIdentifier(node.parameters[0].name) &&
    node.parameters[0].name.text === "incomingRequest" &&
    ast.isFunctionDeclaration(node) &&
    ast.isIdentifier(node.name) &&
    node.name.text === "adaptCensusRequest"
  );
}

function classifyAdapterRead(identifier) {
  const access = identifier.parent;
  if (
    !ast.isPropertyAccessExpression(access) ||
    access.expression !== identifier
  ) {
    return "";
  }
  if (["url", "method", "body"].includes(access.name.text)) {
    return access.name.text;
  }
  if (access.name.text !== "headers") {
    return "";
  }
  const getAccess = access.parent;
  const call = getAccess.parent;
  if (
    !ast.isPropertyAccessExpression(getAccess) ||
    getAccess.expression !== access ||
    getAccess.name.text !== "get" ||
    !ast.isCallExpression(call) ||
    call.expression !== getAccess ||
    call.arguments.length !== 1 ||
    !ast.isStringLiteral(call.arguments[0])
  ) {
    return "";
  }
  const header = call.arguments[0].text;
  return header === "content-type" || header === "content-length"
    ? `headers.get:${header}`
    : "";
}

function isExactIndexForward(identifier) {
  const call = identifier.parent;
  return (
    ast.isCallExpression(call) &&
    call.arguments.length === 1 &&
    call.arguments[0] === identifier &&
    ast.isIdentifier(call.expression) &&
    call.expression.text === "adaptCensusRequest"
  );
}

function inspectParameter(checker, fn, mode) {
  const parameter = fn.parameters[0].name;
  const symbol = checker.getSymbolAtLocation(parameter);
  if (!symbol) {
    return ["could not resolve the incoming Request parameter symbol"];
  }
  const problems = [];
  const reads = new Map();
  function visit(node) {
    if (ast.isIdentifier(node) && node.text === "arguments") {
      problems.push(
        `${mode} must not access the incoming Request through arguments`,
      );
    }
    if (ast.isIdentifier(node) && node !== parameter) {
      const nodeSymbol = checker.getSymbolAtLocation(node);
      if (nodeSymbol?.id === symbol.id) {
        const read =
          mode === "adapter"
            ? classifyAdapterRead(node)
            : isExactIndexForward(node)
              ? "adapter-forward"
              : "";
        if (!read) {
          problems.push(
            `${mode} has a bare, aliased, computed, or additional incoming Request access: ${node.parent.getText()}`,
          );
        } else {
          reads.set(read, (reads.get(read) ?? 0) + 1);
        }
      }
    }
    node.forEachChild(visit);
  }
  visit(fn.body);

  const expected =
    mode === "adapter"
      ? new Map([
          ["url", 1],
          ["method", 2],
          ["body", 1],
          ["headers.get:content-type", 1],
          ["headers.get:content-length", 1],
        ])
      : new Map([["adapter-forward", 1]]);
  for (const [read, expectedCount] of expected) {
    const actual = reads.get(read) ?? 0;
    if (actual !== expectedCount) {
      problems.push(
        `${mode} ${read} read count is ${actual}, expected ${expectedCount}`,
      );
    }
  }
  for (const read of reads.keys()) {
    if (!expected.has(read)) {
      problems.push(`${mode} has unapproved Request read ${read}`);
    }
  }
  return problems;
}

let snapshot;
try {
  snapshot = api.updateSnapshot({
    openProjects: [resolve(workerRoot, "tsconfig.json")],
  });
  const project = snapshot.getProjects().find(
    (candidate) =>
      candidate.configFileName === resolve(workerRoot, "tsconfig.json"),
  );
  const indexFile = project?.program.getSourceFile(
    resolve(workerRoot, "src/index.ts"),
  );
  const adapterFile = project?.program.getSourceFile(
    resolve(workerRoot, "src/request-adapter.ts"),
  );
  const indexFetch = indexFile && findFunction(indexFile, isWorkerFetch);
  const adapter = adapterFile && findFunction(adapterFile, isRequestAdapter);
  const problems =
    !project || !indexFetch || !adapter
      ? ["could not resolve the Worker entry point and request adapter"]
      : [
          ...inspectParameter(project.checker, indexFetch, "index"),
          ...inspectParameter(project.checker, adapter, "adapter"),
        ];
  if (problems.length > 0) {
    console.error("Census Worker request-access allowlist failed:");
    for (const problem of problems) {
      console.error(`- ${problem}`);
    }
    process.exitCode = 1;
  } else {
    console.log(
      "Census Worker Request reads are AST-limited to method, URL, body, content-type, and content-length",
    );
  }
} finally {
  snapshot?.dispose();
  api.close();
}
