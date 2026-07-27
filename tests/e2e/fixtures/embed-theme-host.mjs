import { createServer } from "node:http";

const port = Number(process.env.CLICKCLACK_EMBED_HOST_PORT || "18083");

const server = createServer((request, response) => {
  const url = new URL(request.url || "/", `http://127.0.0.1:${port}`);

  if (url.pathname === "/healthz") {
    response.writeHead(200, { "content-type": "text/plain; charset=utf-8" });
    response.end("ok");
    return;
  }

  const embed = url.searchParams.get("embed");
  if (url.pathname !== "/theme-proof" || !embed) {
    response.writeHead(404, { "content-type": "text/plain; charset=utf-8" });
    response.end("not found");
    return;
  }

  let embedUrl;
  try {
    embedUrl = new URL(embed);
  } catch {
    response.writeHead(400, { "content-type": "text/plain; charset=utf-8" });
    response.end("invalid embed URL");
    return;
  }

  if (
    embedUrl.protocol !== "http:" ||
    embedUrl.hostname !== "127.0.0.1" ||
    !embedUrl.pathname.startsWith("/embed/channel/")
  ) {
    response.writeHead(400, { "content-type": "text/plain; charset=utf-8" });
    response.end("invalid embed origin");
    return;
  }

  response.writeHead(200, { "content-type": "text/html; charset=utf-8" });
  response.end(`<!doctype html>
    <html><head><meta charset="utf-8"><title>Host theme proof</title>
    <style>
      :root { color-scheme: light; }
      body { margin: 0; padding: 24px; background: #faf9f7; color: #27272a;
        font: 14px system-ui, sans-serif; }
      main { display: grid; grid-template-columns: minmax(0, 1fr) minmax(320px, 460px);
        min-height: 620px; overflow: hidden; border: 1px solid #e4e4e7; border-radius: 8px; }
      section { padding: 24px; }
      iframe { width: 100%; height: 620px; border: 0; border-left: 1px solid #e4e4e7; }
    </style></head><body><main>
      <section><h1>OpenClaw host theme</h1><p>Cross-origin ClickClack sidebar</p></section>
      <iframe title="ClickClack discussion" src="${embedUrl.href.replaceAll("&", "&amp;")}"></iframe>
    </main></body></html>`);
});

server.listen(port, "127.0.0.1");
