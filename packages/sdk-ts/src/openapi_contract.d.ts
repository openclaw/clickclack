import type { components } from "./generated/openapi";

type Assert<T extends true> = T;
type RevokeAppInstallationRequest = components["schemas"]["RevokeAppInstallationRequest"];

type RevokeAppInstallationOptionsStayOptional = Assert<
  {} extends RevokeAppInstallationRequest ? true : false
>;
