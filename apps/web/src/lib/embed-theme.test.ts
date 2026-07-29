import assert from "node:assert/strict";
import test from "node:test";
import {
  clearEmbedHostTheme,
  getEmbedHostThemeMode,
  installEmbedHostTheme,
  resolveEmbedHostOrigin,
  resolveEmbedThemeMode,
} from "./embed-theme.ts";

test("accepts explicit light and dark modes on channel and thread embeds", () => {
  assert.equal(
    resolveEmbedThemeMode({ pathname: "/embed/channel/T1/C1", search: "?theme=light" }),
    "light",
  );
  assert.equal(
    resolveEmbedThemeMode({ pathname: "/embed/thread/T1/M1", search: "?theme=dark" }),
    "dark",
  );
});

test("does not apply embed themes to normal ClickClack pages", () => {
  assert.equal(resolveEmbedThemeMode({ pathname: "/app/T1/C1", search: "?theme=dark" }), null);
  assert.equal(resolveEmbedThemeMode({ pathname: "/embed/channel/T1/C1", search: "" }), null);
  assert.equal(
    resolveEmbedThemeMode({ pathname: "/embed/channel/T1/C1", search: "?theme=system" }),
    null,
  );
});

test("accepts only exact HTTP and HTTPS host origins on embed routes", () => {
  assert.equal(
    resolveEmbedHostOrigin({
      pathname: "/embed/channel/T1/C1",
      search: "?hostOrigin=https%3A%2F%2Fcontrol.example.com",
    }),
    "https://control.example.com",
  );
  assert.equal(
    resolveEmbedHostOrigin({
      pathname: "/embed/thread/T1/M1",
      search: "?hostOrigin=http%3A%2F%2Flocalhost%3A18789",
    }),
    "http://localhost:18789",
  );
});

test("rejects host origins with paths, credentials, non-HTTP schemes, and normal app routes", () => {
  for (const origin of [
    "https://control.example.com/",
    "https://control.example.com/path",
    "https://user@control.example.com",
    "javascript:alert(1)",
    "null",
    "*",
  ]) {
    assert.equal(
      resolveEmbedHostOrigin({
        pathname: "/embed/channel/T1/C1",
        search: `?hostOrigin=${encodeURIComponent(origin)}`,
      }),
      null,
      origin,
    );
  }
  assert.equal(
    resolveEmbedHostOrigin({
      pathname: "/app/T1/C1",
      search: "?hostOrigin=https%3A%2F%2Fcontrol.example.com",
    }),
    null,
  );
});

test("revokes the original host theme after leaving the embedded route", () => {
  const globals = ["window", "document", "CSS"] as const;
  const originalDescriptors = new Map(
    globals.map((name) => [name, Object.getOwnPropertyDescriptor(globalThis, name)]),
  );
  const properties = new Map<string, string>();
  const attributes = new Map<string, string>();
  const parent = {};
  const location = {
    pathname: "/embed/channel/T1/C1",
    search: "?theme=dark&hostOrigin=https%3A%2F%2Fcontrol.example.com",
  };
  let listener: ((event: MessageEvent<unknown>) => void) | undefined;

  Object.defineProperty(globalThis, "window", {
    configurable: true,
    value: {
      location,
      parent,
      addEventListener(_type: string, callback: (event: MessageEvent<unknown>) => void) {
        listener = callback;
      },
      removeEventListener() {
        listener = undefined;
      },
    },
  });
  Object.defineProperty(globalThis, "document", {
    configurable: true,
    value: {
      documentElement: {
        setAttribute(name: string, value: string) {
          attributes.set(name, value);
        },
        style: {
          setProperty(name: string, value: string) {
            properties.set(name, value);
          },
          removeProperty(name: string) {
            properties.delete(name);
          },
        },
      },
    },
  });
  Object.defineProperty(globalThis, "CSS", {
    configurable: true,
    value: { supports: () => true },
  });

  try {
    const uninstall = installEmbedHostTheme();
    listener?.({
      origin: "https://control.example.com",
      source: parent,
      data: {
        type: "openclaw:widget-theme",
        mode: "dark",
        tokens: { surface: "#171229" },
      },
    } as MessageEvent<unknown>);

    assert.equal(getEmbedHostThemeMode(), "dark");
    assert.equal(properties.get("--bg"), "#171229");

    location.pathname = "/app/T1/C1";
    location.search = "";
    listener?.({
      origin: "https://control.example.com",
      source: parent,
      data: {
        type: "openclaw:widget-theme",
        mode: "light",
        tokens: { surface: "#ff0000" },
      },
    } as MessageEvent<unknown>);

    assert.equal(getEmbedHostThemeMode(), null);
    assert.equal(properties.get("--bg"), "#171229");
    assert.equal(clearEmbedHostTheme(), true);
    assert.equal(properties.has("--bg"), false);
    assert.equal(clearEmbedHostTheme(), false);
    uninstall();
  } finally {
    for (const name of globals) {
      const descriptor = originalDescriptors.get(name);
      if (descriptor) Object.defineProperty(globalThis, name, descriptor);
      else Reflect.deleteProperty(globalThis, name);
    }
  }
});
