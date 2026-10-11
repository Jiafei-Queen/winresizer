<script lang="ts">
  import {onMount} from 'svelte';
  import {Events, System, Window} from "@wailsio/runtime";
  import {ResizerService} from "../bindings/winresizer";

  interface Size { w: number; h: number; }
  interface AppPreset { id: string; appName: string; sizes: Size[]; lastIndex: number; }
  interface TargetWindow { id: string; app: string; title: string; w: number; h: number; monitor: number; self?: boolean; isRecorded?: boolean; }
  type Theme = 'light' | 'dark';
  type Platform = 'mac' | 'windows' | 'linux';

  const themeStorageKey = 'winresizer-theme';

  function detectPlatform(): Platform {
    // Wails injects window._wails.environment before any page script runs, so
    // this is authoritative inside the app. The UA fallback only matters when
    // the page is opened in a plain browser (vite dev), where the drawn title
    // bar still needs the right height and controls.
    if (System.IsMac()) return 'mac';
    if (System.IsWindows()) return 'windows';
    if (System.IsLinux()) return 'linux';
    const ua = navigator.userAgent;
    if (/Mac|iPhone|iPad/i.test(ua)) return 'mac';
    if (/Windows/i.test(ua)) return 'windows';
    return 'linux';
  }

  // macOS draws no caption buttons — it keeps the native traffic lights and
  // only needs a strip tall enough to keep them clear of the content.
  const platform: Platform = detectPlatform();
  document.documentElement.dataset.platform = platform;

  let hotkey: string = $state('');
  let newHotkey: string = $state('');
  let axGranted: boolean = $state(false);
  let apps: AppPreset[] = $state([]);
  let target: TargetWindow | null = $state(null);
  let message: string = $state('');
  let theme: Theme = $state(document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light');
  let toastEl: HTMLElement | undefined = $state();
  let toastTimer: ReturnType<typeof setTimeout>;

  function setTheme(next: Theme, persist: boolean = true): void {
    theme = next;
    document.documentElement.dataset.theme = next;

    const themeColor = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]');
    if (themeColor) themeColor.content = next === 'dark' ? '#11161e' : '#f4f6fa';

    if (persist) {
      try {
        localStorage.setItem(themeStorageKey, next);
      } catch {
        // The theme still applies for this session if storage is unavailable.
      }
    }
  }

  function toggleTheme(): void {
    setTheme(theme === 'dark' ? 'light' : 'dark');
  }

  function showMessage(m: string): void {
    message = m;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { message = ''; }, 5000);
  }

  function errText(e: unknown): string {
    if (e instanceof Error) return e.message;
    if (typeof e === 'string') return e;
    try { return JSON.stringify(e); } catch { return String(e); }
  }

  let maximised: boolean = $state(false);
  let maxSyncTimer: ReturnType<typeof setTimeout> | undefined;
  let focusSyncTimer: ReturnType<typeof setTimeout> | undefined;

  async function syncMaximised(): Promise<void> {
    try {
      maximised = await Window.IsMaximised();
    } catch {
      // Window API unavailable (browser preview) — keep the last known state.
    }
  }

  // Linux never forwards a maximise event to the frontend, so re-read the
  // real state after every resize instead of trusting the event stream alone.
  function queueMaximisedSync(): void {
    clearTimeout(maxSyncTimer);
    maxSyncTimer = setTimeout(() => void syncMaximised(), 120);
  }

  function minimiseWindow(): void {
    void Window.Minimise();
  }

  function toggleMaximiseWindow(): void {
    void Window.ToggleMaximise();
    void syncMaximised();
  }

  function closeWindow(): void {
    // Reaches the Go WindowClosing hook, which hides to tray rather than
    // quitting — the same behaviour the native close button had.
    void Window.Close();
  }

  function handleTitlebarDoubleClick(): void {
    // On macOS the runtime claims the double click on a drag region and routes
    // it to the native titlebar action, so handling it here too would cancel
    // out and leave the window untouched. Everywhere else nobody else does it.
    if (platform === 'mac') return;
    void Window.ToggleMaximise();
    void syncMaximised();
  }

  async function refresh(): Promise<void> {
    try {
      const cfg: any = await ResizerService.GetConfig();
      hotkey = cfg.hotkey;
      apps = cfg.apps ?? [];
      axGranted = await ResizerService.AXGranted();
    } catch (e) {
      showMessage(errText(e));
    }
  }

  // silent suppresses the toast. Focus-triggered refreshes run on every
  // activation, so "no target yet" would otherwise flash an error at the
  // user who has simply not visited another window yet.
  async function refreshTarget(silent = false): Promise<void> {
    try {
      target = await ResizerService.GetTarget() as TargetWindow;
    } catch (e) {
      target = null;
      if (!silent) showMessage(errText(e));
    }
  }

  // Focus returning to the console is the signal to re-read everything:
  // we are by definition the focused window now, so the tracker holds
  // exactly the window the user wants to act on. Presets come along
  // because the picker records behind our back and re-shows us.
  async function syncOnFocus(): Promise<void> {
    await refresh();
    await refreshTarget(true);
  }

  // Showing from the tray fires both WindowShow and WindowFocus; collapse
  // the pair into one round-trip.
  function queueFocusSync(): void {
    clearTimeout(focusSyncTimer);
    focusSyncTimer = setTimeout(() => { void syncOnFocus(); }, 80);
  }

  function onSummaryKey(e: KeyboardEvent): void {
    if (e.key !== 'Enter' && e.key !== ' ') return;
    e.preventDefault(); // Space would otherwise scroll the console
    void doPick();
  }

  async function doPick(): Promise<void> {
    try {
      await ResizerService.OpenOverlay();
    } catch (e) {
      showMessage(errText(e));
    }
  }

  async function doRecord(): Promise<void> {
    try {
      showMessage(await ResizerService.RecordTarget());
      await refresh();
      await refreshTarget();
    } catch (e) {
      showMessage(errText(e));
    }
  }

  async function doCycle(): Promise<void> {
    try {
      showMessage(await ResizerService.CycleTarget());
      await refresh();
      await refreshTarget();
    } catch (e) {
      showMessage(errText(e));
    }
  }

  async function doSaveHotkey(): Promise<void> {
    try {
      await ResizerService.SetHotkey(newHotkey);
      newHotkey = '';
      await refresh();
      showMessage(`Hotkey saved (${hotkey}). Restart the app to apply it.`);
    } catch (e) {
      showMessage(errText(e));
    }
  }

  async function doRemove(id: string): Promise<void> {
    try {
      await ResizerService.RemoveApp(id);
      await refresh();
    } catch (e) {
      showMessage(errText(e));
    }
  }

  async function doRequestAX(): Promise<void> {
    try {
      await ResizerService.RequestAX();
      await refresh();
    } catch (e) {
      showMessage(errText(e));
    }
  }

  // Kept separate from the async onMount below: a cleanup returned from an
  // async callback becomes a resolved promise, which Svelte cannot unmount.
  onMount(() => {
    void syncMaximised();

    const offs = [
      Events.On('common:WindowMaximise', () => { maximised = true; }),
      Events.On('common:WindowUnMaximise', () => { maximised = false; }),
      Events.On('common:WindowRestore', () => { maximised = false; }),
      Events.On('common:WindowDidResize', queueMaximisedSync),
      Events.On('common:WindowFocus', queueFocusSync),
      Events.On('common:WindowShow', queueFocusSync),
    ];

    return () => {
      offs.forEach((off) => off());
      clearTimeout(maxSyncTimer);
      clearTimeout(focusSyncTimer);
    };
  });

  onMount(async () => {
    try {
      const savedTheme = localStorage.getItem(themeStorageKey);
      if (savedTheme === 'light' || savedTheme === 'dark') {
        setTheme(savedTheme, false);
      } else {
        setTheme(theme, false);
      }
    } catch {
      setTheme(theme, false);
    }

    await refresh();
    await refreshTarget();
  });
</script>

<div class="titlebar">
  <div
    class="titlebar-drag"
    role="presentation"
    ondblclick={handleTitlebarDoubleClick}
  ></div>
  {#if platform !== 'mac'}
    <div class="window-controls">
      <button class="window-control" onclick={minimiseWindow} aria-label="Minimise window" title="Minimise">
        <svg viewBox="0 0 10 10" aria-hidden="true"><path d="M0 5h10" /></svg>
      </button>
      <button
        class="window-control"
        onclick={toggleMaximiseWindow}
        aria-label={maximised ? 'Restore window' : 'Maximise window'}
        title={maximised ? 'Restore' : 'Maximise'}
      >
        {#if maximised}
          <svg viewBox="0 0 10 10" aria-hidden="true">
            <rect x="3.5" y="0.5" width="6" height="6" />
            <path d="M3.5 3.5H0.5v6h6V6.5" />
          </svg>
        {:else}
          <svg viewBox="0 0 10 10" aria-hidden="true"><rect x="0.5" y="0.5" width="9" height="9" /></svg>
        {/if}
      </button>
      <button class="window-control window-control-close" onclick={closeWindow} aria-label="Close window" title="Close">
        <svg viewBox="0 0 10 10" aria-hidden="true"><path d="M0 0l10 10M10 0L0 10" /></svg>
      </button>
    </div>
  {/if}
</div>

<main class="container">
  <header class="app-header">
    <div class="brand-lockup">
      <div class="brand-mark" aria-hidden="true">
        <svg viewBox="0 0 24 24" fill="none">
          <rect x="3.5" y="4" width="17" height="16" stroke="currentColor" stroke-width="1.7" />
          <path d="M8 9h8M8 13h5" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" />
          <circle cx="16.5" cy="15.5" r="1.5" fill="currentColor" />
        </svg>
      </div>
      <div class="brand-copy">
        <h1 class="title">WinResizer</h1>
        <p class="subtitle header-subtitle">Save window sizes and switch between them with a shortcut.</p>
      </div>
    </div>
    <div class="header-actions">
      <button
        class="theme-toggle"
        onclick={toggleTheme}
        aria-label={theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
        title={theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'}
      >
        {#if theme === 'dark'}
          <svg viewBox="0 0 20 20" fill="none" aria-hidden="true">
            <circle cx="10" cy="10" r="3.2" stroke="currentColor" stroke-width="1.5" />
            <path d="M10 2v1.5M10 16.5V18M18 10h-1.5M3.5 10H2m13.66-5.66-1.06 1.06M5.4 14.6l-1.06 1.06m11.32 0-1.06-1.06M5.4 5.4 4.34 4.34" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
          </svg>
        {:else}
          <svg viewBox="0 0 20 20" fill="none" aria-hidden="true">
            <path d="M16.6 12.4A7 7 0 0 1 7.6 3.4a7.1 7.1 0 1 0 9 9Z" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        {/if}
      </button>
    </div>
  </header>

  <!-- Pinned above: this wrapper is the only scrollable region, so the
       title bar and the WinResizer header never move with the cards. -->
  <div class="content">
  {#if !axGranted}
    <section class="permission-card" aria-label="Accessibility permission required">
      <div class="permission-icon" aria-hidden="true">
        <svg viewBox="0 0 20 20" fill="none">
          <path d="M10 2.75 18 17H2L10 2.75Z" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
          <path d="M10 7v4.5M10 14.2v.1" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" />
        </svg>
      </div>
      <div class="permission-copy">
        <p class="eyebrow">Accessibility</p>
        <h2 class="permission-title">Allow window control</h2>
        <p class="hint">WinResizer needs permission to move and resize windows.</p>
      </div>
      <button class="btn request-btn" onclick={doRequestAX}>Enable</button>
    </section>
  {/if}

  <section class="card focus-card" aria-labelledby="active-window-title">
    <div class="card-heading focus-heading">
      <div>
        <p class="eyebrow">Target window</p>
        <h2 id="active-window-title">Active target</h2>
      </div>
    </div>

    <!-- The summary is the "pick a window" entry point: the card shows
         what the auto-tracker picked, clicking it opens the picker when
         that is not the window the user meant. -->
    <div
      class="window-summary"
      class:empty-window={!target}
      role="button"
      tabindex="0"
      aria-label="Pick a window to record"
      title="Pick a window to record"
      onclick={doPick}
      onkeydown={onSummaryKey}
    >
      {#if target}
        <div class="window-overview">
          <div class="window-glyph" aria-hidden="true">
            <svg viewBox="0 0 24 24" fill="none">
              <rect x="3.5" y="4" width="17" height="16" stroke="currentColor" stroke-width="1.6" />
              <path d="M3.8 9h16.4" stroke="currentColor" stroke-width="1.6" />
              <circle cx="7" cy="6.5" r=".7" fill="currentColor" />
              <circle cx="9.5" cy="6.5" r=".7" fill="currentColor" />
            </svg>
          </div>
          <div class="window-copy">
            <div class="window-app" title={target.title || target.app}>{target.app}</div>
            {#if target.title && target.title !== target.app}
              <p class="window-title" title={target.title}>{target.title}</p>
            {/if}
          </div>
          <span class="target-badge" title="Last window focused outside this app">Auto</span>
        </div>
        <p class="dimensions" aria-label={`${target.w} by ${target.h} pixels`}>
          <span>{target.w}</span>
          <span class="dimension-separator" aria-hidden="true">×</span>
          <span>{target.h}</span>
          <small>px</small>
        </p>
        <p class="summary-hint">Click to pick a different window</p>
      {:else}
        <div class="window-glyph" aria-hidden="true">
          <svg viewBox="0 0 24 24" fill="none">
            <rect x="3.5" y="4" width="17" height="16" stroke="currentColor" stroke-width="1.6" />
            <path d="M3.8 9h16.4" stroke="currentColor" stroke-width="1.6" />
            <circle cx="7" cy="6.5" r=".7" fill="currentColor" />
            <circle cx="9.5" cy="6.5" r=".7" fill="currentColor" />
          </svg>
        </div>
        <div>
          <strong>No target window yet</strong>
          <p>Click to pick a window, or focus another app and come back.</p>
        </div>
      {/if}
    </div>

    <!-- The backend marks a target already carrying a saved size. Recording
         it again could only report "already recorded", so the action is
         withdrawn rather than left to fail after the round-trip. -->
    <div class="row action-row">
      <button class="btn primary" onclick={doRecord} disabled={!target || target.isRecorded}>
        <svg viewBox="0 0 20 20" fill="none" aria-hidden="true">
          <path d="M5.5 3.5h9v13l-4.5-3.2-4.5 3.2v-13Z" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
        </svg>
        Record
      </button>
      <button class="btn secondary" onclick={doCycle}>
        <svg viewBox="0 0 20 20" fill="none" aria-hidden="true">
          <path d="M4 6.5h9a3 3 0 0 1 0 6h-1M16 13.5l-3-3-3 3" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
        Cycle sizes
      </button>
    </div>
    {#if target && target.isRecorded}
      <p class="hint recorded-hint">
        {target.w} × {target.h} is already recorded for {target.app}. Resize the window to save another size.
      </p>
    {/if}
  </section>

  <section class="card hotkey-card" aria-labelledby="shortcut-title">
    <div class="card-heading">
      <div>
        <p class="eyebrow">Quick action</p>
        <h2 id="shortcut-title">Keyboard shortcut</h2>
      </div>
      <span class="shortcut-badge" aria-label={`Current shortcut: ${hotkey}`}>{hotkey || '—'}</span>
    </div>
    <label class="field-label" for="hotkey-input">Change shortcut</label>
    <div class="row">
      <input id="hotkey-input" class="input" bind:value={newHotkey} placeholder="e.g. Ctrl+Alt+R" autocomplete="off" />
      <button class="btn save-btn" onclick={doSaveHotkey}>Save</button>
    </div>
    <p class="hint shortcut-hint">Restart the app to apply your new shortcut.</p>
  </section>

  <section class="card presets-card" aria-labelledby="presets-title">
    <div class="card-heading presets-heading">
      <div>
        <p class="eyebrow">Your saved sizes</p>
        <h2 id="presets-title">Presets</h2>
      </div>
      <span class="count-badge" aria-label={`${apps.length} saved apps`}>{apps.length}</span>
    </div>
    {#if apps.length === 0}
      <div class="empty-presets">
        <p>No presets yet</p>
        <span>Record a window size and it will appear here.</span>
      </div>
    {:else}
      <div class="preset-list">
        {#each apps as a (a.id)}
          <div class="preset">
            <div class="preset-info">
              <strong class="preset-name" title={a.appName || a.id}>{a.appName || a.id}</strong>
              <div class="size-list" aria-label={`${a.sizes.length} saved sizes`}>
                {#each a.sizes as size, i (`${a.id}-${i}`)}
                  <span class="size-chip">{size.w} × {size.h}</span>
                {/each}
              </div>
            </div>
            <button class="remove-btn" onclick={() => doRemove(a.id)} aria-label={`Remove ${a.appName || a.id} preset`}>
              <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
                <path d="M3 4.5h10M6 4.5V3h4v1.5m2 0-.5 8h-7l-.5-8m3 2v3.5m2-3.5v3.5" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round" />
              </svg>
              <span>Remove</span>
            </button>
          </div>
        {/each}
      </div>
    {/if}
  </section>
  </div>
</main>

{#if message}
  <div class="toast" role="status" aria-live="polite">
    <span class="toast-msg">{message}</span>
  </div>
{/if}

<style>
  /* The window title bar is drawn here rather than by the OS. It sits above
     the content and shares #app's frosted background, so the window reads as
     one continuous surface instead of a page pasted under a strip of chrome. */
  .titlebar {
    display: flex;
    align-items: stretch;
    justify-content: space-between;
    flex: 0 0 auto;
    width: 100%;
    height: 32px;
    background: transparent;
  }

  :global(:root[data-platform='mac']) .titlebar {
    /* Matches the native title bar the traffic lights are centred in, so the
       gap between them and the content is the same as it always was. */
    height: 28px;
  }

  .titlebar-drag {
    flex: 1 1 auto;
    min-width: 0;
    --wails-draggable: drag;
  }

  .window-controls {
    display: flex;
    flex: 0 0 auto;
    align-items: stretch;
    height: 100%;
  }

  .window-control {
    display: grid;
    place-items: center;
    width: 46px;
    height: 100%;
    padding: 0;
    color: var(--text-secondary);
    background: transparent;
    border: 0;
    /* Caption buttons take the arrow cursor on Windows and Linux. */
    cursor: default;
    transition: background var(--duration-fast) var(--ease-out),
                color var(--duration-fast) var(--ease-out);
  }

  .window-control svg {
    width: 10px;
    height: 10px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1;
  }

  .window-control:hover {
    color: var(--text-primary);
    background: var(--titlebar-hover);
  }

  .window-control:active svg {
    opacity: 0.6;
  }

  .window-control-close:hover {
    color: var(--titlebar-hover-close-fg);
    background: var(--titlebar-hover-close);
  }

  .window-control:focus-visible {
    outline: 2px solid var(--focus-ring);
    outline-offset: -2px;
  }

  .container {
    display: flex;
    flex-direction: column;
    gap: 16px;
    flex: 1 1 auto;
    /* Fills the viewport beneath the title bar and clips instead of growing;
       the cards scroll inside .content, the header above it does not. */
    min-height: 0;
    width: min(100%, var(--container-max));
    margin: 0 auto;
    padding: 28px 24px 0;
  }

  .content {
    display: flex;
    flex-direction: column;
    gap: 16px;
    flex: 1 1 auto;
    min-height: 0;
    overflow-y: auto;
    /* Scrolling here must never chain back out to the window. */
    overscroll-behavior: contain;
    padding-bottom: 34px;
    scrollbar-width: thin;
  }

  .app-header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 14px;
  }

  .brand-lockup {
    display: flex;
    align-items: flex-start;
    gap: 14px;
    min-width: 0;
  }

  .brand-mark {
    display: grid;
    place-items: center;
    width: 44px;
    height: 44px;
    flex: 0 0 auto;
    color: var(--accent);
    background: var(--surface-raised);
    border: 1px solid var(--border);
  }

  .brand-mark svg { width: 24px; height: 24px; }
  .brand-copy { min-width: 0; }

  .eyebrow {
    margin: 0 0 5px;
    color: var(--text-tertiary);
    font-size: 10px;
    font-weight: 550;
    letter-spacing: 0.02em;
    text-transform: none;
  }

  .title {
    margin: 0 0 5px;
    color: var(--text-primary);
    font-family: var(--font-serif);
    font-size: 28px;
    font-weight: 600;
    letter-spacing: -0.02em;
    line-height: 1.05;
  }

  .header-actions {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    margin-left: auto;
    padding-top: 2px;
  }

  .theme-toggle {
    display: grid;
    place-items: center;
    width: 36px;
    height: 36px;
    flex: 0 0 auto;
    padding: 0;
    color: var(--text-secondary);
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    cursor: pointer;
    transition: color var(--duration-fast) var(--ease-out),
                background var(--duration-fast) var(--ease-out),
                border-color var(--duration-fast) var(--ease-out);
  }

  .theme-toggle:hover {
    color: var(--accent);
    background: var(--surface-raised);
    border-color: var(--border-strong);
  }

  .theme-toggle svg { width: 18px; height: 18px; }

  .subtitle {
    margin: 0;
    color: var(--text-secondary);
    font-size: 12px;
    line-height: 1.5;
  }

  .header-subtitle {
    max-width: 260px;
  }

  .card,
  .permission-card {
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
  }

  .card { padding: 20px; }

  .card-heading {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 16px;
  }

  .card h2 {
    margin: 0;
    color: var(--text-primary);
    font-family: var(--font-serif);
    font-size: 18px;
    font-weight: 600;
    letter-spacing: -0.01em;
  }

  .permission-card {
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 14px 16px;
    background: var(--warning-soft);
    border-color: var(--warning-border);
  }

  .permission-icon {
    display: grid;
    place-items: center;
    width: 36px;
    height: 36px;
    flex: 0 0 auto;
    color: var(--warning);
    background: var(--surface-raised);
    border: 1px solid var(--border);
  }

  .permission-icon svg { width: 19px; height: 19px; }
  .permission-copy { min-width: 0; flex: 1; }
  .permission-copy .eyebrow { margin-bottom: 3px; }

  .permission-title {
    margin: 0 0 3px;
    color: var(--text-primary);
    font-family: var(--font-serif);
    font-size: 16px;
    font-weight: 600;
  }

  .hint {
    margin: 0;
    color: var(--text-secondary);
    font-size: 11px;
    line-height: 1.45;
  }

  .focus-card {
    background: var(--surface-raised);
    border-color: var(--border-strong);
  }

  .focus-heading { margin-bottom: 14px; }

  .window-summary {
    min-width: 0;
    margin-bottom: 16px;
    padding: 16px;
    background: var(--surface-subtle);
    border: 1px solid var(--border);
    cursor: pointer;
    transition: background var(--duration-fast) var(--ease-out),
                border-color var(--duration-fast) var(--ease-out);
  }

  .window-summary:hover {
    background: var(--surface-raised);
    border-color: var(--border-strong);
  }

  .window-overview {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
  }

  .window-glyph {
    display: grid;
    place-items: center;
    width: 38px;
    height: 38px;
    flex: 0 0 auto;
    color: var(--accent);
    background: var(--surface);
    border: 1px solid var(--border);
  }

  .window-glyph svg { width: 21px; height: 21px; }
  .window-copy { min-width: 0; flex: 1; }

  .target-badge {
    flex: 0 0 auto;
    margin-left: auto;
    padding: 4px 8px;
    color: var(--text-tertiary);
    background: var(--surface);
    border: 1px solid var(--border);
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.01em;
    white-space: nowrap;
  }

  .window-app {
    overflow: hidden;
    color: var(--text-primary);
    font-size: 13px;
    font-weight: 600;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .window-title {
    overflow: hidden;
    margin: 3px 0 0;
    color: var(--text-secondary);
    font-size: 11px;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .dimensions {
    display: flex;
    align-items: baseline;
    gap: 8px;
    margin: 14px 0 0;
    color: var(--text-primary);
    font-family: var(--font-serif);
    font-size: 34px;
    font-variant-numeric: tabular-nums;
    font-weight: 600;
    letter-spacing: -0.03em;
    line-height: 1;
  }

  .dimension-separator {
    color: var(--text-tertiary);
    font-size: 22px;
    font-weight: 400;
  }

  .dimensions small {
    margin-left: 2px;
    color: var(--text-tertiary);
    font-size: 11px;
    font-weight: 500;
    letter-spacing: 0;
  }

  .summary-hint {
    margin: 12px 0 0;
    color: var(--text-tertiary);
    font-size: 11px;
  }

  .empty-window {
    display: flex;
    align-items: center;
    gap: 12px;
    min-height: 70px;
  }

  .empty-window strong {
    color: var(--text-primary);
    font-size: 12px;
    font-weight: 600;
  }

  .empty-window p {
    margin: 4px 0 0;
    color: var(--text-secondary);
    font-size: 11px;
    line-height: 1.45;
  }

  .row { display: flex; align-items: center; gap: 10px; }
  .action-row .primary { flex: 1.15; }
  .action-row .secondary { flex: 1; }

  .btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 7px;
    min-height: 40px;
    padding: 0 14px;
    color: var(--accent-foreground);
    background: var(--accent);
    border: 1px solid transparent;
    border-radius: var(--radius);
    font-size: 12px;
    font-weight: 600;
    cursor: pointer;
    white-space: nowrap;
    transition: background var(--duration-fast) var(--ease-out),
                border-color var(--duration-fast) var(--ease-out),
                transform var(--duration-fast) var(--ease-out);
  }

  .btn:hover {
    background: var(--accent-hover);
  }

  .btn:active { transform: scale(0.98); }
  .btn svg { width: 16px; height: 16px; flex: 0 0 auto; }

  .btn:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }

  .btn:disabled:hover { background: var(--accent); }
  .btn:disabled:active { transform: none; }

  .btn.secondary,
  .btn.save-btn {
    color: var(--text-primary);
    background: var(--surface);
    border-color: var(--border);
  }

  .btn.secondary:hover,
  .btn.save-btn:hover {
    color: var(--accent);
    background: var(--surface-raised);
    border-color: var(--border-strong);
  }

  .btn.request-btn {
    min-height: 36px;
    padding: 0 13px;
    color: var(--warning);
    background: var(--surface);
    border-color: var(--warning-border);
    font-size: 11px;
  }

  .btn.request-btn:hover {
    background: var(--warning-soft);
    border-color: var(--warning);
  }

  .btn:focus-visible,
  .window-summary:focus-visible,
  .remove-btn:focus-visible,
  .theme-toggle:focus-visible {
    outline: 2px solid var(--focus-ring);
    outline-offset: 2px;
  }

  .hotkey-card .card-heading { margin-bottom: 16px; }

  .recorded-hint {
    margin-top: 10px;
    color: var(--text-tertiary);
  }

  .shortcut-badge {
    max-width: 48%;
    overflow: hidden;
    padding: 6px 9px;
    color: var(--text-secondary);
    background: var(--surface-subtle);
    border: 1px solid var(--border);
    font-family: "Switzer", ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 11px;
    font-variant-numeric: tabular-nums;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .field-label {
    display: block;
    margin-bottom: 6px;
    color: var(--text-secondary);
    font-size: 11px;
    font-weight: 550;
  }

  .input {
    width: 100%;
    min-width: 0;
    height: 40px;
    padding: 0 12px;
    color: var(--text-primary);
    background: var(--input-bg);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    outline: none;
    font-size: 12px;
    transition: border-color var(--duration-fast) var(--ease-out),
                background var(--duration-fast) var(--ease-out),
                box-shadow var(--duration-fast) var(--ease-out);
  }

  .input::placeholder { color: var(--text-tertiary); }
  .input:focus {
    background: var(--input-bg-focus);
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--focus-ring);
  }
  .shortcut-hint { margin-top: 8px; }
  .save-btn { min-width: 66px; }

  .presets-heading { margin-bottom: 12px; }

  .count-badge {
    display: grid;
    place-items: center;
    min-width: 26px;
    height: 26px;
    padding: 0 7px;
    color: var(--text-secondary);
    background: var(--surface-subtle);
    border: 1px solid var(--border);
    font-size: 11px;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }

  .empty-presets {
    padding: 14px 0 2px;
    border-top: 1px solid var(--divider);
  }

  .empty-presets p {
    margin: 0 0 4px;
    color: var(--text-primary);
    font-size: 12px;
    font-weight: 600;
  }

  .empty-presets span {
    color: var(--text-secondary);
    font-size: 11px;
  }

  .preset-list { border-top: 1px solid var(--divider); }

  .preset {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
    padding: 13px 0;
    border-bottom: 1px solid var(--divider);
  }

  .preset:last-child { padding-bottom: 0; border-bottom: 0; }
  .preset-info { min-width: 0; flex: 1; }

  .preset-name {
    display: block;
    overflow: hidden;
    margin-bottom: 7px;
    color: var(--text-primary);
    font-size: 12px;
    font-weight: 600;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .size-list { display: flex; flex-wrap: wrap; gap: 6px; }

  .size-chip {
    padding: 5px 7px;
    color: var(--text-secondary);
    background: var(--surface-subtle);
    border: 1px solid var(--border);
    font-family: "Switzer", ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 10px;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }

  .remove-btn {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    flex: 0 0 auto;
    padding: 6px 7px;
    color: var(--text-tertiary);
    background: transparent;
    border: 1px solid transparent;
    font: inherit;
    font-size: 10px;
    cursor: pointer;
    transition: color var(--duration-fast) var(--ease-out),
                background var(--duration-fast) var(--ease-out),
                border-color var(--duration-fast) var(--ease-out);
  }

  .remove-btn svg { width: 14px; height: 14px; }

  .remove-btn:hover {
    color: var(--danger);
    background: var(--danger-soft);
    border-color: var(--danger-border);
  }

  .toast {
    position: fixed;
    z-index: 10;
    right: 16px;
    bottom: max(16px, env(safe-area-inset-bottom));
    left: 16px;
    width: fit-content;
    max-width: min(90vw, 460px);
    margin: 0 auto;
    padding: 11px 15px;
    color: var(--text-primary);
    background: var(--toast-surface);
    border: 1px solid var(--toast-border);
    border-radius: var(--radius);
    box-shadow: var(--toast-shadow);
    font-size: 12px;
    line-height: 1.45;
  }

  .toast-msg { display: block; overflow-wrap: anywhere; }

  @media (max-width: 420px) {
    .container { padding: 22px 16px 0; }
    .content { padding-bottom: 28px; }
    .card { padding: 16px; }
    .subtitle { margin-left: 0; }
    .permission-card { align-items: flex-start; padding: 12px; }
    .permission-icon { width: 33px; height: 33px; }
    .btn.request-btn { align-self: center; }
  }

  @media (max-width: 360px) {
    .container { padding-right: 12px; padding-left: 12px; }
    .action-row { flex-direction: column; align-items: stretch; }
    .action-row .btn { width: 100%; }
    .remove-btn span { display: none; }
    .remove-btn { padding: 7px; }
  }

</style>
