import{n as e,t}from"./7uQmBizO.js";var n=[{id:`bot:read`,label:`Read`,hint:`View channels, messages, and threads. No write access.`},{id:`bot:write`,label:`Read & write`,hint:`Post and edit messages, send DMs, upload attachments, and publish command menus.`},{id:`bot:admin`,label:`Admin`,hint:`Read & write, publish command menus, and manage channels. Use sparingly.`}];async function r(t){return(await e(`/api/workspaces/${t}/bots`)).bots??[]}async function i(t,n){return e(`/api/workspaces/${t}/bots`,{method:`POST`,body:JSON.stringify(n)})}async function a(t,n){return(await e(`/api/workspaces/${t}/bots/${n}/tokens`)).bot_tokens??[]}async function o(t,n,r){return(await e(`/api/workspaces/${t}/bots/${n}/tokens`,{method:`POST`,body:JSON.stringify(r)})).bot_token}async function s(t,n,r){return(await e(`/api/workspaces/${t}/bots/${n}/setup-codes`,{method:`POST`,body:JSON.stringify(r)})).setup_code}async function c(t){return(await e(`/api/bot-tokens/${t}/revoke`,{method:`POST`,body:JSON.stringify({})})).bot_token}async function l(t,n){await e(`/api/workspaces/${t}/bots/${n}/membership`,{method:`DELETE`})}async function u(t){return(await e(`/api/bots/${t}`,{method:`DELETE`})).deleted_bot}async function d(){return(await e(`/api/me/bots`)).bots??[]}function f(e){if(e instanceof t){if(e.status===401)return`Sign in to manage bots.`;if(e.status===403)return`You don't have permission to manage bots in this workspace.`;if(e.status===404)return`That bot or workspace is no longer available.`;if(e.status===409)return`That handle is already taken. Try another.`;if(e.status===400)return e.message||`That request is invalid.`}return e instanceof Error?e.message:`Something went wrong`}function p(e){return!e.owner_user_id}function m(e){return e?e.filter(e=>!e.revoked_at):[]}function h(e){return e.toLowerCase().replace(/[^a-z0-9]+/g,`-`).replace(/^-+|-+$/g,``).slice(0,32)}function g(e){return e.slug.trim()||e.id}function _(e){return JSON.stringify(e)}function v(e){let t=e.replace(/^@/,``).toUpperCase().replace(/[^A-Z0-9]+/g,`_`).replace(/^_+|_+$/g,``);return t?`CLICKCLACK_${t}_BOT_TOKEN`:`CLICKCLACK_BOT_TOKEN`}function y(e){return`'${e.replaceAll(`'`,`'"'"'`)}'`}function b(e){let t=(e.baseURL||(typeof window<`u`?window.location.origin:``)).replace(/\/$/,``),n=e.botHandle.replace(/^@/,``),r=e.mode===`single`?`CLICKCLACK_BOT_TOKEN`:v(n),i=t||`https://your-clickclack.example.com`,a=e.defaultTo?.trim()||`channel:general`,o=t=>{let n=[`workspace: ${_(e.workspace)},`,`botUserId: ${_(e.botUserID)},`,`defaultTo: ${_(a)},`];return e.allowFrom&&e.allowFrom.length>0&&!e.allowFrom.includes(`*`)&&n.push(`allowFrom: [${e.allowFrom.map(_).join(`, `)}],`),e.agentActivity&&n.push(`agentActivity: true,`),n.map(e=>t+e).join(`
`)};return e.mode===`named`?`{
  channels: {
    clickclack: {
      enabled: true,
      baseUrl: ${_(i)},
      defaultAccount: ${_(n)},
      accounts: {
        ${_(n)}: {
          token: { source: "env", provider: "default", id: ${_(r)} },
${o(`          `)}
        },
      },
    },
  },
}`:`{
  channels: {
    clickclack: {
      enabled: true,
      baseUrl: ${_(i)},
      token: { source: "env", provider: "default", id: ${_(r)} },
${o(`      `)}
    },
  },
}`}function x(e){let t=(e.baseURL||(typeof window<`u`?window.location.origin:``)).replace(/\/$/,``)||`https://your-clickclack.example.com`,n=e.botHandle.replace(/^@/,``);return`openclaw channels add clickclack${e.mode===`named`?` --account ${y(n)}`:``} --code ${y(`${t}/#${e.code}`)}`}function S(e){let t=(e.baseURL||(typeof window<`u`?window.location.origin:``)).replace(/\/$/,``)||`https://your-clickclack.example.com`,n=e.botHandle.replace(/^@/,``);return`openclaw channels add clickclack${e.mode===`named`?` \\\n  --account ${y(n)}`:``} \\
  --base-url ${y(t)} \\
  --token ${y(e.token)} \\
  --workspace ${y(e.workspace)}`}export{c as _,b as a,s as c,p as d,d as f,l as g,g as h,x as i,o as l,r as m,m as n,S as o,a as p,f as r,i as s,n as t,u,h as v};