export const manifest = (() => {
function __memo(fn) {
	let value;
	return () => value ??= (value = fn());
}

return {
	appDir: "_app",
	appPath: "logos/_app",
	assets: new Set([]),
	mimeTypes: {},
	_: {
		client: {start:"_app/immutable/entry/start.CVSlzSGs.js",app:"_app/immutable/entry/app.CWjXQIGw.js",imports:["_app/immutable/entry/start.CVSlzSGs.js","_app/immutable/chunks/G6v9jxNp.js","_app/immutable/chunks/CqTeO3PQ.js","_app/immutable/chunks/QxawSPH6.js","_app/immutable/entry/app.CWjXQIGw.js","_app/immutable/chunks/CqTeO3PQ.js","_app/immutable/chunks/dijwTkEX.js","_app/immutable/chunks/QxawSPH6.js","_app/immutable/chunks/4QOyMDBh.js","_app/immutable/chunks/yoGPB9fT.js"],stylesheets:[],fonts:[],uses_env_dynamic_public:false},
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
