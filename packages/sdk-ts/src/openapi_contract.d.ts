import type { components } from "./generated/openapi";

type Assert<T extends true> = T;
type CreateChannelRequest = components["schemas"]["CreateChannelRequest"];
type CreateMessageRequest = components["schemas"]["CreateMessageRequest"];
type RevokeAppInstallationRequest = components["schemas"]["RevokeAppInstallationRequest"];
type ReconcileManagedChannelRequest = components["schemas"]["ReconcileManagedChannelRequest"];

type CreateChannelDefaultsStayOptional = Assert<
  { name: string } extends CreateChannelRequest ? true : false
>;
type CreateMessageDefaultsStayOptional = Assert<
  { body: string } extends CreateMessageRequest ? true : false
>;
type RevokeAppInstallationOptionsStayOptional = Assert<
  {} extends RevokeAppInstallationRequest ? true : false
>;

type ReconcileManagedChannelDefaultsStayOptional = Assert<
  {
    external_provider: string;
    external_ref: string;
    name: string;
  } extends ReconcileManagedChannelRequest
    ? true
    : false
>;
