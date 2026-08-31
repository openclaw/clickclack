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
