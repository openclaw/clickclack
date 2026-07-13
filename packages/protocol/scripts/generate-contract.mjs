import fs from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

export const packageDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
export const repoDir = path.resolve(packageDir, "../..");
export const specPath = path.join(packageDir, "openapi.yaml");
export const generatedPath = path.join(repoDir, "packages/sdk-ts/src/generated/openapi.d.ts");

export async function generateContractDeclaration() {
  const openapi = await loadOpenAPITypescript();
  const { format } = await loadOxfmt();
  const ast = await openapi.default(pathToFileURL(specPath));
  const source = openapi.COMMENT_HEADER + openapi.astToString(ast);
  const result = await format(generatedPath, source);
  if (result.errors.length > 0) {
    throw new Error(
      `failed to format generated OpenAPI declaration: ${JSON.stringify(result.errors)}`,
    );
  }
  return result.code;
}

async function loadOpenAPITypescript() {
  return loadDependency("openapi-typescript", "openapi-typescript@", "dist/index.mjs");
}

async function loadOxfmt() {
  return loadDependency("oxfmt", "oxfmt@", "dist/index.js");
}

async function loadDependency(specifier, packagePrefix, packageEntry) {
  try {
    return await import(specifier);
  } catch (error) {
    const pnpmDir = path.join(repoDir, "node_modules/.pnpm");
    const pnpmEntry = fs.readdirSync(pnpmDir).find((entry) => entry.startsWith(packagePrefix));
    if (!pnpmEntry) throw error;
    return import(
      pathToFileURL(path.join(pnpmDir, pnpmEntry, "node_modules", specifier, packageEntry)).href
    );
  }
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  fs.writeFileSync(generatedPath, await generateContractDeclaration());
}
