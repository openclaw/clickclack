// A stand-in for a browser push service. It answers 201 the way a real relay
// does and records what arrived, so a test can prove the server encrypted and
// signed a delivery without reaching the internet.
//
// It records metadata only. The body is an RFC 8291 encrypted record and the
// endpoint path is a delivery secret, so neither is kept.

import { createServer } from "node:http";
import { createPublicKey, verify } from "node:crypto";

const port = Number(process.env.CLICKCLACK_PUSH_RELAY_PORT || 18084);
const deliveries = [];

function validVAPID(authorization) {
  try {
    const match = /^vapid t=([^,]+), k=(.+)$/.exec(authorization);
    if (!match) return false;
    const [header, payload, signature] = match[1].split(".");
    const point = Buffer.from(match[2], "base64url");
    if (point.length !== 65 || point[0] !== 4) return false;
    const key = createPublicKey({
      format: "jwk",
      key: {
        kty: "EC",
        crv: "P-256",
        x: point.subarray(1, 33).toString("base64url"),
        y: point.subarray(33).toString("base64url"),
      },
    });
    const claims = JSON.parse(Buffer.from(payload, "base64url").toString("utf8"));
    const tokenHeader = JSON.parse(Buffer.from(header, "base64url").toString("utf8"));
    const now = Date.now() / 1000;
    return (
      tokenHeader.alg === "ES256" &&
      claims.aud === "http://127.0.0.1:" + port &&
      claims.exp > now &&
      claims.exp <= now + 86400 &&
      verify(
        "sha256",
        Buffer.from(header + "." + payload),
        { key, dsaEncoding: "ieee-p1363" },
        Buffer.from(signature, "base64url"),
      )
    );
  } catch {
    return false;
  }
}

const server = createServer((request, response) => {
  if (request.url === "/healthz") {
    response.writeHead(200, { "content-type": "text/plain" });
    response.end("ok");
    return;
  }
  if (request.url === "/deliveries") {
    response.writeHead(200, { "content-type": "application/json" });
    response.end(JSON.stringify(deliveries));
    return;
  }
  if (request.method !== "POST") {
    response.writeHead(405);
    response.end();
    return;
  }
  if (!validVAPID(String(request.headers.authorization || ""))) {
    response.writeHead(401);
    response.end();
    return;
  }
  let length = 0;
  request.on("data", (chunk) => {
    length += chunk.length;
  });
  request.on("end", () => {
    deliveries.push({
      authorization: String(request.headers.authorization || "").slice(0, 6),
      hasVapidToken: true,
      contentEncoding: request.headers["content-encoding"],
      contentType: request.headers["content-type"],
      ttl: request.headers.ttl,
      urgency: request.headers.urgency,
      encryptedBytes: length,
    });
    response.writeHead(201);
    response.end();
  });
});

server.listen(port, "127.0.0.1", () => {
  process.stdout.write(`push relay listening on ${port}\n`);
});
