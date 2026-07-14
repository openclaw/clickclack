// The app catalog: manifest-driven guided installs (Model 1 — agents
// connect INTO ClickClack via bot tokens; ClickClack never embeds agent
// runtimes). Each manifest describes how to install one agent platform:
// what the bot should look like, which token scopes it needs, what
// config the wizard collects, and how to render the platform-specific
// setup snippet. Adding support for a new agent platform means adding
// one manifest here — no core changes.
import {
  buildOpenClawConfigSnippet,
  buildOpenClawShellSnippet,
  type BotScopeBundle,
  type OpenClawAccountMode,
} from "./bots";

// Dedicated scope required for durable agent activity rows (streamed
// "Thinking…" commentary + tool progress). Deliberately excluded from the
// bot:* bundles on the backend; installs opt in explicitly.
export const AGENT_ACTIVITY_SCOPE = "agent_activity:write";

export type AppConfigFieldId = "default_channel" | "allow_from" | "agent_activity";

export type AppConfigField = {
  id: AppConfigFieldId;
  label: string;
  hint: string;
};

export type AppSnippetInput = {
  workspace: string;
  botHandle: string;
  botUserID: string;
  token: string;
  mode: OpenClawAccountMode;
  defaultTo?: string;
  allowFrom?: string[];
  agentActivity?: boolean;
};

export type AppManifest = {
  slug: string;
  name: string;
  description: string;
  // 24×24 stroke icon path data, matching the settings rail convention.
  icon: string[];
  suggestedScopeBundle: BotScopeBundle;
  suggestedBotName: string;
  suggestedBotHandle: string;
  configFields: AppConfigField[];
  // null → no platform snippet (generic install; the token reveal is enough).
  buildConfigSnippet: ((input: AppSnippetInput) => string) | null;
  buildShellSnippet: ((input: AppSnippetInput) => string) | null;
};

export const APP_CATALOG: AppManifest[] = [
  {
    slug: "openclaw",
    name: "OpenClaw",
    description:
      "Connect an OpenClaw agent as a bot user. OpenClaw runs outside ClickClack and connects in over realtime using the bot token minted here.",
    icon: [
      "M12 8V4H8",
      "M5 4h14a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2Z",
      "M9 13v2",
      "M15 13v2",
    ],
    suggestedScopeBundle: "bot:write",
    suggestedBotName: "OpenClaw",
    suggestedBotHandle: "openclaw",
    configFields: [
      {
        id: "default_channel",
        label: "Default channel",
        hint: "Where the agent sends messages when no target is specified.",
      },
      {
        id: "allow_from",
        label: "Who can talk to this agent",
        hint: "Everyone in the workspace, or only specific members.",
      },
      {
        id: "agent_activity",
        label: "Agent activity",
        hint: "Stream the agent's thinking and tool progress into the conversation as it works. Grants the agent_activity:write token scope.",
      },
    ],
    buildConfigSnippet: (input) =>
      buildOpenClawConfigSnippet({
        workspace: input.workspace,
        botHandle: input.botHandle,
        botUserID: input.botUserID,
        mode: input.mode,
        defaultTo: input.defaultTo,
        allowFrom: input.allowFrom,
        agentActivity: input.agentActivity,
      }),
    buildShellSnippet: (input) =>
      buildOpenClawShellSnippet({
        botHandle: input.botHandle,
        token: input.token,
        mode: input.mode,
      }),
  },
  {
    slug: "custom",
    name: "Custom app",
    description:
      "Any external app or script that talks to the ClickClack API with a bot token. No platform-specific setup — you get the bot, the token, and the API.",
    icon: [
      "M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z",
    ],
    suggestedScopeBundle: "bot:write",
    suggestedBotName: "",
    suggestedBotHandle: "",
    configFields: [],
    buildConfigSnippet: null,
    buildShellSnippet: null,
  },
];

export function manifestBySlug(slug: string): AppManifest | undefined {
  return APP_CATALOG.find((manifest) => manifest.slug === slug);
}

// Display metadata for installations whose slug has no manifest (installed
// via the API directly). Falls back to a generic plug icon.
export function manifestForInstallation(appSlug: string): AppManifest {
  return (
    manifestBySlug(appSlug) ?? {
      slug: appSlug,
      name: appSlug,
      description: "",
      icon: ["M9 2v6", "M15 2v6", "M12 17v5", "M5 8h14l-1 7a4 4 0 0 1-4 4h-4a4 4 0 0 1-4-4L5 8Z"],
      suggestedScopeBundle: "bot:write",
      suggestedBotName: "",
      suggestedBotHandle: "",
      configFields: [],
      buildConfigSnippet: null,
      buildShellSnippet: null,
    }
  );
}
