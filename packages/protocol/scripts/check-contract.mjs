import assert from "node:assert/strict";
import fs from "node:fs";

import { generateContractDeclaration, generatedPath, specPath } from "./generate-contract.mjs";

const spec = fs.readFileSync(specPath, "utf8");
const generated = fs.readFileSync(generatedPath, "utf8");

const expectedGenerated = await generateContractDeclaration();
assert.equal(
  generated,
  expectedGenerated,
  "openapi.d.ts is stale; run pnpm --filter @clickclack/protocol generate",
);

const publicOperations = [
  ["/healthz", "get"],
  ["/readyz", "get"],
  ["/metrics", "get"],
  ["/api/auth/magic/request", "post"],
  ["/api/auth/magic/consume", "post"],
  ["/api/auth/github/start", "get"],
  ["/api/auth/github/desktop/start", "get"],
  ["/api/auth/github/desktop/consume", "post"],
  ["/api/auth/github/callback", "get"],
];

assert.match(spec, /^security:\n  - bearerAuth: \[\]\n  - cookieAuth: \[\]$/m);
assert.match(spec, /^  securitySchemes:\n    bearerAuth:/m);
assert.match(spec, /^    cookieAuth:/m);
for (const [route, method] of publicOperations) {
  assert.match(
    yamlOperationBlock(spec, route, method),
    /^      security: \[\]$/m,
    `${method.toUpperCase()} ${route} must explicitly opt out of authentication`,
  );
}
assert.equal(
  spec.match(/^      security: \[\]$/gm)?.length ?? 0,
  publicOperations.length,
  "only documented public operations may opt out of authentication",
);

const nonJSONSuccessOperations = new Set([
  "getMetrics",
  "deleteWorkspace",
  "removeBotFromWorkspace",
  "getUpload",
]);
for (const [operationId, block] of operationBlocks(generated)) {
  const responses = objectPropertyBlock(block, "responses");
  if (!responses || !/\n\s+2\d\d: \{/.test(responses)) continue;
  if (nonJSONSuccessOperations.has(operationId)) continue;
  assert.match(
    responses,
    /"application\/json":/,
    `${operationId} has a 2xx JSON response without a generated content schema`,
  );
}

console.log("OpenAPI contract checks passed");

function yamlOperationBlock(source, route, method) {
  const lines = source.split("\n");
  const routeIndex = lines.findIndex((line) => line === `  ${route}:`);
  assert.notEqual(routeIndex, -1, `missing OpenAPI path ${route}`);
  const methodIndex = lines.findIndex(
    (line, index) => index > routeIndex && line === `    ${method}:`,
  );
  assert.notEqual(methodIndex, -1, `missing ${method.toUpperCase()} ${route}`);
  let end = methodIndex + 1;
  while (end < lines.length && !/^( {0,4}\S|  \/)/.test(lines[end])) end += 1;
  return lines.slice(methodIndex, end).join("\n");
}

function operationBlocks(source) {
  const marker = "export interface operations {";
  const interfaceStart = source.indexOf(marker);
  assert.notEqual(interfaceStart, -1, "generated operations interface is missing");
  const blocks = [];
  const memberPattern = /^  ([A-Za-z0-9_]+): \{$/gm;
  memberPattern.lastIndex = interfaceStart + marker.length;
  for (let match = memberPattern.exec(source); match; match = memberPattern.exec(source)) {
    const start = match.index + match[0].lastIndexOf("{");
    const end = matchingBrace(source, start);
    blocks.push([match[1], source.slice(match.index, end + 2)]);
    memberPattern.lastIndex = end + 1;
  }
  return blocks;
}

function objectPropertyBlock(source, property) {
  const match = new RegExp(`\\n\\s+${property}: \\{`).exec(source);
  if (!match) return "";
  const start = match.index + match[0].lastIndexOf("{");
  const end = matchingBrace(source, start);
  return source.slice(start, end + 1);
}

function matchingBrace(source, start) {
  let depth = 0;
  for (let index = start; index < source.length; index += 1) {
    if (source[index] === "{") depth += 1;
    if (source[index] === "}") depth -= 1;
    if (depth === 0) return index;
  }
  throw new Error(`unclosed generated type block at offset ${start}`);
}
