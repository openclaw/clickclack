import test from "node:test";
import assert from "node:assert/strict";
import {
  appleDevice,
  applicationServerKey,
  deviceLabel,
  pushDeviceKey,
  pushSupported,
  sameApplicationServerKey,
  subscriptionIsStale,
} from "./push-capability.ts";

const globals = globalThis as {
  navigator?: unknown;
  window?: unknown;
};

function withEnvironment(navigatorValue: unknown, windowValue: unknown, run: () => void) {
  const previousNavigator = Object.getOwnPropertyDescriptor(globals, "navigator");
  const previousWindow = Object.getOwnPropertyDescriptor(globals, "window");
  Object.defineProperty(globals, "navigator", { value: navigatorValue, configurable: true });
  Object.defineProperty(globals, "window", { value: windowValue, configurable: true });
  try {
    run();
  } finally {
    if (previousNavigator) Object.defineProperty(globals, "navigator", previousNavigator);
    else delete globals.navigator;
    if (previousWindow) Object.defineProperty(globals, "window", previousWindow);
    else delete globals.window;
  }
}

test("applicationServerKey decodes an unpadded base64url VAPID key", () => {
  // The RFC 8291 example application server key: 65 bytes, uncompressed point.
  const key =
    "BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8";
  const bytes = applicationServerKey(key);
  assert.equal(bytes.length, 65);
  assert.equal(bytes[0], 4);
  assert.equal(bytes[64], 0x0f);
});

test("pushSupported requires the service worker, the push manager, and notifications", () => {
  const complete = { serviceWorker: {} };
  withEnvironment(complete, { PushManager: class {}, Notification: class {} }, () => {
    assert.equal(pushSupported(), true);
  });
  // An iOS Safari tab has a service worker but no push manager.
  withEnvironment(complete, { Notification: class {} }, () => {
    assert.equal(pushSupported(), false);
  });
  withEnvironment({}, { PushManager: class {}, Notification: class {} }, () => {
    assert.equal(pushSupported(), false);
  });
});

test("appleDevice recognizes an iPad reporting a desktop user agent", () => {
  const ipad =
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15";
  withEnvironment({ userAgent: ipad, maxTouchPoints: 5 }, {}, () => {
    assert.equal(appleDevice(), true);
  });
  withEnvironment({ userAgent: ipad, maxTouchPoints: 0 }, {}, () => {
    assert.equal(appleDevice(), false);
  });
  withEnvironment(
    { userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)", maxTouchPoints: 5 },
    {},
    () => {
      assert.equal(appleDevice(), true);
    },
  );
  withEnvironment({ userAgent: "Mozilla/5.0 (X11; Linux x86_64)", maxTouchPoints: 0 }, {}, () => {
    assert.equal(appleDevice(), false);
  });
});

test("deviceLabel names an iPad that presents as a Mac", () => {
  const mac =
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15";
  assert.equal(deviceLabel(mac, 5), "iPad Safari");
  assert.equal(deviceLabel(mac, 0), "Mac Safari");
});

test("deviceLabel names the platform and browser without copying the user agent", () => {
  assert.equal(
    deviceLabel(
      "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Version/18.0 Safari/605.1.15",
    ),
    "iPhone Safari",
  );
  assert.equal(
    deviceLabel(
      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/140.0.0.0 Safari/537.36",
    ),
    "Mac Chrome",
  );
  assert.equal(deviceLabel("something else entirely"), "This device");
});

test("sameApplicationServerKey replaces a subscription made under a rotated key", () => {
  // The RFC 8291 example application server key, and a second real P-256
  // point standing in for the key after an operator rotates the pair.
  const current = applicationServerKey(
    "BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8",
  );
  const rotated = applicationServerKey(
    "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4",
  );
  // A subscription reports its key as an ArrayBuffer.
  assert.equal(sameApplicationServerKey(current.slice().buffer, current), true);
  assert.equal(sameApplicationServerKey(rotated.slice().buffer, current), false);
  // A view over a larger buffer compares only its own bytes.
  const padded = new Uint8Array(current.length + 8);
  padded.set(current, 4);
  assert.equal(sameApplicationServerKey(padded.subarray(4, 4 + current.length), current), true);
  assert.equal(sameApplicationServerKey(current.slice(0, 64).buffer, current), false);
  // A browser that hides the key cannot be compared and keeps its subscription.
  assert.equal(sameApplicationServerKey(null, current), true);
  assert.equal(sameApplicationServerKey(undefined, current), true);
});

test("subscriptionIsStale uses the browser's key when it has one, else the remembered key", () => {
  const current =
    "BNbxGYNMhEIi9zrneh7mqV4oUanjLUK3m-mYZBc62frMKrEoiPJ-pVPSPSFy0WcyTuzTjGjuj_VO5bqYwstlGtE";
  const other =
    "BCTr9HsDUFcPuwSZkBv5t_nnrv-XyDnOr5UXvKZ8Enjex7GADo0ZeCgz7gT5n3UQ5oLjFmT2j03pmBpSSttwq_Y";
  const bytes = applicationServerKey(current);
  assert.equal(subscriptionIsStale(bytes, "", current, false), false);
  assert.equal(subscriptionIsStale(bytes, other, current, false), false);
  assert.equal(subscriptionIsStale(applicationServerKey(other), "", current, false), true);
  assert.equal(subscriptionIsStale(null, current, current, false), false);
  assert.equal(subscriptionIsStale(null, other, current, false), true);
  assert.equal(subscriptionIsStale(null, "", current, false), false);
});

test("subscriptionIsStale takes the server's word when the browser hides the key and none is remembered", () => {
  const current =
    "BNbxGYNMhEIi9zrneh7mqV4oUanjLUK3m-mYZBc62frMKrEoiPJ-pVPSPSFy0WcyTuzTjGjuj_VO5bqYwstlGtE";
  // Safari reports no key, and a device that subscribed before the key was
  // remembered, or lost its storage, remembers none: only the server can tell.
  assert.equal(subscriptionIsStale(null, "", current, true), true);
  assert.equal(subscriptionIsStale(undefined, "", current, true), true);
  // The server's word wins over a browser or a memory that reads current.
  assert.equal(subscriptionIsStale(applicationServerKey(current), current, current, true), true);
  assert.equal(subscriptionIsStale(null, current, current, true), true);
});

test("pushDeviceKey is the unpadded base64url SHA-256 the server computes", async () => {
  // The same vectors the server's test holds, so both sides agree on one encoding.
  assert.equal(
    await pushDeviceKey("https://push.example.com/send/this-device"),
    "roAu0n9u864S8b50dqaahZyGG6xEblpUS-e4wI47j74",
  );
  assert.equal(
    await pushDeviceKey("https://push.example.com/send/other-device"),
    "QRwuTHGa0ab-lUQ-J0DAANtRKTDtZ80bqlhAbRDCtTI",
  );
  assert.equal(await pushDeviceKey(""), "");
});
