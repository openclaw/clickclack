import { a as base64_decode, t as text_encoder, b as base64_encode } from "./utils2.js";
import { M as MAX_ARRAY_INDEX, i as is_plain_object$1, e as enumerable_symbols, v as valid_array_indices, g as get_type, U as UNDEFINED, N as NAN, P as POSITIVE_INFINITY, a as NEGATIVE_INFINITY, b as NEGATIVE_ZERO, S as SPARSE, c as is_valid_array_len, d as is_valid_array_index, H as HOLE, D as DevalueError, s as stringify_key, f as stringify_string, u as uneval } from "./uneval.js";
import { json, text } from "@sveltejs/kit";
import { SvelteKitError, HttpError } from "@sveltejs/kit/internal";
import { with_request_store } from "@sveltejs/kit/internal/server";
function encode_native(array_buffer) {
  return new Uint8Array(array_buffer).toBase64();
}
function decode_native(base64) {
  return Uint8Array.fromBase64(base64).buffer;
}
function encode_buffer(array_buffer) {
  return Buffer.from(array_buffer).toString("base64");
}
function decode_buffer(base64) {
  return Uint8Array.from(Buffer.from(base64, "base64")).buffer;
}
function encode_legacy(array_buffer) {
  const array = new Uint8Array(array_buffer);
  let binary = "";
  const chunk_size = 32768;
  for (let i = 0; i < array.length; i += chunk_size) {
    const chunk = array.subarray(i, i + chunk_size);
    binary += String.fromCharCode.apply(null, chunk);
  }
  return btoa(binary);
}
function decode_legacy(base64) {
  const binary_string = atob(base64);
  const len = binary_string.length;
  const array = new Uint8Array(len);
  for (let i = 0; i < len; i++) {
    array[i] = binary_string.charCodeAt(i);
  }
  return array.buffer;
}
const native = typeof Uint8Array.fromBase64 === "function";
const buffer = typeof process === "object" && process.versions?.node !== void 0;
const encode64 = native ? encode_native : buffer ? encode_buffer : encode_legacy;
const decode64 = native ? decode_native : buffer ? decode_buffer : decode_legacy;
function merge_operations(defaults, overrides) {
  return defaults;
}
const NOT_PLAIN = Object.freeze({ kind: "not-plain" });
const SYMBOL_KEYS = Object.freeze({ kind: "symbol-keys" });
const stringify_operations = {
  identify: (value) => value,
  typeOf: (value) => value === null ? "null" : typeof value,
  toPrimitive: (value) => value,
  tagOf: (value) => get_type(value),
  isThenable: (value) => typeof value.then === "function",
  toPromise: (thenable) => Promise.resolve(thenable),
  unbox: (boxed) => boxed.valueOf(),
  toISOString: (date) => isNaN(date.getDate()) ? "" : date.toISOString(),
  toStringValue: (value) => value.toString(),
  regExpInfo: (regexp) => ({ source: regexp.source, flags: regexp.flags }),
  valuesOf: (set) => set,
  entriesOf: (map) => map,
  viewInfo: (view) => ({
    buffer: view.buffer,
    byteOffset: view.byteOffset,
    byteLength: view.byteLength,
    length: view.length,
    bufferByteLength: view.buffer.byteLength
  }),
  toArrayBuffer: (buffer2) => buffer2,
  lengthOf: (array) => array.length,
  hasOwn: (value, key) => Object.hasOwn(value, key),
  indicesOf: (array) => valid_array_indices(array),
  shapeOf: (value) => {
    if (!is_plain_object$1(value)) return NOT_PLAIN;
    if (enumerable_symbols(value).length > 0) return SYMBOL_KEYS;
    return {
      kind: Object.getPrototypeOf(value) === null ? "null-proto" : "plain",
      keys: Object.keys(value)
    };
  },
  get: (value, key) => value[key]
};
const default_stringify_operations = Object.freeze(stringify_operations);
const parse_operations = {
  fromPrimitive: (primitive) => primitive,
  fromISOString: (iso) => new Date(iso),
  fromStringValue: (tag, text2) => {
    if (tag === "URL") return new URL(text2);
    if (tag === "URLSearchParams") return new URLSearchParams(text2);
    return Temporal[tag.slice(9)].from(text2);
  },
  fromArrayBuffer: (buffer2) => buffer2,
  fromRegExpInfo: (source, flags) => new RegExp(source, flags),
  fromViewInfo: (tag, buffer2, byteOffset, length) => {
    const Constructor = (
      /** @type {any} */
      globalThis[tag]
    );
    return byteOffset !== void 0 ? new Constructor(buffer2, byteOffset, length) : new Constructor(buffer2);
  },
  box: (value) => Object(value),
  createArray: (length) => new Array(length),
  createSparseArray: (length) => {
    const array = [];
    array[MAX_ARRAY_INDEX] = void 0;
    delete array[MAX_ARRAY_INDEX];
    array.length = length;
    return array;
  },
  createObject: () => ({}),
  createNullPrototypeObject: () => /* @__PURE__ */ Object.create(null),
  createSet: () => /* @__PURE__ */ new Set(),
  createMap: () => /* @__PURE__ */ new Map(),
  set: (target, key, value) => {
    target[key] = value;
  },
  addValue: (set, value) => {
    set.add(value);
  },
  addEntry: (map, key, value) => {
    map.set(key, value);
  }
};
const default_parse_operations = Object.freeze(parse_operations);
function parse(serialized, revivers, options) {
  return unflatten(JSON.parse(serialized), revivers);
}
function unflatten(parsed, revivers, options) {
  const ops = merge_operations(default_parse_operations);
  if (typeof parsed === "number") return hydrate(parsed, true);
  if (!Array.isArray(parsed) || parsed.length === 0) {
    throw new Error("Invalid input");
  }
  const values = (
    /** @type {any[]} */
    parsed
  );
  const hydrated = Array(values.length);
  let hydrating = null;
  function hydrate(index, standalone = false) {
    if (index === UNDEFINED) return ops.fromPrimitive(void 0);
    if (index === NAN) return ops.fromPrimitive(NaN);
    if (index === POSITIVE_INFINITY) return ops.fromPrimitive(Infinity);
    if (index === NEGATIVE_INFINITY) return ops.fromPrimitive(-Infinity);
    if (index === NEGATIVE_ZERO) return ops.fromPrimitive(-0);
    if (standalone || typeof index !== "number") {
      throw new Error(`Invalid input`);
    }
    if (index in hydrated) return hydrated[index];
    const value = values[index];
    if (!value || typeof value !== "object") {
      hydrated[index] = ops.fromPrimitive(value);
    } else if (Array.isArray(value)) {
      if (typeof value[0] === "string") {
        const type = value[0];
        const reviver = revivers && Object.hasOwn(revivers, type) ? revivers[type] : void 0;
        if (reviver) {
          let i = value[1];
          if (typeof i !== "number") {
            i = values.push(value[1]) - 1;
          }
          if (Object.hasOwn(hydrated, i)) {
            return hydrated[index] = reviver(hydrated[i]);
          }
          hydrating ??= /* @__PURE__ */ new Set();
          if (hydrating.has(i)) {
            throw new Error("Invalid circular reference");
          }
          hydrating.add(i);
          hydrated[index] = reviver(hydrate(i));
          hydrating.delete(i);
          return hydrated[index];
        }
        switch (type) {
          case "Date":
            hydrated[index] = ops.fromISOString(value[1]);
            break;
          case "Set":
            const set = ops.createSet();
            hydrated[index] = set;
            for (let i = 1; i < value.length; i += 1) {
              ops.addValue(set, hydrate(value[i]));
            }
            break;
          case "Map":
            const map = ops.createMap();
            hydrated[index] = map;
            for (let i = 1; i < value.length; i += 2) {
              ops.addEntry(map, hydrate(value[i]), hydrate(value[i + 1]));
            }
            break;
          case "RegExp":
            hydrated[index] = ops.fromRegExpInfo(value[1], value[2]);
            break;
          case "Object": {
            const wrapped_index = value[1];
            if (typeof values[wrapped_index] === "object" && values[wrapped_index][0] !== "BigInt") {
              throw new Error("Invalid input");
            }
            hydrated[index] = ops.box(hydrate(wrapped_index));
            break;
          }
          case "BigInt":
            hydrated[index] = ops.fromPrimitive(BigInt(value[1]));
            break;
          case "null":
            const obj = ops.createNullPrototypeObject();
            hydrated[index] = obj;
            for (let i = 1; i < value.length; i += 2) {
              if (value[i] === "__proto__") {
                throw new Error("Cannot parse an object with a `__proto__` property");
              }
              ops.set(obj, value[i], hydrate(value[i + 1]));
            }
            break;
          case "Int8Array":
          case "Uint8Array":
          case "Uint8ClampedArray":
          case "Int16Array":
          case "Uint16Array":
          case "Float16Array":
          case "Int32Array":
          case "Uint32Array":
          case "Float32Array":
          case "Float64Array":
          case "BigInt64Array":
          case "BigUint64Array":
          case "DataView": {
            if (values[value[1]][0] !== "ArrayBuffer") {
              throw new Error("Invalid data");
            }
            const buffer2 = hydrate(value[1]);
            hydrated[index] = ops.fromViewInfo(type, buffer2, value[2], value[3]);
            break;
          }
          case "ArrayBuffer": {
            const base64 = value[1];
            if (typeof base64 !== "string") {
              throw new Error("Invalid ArrayBuffer encoding");
            }
            hydrated[index] = ops.fromArrayBuffer(decode64(base64));
            break;
          }
          case "URL":
          case "URLSearchParams":
          case "Temporal.Duration":
          case "Temporal.Instant":
          case "Temporal.PlainDate":
          case "Temporal.PlainTime":
          case "Temporal.PlainDateTime":
          case "Temporal.PlainMonthDay":
          case "Temporal.PlainYearMonth":
          case "Temporal.ZonedDateTime": {
            hydrated[index] = ops.fromStringValue(type, value[1]);
            break;
          }
          default:
            throw new Error(`Unknown type ${type}`);
        }
      } else if (value[0] === SPARSE) {
        const len = value[1];
        if (!is_valid_array_len(len)) {
          throw new Error("Invalid input");
        }
        const array = ops.createSparseArray(len);
        hydrated[index] = array;
        for (let i = 2; i < value.length; i += 2) {
          const idx = value[i];
          if (!is_valid_array_index(idx) || idx >= len) {
            throw new Error("Invalid input");
          }
          ops.set(array, idx, hydrate(value[i + 1]));
        }
      } else {
        const array = ops.createArray(value.length);
        hydrated[index] = array;
        for (let i = 0; i < value.length; i += 1) {
          const n = value[i];
          if (n === HOLE) continue;
          ops.set(array, i, hydrate(n));
        }
      }
    } else {
      const object = ops.createObject();
      hydrated[index] = object;
      for (const key of Object.keys(value)) {
        if (key === "__proto__") {
          throw new Error("Cannot parse an object with a `__proto__` property");
        }
        ops.set(object, key, hydrate(value[key]));
      }
    }
    return hydrated[index];
  }
  return hydrate(0);
}
function stringify$1(value, reducers, options) {
  const stringified = run(false, value, reducers);
  return typeof stringified === "string" ? stringified : `[${stringified.join(",")}]`;
}
function run(async, value, reducers, options) {
  const ops = merge_operations(default_stringify_operations);
  const stringified = [];
  const indexes = /* @__PURE__ */ new Map();
  const custom = [];
  if (reducers) {
    for (const key of Object.getOwnPropertyNames(reducers)) {
      custom.push({ key, fn: reducers[key] });
    }
  }
  const keys = [];
  let p = 0;
  function flatten(thing, index2) {
    const type = ops.typeOf(thing);
    if (type === "undefined") return UNDEFINED;
    let number;
    if (type === "number") {
      number = /** @type {number} */
      ops.toPrimitive(thing);
      if (Number.isNaN(number)) return NAN;
      if (number === Infinity) return POSITIVE_INFINITY;
      if (number === -Infinity) return NEGATIVE_INFINITY;
      if (number === 0 && 1 / number < 0) return NEGATIVE_ZERO;
    }
    const id = ops.identify(thing);
    if (indexes.has(id)) return (
      /** @type {number} */
      indexes.get(id)
    );
    index2 ??= p++;
    indexes.set(id, index2);
    for (const { key, fn } of custom) {
      const value2 = fn(thing);
      if (value2) {
        stringified[index2] = `["${key}",${flatten(value2)}]`;
        return index2;
      }
    }
    if (type === "function") {
      throw new DevalueError(`Cannot stringify a function`, keys, thing, value);
    } else if (type === "symbol") {
      throw new DevalueError(`Cannot stringify a Symbol primitive`, keys, thing, value);
    }
    let str = "";
    if (type !== "object") {
      str = stringify_primitive(type === "number" ? number : ops.toPrimitive(thing));
    } else if (ops.isThenable(thing)) {
      {
        throw new DevalueError(
          `Cannot stringify a Promise or thenable — use stringifyAsync instead`,
          keys,
          thing,
          value
        );
      }
    } else {
      const tag = ops.tagOf(thing);
      switch (tag) {
        case "Number":
        case "String":
        case "Boolean":
        case "BigInt":
          str = `["Object",${flatten(ops.unbox(thing))}]`;
          break;
        case "Date":
          str = `["Date","${ops.toISOString(thing)}"]`;
          break;
        case "URL":
          str = `["URL",${stringify_string(ops.toStringValue(thing))}]`;
          break;
        case "URLSearchParams":
          str = `["URLSearchParams",${stringify_string(ops.toStringValue(thing))}]`;
          break;
        case "RegExp":
          const { source, flags } = ops.regExpInfo(thing);
          str = flags ? `["RegExp",${stringify_string(source)},"${flags}"]` : `["RegExp",${stringify_string(source)}]`;
          break;
        case "Array": {
          let mostly_dense = false;
          const length = ops.lengthOf(thing);
          str = "[";
          for (let i = 0; i < length; i += 1) {
            if (i > 0) str += ",";
            if (ops.hasOwn(thing, i)) {
              keys.push(`[${i}]`);
              str += flatten(ops.get(thing, i));
              keys.pop();
            } else if (mostly_dense) {
              str += HOLE;
            } else {
              const populated_keys = ops.indicesOf(thing);
              const population = populated_keys.length;
              const d = String(length).length;
              const hole_cost = (length - population) * 3;
              const sparse_cost = 4 + d + population * (d + 1);
              if (hole_cost > sparse_cost) {
                str = "[" + SPARSE + "," + length;
                for (let j = 0; j < populated_keys.length; j++) {
                  const key = populated_keys[j];
                  keys.push(`[${key}]`);
                  str += "," + key + "," + flatten(ops.get(thing, key));
                  keys.pop();
                }
                break;
              } else {
                mostly_dense = true;
                str += HOLE;
              }
            }
          }
          str += "]";
          break;
        }
        case "Set":
          str = '["Set"';
          for (const value2 of ops.valuesOf(thing)) {
            str += `,${flatten(value2)}`;
          }
          str += "]";
          break;
        case "Map":
          str = '["Map"';
          for (const [key, value2] of ops.entriesOf(thing)) {
            const key_type = ops.typeOf(key);
            const key_is_primitive = key_type !== "object" && key_type !== "function" && key_type !== "symbol";
            keys.push(
              `.get(${key_is_primitive ? stringify_primitive(ops.toPrimitive(key)) : "..."})`
            );
            str += `,${flatten(key)},${flatten(value2)}`;
            keys.pop();
          }
          str += "]";
          break;
        case "Int8Array":
        case "Uint8Array":
        case "Uint8ClampedArray":
        case "Int16Array":
        case "Uint16Array":
        case "Float16Array":
        case "Int32Array":
        case "Uint32Array":
        case "Float32Array":
        case "Float64Array":
        case "BigInt64Array":
        case "BigUint64Array": {
          const info = ops.viewInfo(thing);
          str = '["' + tag + '",' + flatten(info.buffer);
          if (info.byteLength !== info.bufferByteLength) {
            str += `,${info.byteOffset},${info.length}`;
          }
          str += "]";
          break;
        }
        case "DataView": {
          const info = ops.viewInfo(thing);
          str = '["' + tag + '",' + flatten(info.buffer);
          if (info.byteLength !== info.bufferByteLength) {
            str += `,${info.byteOffset},${info.byteLength}`;
          }
          str += "]";
          break;
        }
        case "ArrayBuffer": {
          const base64 = encode64(ops.toArrayBuffer(thing));
          str = `["ArrayBuffer","${base64}"]`;
          break;
        }
        case "Temporal.Duration":
        case "Temporal.Instant":
        case "Temporal.PlainDate":
        case "Temporal.PlainTime":
        case "Temporal.PlainDateTime":
        case "Temporal.PlainMonthDay":
        case "Temporal.PlainYearMonth":
        case "Temporal.ZonedDateTime":
          str = `["${tag}",${stringify_string(ops.toStringValue(thing))}]`;
          break;
        default: {
          const shape = ops.shapeOf(thing);
          if (shape.kind === "not-plain") {
            throw new DevalueError(`Cannot stringify arbitrary non-POJOs`, keys, thing, value);
          }
          if (shape.kind === "symbol-keys") {
            throw new DevalueError(`Cannot stringify POJOs with symbolic keys`, keys, thing, value);
          }
          if (shape.kind === "null-proto") {
            str = '["null"';
            for (const key of shape.keys) {
              if (key === "__proto__") {
                throw new DevalueError(
                  `Cannot stringify objects with __proto__ keys`,
                  keys,
                  thing,
                  value
                );
              }
              keys.push(stringify_key(key));
              str += `,${stringify_string(key)},${flatten(ops.get(thing, key))}`;
              keys.pop();
            }
            str += "]";
          } else {
            str = "{";
            let started = false;
            for (const key of shape.keys) {
              if (key === "__proto__") {
                throw new DevalueError(
                  `Cannot stringify objects with __proto__ keys`,
                  keys,
                  thing,
                  value
                );
              }
              if (started) str += ",";
              started = true;
              keys.push(stringify_key(key));
              str += `${stringify_string(key)}:${flatten(ops.get(thing, key))}`;
              keys.pop();
            }
            str += "}";
          }
        }
      }
    }
    stringified[index2] = str;
    return index2;
  }
  const index = flatten(value);
  if (index < 0) return `${index}`;
  return stringified;
}
function stringify_primitive(thing) {
  const type = typeof thing;
  if (type === "string") return stringify_string(thing);
  if (thing === void 0) return UNDEFINED.toString();
  if (thing === 0 && 1 / thing < 0) return NEGATIVE_ZERO.toString();
  if (type === "bigint") return `["BigInt","${thing}"]`;
  return String(thing);
}
const SVELTE_KIT_ASSETS = "/_svelte_kit_assets";
const ENDPOINT_METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD"];
const MUTATIVE_METHODS = ["POST", "PUT", "PATCH", "DELETE"];
const PAGE_METHODS = ["GET", "POST", "HEAD"];
function noop() {
}
function once(fn) {
  let done = false;
  let result;
  return () => {
    if (done) return result;
    done = true;
    return result = fn();
  };
}
const decoder = new TextDecoder();
function set_nested_value(object, path_string, value) {
  if (path_string.startsWith("n:")) {
    path_string = path_string.slice(2);
    value = value === "" ? void 0 : parseFloat(value);
  } else if (path_string.startsWith("b:")) {
    path_string = path_string.slice(2);
    value = value === "on";
  }
  deep_set(object, split_path(path_string), value);
}
const DELETE_KEY = {};
function convert_formdata(data) {
  const result = {};
  for (let key of data.keys()) {
    const is_array = key.endsWith("[]");
    let values = data.getAll(key);
    if (is_array) key = key.slice(0, -2);
    values = values.filter(
      (entry) => typeof entry === "string" || entry.name !== "" || entry.size > 0
    );
    if (values.length === 0 && !is_array) continue;
    if (key.startsWith("n:")) {
      key = key.slice(2);
      values = values.map((v) => v === "" ? void 0 : parseFloat(
        /** @type {string} */
        v
      ));
    } else if (key.startsWith("b:")) {
      key = key.slice(2);
      values = values.map((v) => v === "on");
    }
    if (values.length > 1 && !is_array) {
      throw new Error(`Form cannot contain duplicated keys — "${key}" has ${values.length} values`);
    }
    set_nested_value(result, key, is_array ? values : values[0]);
  }
  return result;
}
const BINARY_FORM_CONTENT_TYPE = "application/x-sveltekit-formdata";
const BINARY_FORM_VERSION = 0;
const HEADER_BYTES = 1 + 4 + 2;
async function deserialize_binary_form(request) {
  if (request.headers.get("content-type") !== BINARY_FORM_CONTENT_TYPE) {
    const form_data = await request.formData();
    return { data: convert_formdata(form_data), meta: {}, form_data };
  }
  if (!request.body) {
    throw deserialize_error("no body");
  }
  const reader = request.body.getReader();
  const chunks = [];
  function get_chunk(index) {
    if (index in chunks) return chunks[index];
    let i = chunks.length;
    while (i <= index) {
      chunks[i] = reader.read().then((chunk) => chunk.value);
      i++;
    }
    return chunks[index];
  }
  async function get_buffer(offset, length) {
    let start_chunk;
    let chunk_start = 0;
    let chunk_index;
    for (chunk_index = 0; ; chunk_index++) {
      const chunk = await get_chunk(chunk_index);
      if (!chunk) return null;
      const chunk_end = chunk_start + chunk.byteLength;
      if (offset >= chunk_start && offset < chunk_end) {
        start_chunk = chunk;
        break;
      }
      chunk_start = chunk_end;
    }
    if (offset + length <= chunk_start + start_chunk.byteLength) {
      return start_chunk.subarray(offset - chunk_start, offset + length - chunk_start);
    }
    const chunks2 = [start_chunk.subarray(offset - chunk_start)];
    let cursor = start_chunk.byteLength - offset + chunk_start;
    while (cursor < length) {
      chunk_index++;
      let chunk = await get_chunk(chunk_index);
      if (!chunk) return null;
      if (chunk.byteLength > length - cursor) {
        chunk = chunk.subarray(0, length - cursor);
      }
      chunks2.push(chunk);
      cursor += chunk.byteLength;
    }
    const buffer2 = new Uint8Array(length);
    cursor = 0;
    for (const chunk of chunks2) {
      buffer2.set(chunk, cursor);
      cursor += chunk.byteLength;
    }
    return buffer2;
  }
  const header = await get_buffer(0, HEADER_BYTES);
  if (!header) throw deserialize_error("too short");
  if (header[0] !== BINARY_FORM_VERSION) {
    throw deserialize_error(`got version ${header[0]}, expected version ${BINARY_FORM_VERSION}`);
  }
  const header_view = new DataView(header.buffer, header.byteOffset, header.byteLength);
  const data_length = header_view.getUint32(1, true);
  const file_offsets_length = header_view.getUint16(5, true);
  const data_buffer = await get_buffer(HEADER_BYTES, data_length);
  if (!data_buffer) throw deserialize_error("data too short");
  let file_offsets;
  let files_start_offset;
  if (file_offsets_length > 0) {
    const file_offsets_buffer = await get_buffer(HEADER_BYTES + data_length, file_offsets_length);
    if (!file_offsets_buffer) throw deserialize_error("file offset table too short");
    const parsed_offsets = JSON.parse(decoder.decode(file_offsets_buffer));
    if (!Array.isArray(parsed_offsets) || parsed_offsets.some((n) => typeof n !== "number" || !Number.isInteger(n) || n < 0)) {
      throw deserialize_error("invalid file offset table");
    }
    file_offsets = /** @type {Array<number>} */
    parsed_offsets;
    files_start_offset = HEADER_BYTES + data_length + file_offsets_length;
  }
  const file_spans = [];
  const [data, meta] = parse(decoder.decode(data_buffer), {
    File: ([name, type, size, last_modified, index]) => {
      if (typeof name !== "string" || typeof type !== "string" || typeof size !== "number" || typeof last_modified !== "number" || typeof index !== "number") {
        throw deserialize_error("invalid file metadata");
      }
      let offset = file_offsets[index];
      if (offset === void 0) {
        throw deserialize_error("duplicate file offset table index");
      }
      file_offsets[index] = void 0;
      offset += files_start_offset;
      file_spans.push({ offset, size });
      return new Proxy(new LazyFile(name, type, size, last_modified, get_chunk, offset), {
        getPrototypeOf() {
          return File.prototype;
        }
      });
    }
  });
  file_spans.sort((a, b) => a.offset - b.offset || a.size - b.size);
  for (let i = 1; i < file_spans.length; i++) {
    const previous = file_spans[i - 1];
    const current = file_spans[i];
    const previous_end = previous.offset + previous.size;
    if (previous_end < current.offset) {
      throw deserialize_error("gaps in file data");
    }
    if (previous_end > current.offset) {
      throw deserialize_error("overlapping file data");
    }
  }
  void (async () => {
    let has_more = true;
    while (has_more) {
      const chunk = await get_chunk(chunks.length);
      has_more = !!chunk;
    }
  })().catch(noop);
  return { data, meta, form_data: null };
}
function deserialize_error(message) {
  return new SvelteKitError(400, "Bad Request", `Could not deserialize binary form: ${message}`);
}
class LazyFile {
  /** @type {(index: number) => Promise<Uint8Array<ArrayBuffer> | undefined>} */
  #get_chunk;
  /** @type {number} */
  #offset;
  /**
   * @param {string} name
   * @param {string} type
   * @param {number} size
   * @param {number} last_modified
   * @param {(index: number) => Promise<Uint8Array<ArrayBuffer> | undefined>} get_chunk
   * @param {number} offset
   */
  constructor(name, type, size, last_modified, get_chunk, offset) {
    this.name = name;
    this.type = type;
    this.size = size;
    this.lastModified = last_modified;
    this.webkitRelativePath = "";
    this.#get_chunk = get_chunk;
    this.#offset = offset;
    this.arrayBuffer = this.arrayBuffer.bind(this);
    this.bytes = this.bytes.bind(this);
    this.slice = this.slice.bind(this);
    this.stream = this.stream.bind(this);
    this.text = this.text.bind(this);
  }
  /** @type {ArrayBuffer | undefined} */
  #buffer;
  async arrayBuffer() {
    this.#buffer ??= await new Response(this.stream()).arrayBuffer();
    return this.#buffer;
  }
  async bytes() {
    return new Uint8Array(await this.arrayBuffer());
  }
  /**
   * @param {number=} start
   * @param {number=} end
   * @param {string=} contentType
   */
  slice(start = 0, end = this.size, contentType = this.type) {
    if (start < 0) {
      start = Math.max(this.size + start, 0);
    } else {
      start = Math.min(start, this.size);
    }
    if (end < 0) {
      end = Math.max(this.size + end, 0);
    } else {
      end = Math.min(end, this.size);
    }
    const size = Math.max(end - start, 0);
    const file = new LazyFile(
      this.name,
      contentType,
      size,
      this.lastModified,
      this.#get_chunk,
      this.#offset + start
    );
    return file;
  }
  stream() {
    let cursor = 0;
    let chunk_index = 0;
    return new ReadableStream({
      start: async (controller) => {
        let chunk_start = 0;
        let start_chunk;
        for (chunk_index = 0; ; chunk_index++) {
          const chunk = await this.#get_chunk(chunk_index);
          if (!chunk) return null;
          const chunk_end = chunk_start + chunk.byteLength;
          if (this.#offset >= chunk_start && this.#offset < chunk_end) {
            start_chunk = chunk;
            break;
          }
          chunk_start = chunk_end;
        }
        if (this.#offset + this.size <= chunk_start + start_chunk.byteLength) {
          controller.enqueue(
            start_chunk.subarray(this.#offset - chunk_start, this.#offset + this.size - chunk_start)
          );
          controller.close();
        } else {
          controller.enqueue(start_chunk.subarray(this.#offset - chunk_start));
          cursor = start_chunk.byteLength - this.#offset + chunk_start;
        }
      },
      pull: async (controller) => {
        chunk_index++;
        let chunk = await this.#get_chunk(chunk_index);
        if (!chunk) {
          controller.error("incomplete file data");
          controller.close();
          return;
        }
        if (chunk.byteLength > this.size - cursor) {
          chunk = chunk.subarray(0, this.size - cursor);
        }
        controller.enqueue(chunk);
        cursor += chunk.byteLength;
        if (cursor >= this.size) {
          controller.close();
        }
      }
    });
  }
  async text() {
    return decoder.decode(await this.arrayBuffer());
  }
}
const path_regex = /^[a-zA-Z_$]\w*(\.[a-zA-Z_$]\w*|\[\d+\])*$/;
function split_path(path) {
  if (!path_regex.test(path)) {
    throw new Error(`Invalid path ${path}`);
  }
  return path.split(/\.|\[|\]/).filter(Boolean);
}
function check_prototype_pollution(key) {
  if (key === "__proto__" || key === "constructor" || key === "prototype") {
    throw new Error(
      `Invalid key "${key}"`
    );
  }
}
function deep_set(object, keys, value) {
  let current = object;
  for (let i = 0; i < keys.length - 1; i += 1) {
    const key = keys[i];
    check_prototype_pollution(key);
    const is_array = /^\d+$/.test(keys[i + 1]);
    const inner = Object.hasOwn(current, key) ? current[key] : void 0;
    const exists = inner != null;
    if (exists && is_array !== Array.isArray(inner)) {
      throw new Error(`Invalid array key ${keys[i + 1]}`);
    }
    if (!exists) {
      if (value === DELETE_KEY) {
        return;
      }
      current[key] = is_array ? [] : {};
    }
    current = current[key];
  }
  const final_key = keys[keys.length - 1];
  check_prototype_pollution(final_key);
  if (value === DELETE_KEY) {
    delete current[final_key];
  } else {
    current[final_key] = value;
  }
}
function normalize_issue(issue, server = false) {
  const normalized = { name: "", path: [], message: issue.message, server };
  if (issue.path !== void 0) {
    let name = "";
    for (const segment of issue.path) {
      const key = (
        /** @type {string | number} */
        typeof segment === "object" ? segment.key : segment
      );
      normalized.path.push(key);
      if (typeof key === "number") {
        name += `[${key}]`;
      } else if (typeof key === "string") {
        name += name === "" ? key : "." + key;
      }
    }
    normalized.name = name;
  }
  return normalized;
}
function flatten_issues(issues) {
  const result = {};
  for (const issue of issues) {
    (result.$ ??= []).push(issue);
    let name = "";
    if (issue.path !== void 0) {
      for (const key of issue.path) {
        if (typeof key === "number") {
          name += `[${key}]`;
        } else if (typeof key === "string") {
          name += name === "" ? key : "." + key;
        }
        (result[name] ??= []).push(issue);
      }
    }
  }
  return result;
}
function deep_get(object, path) {
  let current = object;
  for (const key of path) {
    if (current == null || typeof current !== "object") {
      return current;
    }
    current = current[key];
  }
  return current;
}
function get_type_prefix(field_type, is_array, input_value) {
  if (field_type === "number" || field_type === "range") return "n:";
  if (field_type === "checkbox" && !is_array) return "b:";
  if (field_type === "hidden" || field_type === "submit") {
    const input_type = typeof input_value;
    if (input_type === "number") return "n:";
    if (input_type === "boolean") return "b:";
  }
  return "";
}
function deep_clone(value) {
  if (value !== null && typeof value === "object") {
    if (value instanceof File) {
      return value;
    }
    if (Array.isArray(value)) {
      return value.map(deep_clone);
    }
    const clone = {};
    for (const key of Object.keys(value)) {
      clone[key] = deep_clone(value[key]);
    }
    return clone;
  }
  return value;
}
function create_field_proxy(target, get_input, set_input, get_issues, path = []) {
  const get_value = () => {
    const value = deep_get(get_input(), path);
    return deep_clone(value);
  };
  return new Proxy(target, {
    get(target2, prop) {
      if (typeof prop === "symbol") return target2[prop];
      if (/^\d+$/.test(prop)) {
        return create_field_proxy({}, get_input, set_input, get_issues, [
          ...path,
          parseInt(prop, 10)
        ]);
      }
      const key = build_path_string(path);
      if (prop === "set") {
        const set_func = function(newValue) {
          set_input(path, newValue);
          return newValue;
        };
        return create_field_proxy(set_func, get_input, set_input, get_issues, [...path, prop]);
      }
      if (prop === "value") {
        return create_field_proxy(get_value, get_input, set_input, get_issues, [...path, prop]);
      }
      if (prop === "issues" || prop === "allIssues") {
        const issues_func = () => {
          const all_issues = get_issues(path, prop === "allIssues")[key === "" ? "$" : key];
          if (prop === "allIssues") {
            return all_issues?.map((issue) => ({
              path: issue.path,
              message: issue.message
            }));
          }
          const issues = all_issues?.filter((issue) => issue.name === key)?.map((issue) => ({
            path: issue.path,
            message: issue.message
          }));
          return issues?.length ? issues : void 0;
        };
        return create_field_proxy(issues_func, get_input, set_input, get_issues, [...path, prop]);
      }
      if (prop === "as") {
        const as_func = (type, input_value) => {
          const is_array = type === "file multiple" || type === "select multiple" || type === "checkbox" && typeof input_value === "string";
          const prefix = get_type_prefix(type, is_array, input_value);
          const base_props = {
            name: prefix + key + (is_array ? "[]" : ""),
            get "aria-invalid"() {
              const issues = get_issues();
              return key in issues ? "true" : void 0;
            }
          };
          if (type !== "text" && type !== "select" && type !== "select multiple") {
            base_props.type = type === "file multiple" ? "file" : type;
          }
          if (type === "submit" || type === "hidden") {
            const value = typeof input_value === "boolean" ? input_value ? "on" : "off" : input_value;
            return Object.defineProperties(base_props, {
              value: { value, enumerable: true }
            });
          }
          if (type === "select" || type === "select multiple") {
            return Object.defineProperties(base_props, {
              multiple: { value: is_array, enumerable: true },
              value: {
                enumerable: true,
                get() {
                  return get_value() ?? input_value;
                }
              }
            });
          }
          if (type === "checkbox" || type === "radio") {
            if (type === "checkbox" && !is_array) {
              return Object.defineProperties(base_props, {
                defaultChecked: {
                  enumerable: true,
                  get() {
                    return input_value;
                  }
                },
                checked: {
                  enumerable: true,
                  get() {
                    return get_value() ?? input_value;
                  }
                }
              });
            }
            return Object.defineProperties(base_props, {
              value: { value: input_value ?? "on", enumerable: true },
              checked: {
                enumerable: true,
                get() {
                  const value = get_value();
                  if (type === "radio") {
                    return value === input_value;
                  }
                  return (value ?? []).includes(input_value);
                }
              }
            });
          }
          if (type === "file" || type === "file multiple") {
            return Object.defineProperties(base_props, {
              multiple: { value: is_array, enumerable: true },
              files: {
                enumerable: true,
                get() {
                  const value = get_value();
                  if (value instanceof File) {
                    if (typeof DataTransfer !== "undefined") {
                      const fileList = new DataTransfer();
                      fileList.items.add(value);
                      return fileList.files;
                    }
                    return { 0: value, length: 1 };
                  }
                  if (Array.isArray(value) && value.every((f) => f instanceof File)) {
                    if (typeof DataTransfer !== "undefined") {
                      const fileList = new DataTransfer();
                      value.forEach((file) => fileList.items.add(file));
                      return fileList.files;
                    }
                    const fileListLike = { length: value.length };
                    value.forEach((file, index) => {
                      fileListLike[index] = file;
                    });
                    return fileListLike;
                  }
                  return null;
                }
              }
            });
          }
          return Object.defineProperties(base_props, {
            defaultValue: {
              enumerable: true,
              get() {
                return input_value;
              }
            },
            value: {
              enumerable: true,
              get() {
                const value = get_value() ?? input_value;
                return value != null ? String(value) : "";
              }
            }
          });
        };
        return create_field_proxy(as_func, get_input, set_input, get_issues, [...path, "as"]);
      }
      return create_field_proxy({}, get_input, set_input, get_issues, [...path, prop]);
    }
  });
}
function build_path_string(path) {
  let result = "";
  for (const segment of path) {
    if (typeof segment === "number") {
      result += `[${segment}]`;
    } else {
      result += result === "" ? segment : "." + segment;
    }
  }
  return result;
}
const INVALIDATED_PARAM = "x-sveltekit-invalidated";
const TRAILING_SLASH_PARAM = "x-sveltekit-trailing-slash";
function stringify(data, transport) {
  const encoders = Object.fromEntries(Object.entries(transport).map(([k, v]) => [k, v.encode]));
  return stringify$1(data, encoders);
}
const object_proto_names = /* @__PURE__ */ Object.getOwnPropertyNames(Object.prototype).sort().join("\0");
function is_plain_object(thing) {
  if (typeof thing !== "object" || thing === null) return false;
  const proto = Object.getPrototypeOf(thing);
  return proto === Object.prototype || proto === null || Object.getPrototypeOf(proto) === null || Object.getOwnPropertyNames(proto).sort().join("\0") === object_proto_names;
}
function to_sorted(value, clones) {
  const clone = Object.getPrototypeOf(value) === null ? /* @__PURE__ */ Object.create(null) : {};
  clones.set(value, clone);
  Object.defineProperty(clone, remote_arg_marker, { value: true });
  for (const key of Object.keys(value).sort()) {
    const property = value[key];
    Object.defineProperty(clone, key, {
      value: clones.get(property) ?? property,
      enumerable: true,
      configurable: true,
      writable: true
    });
  }
  return clone;
}
const remote_object = "__skrao";
const remote_map = "__skram";
const remote_set = "__skras";
const remote_file = "__skraf";
const remote_regex_guard = "__skrag";
const remote_arg_marker = Symbol(remote_object);
function create_remote_arg_reducers(transport, sort, remote_arg_clones) {
  const remote_fns_reducers = {
    /** @param {unknown} value */
    [remote_regex_guard]: (value) => {
      if (value instanceof RegExp) {
        throw new Error("Regular expressions are not valid remote function arguments");
      }
    }
  };
  {
    remote_fns_reducers[remote_map] = (value) => {
      if (!(value instanceof Map)) {
        return;
      }
      const entries = [];
      for (const [key, val] of value) {
        entries.push([stringify2(key), stringify2(val)]);
      }
      return entries.sort(([a1, a2], [b1, b2]) => {
        if (a1 < b1) return -1;
        if (a1 > b1) return 1;
        if (a2 < b2) return -1;
        if (a2 > b2) return 1;
        return 0;
      });
    };
    remote_fns_reducers[remote_set] = (value) => {
      if (!(value instanceof Set)) {
        return;
      }
      const items = [];
      for (const item of value) {
        items.push(stringify2(item));
      }
      items.sort();
      return items;
    };
    remote_fns_reducers[remote_object] = (value) => {
      if (!is_plain_object(value)) {
        return;
      }
      if (Object.hasOwn(value, remote_arg_marker)) {
        return;
      }
      if (remote_arg_clones.has(value)) {
        return remote_arg_clones.get(value);
      }
      return to_sorted(value, remote_arg_clones);
    };
  }
  const user_reducers = Object.fromEntries(
    Object.entries(transport).map(([k, v]) => [k, v.encode])
  );
  const all_reducers = { ...user_reducers, ...remote_fns_reducers };
  const stringify2 = (value) => stringify$1(value, all_reducers);
  return all_reducers;
}
function create_remote_arg_revivers(transport) {
  const remote_fns_revivers = {
    /** @type {(value: unknown) => unknown} */
    [remote_object]: (value) => value,
    /** @type {(value: unknown) => Map<unknown, unknown>} */
    [remote_map]: (value) => {
      if (!Array.isArray(value)) {
        throw new Error("Invalid data for Map reviver");
      }
      const map = /* @__PURE__ */ new Map();
      for (const item of value) {
        if (!Array.isArray(item) || item.length !== 2 || typeof item[0] !== "string" || typeof item[1] !== "string") {
          throw new Error("Invalid data for Map reviver");
        }
        const [key, val] = item;
        map.set(parse$1(key), parse$1(val));
      }
      return map;
    },
    /** @type {(value: unknown) => Set<unknown>} */
    [remote_set]: (value) => {
      if (!Array.isArray(value)) {
        throw new Error("Invalid data for Set reviver");
      }
      const set = /* @__PURE__ */ new Set();
      for (const item of value) {
        if (typeof item !== "string") {
          throw new Error("Invalid data for Set reviver");
        }
        set.add(parse$1(item));
      }
      return set;
    },
    /** @type {(value: any) => File} */
    [remote_file]: (value) => {
      if (!value || typeof value !== "object" || typeof value.name !== "string" || typeof value.type !== "string" || typeof value.size !== "number" || typeof value.lastModified !== "number" || !(value.data instanceof ArrayBuffer)) {
        throw new Error("Invalid data for File reviver");
      }
      const { data, name, ...meta } = value;
      return new File([data], name, meta);
    }
  };
  const user_revivers = Object.fromEntries(
    Object.entries(transport).map(([k, v]) => [k, v.decode])
  );
  const all_revivers = { ...user_revivers, ...remote_fns_revivers };
  const parse$1 = (data) => parse(data, all_revivers);
  return all_revivers;
}
function stringify_remote_arg(value, transport) {
  if (value === void 0) return "";
  const json2 = stringify$1(value, create_remote_arg_reducers(transport, true, /* @__PURE__ */ new Map()));
  return url_friendly_base64_encode(json2);
}
function url_friendly_base64_encode(string) {
  const bytes = text_encoder.encode(string);
  return base64_encode(bytes).replaceAll("=", "").replaceAll("+", "-").replaceAll("/", "_");
}
function parse_remote_arg(string, transport) {
  if (!string) return void 0;
  const json_string = new TextDecoder().decode(
    // no need to add back `=` characters, atob can handle it
    base64_decode(string.replaceAll("-", "+").replaceAll("_", "/"))
  );
  return parse(json_string, create_remote_arg_revivers(transport));
}
function create_remote_key(id, payload) {
  return id + "/" + payload;
}
function split_remote_key(key) {
  const i = key.lastIndexOf("/");
  if (i === -1) {
    throw new Error(`Invalid remote key: ${key}`);
  }
  return {
    id: key.slice(0, i),
    payload: key.slice(i + 1)
  };
}
function coalesce_to_error(err) {
  return err instanceof Error || err && /** @type {any} */
  err.name && /** @type {any} */
  err.message ? (
    /** @type {Error} */
    err
  ) : new Error(JSON.stringify(err));
}
function normalize_error(error) {
  return (
    /** @type {import('../exports/internal/index.js').Redirect | HttpError | SvelteKitError | Error} */
    error
  );
}
function get_status(error) {
  return error instanceof HttpError || error instanceof SvelteKitError ? error.status : 500;
}
function get_message(error) {
  return error instanceof SvelteKitError ? error.text : "Internal Error";
}
var defaultParseOptions = {
  decodeValues: true,
  map: false,
  silent: false,
  split: "auto"
  // auto = split strings but not arrays
};
function isForbiddenKey(key) {
  return typeof key !== "string" || key in {};
}
function createNullObj() {
  return /* @__PURE__ */ Object.create(null);
}
function isNonEmptyString(str) {
  return typeof str === "string" && !!str.trim();
}
function parseString(setCookieValue, options) {
  var parts = setCookieValue.split(";").filter(isNonEmptyString);
  var nameValuePairStr = parts.shift();
  if (!nameValuePairStr) {
    return null;
  }
  var parsed = parseNameValuePair(nameValuePairStr);
  var name = parsed.name;
  var value = parsed.value;
  options = options ? Object.assign({}, defaultParseOptions, options) : defaultParseOptions;
  if (isForbiddenKey(name)) {
    return null;
  }
  try {
    value = options.decodeValues ? decodeURIComponent(value) : value;
  } catch (e) {
    console.error(
      "set-cookie-parser: failed to decode cookie value. Set options.decodeValues=false to disable decoding.",
      e
    );
  }
  var cookie = createNullObj();
  cookie.name = name;
  cookie.value = value;
  parts.forEach(function(part) {
    var sides = part.split("=");
    var key = sides.shift().trim().toLowerCase();
    if (isForbiddenKey(key)) {
      return;
    }
    var value2 = sides.join("=").trim();
    if (key === "expires") {
      cookie.expires = new Date(value2);
    } else if (key === "max-age") {
      var n = parseInt(value2, 10);
      if (!Number.isNaN(n)) cookie.maxAge = n;
    } else if (key === "secure") {
      cookie.secure = true;
    } else if (key === "httponly") {
      cookie.httpOnly = true;
    } else if (key === "samesite") {
      cookie.sameSite = value2;
    } else if (key === "partitioned") {
      cookie.partitioned = true;
    } else if (key) {
      cookie[key] = value2;
    }
  });
  return cookie;
}
function parseNameValuePair(nameValuePairStr) {
  var name = "";
  var value = "";
  var nameValueArr = nameValuePairStr.split("=");
  if (nameValueArr.length > 1) {
    name = nameValueArr.shift();
    value = nameValueArr.join("=");
  } else {
    value = nameValuePairStr;
  }
  return { name, value };
}
function parseSetCookie(input, options) {
  options = options ? Object.assign({}, defaultParseOptions, options) : defaultParseOptions;
  if (!input) {
    if (!options.map) {
      return [];
    } else {
      return createNullObj();
    }
  }
  if (input.headers) {
    if (typeof input.headers.getSetCookie === "function") {
      input = input.headers.getSetCookie();
    } else if (input.headers["set-cookie"]) {
      input = input.headers["set-cookie"];
    } else {
      var sch = input.headers[Object.keys(input.headers).find(function(key) {
        return key.toLowerCase() === "set-cookie";
      })];
      if (!sch && input.headers.cookie && !options.silent) {
        console.warn(
          "Warning: set-cookie-parser appears to have been called on a request object. It is designed to parse Set-Cookie headers from responses, not Cookie headers from requests. Set the option {silent: true} to suppress this warning."
        );
      }
      input = sch;
    }
  }
  var split = options.split;
  var isArray = Array.isArray(input);
  if (split === "auto") {
    split = !isArray;
  }
  if (!isArray) {
    input = [input];
  }
  input = input.filter(isNonEmptyString);
  if (split) {
    input = input.map(splitCookiesString).flat();
  }
  if (!options.map) {
    return input.map(function(str) {
      return parseString(str, options);
    }).filter(Boolean);
  } else {
    var cookies = createNullObj();
    return input.reduce(function(cookies2, str) {
      var cookie = parseString(str, options);
      if (cookie && !isForbiddenKey(cookie.name)) {
        cookies2[cookie.name] = cookie;
      }
      return cookies2;
    }, cookies);
  }
}
function splitCookiesString(cookiesString) {
  if (Array.isArray(cookiesString)) {
    return cookiesString;
  }
  if (typeof cookiesString !== "string") {
    return [];
  }
  var cookiesStrings = [];
  var pos = 0;
  var start;
  var ch;
  var lastComma;
  var nextStart;
  var cookiesSeparatorFound;
  function skipWhitespace() {
    while (pos < cookiesString.length && /\s/.test(cookiesString.charAt(pos))) {
      pos += 1;
    }
    return pos < cookiesString.length;
  }
  function notSpecialChar() {
    ch = cookiesString.charAt(pos);
    return ch !== "=" && ch !== ";" && ch !== ",";
  }
  while (pos < cookiesString.length) {
    start = pos;
    cookiesSeparatorFound = false;
    while (skipWhitespace()) {
      ch = cookiesString.charAt(pos);
      if (ch === ",") {
        lastComma = pos;
        pos += 1;
        skipWhitespace();
        nextStart = pos;
        while (pos < cookiesString.length && notSpecialChar()) {
          pos += 1;
        }
        if (pos < cookiesString.length && cookiesString.charAt(pos) === "=") {
          cookiesSeparatorFound = true;
          pos = nextStart;
          cookiesStrings.push(cookiesString.substring(start, lastComma));
          start = pos;
        } else {
          pos = lastComma + 1;
        }
      } else {
        pos += 1;
      }
    }
    if (!cookiesSeparatorFound || pos >= cookiesString.length) {
      cookiesStrings.push(cookiesString.substring(start, cookiesString.length));
    }
  }
  return cookiesStrings;
}
parseSetCookie.parseSetCookie = parseSetCookie;
parseSetCookie.parse = parseSetCookie;
parseSetCookie.parseString = parseString;
parseSetCookie.splitCookiesString = splitCookiesString;
function negotiate(accept, types) {
  const parts = [];
  accept.split(",").forEach((str, i) => {
    const match = /^[ \t]*([^/ \t]+)\/([^; \t]+)[ \t]*(?:;[ \t]*q=([0-9.]+))?/.exec(str);
    if (match) {
      const [, type, subtype, q = "1"] = match;
      parts.push({ type, subtype, q: +q, i });
    }
  });
  parts.sort((a, b) => {
    if (a.q !== b.q) {
      return b.q - a.q;
    }
    if (a.subtype === "*" !== (b.subtype === "*")) {
      return a.subtype === "*" ? 1 : -1;
    }
    if (a.type === "*" !== (b.type === "*")) {
      return a.type === "*" ? 1 : -1;
    }
    return a.i - b.i;
  });
  let accepted;
  let min_priority = Infinity;
  for (const mimetype of types) {
    const [type, subtype] = mimetype.split("/");
    const priority = parts.findIndex(
      (part) => (part.type === type || part.type === "*") && (part.subtype === subtype || part.subtype === "*")
    );
    if (priority !== -1 && priority < min_priority) {
      accepted = mimetype;
      min_priority = priority;
    }
  }
  return accepted;
}
function get_set_cookies(headers) {
  if (typeof headers.getSetCookie === "function") {
    return headers.getSetCookie();
  }
  const set_cookie = headers.get("set-cookie");
  return set_cookie ? splitCookiesString(set_cookie) : [];
}
function is_content_type(request, ...types) {
  const type = request.headers.get("content-type")?.split(";", 1)[0].trim() ?? "";
  return types.includes(type.toLowerCase());
}
function is_form_content_type(request) {
  return is_content_type(
    request,
    "application/x-www-form-urlencoded",
    "multipart/form-data",
    "text/plain",
    BINARY_FORM_CONTENT_TYPE
  );
}
const escape_html_attr_dict = {
  "&": "&amp;",
  '"': "&quot;"
  // Svelte also escapes < because the escape function could be called inside a `noscript` there
  // https://github.com/sveltejs/svelte/security/advisories/GHSA-8266-84wp-wv5c
  // However, that doesn't apply in SvelteKit
};
const escape_html_dict = {
  "&": "&amp;",
  "<": "&lt;"
};
const surrogates = (
  // high surrogate without paired low surrogate
  "[\\ud800-\\udbff](?![\\udc00-\\udfff])|[\\ud800-\\udbff][\\udc00-\\udfff]|[\\udc00-\\udfff]"
);
const escape_html_attr_regex = new RegExp(
  `[${Object.keys(escape_html_attr_dict).join("")}]|` + surrogates,
  "g"
);
const escape_html_regex = new RegExp(
  `[${Object.keys(escape_html_dict).join("")}]|` + surrogates,
  "g"
);
function escape_html(str, is_attr) {
  const dict = is_attr ? escape_html_attr_dict : escape_html_dict;
  const escaped_str = str.replace(is_attr ? escape_html_attr_regex : escape_html_regex, (match) => {
    if (match.length === 2) {
      return match;
    }
    return dict[match] ?? `&#${match.charCodeAt(0)};`;
  });
  return escaped_str;
}
function method_not_allowed(mod, method) {
  return text(`${method} method not allowed`, {
    status: 405,
    headers: {
      // https://developer.mozilla.org/en-US/docs/Web/HTTP/Status/405
      // "The server must generate an Allow header field in a 405 status code response"
      allow: allowed_methods(mod).join(", ")
    }
  });
}
function allowed_methods(mod) {
  const allowed = ENDPOINT_METHODS.filter((method) => method in mod);
  if ("GET" in mod && !("HEAD" in mod)) {
    allowed.push("HEAD");
  }
  return allowed;
}
function get_global_name(options) {
  return `__sveltekit_${options.version_hash}`;
}
function static_error_page(options, status, message) {
  let page = options.templates.error({ status, message: escape_html(message) });
  return text(page, {
    headers: { "content-type": "text/html; charset=utf-8" },
    status
  });
}
async function handle_fatal_error(event, state, options, error) {
  error = error instanceof HttpError ? error : coalesce_to_error(error);
  const status = get_status(error);
  const body = await handle_error_and_jsonify(event, state, options, error);
  const type = negotiate(event.request.headers.get("accept") || "text/html", [
    "application/json",
    "text/html"
  ]);
  if (event.isDataRequest || type === "application/json") {
    return json(body, {
      status
    });
  }
  return static_error_page(options, status, body.message);
}
async function handle_error_and_jsonify(event, state, options, error) {
  if (error instanceof HttpError) {
    return { message: "Unknown Error", ...error.body };
  }
  const status = get_status(error);
  const message = get_message(error);
  return await with_request_store(
    { event, state },
    () => options.hooks.handleError({ error, event, status, message })
  ) ?? { message };
}
function redirect_response(status, location) {
  const response = new Response(void 0, {
    status,
    headers: { location }
  });
  return response;
}
function clarify_devalue_error(event, error) {
  if (error.path) {
    return `Data returned from \`load\` while rendering ${event.route.id} is not serializable: ${error.message} (${error.path}). If you need to serialize/deserialize custom types, use transport hooks: https://svelte.dev/docs/kit/hooks#transport.`;
  }
  if (error.path === "") {
    return `Data returned from \`load\` while rendering ${event.route.id} is not a plain object`;
  }
  return error.message;
}
function serialize_uses(node) {
  const uses = {};
  if (node.uses && node.uses.dependencies.size > 0) {
    uses.dependencies = Array.from(node.uses.dependencies);
  }
  if (node.uses && node.uses.search_params.size > 0) {
    uses.search_params = Array.from(node.uses.search_params);
  }
  if (node.uses && node.uses.params.size > 0) {
    uses.params = Array.from(node.uses.params);
  }
  if (node.uses?.parent) uses.parent = 1;
  if (node.uses?.route) uses.route = 1;
  if (node.uses?.url) uses.url = 1;
  return uses;
}
function has_prerendered_path(manifest, pathname) {
  return manifest._.prerendered_routes.has(pathname) || pathname.at(-1) === "/" && manifest._.prerendered_routes.has(pathname.slice(0, -1));
}
function format_server_error(status, error, event) {
  const formatted_text = `
\x1B[1;31m[${status}] ${event.request.method} ${event.url.pathname}\x1B[0m`;
  if (status === 404) {
    return formatted_text;
  }
  return `${formatted_text}
${error.stack}`;
}
function get_node_type(node_id) {
  const parts = node_id?.split("/");
  const filename = parts?.at(-1);
  if (!filename) return "unknown";
  const dot_parts = filename.split(".");
  return dot_parts.slice(0, -1).join(".");
}
function create_replacer(transport) {
  const replacer = (thing) => {
    for (const key in transport) {
      const encoded = transport[key].encode(thing);
      if (encoded) {
        return `app.decode('${key}', ${uneval(encoded, replacer)})`;
      }
    }
  };
  return replacer;
}
export {
  static_error_page as A,
  redirect_response as B,
  once as C,
  has_prerendered_path as D,
  ENDPOINT_METHODS as E,
  get_set_cookies as F,
  parseString as G,
  handle_fatal_error as H,
  INVALIDATED_PARAM as I,
  format_server_error as J,
  MUTATIVE_METHODS as M,
  PAGE_METHODS as P,
  SVELTE_KIT_ASSETS as S,
  TRAILING_SLASH_PARAM as T,
  stringify_remote_arg as a,
  noop as b,
  create_field_proxy as c,
  deep_set as d,
  stringify as e,
  flatten_issues as f,
  create_remote_key as g,
  handle_error_and_jsonify as h,
  parse_remote_arg as i,
  negotiate as j,
  create_replacer as k,
  get_status as l,
  method_not_allowed as m,
  normalize_issue as n,
  is_form_content_type as o,
  parse as p,
  stringify$1 as q,
  normalize_error as r,
  set_nested_value as s,
  get_global_name as t,
  serialize_uses as u,
  clarify_devalue_error as v,
  get_node_type as w,
  escape_html as x,
  deserialize_binary_form as y,
  split_remote_key as z
};
