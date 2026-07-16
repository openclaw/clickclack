<script lang="ts">
  import {
    BOARD_THEMES,
    COLOR_MODES,
    MESSAGE_LAYOUTS,
    loadBoardTheme,
    loadColorMode,
    loadMessageLayout,
    setBoardTheme,
    setColorMode,
    setMessageLayout,
    type BoardTheme,
    type ColorMode,
    type MessageLayout,
  } from "../../lib/appearance";

  // Appearance is a device-local pref with no server state, so the section
  // owns it directly instead of prop-drilling through ChatApp.
  let colorMode = $state<ColorMode>(loadColorMode());
  let boardTheme = $state<BoardTheme>(loadBoardTheme());
  let messageLayout = $state<MessageLayout>(loadMessageLayout());

  function pickMode(mode: ColorMode) {
    colorMode = mode;
    setColorMode(mode);
  }

  function pickBoard(board: BoardTheme) {
    boardTheme = board;
    setBoardTheme(board);
  }

  function pickMessageLayout(layout: MessageLayout) {
    messageLayout = layout;
    setMessageLayout(layout);
  }

  function moveRadioSelection(
    event: KeyboardEvent,
    optionCount: number,
    currentIndex: number,
    select: (index: number) => void,
  ) {
    let nextIndex: number;
    switch (event.key) {
      case "ArrowRight":
      case "ArrowDown":
        nextIndex = (currentIndex + 1) % optionCount;
        break;
      case "ArrowLeft":
      case "ArrowUp":
        nextIndex = (currentIndex - 1 + optionCount) % optionCount;
        break;
      case "Home":
        nextIndex = 0;
        break;
      case "End":
        nextIndex = optionCount - 1;
        break;
      default:
        return;
    }

    event.preventDefault();
    select(nextIndex);
    const radios = (event.currentTarget as HTMLElement).parentElement?.querySelectorAll<HTMLElement>(
      '[role="radio"]',
    );
    radios?.item(nextIndex).focus();
  }
</script>

<header class="settings-page__header">
  <p class="settings-page__eyebrow">Account</p>
  <h2 class="settings-page__h1">Appearance</h2>
  <p class="settings-page__lead">
    Pick a color mode, board, and message layout. Changes apply instantly, everywhere in the
    app, and stay on this device.
  </p>
</header>

<div class="settings-form">
  <h3 class="settings-rows__head">Color mode</h3>
  <div class="appearance-modes" role="radiogroup" aria-label="Color mode">
    {#each COLOR_MODES as mode, index (mode.id)}
      <button
        type="button"
        class="appearance-mode"
        class:is-active={colorMode === mode.id}
        role="radio"
        aria-checked={colorMode === mode.id}
        tabindex={colorMode === mode.id ? 0 : -1}
        onclick={() => pickMode(mode.id)}
        onkeydown={(event) =>
          moveRadioSelection(event, COLOR_MODES.length, index, (nextIndex) =>
            pickMode(COLOR_MODES[nextIndex].id),
          )}
      >
        <span class="appearance-mode__icon" aria-hidden="true">
          {#if mode.id === "light"}
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <circle cx="12" cy="12" r="4" />
              <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
            </svg>
          {:else if mode.id === "dark"}
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8Z" />
            </svg>
          {:else}
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <rect x="2" y="4" width="20" height="13" rx="2" />
              <path d="M8 21h8M12 17v4" />
            </svg>
          {/if}
        </span>
        {mode.label}
      </button>
    {/each}
  </div>

  <h3 class="settings-rows__head">Board</h3>
  <div class="board-grid" role="radiogroup" aria-label="Board theme">
    {#each BOARD_THEMES as board, index (board.id)}
      <button
        type="button"
        class="board-swatch"
        class:is-active={boardTheme === board.id}
        role="radio"
        aria-checked={boardTheme === board.id}
        tabindex={boardTheme === board.id ? 0 : -1}
        data-board={board.id}
        onclick={() => pickBoard(board.id)}
        onkeydown={(event) =>
          moveRadioSelection(event, BOARD_THEMES.length, index, (nextIndex) =>
            pickBoard(BOARD_THEMES[nextIndex].id),
          )}
      >
        <span class="board-swatch__preview" aria-hidden="true">
          <span class="board-swatch__rail"></span>
          <span class="board-swatch__body">
            <span class="board-swatch__key"></span>
            <span class="board-swatch__bubble"></span>
          </span>
        </span>
        <span class="board-swatch__meta">
          <strong>{board.label}</strong>
          <span>{board.blurb}</span>
        </span>
      </button>
    {/each}
  </div>
  <p class="settings-field__hint">
    Every board comes tuned for light and dark; the swatches preview whichever mode is active.
  </p>

  <h3 class="settings-rows__head">Message layout</h3>
  <div class="message-layout-grid" role="radiogroup" aria-label="Message layout">
    {#each MESSAGE_LAYOUTS as layout, index (layout.id)}
      <button
        type="button"
        class="message-layout-option"
        class:is-active={messageLayout === layout.id}
        role="radio"
        aria-checked={messageLayout === layout.id}
        tabindex={messageLayout === layout.id ? 0 : -1}
        onclick={() => pickMessageLayout(layout.id)}
        onkeydown={(event) =>
          moveRadioSelection(event, MESSAGE_LAYOUTS.length, index, (nextIndex) =>
            pickMessageLayout(MESSAGE_LAYOUTS[nextIndex].id),
          )}
      >
        <span class="message-layout-option__preview" data-layout={layout.id} aria-hidden="true">
          <span class="message-layout-option__activity"></span>
          <span class="message-layout-option__answer"></span>
        </span>
        <span class="message-layout-option__meta">
          <strong>{layout.label}</strong>
          <span>{layout.blurb}</span>
        </span>
      </button>
    {/each}
  </div>
</div>
