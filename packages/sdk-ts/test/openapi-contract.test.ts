import type {
  ClickClackClient,
  Message,
  MessageListOptions,
  MessagePage,
  NotificationSettings,
  ThreadState,
  User,
} from "../src/index";
import type { components, operations } from "../src/generated/openapi";

type Equal<Left, Right> =
  (<Value>() => Value extends Left ? 1 : 2) extends <Value>() => Value extends Right ? 1 : 2
    ? true
    : false;
type Expect<Value extends true> = Value;
type Includes<Union, Value> = Value extends Union ? true : false;
type IsAssignable<Value, Target> = Value extends Target ? true : false;

type CreateMessageBody = operations["createMessage"]["requestBody"]["content"]["application/json"];
type UpdateMessageBody = operations["updateMessage"]["requestBody"]["content"]["application/json"];
type _CreateMessageRequest = Expect<
  Equal<CreateMessageBody, components["schemas"]["CreateMessageRequest"]>
>;
type _UpdateMessageRequest = Expect<
  Equal<UpdateMessageBody, components["schemas"]["UpdateMessageRequest"]>
>;

type MessageQuery = NonNullable<operations["listMessages"]["parameters"]["query"]>;
type DirectMessageQuery = NonNullable<operations["listDirectMessages"]["parameters"]["query"]>;
type ThreadQuery = NonNullable<operations["getThread"]["parameters"]["query"]>;
type SearchQuery = operations["search"]["parameters"]["query"];
type _MessageBefore = Expect<Equal<MessageQuery["before_seq"], number | undefined>>;
type _MessageAround = Expect<Equal<MessageQuery["around_seq"], number | undefined>>;
type _DirectBefore = Expect<Equal<DirectMessageQuery["before_seq"], number | undefined>>;
type _DirectAround = Expect<Equal<DirectMessageQuery["around_seq"], number | undefined>>;
type _ThreadLimit = Expect<Equal<ThreadQuery["limit"], number | undefined>>;
type _SearchLimit = Expect<Equal<SearchQuery["limit"], number | undefined>>;

type EphemeralType = components["schemas"]["EphemeralEventRequest"]["type"];
type _AgentProgress = Expect<Includes<EphemeralType, "agent.progress">>;
type _MessagePageResponse = Expect<
  Equal<
    operations["listMessages"]["responses"][200]["content"]["application/json"],
    components["schemas"]["MessagePage"]
  >
>;
type _DirectMessagePageResponse = Expect<
  Equal<
    operations["listDirectMessages"]["responses"][200]["content"]["application/json"],
    components["schemas"]["MessagePage"]
  >
>;
type _SDKMessagePageMessages = Expect<Equal<MessagePage["messages"], Message[]>>;
type _SDKMessagePageMetadata = Expect<
  Equal<Omit<MessagePage, "messages">, Omit<components["schemas"]["MessagePage"], "messages">>
>;
type SlashCommandCallbackResponse = components["schemas"]["SlashCommandCallbackResponse"];
type _SlashCallbackMessage = Expect<
  Equal<
    SlashCommandCallbackResponse["message"],
    components["schemas"]["Message"] | null | undefined
  >
>;
type _SlashCallbackEvent = Expect<
  Equal<SlashCommandCallbackResponse["event"], components["schemas"]["Event"] | null | undefined>
>;
type _ReactionMutationEvent = Expect<
  Equal<
    components["schemas"]["ReactionMutationResponse"]["event"],
    components["schemas"]["Event"] | null | undefined
  >
>;
type _EphemeralMutationEvent = Expect<
  Equal<components["schemas"]["EventMutationResponse"]["event"], components["schemas"]["Event"]>
>;
type _AuthSessionResponse = Expect<
  Equal<
    operations["consumeMagicLink"]["responses"][200]["content"]["application/json"],
    components["schemas"]["AuthSessionResponse"]
  >
>;

type UpdateMeBody = operations["updateMe"]["requestBody"]["content"]["application/json"];
type UpdateMeInput = Parameters<ClickClackClient["updateMe"]>[0];
type _UpdateMeRequest = Expect<Equal<UpdateMeBody, components["schemas"]["UpdateMeRequest"]>>;
type _UpdateMeDisplayName = Expect<Equal<UpdateMeBody["display_name"], string | undefined>>;
type _UpdateMeNotificationSettings = Expect<
  Equal<
    UpdateMeBody["notification_settings"],
    components["schemas"]["NotificationSettingsPatch"] | undefined
  >
>;
type _NotificationSettingsPatch = Expect<
  Equal<components["schemas"]["NotificationSettingsPatch"], Partial<NotificationSettings>>
>;
type _SDKUpdateMeDisplayName = Expect<Equal<UpdateMeInput["display_name"], string | undefined>>;
type _SDKUpdateMeNotificationSettings = Expect<
  Equal<UpdateMeInput["notification_settings"], Partial<NotificationSettings> | undefined>
>;

type _NotificationSettings = Expect<
  Equal<NonNullable<User["notification_settings"]>, NotificationSettings>
>;
type _ThreadState = Expect<Equal<NonNullable<Message["thread_state"]>, ThreadState>>;
type ChannelMessageOptions = NonNullable<Parameters<ClickClackClient["channels"]["messages"]>[1]>;
type _ChannelMessageOptions = Expect<Includes<ChannelMessageOptions, MessageListOptions>>;
type ChannelMessagesResult = Awaited<ReturnType<ClickClackClient["channels"]["messages"]>>;
type ChannelMessagesPageResult = Awaited<ReturnType<ClickClackClient["channels"]["messagesPage"]>>;
type DirectMessagesResult = Awaited<ReturnType<ClickClackClient["dms"]["messages"]>>;
type DirectMessagesPageResult = Awaited<ReturnType<ClickClackClient["dms"]["messagesPage"]>>;
type _ChannelMessagesCompatibility = Expect<Equal<ChannelMessagesResult, Message[]>>;
type _ChannelMessagesPage = Expect<Equal<ChannelMessagesPageResult, MessagePage>>;
type _DirectMessagesCompatibility = Expect<Equal<DirectMessagesResult, Message[]>>;
type _DirectMessagesPage = Expect<Equal<DirectMessagesPageResult, MessagePage>>;
type _MessageListLatest = Expect<IsAssignable<{ limit: number }, MessageListOptions>>;
type _MessageListAfter = Expect<
  IsAssignable<{ afterSeq: number; limit: number }, MessageListOptions>
>;
type _MessageListBefore = Expect<IsAssignable<{ beforeSeq: number }, MessageListOptions>>;
type _MessageListAround = Expect<IsAssignable<{ aroundSeq: number }, MessageListOptions>>;
type _MessageListRejectsAfterBefore = Expect<
  Equal<IsAssignable<{ afterSeq: number; beforeSeq: number }, MessageListOptions>, false>
>;
type _MessageListRejectsAfterAround = Expect<
  Equal<IsAssignable<{ afterSeq: number; aroundSeq: number }, MessageListOptions>, false>
>;
type _MessageListRejectsBeforeAround = Expect<
  Equal<IsAssignable<{ beforeSeq: number; aroundSeq: number }, MessageListOptions>, false>
>;
type PublishedEphemeralType = Parameters<ClickClackClient["events"]["publishEphemeral"]>[0]["type"];
type _PublishedAgentProgress = Expect<Includes<PublishedEphemeralType, "agent.progress">>;
