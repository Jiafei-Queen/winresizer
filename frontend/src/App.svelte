<script lang="ts">
  import {onMount} from 'svelte';
  import {ResizerService} from "../bindings/winresizer";

  interface Size { w: number; h: number; }
  interface AppPreset { id: string; appName: string; sizes: Size[]; lastIndex: number; }
  interface Focused { app: string; title: string; w: number; h: number; monitor: number; }
  type Theme = 'light' | 'dark';

  const themeStorageKey = 'winresizer-theme';

  let hotkey: string = $state('');
  let newHotkey: string = $state('');
  let axGranted: boolean = $state(false);
  let apps: AppPreset[] = $state([]);
  let focused: Focused | null = $state(null);
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

  async function refreshFocused(): Promise<void> {
    try {
      focused = await ResizerService.FocusedInfo() as Focused;
    } catch (e) {
      focused = null;
      showMessage(errText(e));
    }
  }

  async function doRecord(): Promise<void> {
    try {
      showMessage(await ResizerService.RecordFocused());
      await refresh();
      await refreshFocused();
    } catch (e) {
      showMessage(errText(e));
    }
  }

  async function doCycle(): Promise<void> {
    try {
      showMessage(await ResizerService.CycleFocused());
      await refresh();
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
    await refreshFocused();
  });
</script>

<main class="container">
  <header class="app-header">
    <div class="brand-mark" aria-hidden="true">
      <svg viewBox="0 0 24 24" fill="none">
        <rect x="3.5" y="4" width="17" height="16" rx="3" stroke="currentColor" stroke-width="1.7" />
        <path d="M8 9h8M8 13h5" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" />
        <circle cx="16.5" cy="15.5" r="1.5" fill="currentColor" />
      </svg>
    </div>
    <div class="brand-copy">
      <p class="eyebrow">WINDOW WORKSPACE</p>
      <h1 class="title">WinResizer</h1>
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
      <span
        class:needs-attention={!axGranted}
        class="app-status"
        aria-label={axGranted ? 'Accessibility permission granted' : 'Accessibility setup needed'}
      >
        <span class="status-dot" aria-hidden="true"></span>
        {axGranted ? 'Ready' : 'Setup'}
      </span>
    </div>
  </header>

  <p class="subtitle">Save window sizes and switch between them with a shortcut.</p>

  {#if !axGranted}
    <section class="permission-card" aria-label="Accessibility permission required">
      <div class="permission-icon" aria-hidden="true">
        <svg viewBox="0 0 20 20" fill="none">
          <path d="M10 2.75 18 17H2L10 2.75Z" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
          <path d="M10 7v4.5M10 14.2v.1" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" />
        </svg>
      </div>
      <div class="permission-copy">
        <p class="eyebrow">ACCESSIBILITY</p>
        <h2 class="permission-title">Allow window control</h2>
        <p class="hint">WinResizer needs permission to move and resize windows.</p>
      </div>
      <button class="btn request-btn" onclick={doRequestAX}>Enable</button>
    </section>
  {/if}

  <section class="card focus-card" aria-labelledby="active-window-title">
    <div class="card-heading focus-heading">
      <div>
        <p class="eyebrow">ACTIVE WINDOW</p>
        <h2 id="active-window-title">Current size</h2>
      </div>
      <button class="icon-btn" onclick={refreshFocused} aria-label="Refresh focused window" title="Refresh window info">
        <svg viewBox="0 0 20 20" fill="none" aria-hidden="true">
          <path d="M16 8a6.1 6.1 0 0 0-10.5-2L4 7.5M4 4v3.5h3.5M4 12a6.1 6.1 0 0 0 10.5 2L16 12.5M16 16v-3.5h-3.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </button>
    </div>

    {#if focused}
      <div class="window-summary">
        <div class="window-overview">
          <div class="window-glyph" aria-hidden="true">
            <svg viewBox="0 0 24 24" fill="none">
              <rect x="3.5" y="4" width="17" height="16" rx="3" stroke="currentColor" stroke-width="1.6" />
              <path d="M3.8 9h16.4" stroke="currentColor" stroke-width="1.6" />
              <circle cx="7" cy="6.5" r=".7" fill="currentColor" />
              <circle cx="9.5" cy="6.5" r=".7" fill="currentColor" />
            </svg>
          </div>
          <div class="window-copy">
            <div class="window-app" title={focused.title || focused.app}>{focused.app}</div>
            {#if focused.title && focused.title !== focused.app}
              <p class="window-title" title={focused.title}>{focused.title}</p>
            {/if}
          </div>
        </div>
        <p class="dimensions" aria-label={`${focused.w} by ${focused.h} pixels`}>
          <span>{focused.w}</span>
          <span class="dimension-separator" aria-hidden="true">×</span>
          <span>{focused.h}</span>
          <small>px</small>
        </p>
      </div>
    {:else}
      <div class="window-summary empty-window">
        <div class="window-glyph" aria-hidden="true">
          <svg viewBox="0 0 24 24" fill="none">
            <rect x="3.5" y="4" width="17" height="16" rx="3" stroke="currentColor" stroke-width="1.6" />
            <path d="M3.8 9h16.4" stroke="currentColor" stroke-width="1.6" />
            <circle cx="7" cy="6.5" r=".7" fill="currentColor" />
            <circle cx="9.5" cy="6.5" r=".7" fill="currentColor" />
          </svg>
        </div>
        <div>
          <strong>No active window found</strong>
          <p>Focus a window, then refresh to see its size.</p>
        </div>
      </div>
    {/if}

    <div class="row action-row">
      <button class="btn primary" onclick={doRecord}>
        <svg viewBox="0 0 20 20" fill="none" aria-hidden="true">
          <path d="M10 4v12M4 10h12" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" />
        </svg>
        Record this size
      </button>
      <button class="btn secondary" onclick={doCycle}>
        <svg viewBox="0 0 20 20" fill="none" aria-hidden="true">
          <path d="M4 6.5h9a3 3 0 0 1 0 6h-1M16 13.5l-3-3-3 3" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
        Cycle sizes
      </button>
    </div>
  </section>

  <section class="card hotkey-card" aria-labelledby="shortcut-title">
    <div class="card-heading">
      <div>
        <p class="eyebrow">QUICK ACTION</p>
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
        <p class="eyebrow">YOUR SAVED SIZES</p>
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
</main>

{#if message}
  <div class="toast" role="status" aria-live="polite">
    <span class="toast-msg">{message}</span>
  </div>
{/if}

<style>
  .container {
    display: flex;
    flex-direction: column;
    gap: 14px;
    width: min(100%, 580px);
    min-height: 100vh;
    min-height: 100dvh;
    margin: 0 auto;
    padding: 25px 22px 30px;
  }

  .app-header {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .brand-mark {
    display: grid;
    place-items: center;
    width: 44px;
    height: 44px;
    flex: 0 0 auto;
    color: var(--accent);
    background: var(--accent-soft);
    border: 1px solid var(--brand-border);
    border-radius: 14px;
  }

  .brand-mark svg { width: 24px; height: 24px; }
  .brand-copy { min-width: 0; }

  .eyebrow {
    margin: 0 0 4px;
    color: var(--text-tertiary);
    font-size: 10px;
    font-weight: 700;
    letter-spacing: .12em;
  }

  .title {
    margin: 0;
    color: var(--text-primary);
    font-size: 22px;
    font-weight: 700;
    letter-spacing: -.035em;
    line-height: 1.15;
  }

  .header-actions {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-left: auto;
  }

  .theme-toggle {
    display: grid;
    place-items: center;
    width: 36px;
    height: 36px;
    flex: 0 0 auto;
    padding: 0;
    color: var(--icon-muted);
    background: var(--icon-surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    cursor: pointer;
    transition: color .16s ease, background .16s ease, border-color .16s ease;
  }

  .theme-toggle:hover {
    color: var(--accent);
    background: var(--accent-soft);
    border-color: var(--brand-border);
  }

  .theme-toggle svg { width: 18px; height: 18px; }

  .app-status {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    padding: 7px 10px;
    color: var(--success);
    background: var(--success-soft);
    border: 1px solid var(--success-border);
    border-radius: 999px;
    font-size: 11px;
    font-weight: 650;
    white-space: nowrap;
  }

  .app-status.needs-attention {
    color: var(--warning);
    background: var(--warning-soft);
    border-color: var(--warning-border);
  }

  .status-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: currentColor;
  }

  .subtitle {
    margin: -5px 0 1px 56px;
    color: var(--text-secondary);
    font-size: 12px;
    line-height: 1.5;
  }

  .card,
  .permission-card {
    border: 1px solid var(--border);
    border-radius: var(--card-radius);
    background: var(--surface);
    box-shadow: var(--shadow-card);
  }

  .card { padding: 18px; }

  .card-heading {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 15px;
  }

  .card h2 {
    margin: 0;
    color: var(--text-primary);
    font-size: 15px;
    font-weight: 650;
    letter-spacing: -.015em;
  }

  .permission-card {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 13px 15px;
    background: var(--warning-surface);
    border-color: var(--warning-border);
    box-shadow: none;
  }

  .permission-icon {
    display: grid;
    place-items: center;
    width: 36px;
    height: 36px;
    flex: 0 0 auto;
    color: var(--warning);
    background: var(--warning-icon-surface);
    border-radius: 11px;
  }

  .permission-icon svg { width: 19px; height: 19px; }
  .permission-copy { min-width: 0; flex: 1; }
  .permission-copy .eyebrow { margin-bottom: 3px; }

  .permission-title {
    margin: 0 0 3px;
    color: var(--text-primary);
    font-size: 13px;
    font-weight: 650;
  }

  .hint {
    margin: 0;
    color: var(--text-secondary);
    font-size: 11px;
    line-height: 1.45;
  }

  .focus-card {
    background: linear-gradient(145deg, var(--surface) 30%, var(--focus-surface) 100%);
    border-color: var(--focus-border);
    box-shadow: var(--shadow-focus);
  }

  .focus-heading { margin-bottom: 14px; }

  .icon-btn {
    display: grid;
    place-items: center;
    width: 36px;
    height: 36px;
    flex: 0 0 auto;
    padding: 0;
    color: var(--icon-muted);
    background: var(--icon-surface);
    border: 1px solid var(--border);
    border-radius: 10px;
    cursor: pointer;
    transition: color .16s ease, background .16s ease, border-color .16s ease;
  }

  .icon-btn:hover {
    color: var(--accent);
    background: var(--accent-soft);
    border-color: var(--icon-hover-border);
  }

  .icon-btn svg { width: 18px; height: 18px; }

  .window-summary {
    min-width: 0;
    margin-bottom: 14px;
    padding: 14px 15px;
    background: var(--surface-subtle);
    border: 1px solid var(--window-border);
    border-radius: 13px;
  }

  .window-overview {
    display: flex;
    align-items: center;
    gap: 11px;
    min-width: 0;
  }

  .window-glyph {
    display: grid;
    place-items: center;
    width: 38px;
    height: 38px;
    flex: 0 0 auto;
    color: var(--window-icon);
    background: var(--window-icon-surface);
    border-radius: 11px;
  }

  .window-glyph svg { width: 21px; height: 21px; }
  .window-copy { min-width: 0; }

  .window-app {
    overflow: hidden;
    color: var(--text-primary);
    font-size: 13px;
    font-weight: 650;
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
    gap: 7px;
    margin: 13px 0 0;
    color: var(--dimension);
    font-size: 31px;
    font-variant-numeric: tabular-nums;
    font-weight: 680;
    letter-spacing: -.045em;
    line-height: 1.1;
  }

  .dimension-separator {
    color: var(--dimension-muted);
    font-size: 22px;
    font-weight: 400;
  }

  .dimensions small {
    margin-left: 1px;
    color: var(--text-tertiary);
    font-size: 11px;
    font-weight: 550;
    letter-spacing: 0;
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
    font-weight: 650;
  }

  .empty-window p {
    margin: 4px 0 0;
    color: var(--text-secondary);
    font-size: 11px;
    line-height: 1.45;
  }

  .row { display: flex; align-items: center; gap: 9px; }
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
    border-radius: 10px;
    box-shadow: var(--button-shadow);
    font-size: 12px;
    font-weight: 650;
    cursor: pointer;
    white-space: nowrap;
    transition: background .16s ease, border-color .16s ease, box-shadow .16s ease, transform .16s ease;
  }

  .btn:hover {
    background: var(--accent-hover);
    box-shadow: var(--button-hover-shadow);
  }

  .btn:active { transform: translateY(1px); }
  .btn svg { width: 16px; height: 16px; flex: 0 0 auto; }

  .btn.secondary,
  .btn.save-btn {
    color: var(--secondary-text);
    background: var(--secondary-surface);
    border-color: var(--secondary-border);
    box-shadow: none;
  }

  .btn.secondary:hover,
  .btn.save-btn:hover {
    color: var(--accent);
    background: var(--accent-soft);
    border-color: var(--secondary-hover-border);
    box-shadow: none;
  }

  .btn.request-btn {
    min-height: 36px;
    padding: 0 13px;
    color: var(--warning);
    background: var(--request-surface);
    border-color: var(--request-border);
    box-shadow: none;
    font-size: 11px;
  }

  .btn.request-btn:hover {
    background: var(--request-hover-surface);
    border-color: var(--request-hover-border);
  }

  .btn:focus-visible,
  .icon-btn:focus-visible,
  .remove-btn:focus-visible {
    outline: 3px solid var(--focus-ring);
    outline-offset: 2px;
  }

  .theme-toggle:focus-visible {
    outline: 3px solid var(--focus-ring);
    outline-offset: 2px;
  }

  .hotkey-card .card-heading { margin-bottom: 15px; }

  .shortcut-badge {
    max-width: 48%;
    overflow: hidden;
    padding: 7px 9px;
    color: var(--shortcut-text);
    background: var(--shortcut-surface);
    border: 1px solid var(--shortcut-border);
    border-radius: 8px;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
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
    padding: 0 11px;
    color: var(--text-primary);
    background: var(--input-surface);
    border: 1px solid var(--input-border);
    border-radius: 10px;
    outline: none;
    font-size: 12px;
    transition: border-color .16s ease, box-shadow .16s ease, background .16s ease;
  }

  .input::placeholder { color: var(--input-placeholder); }
  .input:focus { background: var(--surface); border-color: var(--input-focus-border); box-shadow: 0 0 0 3px var(--input-focus-ring); }
  .shortcut-hint { margin-top: 8px; }
  .save-btn { min-width: 66px; }

  .presets-heading { margin-bottom: 11px; }

  .count-badge {
    display: grid;
    place-items: center;
    min-width: 26px;
    height: 26px;
    padding: 0 7px;
    color: var(--count-text);
    background: var(--count-surface);
    border: 1px solid var(--count-border);
    border-radius: 8px;
    font-size: 11px;
    font-weight: 650;
    font-variant-numeric: tabular-nums;
  }

  .empty-presets {
    padding: 13px 0 1px;
    border-top: 1px solid var(--divider);
  }

  .empty-presets p {
    margin: 0 0 4px;
    color: var(--empty-text);
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
    padding: 12px 0;
    border-bottom: 1px solid var(--divider);
  }

  .preset:last-child { padding-bottom: 0; border-bottom: 0; }
  .preset-info { min-width: 0; flex: 1; }

  .preset-name {
    display: block;
    overflow: hidden;
    margin-bottom: 7px;
    color: var(--preset-text);
    font-size: 12px;
    font-weight: 650;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .size-list { display: flex; flex-wrap: wrap; gap: 6px; }

  .size-chip {
    padding: 5px 7px;
    color: var(--chip-text);
    background: var(--chip-surface);
    border: 1px solid var(--chip-border);
    border-radius: 7px;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
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
    color: var(--remove-muted);
    background: transparent;
    border: 1px solid transparent;
    border-radius: 8px;
    font: inherit;
    font-size: 10px;
    cursor: pointer;
    transition: color .16s ease, background .16s ease, border-color .16s ease;
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
    color: var(--toast-text);
    background: var(--toast-surface);
    border: 1px solid var(--toast-border);
    border-radius: 11px;
    box-shadow: var(--toast-shadow);
    font-size: 12px;
    line-height: 1.45;
  }

  .toast-msg { display: block; overflow-wrap: anywhere; }

  @media (max-width: 420px) {
    .container { padding: 20px 15px 26px; }
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

  @media (prefers-reduced-motion: reduce) {
    .btn,
    .theme-toggle,
    .icon-btn,
    .remove-btn,
    .input { transition: none; }
  }
</style>
