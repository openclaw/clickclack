<script lang="ts">
  import { goto } from "$app/navigation";
  import { onMount } from "svelte";
  import { untrack } from "svelte";
  import {
    ArrowLeft,
    Check,
    Copy,
    ExternalLink,
    FolderGit2,
    GitPullRequest,
    LockKeyhole,
    Plus,
    Search,
    ShieldCheck,
    Trash2,
    Users,
    X,
  } from "@lucide/svelte";
  import { api, readableAPIError } from "$lib/api";
  import type { Project } from "$lib/types";

  let { data } = $props();

  type GitHubRepositoryOption = {
    full_name: string;
    name: string;
    owner: string;
    private: boolean;
    html_url: string;
    description?: string;
    updated_at?: string;
  };

  let projects = $state<Project[]>(untrack(() => [...data.projects]));
  let formOpen = $state(
    untrack(
      () =>
        data.projects.length === 0 ||
        Boolean(data.githubSetupError) ||
        data.githubSetupState === "select",
    ),
  );
  let setupPhase = $state<"details" | "repositories">(
    untrack(() => (data.githubSetupState === "select" ? "repositories" : "details")),
  );
  let description = $state("");
  let repositories = $state([""]);
  let memberIDs = $state<string[]>([]);
  let manualMode = $state(false);
  let availableRepositories = $state<GitHubRepositoryOption[]>([]);
  let selectedRepositories = $state<string[]>([]);
  let repositorySearch = $state("");
  let loadingRepositories = $state(false);
  let repositoryListTruncated = $state(false);
  let submitting = $state(false);
  let formError = $state(untrack(() => githubSetupMessage(data.githubSetupError)));
  let webhook = $state<{ url: string; secret: string } | null>(null);
  let copied = $state<"url" | "secret" | "">("");

  const canManage = $derived(
    data.workspace?.role === "owner" || data.workspace?.role === "moderator",
  );
  const filteredRepositories = $derived(
    availableRepositories.filter((repository) => {
      const query = repositorySearch.trim().toLowerCase();
      if (!query) return true;
      return (
        repository.full_name.toLowerCase().includes(query) ||
        repository.description?.toLowerCase().includes(query)
      );
    }),
  );
  const primarySelectedRepository = $derived(
    selectedRepositories.length > 0
      ? availableRepositories.find((repository) => repository.full_name === selectedRepositories[0])
      : undefined,
  );

  onMount(() => {
    if (data.githubSetupError) {
      const savedDraft = window.sessionStorage.getItem(projectDraftKey());
      if (!savedDraft) return;
      try {
        const draft = JSON.parse(savedDraft) as {
          description?: string;
          memberIDs?: string[];
        };
        description = draft.description || "";
        memberIDs = draft.memberIDs || [];
      } catch {
        window.sessionStorage.removeItem(projectDraftKey());
      }
      return;
    }
    if (data.githubSetupState === "select") void loadGitHubRepositories();
  });

  async function closeForm() {
    if (setupPhase === "repositories") {
      await cancelGitHubSetup();
    }
    formOpen = false;
    formError = "";
    manualMode = false;
  }

  function addRepository() {
    repositories = [...repositories, ""];
  }

  function setRepository(index: number, value: string) {
    repositories = repositories.map((item, itemIndex) => (itemIndex === index ? value : item));
  }

  function removeRepository(index: number) {
    repositories = repositories.filter((_, itemIndex) => itemIndex !== index);
    if (repositories.length === 0) repositories = [""];
  }

  function toggleMember(id: string) {
    memberIDs = memberIDs.includes(id)
      ? memberIDs.filter((memberID) => memberID !== id)
      : [...memberIDs, id];
  }

  function githubSetupMessage(reason: string): string {
    switch (reason) {
      case "":
        return "";
      case "cancelled":
        return "GitHub authorization was cancelled. You can try again or create the webhook manually.";
      case "permission":
        return "That GitHub account cannot manage webhooks for one of these repositories.";
      case "repository":
        return "GitHub could not find one of these repositories for the authorized account.";
      case "webhook_conflict":
        return "GitHub could not create the webhook because a repository webhook conflicts with this setup.";
      case "session":
        return "Your ClickClack session changed during GitHub authorization. Start the connection again.";
      case "authorization":
        return "GitHub authorization could not be completed. Connect GitHub again.";
      default:
        return "GitHub could not start repository setup. Try again or use manual setup.";
    }
  }

  function projectDraftKey(): string {
    return `clickclack.project-draft.${data.workspaceID}`;
  }

  function projectDetailsRequest() {
    return {
      description: description.trim(),
      member_ids: memberIDs,
    };
  }

  function manualProjectRequest() {
    return {
      ...projectDetailsRequest(),
      repositories: repositories.map((value) => value.trim()).filter(Boolean),
    };
  }

  function validateManualProject() {
    formError = "";
    if (manualProjectRequest().repositories.length === 0) {
      formError = "Add at least one GitHub repository.";
      return false;
    }
    return true;
  }

  async function connectGitHub() {
    formError = "";
    submitting = true;
    window.sessionStorage.setItem(
      projectDraftKey(),
      JSON.stringify({ description, memberIDs }),
    );
    try {
      const response = await api<{ authorization_url: string }>(
        `/api/workspaces/${data.workspaceID}/projects/github/connect`,
        {
          method: "POST",
          body: JSON.stringify(projectDetailsRequest()),
        },
      );
      window.location.assign(response.authorization_url);
    } catch (error) {
      formError = readableAPIError(error, "Could not start GitHub authorization");
      submitting = false;
    }
  }

  async function loadGitHubRepositories() {
    loadingRepositories = true;
    formError = "";
    try {
      const response = await api<{
        setup: { description: string; expires_at: string };
        repositories: GitHubRepositoryOption[];
        truncated: boolean;
      }>(`/api/workspaces/${data.workspaceID}/projects/github/repositories`);
      description = response.setup.description;
      availableRepositories = response.repositories;
      repositoryListTruncated = response.truncated;
      window.sessionStorage.removeItem(projectDraftKey());
    } catch (error) {
      formError = readableAPIError(error, "Could not load GitHub repositories");
    } finally {
      loadingRepositories = false;
    }
  }

  function toggleRepository(fullName: string) {
    selectedRepositories = selectedRepositories.includes(fullName)
      ? selectedRepositories.filter((repository) => repository !== fullName)
      : [...selectedRepositories, fullName];
  }

  async function completeGitHubSetup() {
    formError = "";
    if (selectedRepositories.length === 0) {
      formError = "Select at least one GitHub repository.";
      return;
    }
    submitting = true;
    try {
      const response = await api<{ project: Project }>(
        `/api/workspaces/${data.workspaceID}/projects/github/complete`,
        {
          method: "POST",
          body: JSON.stringify({ repositories: selectedRepositories }),
        },
      );
      window.sessionStorage.removeItem(projectDraftKey());
      await goto(chatPath(response.project));
    } catch (error) {
      formError = readableAPIError(error, "Could not create the GitHub project");
    } finally {
      submitting = false;
    }
  }

  async function cancelGitHubSetup() {
    try {
      await api<void>(`/api/workspaces/${data.workspaceID}/projects/github/cancel`, {
        method: "POST",
      });
    } catch {
      // Expired setup cookies are already unusable.
    }
    setupPhase = "details";
    availableRepositories = [];
    selectedRepositories = [];
    repositorySearch = "";
    await goto(`/app/${data.workspace?.route_id || data.workspaceID}/projects`, {
      replaceState: true,
      noScroll: true,
    });
  }

  async function createProjectManually() {
    if (!validateManualProject()) return;
    submitting = true;
    try {
      const response = await api<{
        project: Project;
        webhook: { url: string; secret: string };
      }>(`/api/workspaces/${data.workspaceID}/projects`, {
        method: "POST",
        body: JSON.stringify(manualProjectRequest()),
      });
      projects = [...projects, response.project].sort((a, b) => a.name.localeCompare(b.name));
      webhook = response.webhook;
      description = "";
      repositories = [""];
      memberIDs = [];
      manualMode = false;
      window.sessionStorage.removeItem(projectDraftKey());
      formOpen = false;
    } catch (error) {
      formError = readableAPIError(error, "Could not create project");
    } finally {
      submitting = false;
    }
  }

  async function copyCredential(kind: "url" | "secret", value: string) {
    await navigator.clipboard.writeText(value);
    copied = kind;
    window.setTimeout(() => {
      if (copied === kind) copied = "";
    }, 1800);
  }

  function chatPath(project: Project) {
    const workspaceRoute = data.workspace?.route_id || data.workspaceID;
    return `/app/${workspaceRoute}/${project.channel.route_id || project.channel.id}`;
  }
</script>

<svelte:head>
  <title>Projects - ClickClack</title>
</svelte:head>

<div class="projects-page">
  <header class="projects-topbar">
    <button
      type="button"
      class="projects-icon-button"
      aria-label="Back to chat"
      title="Back to chat"
      onclick={() => void goto(`/app/${data.workspace?.route_id || data.workspaceID}`)}
    >
      <ArrowLeft size={18} />
    </button>
    <div class="projects-topbar__identity">
      <FolderGit2 size={18} />
      <strong>{data.workspace?.name || "Workspace"}</strong>
      <span>/</span>
      <span>Projects</span>
    </div>
    {#if canManage && !formOpen}
      <button type="button" class="projects-button projects-button--primary" onclick={() => (formOpen = true)}>
        <Plus size={16} />
        Add project
      </button>
    {/if}
  </header>

  <main class="projects-main">
    <header class="projects-heading">
      <div>
        <h1>Projects</h1>
        <p>GitHub context and collaboration channels for this workspace.</p>
      </div>
      <div class="projects-heading__count">{projects.length} {projects.length === 1 ? "project" : "projects"}</div>
    </header>

    {#if data.loadError}
      <div class="projects-notice projects-notice--error">{data.loadError}</div>
    {/if}

    {#if webhook}
      <section class="webhook-handoff" aria-labelledby="webhook-title">
        <div class="webhook-handoff__heading">
          <div>
            <h2 id="webhook-title">Connect the GitHub webhook</h2>
            <p>The secret is shown only for this setup. Use these settings in each linked repository.</p>
          </div>
          <button
            type="button"
            class="projects-icon-button"
            aria-label="Dismiss webhook setup"
            title="Dismiss"
            onclick={() => (webhook = null)}
          >
            <X size={17} />
          </button>
        </div>
        <div class="webhook-requirements">
          <div>
            <strong>Content type</strong>
            <code>application/json</code>
          </div>
          <div>
            <strong>Individual events</strong>
            <span>
              Pull requests, Pull request reviews, Pull request review comments, Issue comments, Check runs, and
              Check suites
            </span>
          </div>
        </div>
        <div class="credential-row">
          <span>Payload URL</span>
          <code>{webhook.url}</code>
          <button
            type="button"
            class="projects-icon-button"
            aria-label="Copy payload URL"
            title="Copy payload URL"
            onclick={() => void copyCredential("url", webhook!.url)}
          >
            {#if copied === "url"}<Check size={16} />{:else}<Copy size={16} />{/if}
          </button>
        </div>
        <div class="credential-row">
          <span>Secret</span>
          <code>{webhook.secret}</code>
          <button
            type="button"
            class="projects-icon-button"
            aria-label="Copy webhook secret"
            title="Copy webhook secret"
            onclick={() => void copyCredential("secret", webhook!.secret)}
          >
            {#if copied === "secret"}<Check size={16} />{:else}<Copy size={16} />{/if}
          </button>
        </div>
      </section>
    {/if}

    {#if formOpen && canManage}
      <section class="project-form" aria-labelledby="new-project-title">
        <div class="project-form__heading">
          <div>
            <h2 id="new-project-title">
              {setupPhase === "repositories" ? "Choose repositories" : "Add a GitHub project"}
            </h2>
            <p>
              {setupPhase === "repositories"
                ? "Select the repositories that should feed this project's collaboration channel."
                : "A public collaboration channel is created automatically."}
            </p>
          </div>
          {#if projects.length > 0 && setupPhase === "details"}
            <button
              type="button"
              class="projects-icon-button"
              aria-label="Close form"
              title="Close"
              onclick={() => void closeForm()}
            >
              <X size={17} />
            </button>
          {/if}
        </div>

        {#if setupPhase === "details"}
          <div class="project-form__grid">
            <label class="project-form__description">
              <span>Description</span>
              <textarea
                bind:value={description}
                rows="3"
                maxlength="500"
                placeholder="What this project owns and where collaboration belongs"
              ></textarea>
            </label>
          </div>

          {#if manualMode}
            <fieldset class="project-form__fieldset">
              <legend><GitPullRequest size={16} /> GitHub repositories</legend>
              <div class="repository-inputs">
                {#each repositories as repository, index (index)}
                  <div class="repository-input">
                    <input
                      value={repository}
                      oninput={(event) => setRepository(index, event.currentTarget.value)}
                      placeholder="https://github.com/owner/repository"
                      aria-label={`GitHub repository ${index + 1}`}
                    />
                    <button
                      type="button"
                      class="projects-icon-button"
                      aria-label={`Remove repository ${index + 1}`}
                      title="Remove repository"
                      disabled={repositories.length === 1}
                      onclick={() => removeRepository(index)}
                    >
                      <Trash2 size={16} />
                    </button>
                  </div>
                {/each}
              </div>
              <button type="button" class="projects-button projects-button--quiet" onclick={addRepository}>
                <Plus size={15} />
                Add repository
              </button>
            </fieldset>
          {/if}

          <fieldset class="project-form__fieldset">
            <legend><Users size={16} /> Participants</legend>
            <div class="participant-list">
              {#each data.members as member (member.user.id)}
                <label class="participant-row">
                  <input
                    type="checkbox"
                    checked={memberIDs.includes(member.user.id)}
                    onchange={() => toggleMember(member.user.id)}
                  />
                  <span class="participant-row__name">{member.user.display_name}</span>
                  <span class="participant-row__meta">
                    {member.user.kind === "bot" ? "Bot" : member.role}
                  </span>
                </label>
              {/each}
            </div>
          </fieldset>

          {#if formError}
            <div class="projects-notice projects-notice--error">{formError}</div>
          {/if}

          <div class="project-form__actions">
            {#if projects.length > 0}
              <button type="button" class="projects-button" onclick={() => void closeForm()} disabled={submitting}>
                Cancel
              </button>
            {/if}
            {#if manualMode}
              <button
                type="button"
                class="projects-button"
                onclick={() => (manualMode = false)}
                disabled={submitting}
              >
                Back
              </button>
              <button
                type="button"
                class="projects-button projects-button--primary"
                onclick={() => void createProjectManually()}
                disabled={submitting}
              >
                <FolderGit2 size={16} />
                {submitting ? "Creating..." : "Create manually"}
              </button>
            {:else}
              <button type="button" class="projects-button" onclick={() => (manualMode = true)} disabled={submitting}>
                <FolderGit2 size={16} />
                Manual setup
              </button>
              <button
                type="button"
                class="projects-button projects-button--primary"
                onclick={() => void connectGitHub()}
                disabled={submitting}
              >
                <GitPullRequest size={16} />
                {submitting ? "Connecting..." : "Connect GitHub"}
              </button>
            {/if}
          </div>
          {#if manualMode}
            <p class="project-form__authorization">
              ClickClack will show the webhook URL and one-time secret after the project is created.
            </p>
          {:else}
            <p class="project-form__authorization">
              GitHub grants temporary repository access for the picker and selected webhooks. The access token expires
              from ClickClack setup after 10 minutes, is revoked after setup or cancellation, and is never saved to the
              database.
            </p>
          {/if}
        {:else}
          <div class="repository-picker__toolbar">
            <label class="repository-search">
              <Search size={16} />
              <input
                bind:value={repositorySearch}
                placeholder="Search repositories"
                aria-label="Search GitHub repositories"
                autocomplete="off"
              />
            </label>
            <span>
              {#if primarySelectedRepository}Project: {primarySelectedRepository.name}, {/if}
              {selectedRepositories.length} selected
            </span>
          </div>

          <div class="repository-picker" aria-busy={loadingRepositories}>
            {#if loadingRepositories}
              <div class="repository-picker__state">Loading repositories from GitHub...</div>
            {:else if availableRepositories.length === 0 && !formError}
              <div class="repository-picker__state">
                No repositories with webhook administration access were found.
              </div>
            {:else if filteredRepositories.length === 0}
              <div class="repository-picker__state">No repositories match this search.</div>
            {:else}
              {#each filteredRepositories as repository (repository.full_name)}
                <label class="repository-option">
                  <input
                    type="checkbox"
                    checked={selectedRepositories.includes(repository.full_name)}
                    onchange={() => toggleRepository(repository.full_name)}
                  />
                  <span class="repository-option__main">
                    <strong>{repository.full_name}</strong>
                    {#if repository.description}<span>{repository.description}</span>{/if}
                  </span>
                  <span class="repository-option__visibility">
                    {#if repository.private}<LockKeyhole size={13} /> Private{:else}<ShieldCheck size={13} /> Public{/if}
                  </span>
                </label>
              {/each}
            {/if}
          </div>

          {#if repositoryListTruncated}
            <div class="projects-notice">
              GitHub returned more than 1,000 repositories. Use search or manual setup if the needed repository is not
              listed.
            </div>
          {/if}

          {#if formError}
            <div class="projects-notice projects-notice--error">{formError}</div>
          {/if}

          <div class="project-form__actions">
            <button type="button" class="projects-button" onclick={() => void cancelGitHubSetup()} disabled={submitting}>
              Cancel
            </button>
            <button
              type="button"
              class="projects-button projects-button--primary"
              onclick={() => void completeGitHubSetup()}
              disabled={submitting || loadingRepositories || selectedRepositories.length === 0}
            >
              <FolderGit2 size={16} />
              {submitting
                ? "Creating project..."
                : `Create project with ${selectedRepositories.length || 0} ${selectedRepositories.length === 1 ? "repository" : "repositories"}`}
            </button>
          </div>
        {/if}
      </section>
    {:else if projects.length === 0 && !data.loadError}
      <div class="projects-empty">
        <FolderGit2 size={28} />
        <h2>No projects yet</h2>
        <p>A workspace owner or moderator can add the first GitHub project.</p>
      </div>
    {/if}

    {#if projects.length > 0}
      <section class="project-list" aria-label="Workspace projects">
        {#each projects as project (project.id)}
          <article class="project-row">
            <div class="project-row__main">
              <div class="project-row__title">
                <FolderGit2 size={18} />
                <h2>{project.name}</h2>
              </div>
              {#if project.description}
                <p>{project.description}</p>
              {/if}
              <div class="repository-links">
                {#each project.repositories as repository (repository.id)}
                  <a href={repository.url} target="_blank" rel="noreferrer">
                    <GitPullRequest size={14} />
                    {repository.full_name}
                    <ExternalLink size={12} />
                  </a>
                {/each}
              </div>
            </div>
            <div class="project-row__participants">
              <Users size={15} />
              <span>{project.members.length} {project.members.length === 1 ? "participant" : "participants"}</span>
            </div>
            <a class="projects-button projects-button--channel" href={chatPath(project)}>
              Open channel
              <ExternalLink size={14} />
            </a>
          </article>
        {/each}
      </section>
    {/if}
  </main>
</div>
