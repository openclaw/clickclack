<script lang="ts">
  import { goto } from "$app/navigation";
  import { untrack } from "svelte";
  import {
    ArrowLeft,
    Check,
    Copy,
    ExternalLink,
    FolderGit2,
    GitPullRequest,
    Plus,
    Trash2,
    Users,
    X,
  } from "@lucide/svelte";
  import { api, readableAPIError } from "$lib/api";
  import type { Project } from "$lib/types";

  let { data } = $props();

  let projects = $state<Project[]>(untrack(() => [...data.projects]));
  let formOpen = $state(untrack(() => data.projects.length === 0));
  let name = $state("");
  let description = $state("");
  let repositories = $state([""]);
  let memberIDs = $state<string[]>([]);
  let submitting = $state(false);
  let formError = $state("");
  let webhook = $state<{ url: string; secret: string } | null>(null);
  let copied = $state<"url" | "secret" | "">("");

  const canManage = $derived(
    data.workspace?.role === "owner" || data.workspace?.role === "moderator",
  );

  function closeForm() {
    formOpen = false;
    formError = "";
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

  async function createProject() {
    formError = "";
    const repositoryValues = repositories.map((value) => value.trim()).filter(Boolean);
    if (!name.trim()) {
      formError = "Project name is required.";
      return;
    }
    if (repositoryValues.length === 0) {
      formError = "Add at least one GitHub repository.";
      return;
    }
    submitting = true;
    try {
      const response = await api<{
        project: Project;
        webhook: { url: string; secret: string };
      }>(`/api/workspaces/${data.workspaceID}/projects`, {
        method: "POST",
        body: JSON.stringify({
          name: name.trim(),
          description: description.trim(),
          repositories: repositoryValues,
          member_ids: memberIDs,
        }),
      });
      projects = [...projects, response.project].sort((a, b) => a.name.localeCompare(b.name));
      webhook = response.webhook;
      name = "";
      description = "";
      repositories = [""];
      memberIDs = [];
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
            <h2 id="new-project-title">Add a GitHub project</h2>
            <p>A public collaboration channel is created automatically.</p>
          </div>
          {#if projects.length > 0}
            <button type="button" class="projects-icon-button" aria-label="Close form" title="Close" onclick={closeForm}>
              <X size={17} />
            </button>
          {/if}
        </div>

        <div class="project-form__grid">
          <label>
            <span>Project name</span>
            <input bind:value={name} maxlength="80" placeholder="ClickClack" autocomplete="off" />
          </label>
          <label class="project-form__description">
            <span>Description</span>
            <textarea bind:value={description} rows="3" maxlength="500" placeholder="What this project owns and where collaboration belongs"></textarea>
          </label>
        </div>

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
            <button type="button" class="projects-button" onclick={closeForm} disabled={submitting}>Cancel</button>
          {/if}
          <button
            type="button"
            class="projects-button projects-button--primary"
            onclick={() => void createProject()}
            disabled={submitting}
          >
            <FolderGit2 size={16} />
            {submitting ? "Creating..." : "Create project"}
          </button>
        </div>
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
