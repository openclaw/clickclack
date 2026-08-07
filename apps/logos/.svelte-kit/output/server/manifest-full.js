export const manifest = (() => {
function __memo(fn) {
	let value;
	return () => value ??= (value = fn());
}

return {
	appDir: "_app",
	appPath: "_app",
	assets: new Set([]),
	mimeTypes: {},
	_: {
		client: {start:"_app/immutable/entry/start.CAD5a-7w.js",app:"_app/immutable/entry/app.DkqxAfI1.js",imports:["_app/immutable/entry/start.CAD5a-7w.js","_app/immutable/chunks/B9wd6sCs.js","_app/immutable/chunks/jS0bYa_Q.js","_app/immutable/chunks/C3KNXKw0.js","_app/immutable/entry/app.DkqxAfI1.js","_app/immutable/chunks/jS0bYa_Q.js","_app/immutable/chunks/DKtNoAb-.js","_app/immutable/chunks/C3KNXKw0.js","_app/immutable/chunks/wMsLtkI4.js","_app/immutable/chunks/_QqGUZMI.js"],stylesheets:[],fonts:[],uses_env_dynamic_public:false},
		nodes: [
			__memo(() => import('./nodes/0.js')),
			__memo(() => import('./nodes/1.js')),
			__memo(() => import('./nodes/2.js'))
		],
		remotes: {
			
		},
		routes: [
			{
				id: "/",
				pattern: /^\/$/,
				params: [],
				page: { layouts: [0,], errors: [1,], leaf: 2 },
				endpoint: null
			}
		],
		prerendered_routes: new Set([]),
		matchers: async () => {
			
			return {  };
		},
		server_assets: {}
	}
}
})();
