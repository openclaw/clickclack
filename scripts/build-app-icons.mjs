// Renders the home-screen icons from apps/web/static/favicon.svg.
//
// Run it after changing the favicon, and commit what it writes:
//   node scripts/build-app-icons.mjs
//
// The source is five solid rounded rectangles, so a scanline rasterizer with
// 4x4 supersampling reproduces it exactly and the repository keeps its
// dependency-free build. The Apple touch icon is drawn without the corner
// radius: iOS applies its own mask and renders transparent corners black.

import { deflateSync } from "node:zlib";
import { readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const crcTable = Array.from({ length: 256 }, (_unused, index) => {
  let value = index;
  for (let bit = 0; bit < 8; bit += 1) {
    value = value & 1 ? 0xedb88320 ^ (value >>> 1) : value >>> 1;
  }
  return value >>> 0;
});

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const staticDir = path.join(root, "apps/web/static");

const outputs = [
  { file: "icon-192.png", size: 192, squareBackground: false },
  { file: "icon-512.png", size: 512, squareBackground: false },
  { file: "apple-touch-icon.png", size: 180, squareBackground: true },
];

const source = readFileSync(path.join(staticDir, "favicon.svg"), "utf8");
const viewBox = source.match(/viewBox="0 0 (\d+) (\d+)"/);
if (!viewBox) throw new Error("favicon.svg must declare a viewBox starting at the origin");
const [, viewWidth, viewHeight] = viewBox.map(Number);
if (viewWidth !== viewHeight) throw new Error("favicon.svg must be square");

const rects = [...source.matchAll(/<rect\b[^>]*>/g)].map((match) => {
  // The word boundary matters: a plain x="..." search also matches rx="...".
  const attribute = (name, fallback) => {
    const found = match[0].match(new RegExp(`\\b${name}="([^"]+)"`));
    return found ? found[1] : fallback;
  };
  return {
    x: Number(attribute("x", "0")),
    y: Number(attribute("y", "0")),
    width: Number(attribute("width", "0")),
    height: Number(attribute("height", "0")),
    radius: Number(attribute("rx", "0")),
    fill: parseColor(attribute("fill", "#000000")),
  };
});
if (rects.length === 0) throw new Error("favicon.svg has no rectangles to draw");

for (const output of outputs) {
  const shapes = rects.map((rect, index) =>
    index === 0 && output.squareBackground ? { ...rect, radius: 0 } : rect
  );
  writeFileSync(path.join(staticDir, output.file), encodePNG(render(shapes, output.size), output.size));
  process.stdout.write(`${output.file} ${output.size}x${output.size}\n`);
}

function parseColor(value) {
  const hex = value.replace("#", "");
  const full = hex.length === 3 ? [...hex].map((character) => character + character).join("") : hex;
  return [
    Number.parseInt(full.slice(0, 2), 16),
    Number.parseInt(full.slice(2, 4), 16),
    Number.parseInt(full.slice(4, 6), 16),
  ];
}

// render returns straight (not premultiplied) RGBA pixels. Every shape is
// opaque, so a sample either takes the topmost covering fill or stays clear,
// and the averaged coverage becomes the pixel's alpha.
function render(shapes, size) {
  const samples = 4;
  const scale = viewWidth / size;
  const pixels = Buffer.alloc(size * size * 4);
  for (let y = 0; y < size; y += 1) {
    for (let x = 0; x < size; x += 1) {
      let red = 0;
      let green = 0;
      let blue = 0;
      let covered = 0;
      for (let subY = 0; subY < samples; subY += 1) {
        for (let subX = 0; subX < samples; subX += 1) {
          const pointX = (x + (subX + 0.5) / samples) * scale;
          const pointY = (y + (subY + 0.5) / samples) * scale;
          const fill = topmostFill(shapes, pointX, pointY);
          if (!fill) continue;
          red += fill[0];
          green += fill[1];
          blue += fill[2];
          covered += 1;
        }
      }
      const offset = (y * size + x) * 4;
      if (covered === 0) continue;
      pixels[offset] = Math.round(red / covered);
      pixels[offset + 1] = Math.round(green / covered);
      pixels[offset + 2] = Math.round(blue / covered);
      pixels[offset + 3] = Math.round((covered / (samples * samples)) * 255);
    }
  }
  return pixels;
}

function topmostFill(shapes, pointX, pointY) {
  let fill = null;
  for (const shape of shapes) {
    if (insideRoundedRect(shape, pointX, pointY)) fill = shape.fill;
  }
  return fill;
}

function insideRoundedRect(rect, pointX, pointY) {
  const right = rect.x + rect.width;
  const bottom = rect.y + rect.height;
  if (pointX < rect.x || pointX > right || pointY < rect.y || pointY > bottom) return false;
  const radius = Math.min(rect.radius, rect.width / 2, rect.height / 2);
  if (radius <= 0) return true;
  const cornerX = pointX < rect.x + radius ? rect.x + radius : pointX > right - radius ? right - radius : pointX;
  const cornerY = pointY < rect.y + radius ? rect.y + radius : pointY > bottom - radius ? bottom - radius : pointY;
  if (cornerX === pointX && cornerY === pointY) return true;
  return Math.hypot(pointX - cornerX, pointY - cornerY) <= radius;
}

function encodePNG(pixels, size) {
  const stride = size * 4;
  const raw = Buffer.alloc((stride + 1) * size);
  for (let y = 0; y < size; y += 1) {
    raw[y * (stride + 1)] = 0;
    pixels.copy(raw, y * (stride + 1) + 1, y * stride, (y + 1) * stride);
  }
  const header = Buffer.alloc(13);
  header.writeUInt32BE(size, 0);
  header.writeUInt32BE(size, 4);
  header[8] = 8; // bit depth
  header[9] = 6; // truecolour with alpha
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk("IHDR", header),
    chunk("IDAT", deflateSync(raw, { level: 9 })),
    chunk("IEND", Buffer.alloc(0)),
  ]);
}

function chunk(type, body) {
  const length = Buffer.alloc(4);
  length.writeUInt32BE(body.length, 0);
  const payload = Buffer.concat([Buffer.from(type, "ascii"), body]);
  const checksum = Buffer.alloc(4);
  checksum.writeUInt32BE(crc32(payload), 0);
  return Buffer.concat([length, payload, checksum]);
}

function crc32(buffer) {
  let crc = 0xffffffff;
  for (const byte of buffer) crc = crcTable[(crc ^ byte) & 0xff] ^ (crc >>> 8);
  return (crc ^ 0xffffffff) >>> 0;
}
