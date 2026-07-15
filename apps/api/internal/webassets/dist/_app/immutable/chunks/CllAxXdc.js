import{n as e,t}from"./7uQmBizO.js";var n=[{id:`bot:read`,label:`Read`,hint:`View channels, messages, and threads. No write access.`},{id:`bot:write`,label:`Read & write`,hint:`Post and edit messages, send DMs, upload attachments, and publish command menus.`},{id:`bot:admin`,label:`Admin`,hint:`Read & write, publish command menus, and manage channels. Use sparingly.`}];async function r(t){return(await e(`/api/workspaces/${t}/bots`)).bots??[]}async function i(t,n){return e(`/api/workspaces/${t}/bots`,{method:`POST`,body:JSON.stringify(n)})}async function a(t,n){return(await e(`/api/workspaces/${t}/bots/${n}/tokens`)).bot_tokens??[]}async function o(t,n,r){return(await e(`/api/workspaces/${t}/bots/${n}/tokens`,{method:`POST`,body:JSON.stringify(r)})).bot_token}async function s(t){return(await e(`/api/bot-tokens/${t}/revoke`,{method:`POST`,body:JSON.stringify({})})).bot_token}async function c(t,n){await e(`/api/workspaces/${t}/bots/${n}/membership`,{method:`DELETE`})}async function l(t){return(await e(`/api/bots/${t}`,{method:`DELETE`})).deleted_bot}async function u(){return(await e(`/api/me/bots`)).bots??[]}function d(e){if(e instanceof t){if(e.status===401)return`Sign in to manage bots.`;if(e.status===403)return`You don't have permission to manage bots in this workspace.`;if(e.status===404)return`That bot or workspace is no longer available.`;if(e.status===409)return`That handle is already taken. Try another.`;if(e.status===400)return e.message||`That request is invalid.`}return e instanceof Error?e.message:`Something went wrong`}function f(e){return!e.owner_user_id}function p(e){return e?e.filter(e=>!e.revoked_at):[]}function m(e){return e.toLowerCase().replace(/[^a-z0-9]+/g,`-`).replace(/^-+|-+$/g,``).slice(0,32)}function h(e){return e.slug.trim()||e.id}function g(e){return JSON.stringify(e)}function _(e){let t=e.replace(/^@/,``).toUpperCase().replace(/[^A-Z0-9]+/g,`_`).replace(/^_+|_+$/g,``);return t?`CLICKCLACK_${t}_BOT_TOKEN`:`CLICKCLACK_BOT_TOKEN`}function v(e){return`'${e.replaceAll(`'`,`'"'"'`)}'`}function y(e){let t=(e.baseURL||(typeof window<`u`?window.location.origin:``)).replace(/\/$/,``),n=e.botHandle.replace(/^@/,``),r=e.mode===`single`?`CLICKCLACK_BOT_TOKEN`:_(n),i=t||`https://your-clickclack.example.com`,a=e.defaultTo?.trim()||`channel:general`,o=t=>{let n=[`workspace: ${g(e.workspace)},`,`botUserId: ${g(e.botUserID)},`,`defaultTo: ${g(a)},`];return e.allowFrom&&e.allowFrom.length>0&&!e.allowFrom.includes(`*`)&&n.push(`allowFrom: [${e.allowFrom.map(g).join(`, `)}],`),e.agentActivity&&n.push(`agentActivity: true,`),n.map(e=>t+e).join(`
`)};return e.mode===`named`?`{
  channels: {
    clickclack: {
      enabled: true,
      baseUrl: ${g(i)},
      defaultAccount: ${g(n)},
      accounts: {
        ${g(n)}: {
          token: { source: "env", provider: "default", id: ${g(r)} },
${o(`          `)}
        },
      },
    },
  },
}`:`{
  channels: {
    clickclack: {
      enabled: true,
      baseUrl: ${g(i)},
      token: { source: "env", provider: "default", id: ${g(r)} },
${o(`      `)}
    },
  },
}`}function b(e){let t=(e.baseURL||(typeof window<`u`?window.location.origin:``)).replace(/\/$/,``)||`https://your-clickclack.example.com`,n=e.botHandle.replace(/^@/,``);return`openclaw channels add clickclack${e.mode===`named`?` \\\n  --account ${v(n)}`:``} \\
  --base-url ${v(t)} \\
  --token ${v(e.token)} \\
  --workspace ${v(e.workspace)}`}export{b as a,l as c,a as d,r as f,m as g,s as h,y as i,f as l,c as m,p as n,i as o,h as p,d as r,o as s,n as t,u};