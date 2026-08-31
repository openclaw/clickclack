import type { ClickClackClient, Message, Thread, ThreadPage, ThreadState } from "./index";

type Assert<T extends true> = T;
type IsAssignable<Input, Target> = Input extends Target ? true : false;

type LegacyThread = {
  root: Message;
  replies: Message[];
  thread_state: ThreadState;
};

type _LegacyThreadAllowed = Assert<IsAssignable<LegacyThread, Thread>>;

type GetResult = Awaited<ReturnType<ClickClackClient["threads"]["get"]>>;
type _GetResultIsThreadPage = Assert<IsAssignable<GetResult, ThreadPage>>;
type _GetResultIncludesRequiredPaging = Assert<
  IsAssignable<
    GetResult,
    LegacyThread & {
      oldest_seq: number;
      newest_seq: number;
      has_older: boolean;
      has_newer: boolean;
    }
  >
>;

type ChannelMessage = Awaited<ReturnType<ClickClackClient["channels"]["messages"]>>[number];
type DirectMessage = Awaited<ReturnType<ClickClackClient["dms"]["messages"]>>[number];
type _ChannelSummaryAvailable = Assert<
  IsAssignable<ChannelMessage["thread_state"], ThreadState | undefined>
>;
type _DirectSummaryAvailable = Assert<
  IsAssignable<DirectMessage["thread_state"], ThreadState | undefined>
>;
type _AttachmentsPreserved = Assert<
  IsAssignable<NonNullable<Message["attachments"]>[number], import("./index").Upload>
>;
