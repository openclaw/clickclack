import type { AgentProgressPayload, EphemeralEventInput } from "./index";

type Assert<T extends true> = T;
type IsAssignable<Input, Target> = Input extends Target ? true : false;
type IsRejected<Input, Target> = Input extends Target ? false : true;

type ProgressPayload = {
  turn_id: "turn-1";
  op: "append";
  line: { id: "tool-1"; kind: "tool"; tool_name: "shell"; status: "running" };
};

type _WorkspacePresenceAllowed = Assert<
  IsAssignable<
    { workspaceId: "wsp-1"; type: "presence.changed"; payload: { status: "away" } },
    EphemeralEventInput
  >
>;
type _TargetedPresenceAllowed = Assert<
  IsAssignable<
    { workspaceId: "wsp-1"; channelId: "chn-1"; type: "presence.changed" },
    EphemeralEventInput
  >
>;
type _TargetedTypingAllowed = Assert<
  IsAssignable<
    { workspaceId: "wsp-1"; directConversationId: "dm-1"; type: "typing.started" },
    EphemeralEventInput
  >
>;
type _TargetedProgressAllowed = Assert<
  IsAssignable<
    {
      workspaceId: "wsp-1";
      channelId: "chn-1";
      type: "agent.progress";
      payload: ProgressPayload;
    },
    EphemeralEventInput
  >
>;
type _ClearProgressAllowed = Assert<
  IsAssignable<{ turn_id: "turn-1"; op: "clear" }, AgentProgressPayload>
>;
type _TargetlessTypingRejected = Assert<
  IsRejected<{ workspaceId: "wsp-1"; type: "typing.stopped" }, EphemeralEventInput>
>;
type _TargetlessProgressRejected = Assert<
  IsRejected<
    {
      workspaceId: "wsp-1";
      type: "agent.progress";
      payload: ProgressPayload;
    },
    EphemeralEventInput
  >
>;
type _DualTargetProgressRejected = Assert<
  IsRejected<
    {
      workspaceId: "wsp-1";
      channelId: "chn-1";
      directConversationId: "dm-1";
      type: "agent.progress";
      payload: ProgressPayload;
    },
    EphemeralEventInput
  >
>;
type _ProgressLineRequired = Assert<
  IsRejected<{ turn_id: "turn-1"; op: "append" }, AgentProgressPayload>
>;
