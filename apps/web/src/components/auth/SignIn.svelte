<script lang="ts">
  import { api, apiURL, authMethods, readableAPIError } from "../../lib/api";
  import { desktop } from "../../lib/desktop";
  import KeystrokeMark from "../KeystrokeMark.svelte";

  const integratedTitleBar = desktop?.integratedTitleBar === true;
  let desktopAuthStatus = $state("");
  const enabledAuthMethods = authMethods();
  const githubAuthEnabled = enabledAuthMethods.includes("github");
  const passwordAuthEnabled = enabledAuthMethods.includes("password");
  let passwordIdentifier = $state("");
  let passwordSecret = $state("");
  let magicToken = $state("");
  let authSubmitting = $state(false);
  let authError = $state("");
  const authLead = passwordAuthEnabled
    ? githubAuthEnabled
      ? "Sign in with your ClickClack account, or continue with GitHub."
      : "Sign in with your ClickClack account."
    : githubAuthEnabled
      ? "Sign in with GitHub to join the guest room."
      : "Sign in with a token from your ClickClack administrator.";
  const authFoot = githubAuthEnabled && !passwordAuthEnabled ? "Any GitHub account can join." : "";

  async function signInWithGitHub(event: MouseEvent) {
    if (!desktop) return;
    event.preventDefault();
    desktopAuthStatus = "Opening GitHub in your browser…";
    try {
      await desktop.signInWithGitHub();
      desktopAuthStatus = "Finish signing in in your browser. ClickClack will complete here automatically.";
    } catch {
      desktopAuthStatus = "Could not open your browser. Try again.";
    }
  }

  // A fresh document prevents account-scoped state from crossing sign-ins.
  async function completeSignIn(path: string, body: Record<string, string>) {
    if (authSubmitting) return;
    authSubmitting = true;
    authError = "";
    try {
      await api(path, { method: "POST", body: JSON.stringify(body) });
      window.location.reload();
    } catch (error) {
      authError = readableAPIError(error, "Could not sign in.");
      authSubmitting = false;
    }
  }

  function submitPasswordLogin(event: SubmitEvent) {
    event.preventDefault();
    void completeSignIn("/api/auth/password/login", {
      identifier: passwordIdentifier,
      password: passwordSecret,
    });
  }

  function submitMagicToken(event: SubmitEvent) {
    event.preventDefault();
    void completeSignIn("/api/auth/magic/consume", { token: magicToken });
  }

</script>

{#if integratedTitleBar && desktop}
  <div class="desktop-auth-titlebar" data-platform={desktop.platform} aria-hidden="true"></div>
{/if}
<main class="auth-shell">
  <section class="auth-panel" aria-label="Sign in">
    <div class="auth-brand">
      <KeystrokeMark class="mark" size={44} />
      <div class="brand-text">
        <strong>ClickClack</strong>
        <span>OpenClaw workspace chat</span>
      </div>
    </div>
    <div class="auth-copy">
      <h1>Welcome.</h1>
      <p>{authLead}</p>
    </div>
    {#if passwordAuthEnabled}
      <form class="auth-form" onsubmit={submitPasswordLogin}>
        <label class="field">
          <span>Email or username</span>
          <input
            bind:value={passwordIdentifier}
            autocomplete="username"
            name="identifier"
            required
            type="text"
          />
        </label>
        <label class="field">
          <span>Password</span>
          <input
            bind:value={passwordSecret}
            autocomplete="current-password"
            name="password"
            required
            type="password"
          />
        </label>
        <button class="auth-submit" type="submit" disabled={authSubmitting}>
          {authSubmitting ? "Signing in..." : "Sign in"}
        </button>
      </form>
    {/if}
    {#if githubAuthEnabled}
      {#if passwordAuthEnabled}
        <p class="auth-divider"><span>or</span></p>
      {/if}
      <a class="github-login" href={apiURL("/api/auth/github/start")} onclick={signInWithGitHub}>
        <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
          <path fill="currentColor" d="M12 .5C5.65.5.5 5.65.5 12c0 5.08 3.29 9.39 7.86 10.91.58.1.79-.25.79-.56v-2c-3.2.69-3.87-1.37-3.87-1.37-.52-1.32-1.27-1.67-1.27-1.67-1.04-.71.08-.7.08-.7 1.15.08 1.76 1.18 1.76 1.18 1.02 1.75 2.68 1.25 3.34.96.1-.74.4-1.25.73-1.54-2.55-.29-5.24-1.28-5.24-5.69 0-1.26.45-2.29 1.18-3.1-.12-.29-.51-1.46.11-3.05 0 0 .96-.31 3.15 1.18a10.94 10.94 0 0 1 5.74 0c2.19-1.49 3.15-1.18 3.15-1.18.62 1.59.23 2.76.12 3.05.74.81 1.18 1.84 1.18 3.1 0 4.42-2.69 5.39-5.25 5.68.41.36.78 1.06.78 2.13v3.16c0 .31.21.67.8.56 4.56-1.52 7.85-5.83 7.85-10.91C23.5 5.65 18.35.5 12 .5z"/>
        </svg>
        Continue with GitHub
      </a>
    {/if}
    {#if !desktop}
      <a class="openclaw-login" href={apiURL("/api/auth/openclaw/start")}>
        <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
          <path
            fill="currentColor"
            d="M12 2a10 10 0 1 0 10 10A10 10 0 0 0 12 2Zm0 3.5A6.5 6.5 0 1 1 5.5 12 6.51 6.51 0 0 1 12 5.5Zm0 3A3.5 3.5 0 1 0 15.5 12 3.5 3.5 0 0 0 12 8.5Z"
          />
        </svg>
        Sign in with OpenClaw ID
      </a>
    {/if}
    {#if authError}
      <p class="auth-error" role="alert">{authError}</p>
    {/if}
    <details class="auth-magic" open={!githubAuthEnabled && !passwordAuthEnabled}>
      <summary>Have a sign-in token?</summary>
      <form class="auth-form" onsubmit={submitMagicToken}>
        <label class="field">
          <span>Sign-in token</span>
          <input
            bind:value={magicToken}
            autocomplete="one-time-code"
            name="token"
            required
            type="text"
          />
        </label>
        <button class="auth-submit auth-submit-quiet" type="submit" disabled={authSubmitting}>
          Use token
        </button>
      </form>
    </details>
    {#if desktopAuthStatus || authFoot}
      <p class="auth-foot">{desktopAuthStatus || authFoot}</p>
    {/if}
  </section>
</main>
