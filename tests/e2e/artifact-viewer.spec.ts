import { expect, test, type Page } from "@playwright/test";
import { classifyArtifact } from "../../apps/web/src/lib/artifacts";
import {
  assertSafePDFCanvas,
  PDF_CANVAS_DIMENSION_LIMIT,
  PDF_CANVAS_PIXEL_LIMIT,
} from "../../apps/web/src/lib/pdf";
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

function oversizedPagePDF(): Buffer {
  const objects = [
    "<< /Type /Catalog /Pages 2 0 R >>",
    "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 20000 20000] /Contents 4 0 R >>",
    "<< /Length 0 >>\nstream\n\nendstream",
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

test("bounds PDF canvas dimensions and total backing pixels", () => {
  expect(() => assertSafePDFCanvas(4_096, 4_096)).not.toThrow();
  expect(() => assertSafePDFCanvas(PDF_CANVAS_DIMENSION_LIMIT + 1, 1)).toThrow(
    "too large to preview safely",
  );
  expect(() => assertSafePDFCanvas(PDF_CANVAS_PIXEL_LIMIT / 4_096 + 1, 4_096)).toThrow(
    "too large to preview safely",
  );
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
        '<!doctype html><html><head><style>@import "https://artifact-proof.invalid/import.css"; h1{color:teal;background:url(https://artifact-proof.invalid/background.png)}</style></head><body><h1>Sandboxed web artifact</h1><a href="https://artifact-proof.invalid/navigate">external link</a><img src="https://artifact-proof.invalid/leak.png"><form action="https://artifact-proof.invalid/submit"><button>submit</button></form><iframe src="https://artifact-proof.invalid/frame"></iframe><script>window.parent.__artifactScriptRan = true</script></body></html>',
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
  const iframe = viewer.locator("iframe");
  const frame = iframe.contentFrame();
  await expect(frame.getByRole("heading", { name: "Sandboxed web artifact" })).toBeVisible();
  await expect(iframe).toHaveAttribute("sandbox", "");
  await expect(frame.locator("script, form, iframe")).toHaveCount(0);
  await expect(frame.getByText("external link")).not.toHaveAttribute("href");
  await expect(frame.locator("img")).not.toHaveAttribute("src");
  await expect(frame.locator("style")).not.toContainText("artifact-proof.invalid");
  await expect.poll(() => externalRequests).toBe(0);
  const scriptMarker = await page.evaluate(
    () => (window as Window & { __artifactScriptRan?: boolean }).__artifactScriptRan,
  );
  expect(scriptMarker).toBeUndefined();

  if (process.env.CAPTURE_ARTIFACT_PROOF === "1") {
    await page.evaluate(
      ({ requests, scriptRan }) => {
        const diagnostics = document.createElement("aside");
        diagnostics.setAttribute("data-artifact-proof", "");
        diagnostics.style.cssText =
          "position:fixed;left:24px;bottom:24px;z-index:9999;width:430px;padding:20px;border:1px solid #35516f;border-radius:12px;background:#101820;color:#eef6ff;font:14px/1.5 ui-monospace,monospace;box-shadow:0 18px 50px #0008";
        diagnostics.innerHTML = `<strong style="display:block;margin-bottom:10px;color:#7ee787">Playwright live browser diagnostics: PASS</strong>
          <div>iframe sandbox tokens: none</div>
          <div>script execution marker: ${scriptRan ? "SET (FAIL)" : "absent"}</div>
          <div>requests to artifact-proof.invalid: ${requests}</div>
          <div>scripts/forms/frames in preview: 0 / 0 / 0</div>
          <div>external href/src/CSS references: stripped</div>`;
        document.body.append(diagnostics);
      },
      { requests: externalRequests, scriptRan: scriptMarker === true },
    );
    await page.screenshot({
      path: "docs/proof/artifact-viewer-html-isolation.png",
      fullPage: true,
    });
  }

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
    {
      filename: "oversized-page.pdf",
      contentType: "application/pdf",
      body: oversizedPagePDF(),
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
  await page.waitForTimeout(250);

  await page.getByRole("button", { name: "Open oversized-page.pdf" }).click();
  await expect(viewer.getByRole("alert")).toContainText("too large to preview safely");
  await expect(viewer.getByRole("link", { name: "Download original" })).toBeVisible();
  await expect(viewer.locator("canvas")).toHaveCount(0);

  if (process.env.CAPTURE_PDF_LIMIT_PROOF === "1") {
    const canvasCount = await viewer.locator("canvas").count();
    await page.evaluate(
      ({ canvases, dimensionLimit, pixelLimit }) => {
        const diagnostics = document.createElement("aside");
        diagnostics.setAttribute("data-artifact-proof", "");
        diagnostics.style.cssText =
          "position:fixed;left:24px;bottom:24px;z-index:9999;width:450px;padding:20px;border:1px solid #35516f;border-radius:12px;background:#101820;color:#eef6ff;font:14px/1.5 ui-monospace,monospace;box-shadow:0 18px 50px #0008";
        diagnostics.innerHTML = `<strong style="display:block;margin-bottom:10px;color:#7ee787">Playwright live PDF diagnostics: PASS</strong>
          <div>crafted page geometry: 20,000 × 20,000 pt</div>
          <div>backing dimension limit: ${dimensionLimit.toLocaleString()} px</div>
          <div>backing pixel budget: ${(pixelLimit / 1024 / 1024).toFixed(0)} MP</div>
          <div>viewer canvases allocated: ${canvases}</div>
          <div>safe download fallback: visible</div>`;
        document.body.append(diagnostics);
      },
      {
        canvases: canvasCount,
        dimensionLimit: PDF_CANVAS_DIMENSION_LIMIT,
        pixelLimit: PDF_CANVAS_PIXEL_LIMIT,
      },
    );
    await page.screenshot({
      path: "docs/proof/artifact-viewer-pdf-canvas-limit.png",
      fullPage: true,
    });
  }
});

test("near-limit code remains interruptible and falls back to escaped source", async ({ page }) => {
  const nearLimitSource = `<script>window.__artifactCodeRan = true</script>\n${"const value = 1;\n".repeat(120_000)}`;
  const { channel } = await seedArtifacts(page, [
    {
      filename: "near-limit.ts",
      contentType: "text/typescript",
      body: Buffer.from(nearLimitSource.slice(0, 2 * 1024 * 1024)),
    },
  ]);
  await page.goto("/app");
  await page.getByRole("link", { name: `# ${channel.name}` }).click();

  await page.getByRole("button", { name: "Open near-limit.ts" }).click();
  const viewer = page.getByRole("complementary", { name: "Artifact viewer" });
  await expect(viewer.locator("pre")).toContainText("window.__artifactCodeRan", { timeout: 5_000 });
  await expect(viewer.locator(".hljs-keyword")).toHaveCount(0);
  expect(
    await page.evaluate(
      () => (window as Window & { __artifactCodeRan?: boolean }).__artifactCodeRan,
    ),
  ).toBeUndefined();

  await page.keyboard.press("Escape");
  await expect(viewer).toHaveCount(0, { timeout: 1_000 });
  await expect(page.getByRole("button", { name: "Open near-limit.ts" })).toBeFocused();
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
