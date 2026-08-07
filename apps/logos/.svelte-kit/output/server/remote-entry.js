import { get_request_store, with_request_store } from "@sveltejs/kit/internal/server";
import { error, json } from "@sveltejs/kit";
import { p as parse, M as MUTATIVE_METHODS, c as create_field_proxy, n as normalize_issue, s as set_nested_value, f as flatten_issues, d as deep_set, a as stringify_remote_arg, b as noop, e as stringify, g as create_remote_key, h as handle_error_and_jsonify, i as parse_remote_arg } from "./chunks/utils.js";
import { ValidationError, HttpError, SvelteKitError } from "@sveltejs/kit/internal";
import { b as base, a as app_dir, p as prerendering } from "./chunks/server.js";
function create_validator(validate_or_fn, maybe_fn) {
  if (!maybe_fn) {
    return (arg) => {
      if (arg !== void 0) {
        error(400, "Bad Request");
      }
    };
  }
  if (validate_or_fn === "unchecked") {
    return (arg) => arg;
  }
  if ("~standard" in validate_or_fn) {
    return async (arg) => {
      const { event, state } = get_request_store();
      const result = await validate_or_fn["~standard"].validate(arg);
      if (result.issues) {
        error(
          400,
          await state.handleValidationError({
            issues: result.issues,
            event
          })
        );
      }
      return result.value;
    };
  }
  throw new Error(
    'Invalid validator passed to remote function. Expected "unchecked" or a Standard Schema (https://standardschema.dev)'
  );
}
async function get_response(internals, payload, state, get_result) {
  await 0;
  const cache = get_cache(internals, state);
  if (!state.is_in_remote_query) {
    get_implicit_lookup(internals, state)[payload] = get_result;
  }
  return cache[payload] ??= get_result();
}
function parse_remote_response(data, transport) {
  const revivers = {};
  for (const key in transport) {
    revivers[key] = transport[key].decode;
  }
  return parse(data, revivers);
}
function derive_remote_function_event(event, state, allow_cookies) {
  return {
    event: {
      ...event,
      setHeaders: () => {
        throw new Error("setHeaders is not allowed in remote functions");
      },
      cookies: {
        ...event.cookies,
        set: (name, value, opts) => {
          if (!allow_cookies) {
            throw new Error("Cannot set cookies in `query` or `prerender` functions");
          }
          if (opts.path && !opts.path.startsWith("/")) {
            throw new Error("Cookies set in remote functions must have an absolute path");
          }
          return event.cookies.set(name, value, opts);
        },
        delete: (name, opts) => {
          if (!allow_cookies) {
            throw new Error("Cannot delete cookies in `query` or `prerender` functions");
          }
          if (opts.path && !opts.path.startsWith("/")) {
            throw new Error("Cookies deleted in remote functions must have an absolute path");
          }
          return event.cookies.delete(name, opts);
        }
      }
    },
    state: {
      ...state,
      is_in_remote_function: true
    }
  };
}
async function run_remote_function(event, state, allow_cookies, get_input, fn) {
  const store = derive_remote_function_event(event, state, allow_cookies);
  const input = await with_request_store(store, get_input);
  return with_request_store(store, () => fn(input));
}
async function* run_remote_generator(event, state, allow_cookies, get_input, fn, name) {
  const store = derive_remote_function_event(event, state, allow_cookies);
  const input = await with_request_store(store, get_input);
  const source = await with_request_store(store, () => fn(input));
  const iterator = to_iterator(source, name);
  let done = false;
  try {
    while (true) {
      const result = await with_request_store(store, () => iterator.next());
      if (result.done) {
        done = true;
        return result.value;
      }
      yield result.value;
    }
  } finally {
    if (!done && typeof iterator.return === "function") {
      await with_request_store(store, () => iterator.return?.(void 0));
    }
  }
}
function to_iterator(source, name) {
  if ("next" in source && typeof source.next === "function") {
    return source;
  }
  if (Symbol.asyncIterator in source && typeof source[Symbol.asyncIterator] === "function") {
    return source[Symbol.asyncIterator]();
  }
  if (Symbol.iterator in source && typeof source[Symbol.iterator] === "function") {
    return source[Symbol.iterator]();
  }
  throw new Error(
    `query.live '${name}' must return an Iterator, Iterable, AsyncIterator or AsyncIterable`
  );
}
function get_cache(internals, state) {
  let cache = state.remote.data?.get(internals);
  if (cache === void 0) {
    cache = {};
    (state.remote.data ??= /* @__PURE__ */ new Map()).set(internals, cache);
  }
  return cache;
}
function get_implicit_lookup(internals, state) {
  let cache = state.remote.implicit?.get(internals);
  if (cache === void 0) {
    cache = {};
    (state.remote.implicit ??= /* @__PURE__ */ new Map()).set(internals, cache);
  }
  return cache;
}
// @__NO_SIDE_EFFECTS__
function command(validate_or_fn, maybe_fn) {
  const fn = maybe_fn ?? validate_or_fn;
  const validate = create_validator(validate_or_fn, maybe_fn);
  const __ = { type: "command", id: "", name: "" };
  const wrapper = (arg) => {
    const { event, state } = get_request_store();
    if (!MUTATIVE_METHODS.includes(event.request.method)) {
      throw new Error(
        `Cannot call a command (\`${__.name}(${maybe_fn ? "..." : ""})\`) from a ${event.request.method} handler`
      );
    }
    if (state.is_in_render) {
      throw new Error(
        `Cannot call a command (\`${__.name}(${maybe_fn ? "..." : ""})\`) during server-side rendering`
      );
    }
    const promise = Promise.resolve(
      run_remote_function(event, state, true, () => validate(arg), fn)
    );
    promise.updates = () => {
      throw new Error(`Cannot call '${__.name}(...).updates(...)' on the server`);
    };
    return (
      /** @type {ReturnType<RemoteCommand<Input, Output>>} */
      promise
    );
  };
  Object.defineProperty(wrapper, "__", { value: __ });
  Object.defineProperty(wrapper, "pending", {
    get: () => 0
  });
  return wrapper;
}
// @__NO_SIDE_EFFECTS__
function form(validate_or_fn, maybe_fn) {
  const fn = maybe_fn ?? validate_or_fn;
  const schema = !maybe_fn || validate_or_fn === "unchecked" ? null : (
    /** @type {any} */
    validate_or_fn
  );
  function create_instance(key) {
    const instance = {};
    instance.method = "POST";
    Object.defineProperty(instance, "enhance", {
      value: () => {
        return { action: instance.action, method: instance.method };
      }
    });
    const __ = {
      type: "form",
      name: "",
      id: "",
      fn: async (data, meta, form_data) => {
        const output = {};
        output.submission = true;
        const { event, state } = get_request_store();
        const validated = await schema?.["~standard"].validate(data);
        if (meta.validate_only) {
          return validated?.issues?.map((issue) => normalize_issue(issue, true)) ?? [];
        }
        if (validated?.issues !== void 0) {
          handle_issues(output, validated.issues, form_data);
        } else {
          if (validated !== void 0) {
            data = validated.value;
          }
          const issue = create_issues();
          try {
            output.result = await run_remote_function(
              event,
              state,
              true,
              () => data,
              (data2) => !maybe_fn ? fn() : fn(data2, issue)
            );
          } catch (e) {
            if (e instanceof ValidationError) {
              handle_issues(output, e.issues, form_data);
            } else {
              throw e;
            }
          }
        }
        if (!event.isRemoteRequest) {
          const cache = get_cache(__, state);
          cache[""] ??= output;
          get_implicit_lookup(__, state)[__.action_id ?? __.id] = () => cache[""];
        }
        return output;
      }
    };
    Object.defineProperty(instance, "__", { value: __ });
    Object.defineProperty(instance, "action", {
      get: () => `?/remote=${__.id}`,
      enumerable: true
    });
    Object.defineProperty(instance, "fields", {
      get() {
        return create_field_proxy(
          {},
          () => get_cache(__, get_request_store().state)?.[""]?.input ?? {},
          (path, value) => {
            const cache = get_cache(__, get_request_store().state);
            const entry = cache[""];
            if (entry?.submission) {
              return;
            }
            if (path.length === 0) {
              (cache[""] ??= {}).input = value;
              return;
            }
            const input = entry?.input ?? {};
            deep_set(input, path.map(String), value);
            (cache[""] ??= {}).input = input;
          },
          () => flatten_issues(get_cache(__, get_request_store().state)?.[""]?.issues ?? [])
        );
      }
    });
    Object.defineProperty(instance, "result", {
      get() {
        try {
          return get_cache(__, get_request_store().state)?.[""]?.result;
        } catch {
          return void 0;
        }
      }
    });
    Object.defineProperty(instance, "pending", {
      get: () => 0
    });
    Object.defineProperty(instance, "submitted", {
      get: () => false
    });
    Object.defineProperty(instance, "preflight", {
      // preflight is a noop on the server
      value: () => instance
    });
    Object.defineProperty(instance, "validate", {
      value: () => {
        throw new Error("Cannot call validate() on the server");
      }
    });
    Object.defineProperty(instance, "submit", {
      value: () => {
        throw new Error("Cannot call submit() on the server");
      }
    });
    Object.defineProperty(instance, "element", {
      get: () => null
    });
    if (key == void 0) {
      Object.defineProperty(instance, "for", {
        /** @type {RemoteForm<any, any>['for']} */
        value: (key2) => {
          const { state } = get_request_store();
          const cache_key = __.id + "|" + JSON.stringify(key2);
          let instance2 = (state.remote.forms ??= /* @__PURE__ */ new Map()).get(cache_key);
          if (!instance2) {
            instance2 = create_instance(key2);
            instance2.__.id = `${__.id}/${encodeURIComponent(JSON.stringify(key2))}`;
            instance2.__.action_id = `${__.id}/${JSON.stringify(key2)}`;
            instance2.__.name = __.name;
            state.remote.forms.set(cache_key, instance2);
          }
          return instance2;
        }
      });
    }
    return instance;
  }
  return create_instance();
}
function handle_issues(output, issues, form_data) {
  output.issues = issues.map((issue) => normalize_issue(issue, true));
  if (form_data) {
    output.input = {};
    for (let key of form_data.keys()) {
      if (/^[.\]]?_/.test(key)) continue;
      const is_array = key.endsWith("[]");
      const values = form_data.getAll(key).filter((value) => typeof value === "string");
      if (is_array) key = key.slice(0, -2);
      set_nested_value(
        /** @type {Record<string, any>} */
        output.input,
        key,
        is_array ? values : values[0]
      );
    }
  }
}
function create_issues() {
  return (
    /** @type {InvalidField<any>} */
    new Proxy(
      /** @param {string} message */
      (message) => {
        if (typeof message !== "string") {
          throw new Error(
            "`invalid` should now be imported from `@sveltejs/kit` to throw validation issues. The second parameter provided to the form function (renamed to `issue`) is still used to construct issues, e.g. `invalid(issue.field('message'))`. For more info see https://github.com/sveltejs/kit/pulls/14768"
          );
        }
        return create_issue(message);
      },
      {
        get(target, prop) {
          if (typeof prop === "symbol") return (
            /** @type {any} */
            target[prop]
          );
          return create_issue_proxy(prop, []);
        }
      }
    )
  );
  function create_issue(message, path = []) {
    return {
      message,
      path
    };
  }
  function create_issue_proxy(key, path) {
    const new_path = [...path, key];
    const issue_func = (message) => create_issue(message, new_path);
    return new Proxy(issue_func, {
      get(target, prop) {
        if (typeof prop === "symbol") return (
          /** @type {any} */
          target[prop]
        );
        if (/^\d+$/.test(prop)) {
          return create_issue_proxy(parseInt(prop, 10), new_path);
        }
        return create_issue_proxy(prop, new_path);
      }
    });
  }
}
// @__NO_SIDE_EFFECTS__
function prerender(validate_or_fn, fn_or_options, maybe_options) {
  const maybe_fn = typeof fn_or_options === "function" ? fn_or_options : void 0;
  const options = maybe_options ?? (maybe_fn ? void 0 : fn_or_options);
  const fn = maybe_fn ?? validate_or_fn;
  const validate = create_validator(validate_or_fn, maybe_fn);
  const __ = {
    type: "prerender",
    id: "",
    name: "",
    has_arg: !!maybe_fn,
    inputs: options?.inputs,
    dynamic: options?.dynamic
  };
  const wrapper = (arg) => {
    const { event, state } = get_request_store();
    const payload = stringify_remote_arg(arg, state.transport);
    const promise = get_response(__, payload, state, async () => {
      const id = __.id;
      const url = `${base}/${app_dir}/remote/${id}${payload ? `/${payload}` : ""}`;
      if (!state.prerendering && true && !event.isRemoteRequest) {
        try {
          const response = await fetch(new URL(url, event.url.origin).href);
          if (!response.ok) {
            throw new Error("Prerendered response not found");
          }
          const prerendered = (
            /** @type {RemoteFunctionResponse} */
            await response.json()
          );
          if (prerendered.type === "error") {
            error(prerendered.status, prerendered.error);
          }
          return parse_remote_response(prerendered.data, state.transport)._;
        } catch {
        }
      }
      if (state.prerendering?.remote_responses.has(url)) {
        return (
          /** @type {Promise<any>} */
          state.prerendering.remote_responses.get(url)
        );
      }
      const promise2 = run_remote_function(event, state, false, () => validate(arg), fn);
      if (state.prerendering) {
        state.prerendering.remote_responses.set(url, promise2);
      }
      const result = await promise2;
      if (state.prerendering) {
        const body = { type: "result", data: stringify({ _: result }, state.transport) };
        state.prerendering.dependencies.set(url, {
          body: JSON.stringify(body),
          response: json(body)
        });
      }
      return result;
    });
    promise.catch(noop);
    return (
      /** @type {RemoteResource<Output>} */
      promise
    );
  };
  Object.defineProperty(wrapper, "__", { value: __ });
  return wrapper;
}
class SharedIterator {
  /**
   * @typedef {object} Subscriber
   * @property {{ value: any } | null} pending
   * @property {{ error: unknown } | null} pending_error
   * @property {boolean} finished
   * @property {((result: IteratorResult<any, void>) => void) | null} waiting_resolve
   * @property {((reason: unknown) => void) | null} waiting_reject
   */
  /** @type {Set<Subscriber>} */
  #subscribers = /* @__PURE__ */ new Set();
  /** @type {((instance: SharedIterator<T>) => (() => void)) | undefined} */
  #start = void 0;
  /** @type {(() => void) | undefined} */
  #stop = void 0;
  /** Once `done()` or `fail()` has been broadcast, no new values are accepted. */
  #closed = false;
  /** @type {unknown} */
  #terminal_error = void 0;
  /** Whether `done()` or `fail()` has been broadcast. */
  get closed() {
    return this.#closed;
  }
  /**
   * @param {(instance: SharedIterator<T>) => (() => void)} [start]
   */
  constructor(start) {
    this.#start = start;
  }
  /** @param {T} value */
  push(value) {
    if (this.#closed) return;
    for (const subscriber of this.#subscribers) {
      if (subscriber.waiting_resolve) {
        const resolve = subscriber.waiting_resolve;
        subscriber.waiting_resolve = null;
        subscriber.waiting_reject = null;
        resolve({ value, done: false });
      } else {
        subscriber.pending = { value };
      }
    }
  }
  /**
   * Signal natural completion to all current subscribers, and to any future
   * subscriber (which will receive an immediately-done iterator).
   */
  done() {
    if (this.#closed) return;
    this.#closed = true;
    for (const subscriber of this.#subscribers) {
      subscriber.finished = true;
      if (subscriber.waiting_resolve) {
        const resolve = subscriber.waiting_resolve;
        subscriber.waiting_resolve = null;
        subscriber.waiting_reject = null;
        resolve({ value: void 0, done: true });
      }
    }
    this.#subscribers.clear();
  }
  /**
   * Broadcast a terminal error. All current subscribers will reject their
   * next `.next()` call with `error`. Future subscribers will also reject
   * their first `.next()`.
   *
   * @param {unknown} error
   */
  fail(error2) {
    if (this.#closed) return;
    this.#closed = true;
    this.#terminal_error = error2;
    for (const subscriber of this.#subscribers) {
      subscriber.finished = true;
      if (subscriber.waiting_reject) {
        const reject = subscriber.waiting_reject;
        subscriber.waiting_resolve = null;
        subscriber.waiting_reject = null;
        reject(error2);
      } else {
        subscriber.pending_error = { error: error2 };
      }
    }
    this.#subscribers.clear();
  }
  /**
   * Subscribe to the shared stream. Returns an `AsyncGenerator<T>` that
   * yields every value pushed after this call (and, if `initial_value` is
   * provided, that value as the first yield).
   *
   * @param {{ initial_value?: { value: T } }} [options]
   *   `initial_value` lets the caller seed the iterator with a synchronously-
   *   available current value before any new pushes arrive (e.g. the
   *   "last-seen value" of a reactive resource). Pass it wrapped in an
   *   object so `undefined` can be distinguished from "no initial value".
   * @returns {AsyncGenerator<T, void, void>}
   */
  subscribe(options) {
    const subscriber = {
      pending: options?.initial_value ? { value: options.initial_value.value } : null,
      pending_error: this.#closed && this.#terminal_error !== void 0 ? { error: this.#terminal_error } : null,
      finished: this.#closed && this.#terminal_error === void 0,
      waiting_resolve: null,
      waiting_reject: null
    };
    if (!subscriber.finished && subscriber.pending_error === null) {
      this.#subscribers.add(subscriber);
    }
    if (!this.#closed) {
      this.#stop ??= this.#start?.(this);
    }
    const unsubscribe = () => {
      subscriber.finished = true;
      const was_present = this.#subscribers.delete(subscriber);
      if (was_present && this.#subscribers.size === 0) {
        this.#stop?.();
      }
    };
    const iterator = {
      next() {
        if (subscriber.pending_error) {
          const { error: error2 } = subscriber.pending_error;
          subscriber.pending_error = null;
          unsubscribe();
          return Promise.reject(error2);
        }
        if (subscriber.pending) {
          const { value } = subscriber.pending;
          subscriber.pending = null;
          return Promise.resolve({ value, done: false });
        }
        if (subscriber.finished) {
          return Promise.resolve({ value: void 0, done: true });
        }
        return new Promise((resolve, reject) => {
          subscriber.waiting_resolve = resolve;
          subscriber.waiting_reject = reject;
        });
      },
      return(value) {
        unsubscribe();
        if (subscriber.waiting_resolve) {
          const resolve = subscriber.waiting_resolve;
          subscriber.waiting_resolve = null;
          subscriber.waiting_reject = null;
          resolve({ value: void 0, done: true });
        }
        return Promise.resolve({ value: (
          /** @type {void} */
          value
        ), done: true });
      },
      throw(error2) {
        unsubscribe();
        if (subscriber.waiting_reject) {
          const reject = subscriber.waiting_reject;
          subscriber.waiting_resolve = null;
          subscriber.waiting_reject = null;
          reject(error2);
        }
        return Promise.reject(error2);
      },
      [Symbol.asyncIterator]() {
        return iterator;
      }
    };
    return iterator;
  }
}
// @__NO_SIDE_EFFECTS__
function query(validate_or_fn, maybe_fn) {
  const fn = maybe_fn ?? validate_or_fn;
  const validate = create_validator(validate_or_fn, maybe_fn);
  const __ = {
    type: "query",
    id: "",
    name: "",
    validate,
    bind(payload, validated_arg) {
      const { event, state } = get_request_store();
      return create_query_resource(
        __,
        payload,
        event,
        state,
        () => run_remote_function(
          event,
          { ...state, is_in_remote_query: true },
          false,
          () => validated_arg,
          fn
        )
      );
    }
  };
  const wrapper = (arg) => {
    if (prerendering) {
      throw new Error(
        `Cannot call query '${__.name}' while prerendering, as prerendered pages need static data. Use 'prerender' from $app/server instead`
      );
    }
    const { event, state } = get_request_store();
    const payload = stringify_remote_arg(arg, state.transport);
    return create_query_resource(
      __,
      payload,
      event,
      state,
      () => run_remote_function(
        event,
        { ...state, is_in_remote_query: true },
        false,
        () => validate(arg),
        fn
      )
    );
  };
  Object.defineProperty(wrapper, "__", { value: __ });
  return wrapper;
}
// @__NO_SIDE_EFFECTS__
function live(validate_or_fn, maybe_fn) {
  const fn = maybe_fn ?? validate_or_fn;
  const validate = create_validator(validate_or_fn, maybe_fn);
  const run = (event, state, get_input) => run_remote_generator(
    event,
    { ...state, is_in_remote_query: true },
    false,
    get_input,
    fn,
    __.name
  );
  const __ = {
    type: "query_live",
    id: "",
    name: "",
    run: (event, state, arg) => run(event, state, () => validate(arg)),
    validate,
    bind(payload, validated_arg) {
      const { event, state } = get_request_store();
      return create_live_query_resource(
        __,
        payload,
        event,
        state,
        () => run(event, state, () => validated_arg)
      );
    }
  };
  const wrapper = (arg) => {
    if (prerendering) {
      throw new Error(
        `Cannot call query.live '${__.name}' while prerendering, as prerendered pages need static data. Use 'prerender' from $app/server instead`
      );
    }
    const { event, state } = get_request_store();
    const payload = stringify_remote_arg(arg, state.transport);
    return create_live_query_resource(
      __,
      payload,
      event,
      state,
      () => run(event, state, () => validate(arg))
    );
  };
  Object.defineProperty(wrapper, "__", { value: __ });
  return wrapper;
}
// @__NO_SIDE_EFFECTS__
function batch(validate_or_fn, maybe_fn) {
  const fn = maybe_fn ?? validate_or_fn;
  const validate = create_validator(validate_or_fn, maybe_fn);
  const enqueue = (payload, get_validated) => {
    const { event, state } = get_request_store();
    return new Promise((resolve, reject) => {
      const batches = state.remote.batches ??= /** @type {NonNullable<typeof state.remote.batches>} */
      /* @__PURE__ */ new Map();
      let batched = batches.get(__.id);
      if (!batched) {
        batched = /* @__PURE__ */ new Map();
        batches.set(__.id, batched);
      }
      const entry = batched.get(payload);
      if (entry) {
        entry.resolvers.push({ resolve, reject });
        return;
      }
      batched.set(payload, {
        get_validated,
        resolvers: [{ resolve, reject }]
      });
      if (batched.size > 1) return;
      setTimeout(async () => {
        batches.delete(__.id);
        const entries = Array.from(batched.values());
        try {
          return await run_remote_function(
            event,
            { ...state, is_in_remote_query: true },
            false,
            async () => Promise.all(entries.map((entry2) => entry2.get_validated())),
            async (input) => {
              const get_result = await fn(input);
              for (let i = 0; i < entries.length; i++) {
                try {
                  const result = get_result(input[i], i);
                  for (const resolver of entries[i].resolvers) {
                    resolver.resolve(result);
                  }
                } catch (error2) {
                  for (const resolver of entries[i].resolvers) {
                    resolver.reject(error2);
                  }
                }
              }
            }
          );
        } catch (error2) {
          for (const entry2 of batched.values()) {
            for (const resolver of entry2.resolvers) {
              resolver.reject(error2);
            }
          }
        }
      }, 0);
    });
  };
  const __ = {
    type: "query_batch",
    id: "",
    name: "",
    validate,
    run: async (args, options) => {
      const { event, state } = get_request_store();
      return run_remote_function(
        event,
        { ...state, is_in_remote_query: true },
        false,
        async () => Promise.all(args.map(validate)),
        async (input) => {
          const get_result = await fn(input);
          return Promise.all(
            input.map(async (arg, i) => {
              try {
                const data = get_result(arg, i);
                return { type: "result", data };
              } catch (error2) {
                return {
                  type: "error",
                  error: await handle_error_and_jsonify(event, state, options, error2),
                  status: error2 instanceof HttpError || error2 instanceof SvelteKitError ? error2.status : 500
                };
              }
            })
          );
        }
      );
    },
    bind(payload, validated_arg) {
      const { event, state } = get_request_store();
      return create_query_resource(
        __,
        payload,
        event,
        state,
        () => enqueue(payload, () => validated_arg)
      );
    }
  };
  const wrapper = (arg) => {
    if (prerendering) {
      throw new Error(
        `Cannot call query.batch '${__.name}' while prerendering, as prerendered pages need static data. Use 'prerender' from $app/server instead`
      );
    }
    const { event, state } = get_request_store();
    const payload = stringify_remote_arg(arg, state.transport);
    return create_query_resource(
      __,
      payload,
      event,
      state,
      () => (
        // Collect all the calls to the same query in the same macrotask,
        // then execute them as one backend request.
        enqueue(payload, () => validate(arg))
      )
    );
  };
  Object.defineProperty(wrapper, "__", { value: __ });
  return wrapper;
}
function refresh(event, state, internals, payload, fn) {
  if (!internals.id) {
    return;
  }
  if (!event.isRemoteRequest) {
    return;
  }
  const key = create_remote_key(internals.id, payload);
  const promise = fn();
  promise.catch(() => {
  });
  (state.remote.explicit ??= /* @__PURE__ */ new Map()).set(key, {
    internals,
    promise
  });
}
function create_query_resource(__, payload, event, state, fn) {
  let promise = null;
  const get_promise = () => {
    return promise ??= get_response(__, payload, state, fn);
  };
  const populate_hydratable = () => {
    if (__.id && state.is_in_render) {
      get_promise().catch(noop);
    }
  };
  return {
    /** @type {Promise<any>['catch']} */
    catch(onrejected) {
      return get_promise().catch(onrejected);
    },
    get current() {
      populate_hydratable();
      return void 0;
    },
    get error() {
      populate_hydratable();
      return void 0;
    },
    /** @type {Promise<any>['finally']} */
    finally(onfinally) {
      return get_promise().finally(onfinally);
    },
    get loading() {
      populate_hydratable();
      return true;
    },
    get ready() {
      populate_hydratable();
      return false;
    },
    refresh() {
      promise = null;
      delete get_cache(__, state)[payload];
      refresh(event, state, __, payload, get_promise);
      return Promise.resolve();
    },
    /** @param {any} value */
    set(value) {
      const p = promise = Promise.resolve(value);
      get_cache(__, state)[payload] = p;
      refresh(event, state, __, payload, () => p);
    },
    // TODO 3.0 remove this
    // @ts-expect-error This method no longer exists
    run() {
      throw new Error(
        `\`myQuery().run()\` has been removed — please replace it with \`myQuery()\`. See https://github.com/sveltejs/kit/pull/15779 for more details`
      );
    },
    /** @type {Promise<any>['then']} */
    then(onfulfilled, onrejected) {
      return get_promise().then(onfulfilled, onrejected);
    },
    withOverride() {
      throw new Error(`Cannot call '${__.name}.withOverride()' on the server`);
    },
    get [Symbol.toStringTag]() {
      return "QueryResource";
    }
  };
}
function create_live_query_resource(__, payload, event, state, get_generator) {
  let promise = null;
  const get_first_value = async () => {
    for await (const value of get_generator()) {
      return value;
    }
    throw new Error(`query.live '${__.name}' did not yield a value`);
  };
  const get_promise = () => {
    return promise ??= get_response(__, payload, state, get_first_value);
  };
  const populate_hydratable = () => {
    if (__.id && state.is_in_render) {
      get_promise().catch(noop);
    }
  };
  return {
    /** @type {Promise<any>['catch']} */
    catch(onrejected) {
      return get_promise().catch(onrejected);
    },
    get current() {
      populate_hydratable();
      return void 0;
    },
    get error() {
      populate_hydratable();
      return void 0;
    },
    /** @type {Promise<any>['finally']} */
    finally(onfinally) {
      return get_promise().finally(onfinally);
    },
    get done() {
      populate_hydratable();
      return false;
    },
    get loading() {
      populate_hydratable();
      return true;
    },
    get ready() {
      populate_hydratable();
      return false;
    },
    get connected() {
      populate_hydratable();
      return false;
    },
    reconnect() {
      promise = null;
      delete get_cache(__, state)[payload];
      refresh(event, state, __, payload, get_promise);
      return Promise.resolve();
    },
    /** @ts-expect-error This method no longer exists */
    run() {
      throw new Error(
        "`.run()` has been removed from live queries. Use `for await (const value of liveQuery())` instead."
      );
    },
    /** @type {Promise<any>['then']} */
    then(onfulfilled, onrejected) {
      return get_promise().then(onfulfilled, onrejected);
    },
    [Symbol.asyncIterator]() {
      const key = create_remote_key(__.id, payload);
      const cache = state.remote.live_iterators ??= /* @__PURE__ */ new Map();
      let cached = cache.get(key);
      if (!cached) {
        cached = create_shared_live_iterator(event.request.signal, get_generator);
        cache.set(key, cached);
      }
      return cached.subscribe();
    },
    get [Symbol.toStringTag]() {
      return "LiveQueryResource";
    }
  };
}
function create_shared_live_iterator(signal, get_generator) {
  return new SharedIterator((instance) => {
    if (signal.aborted) {
      instance.done();
      return noop;
    }
    const generator = get_generator();
    let aborted = false;
    const close = () => {
      aborted = true;
      void generator.return().catch(noop);
    };
    signal.addEventListener("abort", () => (close(), instance.done()), { once: true });
    void (async () => {
      try {
        while (true) {
          const result = await generator.next();
          if (result.done) {
            instance.done();
            return;
          }
          instance.push(result.value);
        }
      } catch (error2) {
        if (!aborted) instance.fail(error2);
      } finally {
        close();
      }
    })();
    return close;
  });
}
Object.defineProperty(query, "batch", { value: batch, enumerable: true });
Object.defineProperty(query, "live", { value: live, enumerable: true });
function requested(query2, limit) {
  const { event, state } = get_request_store();
  const internals = (
    /** @type {RemoteAnyQueryInternals | undefined} */
    /** @type {any} */
    query2.__
  );
  if (internals?.type !== "query" && internals?.type !== "query_batch" && internals?.type !== "query_live") {
    throw new Error(
      "requested(...) expects a query function created with query(...), query.batch(...), or query.live(...)"
    );
  }
  const __ = internals;
  const requested2 = state.remote.requested;
  const payloads = requested2?.get(__.id) ?? [];
  if (!state.is_in_remote_form_or_command) {
    throw new Error(
      "requested(...) can only be called in the context of a command/form remote function"
    );
  }
  const [selected, skipped] = split_limit(payloads, limit);
  const record_failure = (payload, error2) => {
    const promise = Promise.reject(error2);
    promise.catch(noop);
    get_cache(__, state)[payload] = promise;
    refresh(event, state, __, payload, () => promise);
  };
  for (const payload of skipped) {
    record_failure(
      payload,
      new HttpError(
        400,
        `Requested refresh was rejected because it exceeded requested(${__.name}, ${limit}) limit`
      )
    );
  }
  const result = {
    *[Symbol.iterator]() {
      for (const payload of selected) {
        try {
          const parsed = parse_remote_arg(payload, state.transport);
          const validated = __.validate(parsed);
          if (is_thenable(validated)) {
            throw new Error(
              // TODO improve
              `requested(${__.name}, ${limit}) cannot be used with synchronous iteration because the query validator is async. Use \`for await ... of\` instead`
            );
          }
          yield { arg: validated, query: __.bind(payload, validated) };
        } catch (error2) {
          record_failure(payload, error2);
          continue;
        }
      }
    },
    async *[Symbol.asyncIterator]() {
      yield* race_all(selected, async (payload) => {
        try {
          const parsed = parse_remote_arg(payload, state.transport);
          const validated = await __.validate(parsed);
          return { arg: validated, query: __.bind(payload, validated) };
        } catch (error2) {
          record_failure(payload, error2);
          throw new Error(`Skipping ${__.name}(${payload})`, { cause: error2 });
        }
      });
    },
    async refreshAll() {
      if (__.type === "query_live") {
        throw new Error("refreshAll() is invalid for live queries. Use reconnectAll() instead.");
      }
      for await (const { query: query3 } of result) {
        void /** @type {RemoteQuery<Output>} */
        query3.refresh();
      }
    },
    async reconnectAll() {
      if (__.type !== "query_live") {
        throw new Error("reconnectAll() is invalid for regular queries. Use refreshAll() instead.");
      }
      for await (const { query: query3 } of result) {
        void /** @type {RemoteLiveQuery<Output>} */
        query3.reconnect();
      }
    }
  };
  return (
    /** @type {RequestedResult<Validated, Output>} */
    /** @type {unknown} */
    result
  );
}
function split_limit(array, limit) {
  if (limit === Infinity) {
    return [array, []];
  }
  if (!Number.isInteger(limit) || limit < 0) {
    throw new Error("Limit must be a non-negative integer or Infinity");
  }
  return [array.slice(0, limit), array.slice(limit)];
}
function is_thenable(value) {
  return !!value && (typeof value === "object" || typeof value === "function") && "then" in value;
}
async function* race_all(array, fn) {
  const pending = /* @__PURE__ */ new Set();
  for (const value of array) {
    const promise = Promise.resolve(fn(value)).then((result) => ({
      promise,
      value: result
    }));
    promise.catch(() => pending.delete(promise));
    pending.add(promise);
  }
  while (pending.size > 0) {
    try {
      const { promise, value } = await Promise.race(pending);
      pending.delete(promise);
      yield value;
    } catch {
    }
  }
}
export {
  command,
  form,
  prerender,
  query,
  requested
};
