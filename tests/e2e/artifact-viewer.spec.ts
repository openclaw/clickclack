import { expect, test, type Page } from "@playwright/test";
import { classifyArtifact } from "../../apps/web/src/lib/artifacts";
import type { Upload } from "../../apps/web/src/lib/types";

const DOCX_FIXTURE = Buffer.from(
  "UEsDBBQAAAAIAKJI61zMVIwQ4AAAAJwBAAATAAAAW0NvbnRlbnRfVHlwZXNdLnhtbH2Qy07DMBBFf8XyFsUTukAIJekCyhJYlA+w7Eli4Zc8bil/z6QtXaDC0r6PM7rd+hC82GMhl2Ivb1UrBUaTrItTL9+3z829XA/d9isjCbZG6uVca34AIDNj0KRSxsjKmErQlZ9lgqzNh54QVm17BybFirE2demQQ/eEo975KjYH/j5hC3qS4vFkXFi91Dl7Z3RlHfbR/qI0Z4Li5NFDs8t0wwYJVwmL8jfgnHvlHYqzKN50qS86sAs+U7Fgk9kFTqr/a67cmcbRGbzkl7ZckkEiHjh4dVGCdvHnfjjOPXwDUEsDBBQAAAAIAKJI61w2V97cogAAABgBAAALAAAAX3JlbHMvLnJlbHONzzsOwjAMBuCrRN6pCwNCqGkXhNQVlQNEiZtGNA8l4XV7MjBQxMBo+/dnuekedmY3isl4x2Fd1cDISa+M0xzOw3G1g65tTjSLXBJpMiGxsuIShynnsEdMciIrUuUDuTIZfbQilzJqDEJehCbc1PUW46cBS5P1ikPs1RrY8Az0j+3H0Ug6eHm15PKPE1+JIouoKXO4+6hQvdtVYQHbBhcvti9QSwMEFAAAAAgAokjrXI3J6pj/AAAA2AEAABEAAAB3b3JkL2RvY3VtZW50LnhtbHWRT0/DMAzFv0qUO03hgFDVdkJFXJm2IXHNEneLlj+V463025O2rEgwLn6y8t7PkV2uPp1lF8Bogq/4fZZzBl4Fbfyh4u+717snziJJr6UNHio+QOSruuwLHdTZgSeWAD4WfcWPRF0hRFRHcDJmoQOf3tqATlJq8SD6gLrDoCDGxHdWPOT5o3DSeD4i90EPo3ZTWeMkWxossL64SFvxnSELXNSlWAxTofoZybRSEUv80LLr70YjTXacQwv/O7cBrwFBsxaDYy9vzQcznjXWqFNjpTplNwm0t5PMEPUbuiVJ53gjKa7uv5kNSD38HxHzNLHMjqBojfMq5sWJn6PUX1BLAQIUAxQAAAAIAKJI61zMVIwQ4AAAAJwBAAATAAAAAAAAAAAAAACAAQAAAABbQ29udGVudF9UeXBlc10ueG1sUEsBAhQDFAAAAAgAokjrXDZX3tyiAAAAGAEAAAsAAAAAAAAAAAAAAIABEQEAAF9yZWxzLy5yZWxzUEsBAhQDFAAAAAgAokjrXI3J6pj/AAAA2AEAABEAAAAAAAAAAAAAAIAB3AEAAHdvcmQvZG9jdW1lbnQueG1sUEsFBgAAAAADAAMAuQAAAAoDAAAAAA==",
  "base64",
);

function highExpansionDocx(): Buffer {
  const fixture = Buffer.from(DOCX_FIXTURE);
  for (let offset = 0; offset <= fixture.length - 46; offset += 1) {
    if (fixture.readUInt32LE(offset) !== 0x02014b50) continue;
    fixture.writeUInt32LE(64 * 1024 * 1024, offset + 24);
    break;
  }
  return fixture;
}

type Fixture = { filename: string; contentType: string; body: Buffer };

function uploadShape(filename: string, contentType: string): Upload {
  return {
    id: "upl_test",
    workspace_id: "wsp_test",
    owner_id: "usr_test",
    filename,
    content_type: contentType,
    byte_size: 1,
    created_at: new Date(0).toISOString(),
  };
}

function minimalPDF(): Buffer {
  const objects = [
    "<< /Type /Catalog /Pages 2 0 R >>",
    "<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 2 >>",
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 180] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
    "<< /Length 52 >>\nstream\nBT /F1 18 Tf 36 100 Td (Artifact PDF proof) Tj ET\nendstream",
    "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 180] /Resources << /Font << /F1 5 0 R >> >> /Contents 7 0 R >>",
    "<< /Length 57 >>\nstream\nBT /F1 18 Tf 36 100 Td (Artifact PDF page two) Tj ET\nendstream",
  ];
  let pdf = "%PDF-1.4\n";
  const offsets = [0];
  objects.forEach((object, index) => {
    offsets.push(Buffer.byteLength(pdf));
    pdf += `${index + 1} 0 obj\n${object}\nendobj\n`;
  });
  const xref = Buffer.byteLength(pdf);
  pdf += `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n`;
  pdf += offsets
    .slice(1)
    .map((offset) => `${offset.toString().padStart(10, "0")} 00000 n \n`)
    .join("");
  pdf += `trailer\n<< /Size ${objects.length + 1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`;
  return Buffer.from(pdf);
}

async function seedArtifacts(page: Page, fixtures: Fixture[]) {
  const workspaceResponse = await page.request.get("/api/workspaces");
  const { workspaces } = (await workspaceResponse.json()) as { workspaces: { id: string }[] };
  const workspaceID = workspaces[0].id;
  const name = `zz-artifacts-${Date.now()}`;
  const channelResponse = await page.request.post(`/api/workspaces/${workspaceID}/channels`, {
    data: { name, kind: "public" },
  });
  const { channel } = (await channelResponse.json()) as { channel: { id: string; name: string } };
  const messages: Record<string, string> = {};

  for (const fixture of fixtures) {
    const messageResponse = await page.request.post(`/api/channels/${channel.id}/messages`, {
      data: { body: `Viewer fixture: ${fixture.filename}` },
    });
    expect(messageResponse.ok()).toBe(true);
    const { message } = (await messageResponse.json()) as { message: { id: string } };
    const uploadResponse = await page.request.post(`/api/uploads?workspace_id=${workspaceID}`, {
      multipart: {
        file: { name: fixture.filename, mimeType: fixture.contentType, buffer: fixture.body },
      },
    });
    expect(uploadResponse.ok()).toBe(true);
    const { upload } = (await uploadResponse.json()) as { upload: { id: string } };
    const attachResponse = await page.request.post(`/api/messages/${message.id}/attachments`, {
      data: { upload_id: upload.id },
    });
    expect(attachResponse.ok()).toBe(true);
    messages[fixture.filename] = message.id;
  }

  return { channel, messages };
}

test("classifies artifacts by filename and original MIME metadata", () => {
  expect(classifyArtifact(uploadShape("README.md", "application/octet-stream"))).toBe("markdown");
  expect(classifyArtifact(uploadShape("worker.ts", "text/plain"))).toBe("code");
  expect(classifyArtifact(uploadShape("page.html", "application/octet-stream"))).toBe("html");
  expect(classifyArtifact(uploadShape("report.pdf", "application/octet-stream"))).toBe("pdf");
  expect(classifyArtifact(uploadShape("brief.docx", "application/octet-stream"))).toBe("docx");
  expect(classifyArtifact(uploadShape("notes.log", "application/octet-stream"))).toBe("text");
  expect(classifyArtifact(uploadShape("archive.zip", "application/zip"))).toBe("unsupported");
});

test("opens safe code, markdown, PDF, DOCX, and HTML previews in the side pane", async ({
  page,
}) => {
  const pageErrors: string[] = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));
  let externalRequests = 0;
  page.on("request", (request) => {
    if (request.url().startsWith("https://artifact-proof.invalid/")) externalRequests += 1;
  });
  const fixtures: Fixture[] = [
    {
      filename: "viewer-proof.ts",
      contentType: "text/typescript",
      body: Buffer.from("const proof: string = 'highlighted';\nconsole.log(proof);\n"),
    },
    {
      filename: "viewer-proof.md",
      contentType: "text/markdown",
      body: Buffer.from(
        "# Markdown artifact\n\n**Safe preview**\n\n<script>window.parent.__artifactScriptRan = true</script>",
      ),
    },
    { filename: "viewer-proof.pdf", contentType: "application/pdf", body: minimalPDF() },
    {
      filename: "viewer-proof.docx",
      contentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
      body: DOCX_FIXTURE,
    },
    {
      filename: "viewer-proof.html",
      contentType: "text/html",
      body: Buffer.from(
        '<!doctype html><html><head><style>h1{color:teal}</style></head><body><h1>Sandboxed web artifact</h1><img src="https://artifact-proof.invalid/leak.png"><script>window.parent.__artifactScriptRan = true</script></body></html>',
      ),
    },
  ];
  const { channel } = await seedArtifacts(page, fixtures);

  await page.goto("/app");
  await page.getByRole("link", { name: `# ${channel.name}` }).click();

  await page.getByRole("button", { name: "Open viewer-proof.ts" }).click();
  const viewer = page.getByRole("complementary", { name: "Artifact viewer" });
  await expect(viewer).toBeVisible();
  await expect(viewer.locator(".hljs-keyword")).toContainText("const");
  await page.keyboard.press("Escape");
  await expect(viewer).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Open viewer-proof.ts" })).toBeFocused();
  await page.waitForTimeout(250);

  await page.getByRole("button", { name: "Open viewer-proof.md" }).click();
  await expect(viewer.getByRole("heading", { name: "Markdown artifact" })).toBeVisible();
  await expect(viewer.locator("script")).toHaveCount(0);
  await viewer.getByRole("button", { name: "Source" }).click();
  await expect(viewer.locator("pre")).toContainText("# Markdown artifact");
  await viewer.getByRole("button", { name: "Close artifact viewer" }).click();
  await page.waitForTimeout(250);

  await page.getByRole("button", { name: "Open viewer-proof.pdf" }).click();
  await expect(viewer.getByText("Page 1 of 2")).toBeVisible();
  await expect(viewer.locator("canvas")).toBeVisible();
  await viewer.getByRole("button", { name: "Next" }).click();
  await expect(viewer.getByText("Page 2 of 2")).toBeVisible();
  await viewer.getByRole("button", { name: "Zoom in" }).click();
  await expect(viewer.getByText("120%")).toBeVisible();
  await viewer.getByRole("button", { name: "Close artifact viewer" }).click();
  await page.waitForTimeout(250);

  await page.getByRole("button", { name: "Open viewer-proof.docx" }).click();
  await page.waitForTimeout(100);
  expect(pageErrors).toEqual([]);
  await expect(viewer).toBeVisible();
  await expect(viewer.getByText("Artifact proof document")).toBeVisible();
  await expect(viewer.getByText("Rendered from DOCX in ClickClack.")).toBeVisible();
  await expect(viewer.getByText("Ready")).toBeVisible();
  await viewer.getByRole("button", { name: "Close artifact viewer" }).click();
  await page.waitForTimeout(250);

  await page.getByRole("button", { name: "Open viewer-proof.html" }).click();
  const frame = viewer.locator("iframe").contentFrame();
  await expect(frame.getByRole("heading", { name: "Sandboxed web artifact" })).toBeVisible();
  await expect.poll(() => externalRequests).toBe(0);
  expect(
    await page.evaluate(
      () => (window as Window & { __artifactScriptRan?: boolean }).__artifactScriptRan,
    ),
  ).toBeUndefined();

  await page.setViewportSize({ width: 390, height: 844 });
  const bounds = await viewer.evaluate((element) => {
    const rect = element.getBoundingClientRect();
    return { left: rect.left, top: rect.top, width: rect.width, height: rect.height };
  });
  expect(bounds.left).toBe(0);
  expect(bounds.top).toBe(0);
  expect(bounds.width).toBe(390);
  expect(bounds.height).toBe(844);
});

test("shows local fallbacks for oversized and malformed artifacts", async ({ page }) => {
  const fixtures: Fixture[] = [
    {
      filename: "oversized.txt",
      contentType: "text/plain",
      body: Buffer.alloc(2 * 1024 * 1024 + 1, 0x61),
    },
    {
      filename: "malformed.docx",
      contentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
      body: Buffer.from("not a DOCX package"),
    },
    {
      filename: "high-expansion.docx",
      contentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
      body: highExpansionDocx(),
    },
  ];
  const { channel } = await seedArtifacts(page, fixtures);
  await page.goto("/app");
  await page.getByRole("link", { name: `# ${channel.name}` }).click();
  const viewer = page.getByRole("complementary", { name: "Artifact viewer" });

  await page.getByRole("button", { name: "Open oversized.txt" }).click();
  await expect(viewer.getByRole("alert")).toContainText("Preview is limited to 2.0 MB");
  await expect(viewer.getByRole("link", { name: "Download original" })).toBeVisible();
  await viewer.getByRole("button", { name: "Close artifact viewer" }).click();
  await page.waitForTimeout(250);

  await page.getByRole("button", { name: "Open malformed.docx" }).click();
  await expect(viewer.getByRole("alert")).toContainText("Preview unavailable");
  await expect(viewer.getByRole("link", { name: "Download original" })).toBeVisible();
  await viewer.getByRole("button", { name: "Close artifact viewer" }).click();
  await page.waitForTimeout(250);

  await page.getByRole("button", { name: "Open high-expansion.docx" }).click();
  await expect(viewer.getByRole("alert")).toContainText("too complex to preview safely");
  await expect(viewer.getByRole("link", { name: "Download original" })).toBeVisible();
  await viewer.getByRole("button", { name: "Close artifact viewer" }).click();
  await expect(page.getByRole("button", { name: "Open high-expansion.docx" })).toBeFocused();
});

test("adds an attachment from message.updated without reloading", async ({ page }) => {
  const { channel } = await seedArtifacts(page, []);
  const messageResponse = await page.request.post(`/api/channels/${channel.id}/messages`, {
    data: { body: "Realtime artifact delivery" },
  });
  const { message } = (await messageResponse.json()) as { message: { id: string } };
  await page.goto("/app");
  await page.getByRole("link", { name: `# ${channel.name}` }).click();
  await expect(page.getByText("Realtime artifact delivery")).toBeVisible();

  const workspaceResponse = await page.request.get("/api/workspaces");
  const { workspaces } = (await workspaceResponse.json()) as { workspaces: { id: string }[] };
  const uploadResponse = await page.request.post(`/api/uploads?workspace_id=${workspaces[0].id}`, {
    multipart: {
      file: {
        name: "realtime-proof.md",
        mimeType: "text/markdown",
        buffer: Buffer.from("# Delivered through message.updated"),
      },
    },
  });
  const { upload } = (await uploadResponse.json()) as { upload: { id: string } };
  await page.request.post(`/api/messages/${message.id}/attachments`, {
    data: { upload_id: upload.id },
  });

  await expect(page.getByRole("button", { name: "Open realtime-proof.md" })).toBeVisible();
});
