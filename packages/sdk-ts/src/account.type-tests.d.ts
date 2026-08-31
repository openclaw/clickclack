import type { ClickClackClient, User } from "./index";

type Assert<T extends true> = T;
type IsAssignable<Input, Target> = Input extends Target ? true : false;
type UpdateMeInput = Parameters<ClickClackClient["updateMe"]>[0];

type _HandleOnlyAllowed = Assert<IsAssignable<{ handle: "captain" }, UpdateMeInput>>;
type _NotificationOnlyAllowed = Assert<
  IsAssignable<
    { notification_settings: { pushover_enabled: false; pushover_user_key: "" } },
    UpdateMeInput
  >
>;
type _AppearanceOnlyAllowed = Assert<
  IsAssignable<{ appearance_preferences: { board_theme: "iris" } }, UpdateMeInput>
>;
type _InvalidAppearanceRejected = Assert<
  IsAssignable<{ appearance_preferences: { board_theme: "invalid" } }, UpdateMeInput> extends false
    ? true
    : false
>;

type LegacyUser = {
  id: string;
  kind: "human" | "bot";
  owner_user_id?: string;
  display_name: string;
  handle: string;
  former_handle?: string;
  deleted_at?: string;
  avatar_url: string;
  created_at: string;
};
type _LegacyUserAllowed = Assert<IsAssignable<LegacyUser, User>>;
type _UserStillAssignableToLegacy = Assert<IsAssignable<User, LegacyUser>>;
type MeResult = Awaited<ReturnType<ClickClackClient["me"]>>;
type _NotificationsReadable = MeResult["notification_settings"];
type _AppearanceReadable = MeResult["appearance_preferences"];
