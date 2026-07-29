const allowedRootKeys = new Set([
  "name",
  "run-name",
  "on",
  "permissions",
  "concurrency",
  "jobs",
]);
const nonCanonicalNestedKey =
  /^(?: {4}| {8})(?:["']|[?:]\s|<<\s*:|[!&*]|[^#\s][^:]*[ \t]+:)/;

export function workflowSyntaxProblem(lines) {
  const rootKeys = new Set();
  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index];
    if (line.trim() === "" || line.trimStart().startsWith("#")) {
      continue;
    }
    if (/^\S/.test(line)) {
      const match = /^([a-z][a-z0-9-]*):(?:\s|$)/.exec(line);
      if (!match || !allowedRootKeys.has(match[1])) {
        return `contains an unsupported workflow-wide key at line ${index + 1}`;
      }
      if (rootKeys.has(match[1])) {
        return `contains duplicate workflow-wide key '${match[1]}'`;
      }
      rootKeys.add(match[1]);
      continue;
    }
    if (nonCanonicalNestedKey.test(line)) {
      return `contains non-canonical mapping-key syntax at line ${index + 1}`;
    }
  }
  if (!rootKeys.has("jobs")) {
    return "must define one canonical jobs key";
  }
  return null;
}
