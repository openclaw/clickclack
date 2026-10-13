import type { ClickClackClient, MessageQuestion, QuestionSpec } from "./index";

type Assert<T extends true> = T;
type IsAssignable<Input, Target> = Input extends Target ? true : false;
type QuestionWithoutSkip = {
  expires_at: "2026-10-14T12:00:00Z";
  items: [{ id: "day"; header: "Day"; prompt: "Which day?" }];
};
type ChannelMessageInput = Parameters<ClickClackClient["channels"]["sendMessage"]>[1];

type _OmittedSkipAllowed = Assert<IsAssignable<QuestionWithoutSkip, QuestionSpec>>;
type _ExplicitFalseAllowed = Assert<
  IsAssignable<QuestionWithoutSkip & { allow_skip: false }, QuestionSpec>
>;
type _ChannelInputOmittedSkipAllowed = Assert<
  IsAssignable<{ body: "Which day?"; question: QuestionWithoutSkip }, ChannelMessageInput>
>;
type _ChannelInputExplicitFalseAllowed = Assert<
  IsAssignable<
    { body: "Which day?"; question: QuestionWithoutSkip & { allow_skip: false } },
    ChannelMessageInput
  >
>;
type _ResponseSkipRequired = Assert<IsAssignable<MessageQuestion["allow_skip"], boolean>>;
