import{n as e,t}from"./7uQmBizO.js";var n=[{id:`bot:read`,label:`Read`,hint:`View channels, messages, and threads. No write access.`},{id:`bot:write`,label:`Read & write`,hint:`Post and edit messages, send DMs, upload attachments.`},{id:`bot:admin`,label:`Admin`,hint:`Read & write plus manage channels. Use sparingly.`}];async function r(t){return(await e(`/api/workspaces/${t}/bots`)).bots??[]}async function i(t,n){return e(`/api/workspaces/${t}/bots`,{method:`POST`,body:JSON.stringify(n)})}async function a(t,n){return(await e(`/api/workspaces/${t}/bots/${n}/tokens`)).bot_tokens??[]}async function o(t,n,r){return(await e(`/api/workspaces/${t}/bots/${n}/tokens`,{method:`POST`,body:JSON.stringify(r)})).bot_token}async function s(t){return(await e(`/api/bot-tokens/${t}/revoke`,{method:`POST`,body:JSON.stringify({})})).bot_token}async function c(t,n){await e(`/api/workspaces/${t}/bots/${n}/membership`,{method:`DELETE`})}async function l(){return(await e(`/api/me/bots`)).bots??[]}function u(e){if(e instanceof t){if(e.status===401)return`Sign in to manage bots.`;if(e.status===403)return`You don't have permission to manage bots in this workspace.`;if(e.status===404)return`That bot or workspace is no longer available.`;if(e.status===409)return`That handle is already taken. Try another.`;if(e.status===400)return e.message||`That request is invalid.`}return e instanceof Error?e.message:`Something went wrong`}function d(e){return!e.owner_user_id}function f(e){return e?e.filter(e=>!e.revoked_at):[]}function p(e){return e.toLowerCase().replace(/[^a-z0-9]+/g,`-`).replace(/^-+|-+$/g,``).slice(0,32)}function m(e){return JSON.stringify(e)}function h(e){let t=e.replace(/^@/,``).toUpperCase().replace(/[^A-Z0-9]+/g,`_`).replace(/^_+|_+$/g,``);return t?`CLICKCLACK_${t}_BOT_TOKEN`:`CLICKCLACK_BOT_TOKEN`}function g(e){return`'${e.replaceAll(`'`,`'"'"'`)}'`}function _(e){let t=(e.baseURL||(typeof window<`u`?window.location.origin:``)).replace(/\/$/,``),n=e.botHandle.replace(/^@/,``),r=e.mode===`single`?`CLICKCLACK_BOT_TOKEN`:h(n),i=t||`https://your-clickclack.example.com`;return e.mode===`named`?`{
  channels: {
    clickclack: {
      enabled: true,
      baseUrl: ${m(i)},
      defaultAccount: ${m(n)},
      accounts: {
        ${m(n)}: {
          token: { source: "env", provider: "default", id: ${m(r)} },
          workspace: ${m(e.workspaceRouteID)},
          botUserId: ${m(e.botUserID)},
          defaultTo: "channel:general",
        },
      },
    },
  },
}`:`{
  channels: {
    clickclack: {
      enabled: true,
      baseUrl: ${m(i)},
      token: { source: "env", provider: "default", id: ${m(r)} },
      workspace: ${m(e.workspaceRouteID)},
      botUserId: ${m(e.botUserID)},
      defaultTo: "channel:general",
    },
  },
}`}function v(e){return`export ${e.mode===`single`?`CLICKCLACK_BOT_TOKEN`:h(e.botHandle)}=${g(e.token)}
openclaw gateway`}export{v as a,d as c,r as d,c as f,_ as i,l,p as m,f as n,i as o,s as p,u as r,o as s,n as t,a as u};