
// this file is generated — do not edit it


/// <reference types="@sveltejs/kit" />

/**
 * This module provides access to environment variables that are injected _statically_ into your bundle at build time and are limited to _private_ access.
 * 
 * |         | Runtime                                                                    | Build time                                                               |
 * | ------- | -------------------------------------------------------------------------- | ------------------------------------------------------------------------ |
 * | Private | [`$env/dynamic/private`](https://svelte.dev/docs/kit/$env-dynamic-private) | [`$env/static/private`](https://svelte.dev/docs/kit/$env-static-private) |
 * | Public  | [`$env/dynamic/public`](https://svelte.dev/docs/kit/$env-dynamic-public)   | [`$env/static/public`](https://svelte.dev/docs/kit/$env-static-public)   |
 * 
 * Static environment variables are [loaded by Vite](https://vitejs.dev/guide/env-and-mode.html#env-files) from `.env` files and `process.env` at build time and then statically injected into your bundle at build time, enabling optimisations like dead code elimination.
 * 
 * **_Private_ access:**
 * 
 * - This module cannot be imported into client-side code
 * - This module only includes variables that _do not_ begin with [`config.kit.env.publicPrefix`](https://svelte.dev/docs/kit/configuration#env) _and do_ start with [`config.kit.env.privatePrefix`](https://svelte.dev/docs/kit/configuration#env) (if configured)
 * 
 * For example, given the following build time environment:
 * 
 * ```env
 * ENVIRONMENT=production
 * PUBLIC_BASE_URL=http://site.com
 * ```
 * 
 * With the default `publicPrefix` and `privatePrefix`:
 * 
 * ```ts
 * import { ENVIRONMENT, PUBLIC_BASE_URL } from '$env/static/private';
 * 
 * console.log(ENVIRONMENT); // => "production"
 * console.log(PUBLIC_BASE_URL); // => throws error during build
 * ```
 * 
 * The above values will be the same _even if_ different values for `ENVIRONMENT` or `PUBLIC_BASE_URL` are set at runtime, as they are statically replaced in your code with their build time values.
 */
declare module '$env/static/private' {
	export const SVELTEKIT_FORK: string;
	export const NODE_ENV: string;
	export const EDITOR: string;
	export const OPENCLAW_SERVICE_VERSION: string;
	export const _: string;
	export const COLOR: string;
	export const OLDPWD: string;
	export const JOURNAL_STREAM: string;
	export const npm_config_local_prefix: string;
	export const npm_config_npm_version: string;
	export const npm_config_noproxy: string;
	export const npm_config_globalconfig: string;
	export const SYSTEMD_EXEC_PID: string;
	export const OPENCLAW_GATEWAY_PORT: string;
	export const XDG_RUNTIME_DIR: string;
	export const HOME: string;
	export const npm_config_node_gyp: string;
	export const OPENCLAW_SYSTEMD_UNIT: string;
	export const npm_config_prefix: string;
	export const npm_lifecycle_event: string;
	export const MEMORY_PRESSURE_WRITE: string;
	export const CLICKCLACK_BOT_TOKEN: string;
	export const PATH: string;
	export const LOGNAME: string;
	export const npm_command: string;
	export const NODE_EXTRA_CA_CERTS: string;
	export const npm_node_execpath: string;
	export const SHLVL: string;
	export const TMPDIR: string;
	export const OPENCLAW_PATH_BOOTSTRAPPED: string;
	export const MEMORY_PRESSURE_WATCH: string;
	export const npm_config_userconfig: string;
	export const GSM_SKIP_SSH_AGENT_WORKAROUND: string;
	export const NODE_PATH: string;
	export const USER: string;
	export const npm_config_cache: string;
	export const npm_config_user_agent: string;
	export const DBUS_SESSION_BUS_ADDRESS: string;
	export const npm_package_json: string;
	export const MANAGERPID: string;
	export const NODE: string;
	export const OPENCLAW_GATEWAY_SERVICE_PID: string;
	export const npm_package_name: string;
	export const LANG: string;
	export const npm_config_allow_scripts: string;
	export const OPENCLAW_WINDOWS_TASK_NAME: string;
	export const npm_lifecycle_script: string;
	export const npm_package_version: string;
	export const npm_config_global_prefix: string;
	export const SSH_AUTH_SOCK: string;
	export const OPENCLAW_SERVICE_KIND: string;
	export const OPENCLAW_SERVICE_MARKER: string;
	export const OPENCLAW_CLI: string;
	export const QT_ACCESSIBILITY: string;
	export const npm_config_init_module: string;
	export const PWD: string;
	export const npm_execpath: string;
	export const OPENCLAW_WINDOWS_TASK_HIDDEN_LAUNCHER: string;
	export const XDG_DATA_DIRS: string;
	export const INVOCATION_ID: string;
	export const OPENCLAW_SHELL: string;
	export const INIT_CWD: string;
}

/**
 * This module provides access to environment variables that are injected _statically_ into your bundle at build time and are _publicly_ accessible.
 * 
 * |         | Runtime                                                                    | Build time                                                               |
 * | ------- | -------------------------------------------------------------------------- | ------------------------------------------------------------------------ |
 * | Private | [`$env/dynamic/private`](https://svelte.dev/docs/kit/$env-dynamic-private) | [`$env/static/private`](https://svelte.dev/docs/kit/$env-static-private) |
 * | Public  | [`$env/dynamic/public`](https://svelte.dev/docs/kit/$env-dynamic-public)   | [`$env/static/public`](https://svelte.dev/docs/kit/$env-static-public)   |
 * 
 * Static environment variables are [loaded by Vite](https://vitejs.dev/guide/env-and-mode.html#env-files) from `.env` files and `process.env` at build time and then statically injected into your bundle at build time, enabling optimisations like dead code elimination.
 * 
 * **_Public_ access:**
 * 
 * - This module _can_ be imported into client-side code
 * - **Only** variables that begin with [`config.kit.env.publicPrefix`](https://svelte.dev/docs/kit/configuration#env) (which defaults to `PUBLIC_`) are included
 * 
 * For example, given the following build time environment:
 * 
 * ```env
 * ENVIRONMENT=production
 * PUBLIC_BASE_URL=http://site.com
 * ```
 * 
 * With the default `publicPrefix` and `privatePrefix`:
 * 
 * ```ts
 * import { ENVIRONMENT, PUBLIC_BASE_URL } from '$env/static/public';
 * 
 * console.log(ENVIRONMENT); // => throws error during build
 * console.log(PUBLIC_BASE_URL); // => "http://site.com"
 * ```
 * 
 * The above values will be the same _even if_ different values for `ENVIRONMENT` or `PUBLIC_BASE_URL` are set at runtime, as they are statically replaced in your code with their build time values.
 */
declare module '$env/static/public' {
	
}

/**
 * This module provides access to environment variables set _dynamically_ at runtime and that are limited to _private_ access.
 * 
 * |         | Runtime                                                                    | Build time                                                               |
 * | ------- | -------------------------------------------------------------------------- | ------------------------------------------------------------------------ |
 * | Private | [`$env/dynamic/private`](https://svelte.dev/docs/kit/$env-dynamic-private) | [`$env/static/private`](https://svelte.dev/docs/kit/$env-static-private) |
 * | Public  | [`$env/dynamic/public`](https://svelte.dev/docs/kit/$env-dynamic-public)   | [`$env/static/public`](https://svelte.dev/docs/kit/$env-static-public)   |
 * 
 * Dynamic environment variables are defined by the platform you're running on. For example if you're using [`adapter-node`](https://github.com/sveltejs/kit/tree/main/packages/adapter-node) (or running [`vite preview`](https://svelte.dev/docs/kit/cli)), this is equivalent to `process.env`.
 * 
 * **_Private_ access:**
 * 
 * - This module cannot be imported into client-side code
 * - This module includes variables that _do not_ begin with [`config.kit.env.publicPrefix`](https://svelte.dev/docs/kit/configuration#env) _and do_ start with [`config.kit.env.privatePrefix`](https://svelte.dev/docs/kit/configuration#env) (if configured)
 * 
 * > [!NOTE] In `dev`, `$env/dynamic` includes environment variables from `.env`. In `prod`, this behavior will depend on your adapter.
 * 
 * > [!NOTE] To get correct types, environment variables referenced in your code should be declared (for example in an `.env` file), even if they don't have a value until the app is deployed:
 * >
 * > ```env
 * > MY_FEATURE_FLAG=
 * > ```
 * >
 * > You can override `.env` values from the command line like so:
 * >
 * > ```sh
 * > MY_FEATURE_FLAG="enabled" npm run dev
 * > ```
 * 
 * For example, given the following runtime environment:
 * 
 * ```env
 * ENVIRONMENT=production
 * PUBLIC_BASE_URL=http://site.com
 * ```
 * 
 * With the default `publicPrefix` and `privatePrefix`:
 * 
 * ```ts
 * import { env } from '$env/dynamic/private';
 * 
 * console.log(env.ENVIRONMENT); // => "production"
 * console.log(env.PUBLIC_BASE_URL); // => undefined
 * ```
 */
declare module '$env/dynamic/private' {
	export const env: {
		SVELTEKIT_FORK: string;
		NODE_ENV: string;
		EDITOR: string;
		OPENCLAW_SERVICE_VERSION: string;
		_: string;
		COLOR: string;
		OLDPWD: string;
		JOURNAL_STREAM: string;
		npm_config_local_prefix: string;
		npm_config_npm_version: string;
		npm_config_noproxy: string;
		npm_config_globalconfig: string;
		SYSTEMD_EXEC_PID: string;
		OPENCLAW_GATEWAY_PORT: string;
		XDG_RUNTIME_DIR: string;
		HOME: string;
		npm_config_node_gyp: string;
		OPENCLAW_SYSTEMD_UNIT: string;
		npm_config_prefix: string;
		npm_lifecycle_event: string;
		MEMORY_PRESSURE_WRITE: string;
		CLICKCLACK_BOT_TOKEN: string;
		PATH: string;
		LOGNAME: string;
		npm_command: string;
		NODE_EXTRA_CA_CERTS: string;
		npm_node_execpath: string;
		SHLVL: string;
		TMPDIR: string;
		OPENCLAW_PATH_BOOTSTRAPPED: string;
		MEMORY_PRESSURE_WATCH: string;
		npm_config_userconfig: string;
		GSM_SKIP_SSH_AGENT_WORKAROUND: string;
		NODE_PATH: string;
		USER: string;
		npm_config_cache: string;
		npm_config_user_agent: string;
		DBUS_SESSION_BUS_ADDRESS: string;
		npm_package_json: string;
		MANAGERPID: string;
		NODE: string;
		OPENCLAW_GATEWAY_SERVICE_PID: string;
		npm_package_name: string;
		LANG: string;
		npm_config_allow_scripts: string;
		OPENCLAW_WINDOWS_TASK_NAME: string;
		npm_lifecycle_script: string;
		npm_package_version: string;
		npm_config_global_prefix: string;
		SSH_AUTH_SOCK: string;
		OPENCLAW_SERVICE_KIND: string;
		OPENCLAW_SERVICE_MARKER: string;
		OPENCLAW_CLI: string;
		QT_ACCESSIBILITY: string;
		npm_config_init_module: string;
		PWD: string;
		npm_execpath: string;
		OPENCLAW_WINDOWS_TASK_HIDDEN_LAUNCHER: string;
		XDG_DATA_DIRS: string;
		INVOCATION_ID: string;
		OPENCLAW_SHELL: string;
		INIT_CWD: string;
		[key: `PUBLIC_${string}`]: undefined;
		[key: `${string}`]: string | undefined;
	}
}

/**
 * This module provides access to environment variables set _dynamically_ at runtime and that are _publicly_ accessible.
 * 
 * |         | Runtime                                                                    | Build time                                                               |
 * | ------- | -------------------------------------------------------------------------- | ------------------------------------------------------------------------ |
 * | Private | [`$env/dynamic/private`](https://svelte.dev/docs/kit/$env-dynamic-private) | [`$env/static/private`](https://svelte.dev/docs/kit/$env-static-private) |
 * | Public  | [`$env/dynamic/public`](https://svelte.dev/docs/kit/$env-dynamic-public)   | [`$env/static/public`](https://svelte.dev/docs/kit/$env-static-public)   |
 * 
 * Dynamic environment variables are defined by the platform you're running on. For example if you're using [`adapter-node`](https://github.com/sveltejs/kit/tree/main/packages/adapter-node) (or running [`vite preview`](https://svelte.dev/docs/kit/cli)), this is equivalent to `process.env`.
 * 
 * **_Public_ access:**
 * 
 * - This module _can_ be imported into client-side code
 * - **Only** variables that begin with [`config.kit.env.publicPrefix`](https://svelte.dev/docs/kit/configuration#env) (which defaults to `PUBLIC_`) are included
 * 
 * > [!NOTE] In `dev`, `$env/dynamic` includes environment variables from `.env`. In `prod`, this behavior will depend on your adapter.
 * 
 * > [!NOTE] To get correct types, environment variables referenced in your code should be declared (for example in an `.env` file), even if they don't have a value until the app is deployed:
 * >
 * > ```env
 * > MY_FEATURE_FLAG=
 * > ```
 * >
 * > You can override `.env` values from the command line like so:
 * >
 * > ```sh
 * > MY_FEATURE_FLAG="enabled" npm run dev
 * > ```
 * 
 * For example, given the following runtime environment:
 * 
 * ```env
 * ENVIRONMENT=production
 * PUBLIC_BASE_URL=http://example.com
 * ```
 * 
 * With the default `publicPrefix` and `privatePrefix`:
 * 
 * ```ts
 * import { env } from '$env/dynamic/public';
 * console.log(env.ENVIRONMENT); // => undefined, not public
 * console.log(env.PUBLIC_BASE_URL); // => "http://example.com"
 * ```
 * 
 * ```
 * 
 * ```
 */
declare module '$env/dynamic/public' {
	export const env: {
		[key: `PUBLIC_${string}`]: string | undefined;
	}
}
