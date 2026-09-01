import type { ReactionMutationResponse, RealtimeEventPayload } from "./index";

type Assert<T extends true> = T;

type OpaqueCorrelation = Assert<
  { correlation_id: number | null } extends RealtimeEventPayload ? true : false
>;
type RequiresNarrowing = Assert<
  unknown extends RealtimeEventPayload["correlation_id"] ? true : false
>;
type EmittedPayloadIsObject = Assert<null extends RealtimeEventPayload ? false : true>;
type NoOpReceiptCanBeNull = Assert<
  null extends ReactionMutationResponse["event"]["payload"] ? true : false
>;
