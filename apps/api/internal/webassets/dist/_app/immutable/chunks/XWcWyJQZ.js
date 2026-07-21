import{n as e,r as t,t as n}from"./BEqsQr3A.js";var r=[{id:`bot:read`,label:`Read`,hint:`View channels, messages, and threads. No write access.`},{id:`bot:write`,label:`Read & write`,hint:`Post and edit messages, send DMs, upload attachments, and publish command menus.`},{id:`bot:admin`,label:`Admin`,hint:`Read & write, publish command menus, and manage channels. Use sparingly.`}];async function i(t){return(await e(`/api/workspaces/${t}/bots`)).bots??[]}async function a(t,n){return e(`/api/workspaces/${t}/bots`,{method:`POST`,body:JSON.stringify(n)})}async function o(t,n){return(await e(`/api/workspaces/${t}/bots/${n}/tokens`)).bot_tokens??[]}async function s(t,n,r){return(await e(`/api/workspaces/${t}/bots/${n}/tokens`,{method:`POST`,body:JSON.stringify(r)})).bot_token}async function c(t,n,r){return(await e(`/api/workspaces/${t}/bots/${n}/setup-codes`,{method:`POST`,body:JSON.stringify(r)})).setup_code}async function l(t){return(await e(`/api/bot-tokens/${t}/revoke`,{method:`POST`,body:JSON.stringify({})})).bot_token}async function u(t,n){await e(`/api/workspaces/${t}/bots/${n}/membership`,{method:`DELETE`})}async function d(t){return(await e(`/api/bots/${t}`,{method:`DELETE`})).deleted_bot}async function f(){return(await e(`/api/me/bots`)).bots??[]}function p(e){if(e instanceof n){if(e.status===401)return`Sign in to manage bots.`;if(e.status===403)return`You don't have permission to manage bots in this workspace.`;if(e.status===404)return`That bot or workspace is no longer available.`;if(e.status===409)return`That handle is already taken. Try another.`;if(e.status===400)return e.message||`That request is invalid.`}return e instanceof Error?e.message:`Something went wrong`}function m(e){return!e.owner_user_id}function h(e){return e?e.filter(e=>!e.revoked_at):[]}function g(e){return e.toLowerCase().replace(/[^a-z0-9]+/g,`-`).replace(/^-+|-+$/g,``).slice(0,32)}function _(e){return e.slug.trim()||e.id}function v(e){return JSON.stringify(e)}function y(e){let t=e.replace(/^@/,``).toUpperCase().replace(/[^A-Z0-9]+/g,`_`).replace(/^_+|_+$/g,``);return t?`CLICKCLACK_${t}_BOT_TOKEN`:`CLICKCLACK_BOT_TOKEN`}function b(e){return`'${e.replaceAll(`'`,`'"'"'`)}'`}function x(e){let n=(e.baseURL||t()||(typeof window<`u`?window.location.origin:``)).replace(/\/$/,``),r=e.botHandle.replace(/^@/,``),i=e.mode===`single`?`CLICKCLACK_BOT_TOKEN`:y(r),a=n||`https://your-clickclack.example.com`,o=e.defaultTo?.trim()||`channel:general`,s=t=>{let n=[`workspace: ${v(e.workspace)},`,`botUserId: ${v(e.botUserID)},`,`defaultTo: ${v(o)},`];return e.allowFrom&&e.allowFrom.length>0&&!e.allowFrom.includes(`*`)&&n.push(`allowFrom: [${e.allowFrom.map(v).join(`, `)}],`),e.agentActivity&&n.push(`agentActivity: true,`),n.map(e=>t+e).join(`
`)};return e.mode===`named`?`{
  channels: {
    clickclack: {
      enabled: true,
      baseUrl: ${v(a)},
      defaultAccount: ${v(r)},
      accounts: {
        ${v(r)}: {
          token: { source: "env", provider: "default", id: ${v(i)} },
${s(`          `)}
        },
      },
    },
  },
}`:`{
  channels: {
    clickclack: {
      enabled: true,
      baseUrl: ${v(a)},
      token: { source: "env", provider: "default", id: ${v(i)} },
${s(`      `)}
    },
  },
}`}function S(e){let n=e.frontendURL||(typeof window<`u`?window.location.origin:``),r=(e.apiBaseURL||t()).replace(/\/$/,``),i=(e.baseURL||r||n).replace(/\/$/,``),a=(e.claimURL||``).replace(/\/$/,``),o=a&&r&&r!==n?a:i||`https://your-clickclack.example.com/`,s=e.botHandle.replace(/^@/,``),c=e.mode===`named`?` --account ${b(s)}`:``,l=o.endsWith(`/`)?`#`:`/#`;return`openclaw channels add clickclack${c} --code ${b(a&&o===a?`${o}#${e.code}`:`${o}${l}${e.code}`)}`}function C(e){let n=(e.baseURL||t()||(typeof window<`u`?window.location.origin:``)).replace(/\/$/,``)||`https://your-clickclack.example.com`,r=e.botHandle.replace(/^@/,``);return`openclaw channels add clickclack${e.mode===`named`?` \\\n  --account ${b(r)}`:``} \\
  --base-url ${b(n)} \\
  --token ${b(e.token)} \\
  --workspace ${b(e.workspace)}`}export{l as _,x as a,c,m as d,f,u as g,_ as h,S as i,s as l,i as m,h as n,C as o,o as p,p as r,a as s,r as t,d as u,g as v};