import { mkdir, copyFile } from "node:fs/promises";

await mkdir("dist/generated", { recursive: true });
await copyFile("src/generated/openapi.d.ts", "dist/generated/openapi.d.ts");
