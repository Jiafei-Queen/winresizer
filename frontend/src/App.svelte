<script lang="ts">
  import {onMount} from 'svelte';
  import {ResizerService} from "../bindings/winresizer";

  interface Size { w: number; h: number; }
  interface AppPreset { id: string; appName: string; sizes: Size[]; lastIndex: number; }
  interface Focused { app: string; title: string; w: number; h: number; monitor: number; }

  let hotkey: string = $state('');
  let newHotkey: string = $state('');
  let axGranted: boolean = $state(false);
  let apps: AppPreset[] = $state([]);
  let focused: Focused | null = $state(null);
  let message: string = $state('');
  let toastEl: HTMLElement | undefined = $state();
  let toastTimer: ReturnType<typeof setTimeout>;

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
    await refresh();
    await refreshFocused();
  });
</script>

<main class="container">
  <h1 class="title">WinResizer</h1>
  <p class="subtitle">Focus a window, size it by hand, record it — then cycle sizes with the global hotkey.</p>

  <section class="card">
    <h2>1 · Permission</h2>
    <p>
      Accessibility: <strong class={axGranted ? 'ok' : 'warn'}>{axGranted ? 'granted' : 'not granted'}</strong>
      {#if !axGranted}
        <button class="btn" onclick={doRequestAX}>Request</button>
      {/if}
    </p>
    {#if !axGranted}
      <p class="hint">Move/Resize need Accessibility on macOS. Grant it in System Settings, then re-focus.</p>
    {/if}
  </section>

  <section class="card">
    <h2>2 · Hotkey</h2>
    <p>Current: <code>{hotkey}</code></p>
    <div class="row">
      <input class="input" bind:value={newHotkey} placeholder="e.g. Ctrl+Alt+R" autocomplete="off"/>
      <button class="btn" onclick={doSaveHotkey}>Save</button>
    </div>
    <p class="hint">Takes effect after an app restart.</p>
  </section>

  <section class="card">
    <h2>3 · Record</h2>
    {#if focused}
      <p><strong>{focused.app}</strong> — {focused.w}×{focused.h}{#if focused.title} <span class="dim">({focused.title})</span>{/if}</p>
    {:else}
      <p class="dim">No focused window detected.</p>
    {/if}
    <div class="row">
      <button class="btn" onclick={refreshFocused}>Refresh</button>
      <button class="btn primary" onclick={doRecord}>Record this size</button>
      <button class="btn" onclick={doCycle}>Cycle now</button>
    </div>
  </section>

  <section class="card">
    <h2>Presets</h2>
    {#if apps.length === 0}
      <p class="dim">Nothing recorded yet.</p>
    {:else}
      {#each apps as a (a.id)}
        <div class="preset">
          <div>
            <strong>{a.appName || a.id}</strong>
            <span class="dim"> — {a.sizes.map((s) => `${s.w}×${s.h}`).join(', ')}</span>
          </div>
          <button class="btn danger" onclick={() => doRemove(a.id)}>Remove</button>
        </div>
      {/each}
    {/if}
  </section>
</main>

{#if message}
  <div class="toast is-visible" role="status" aria-live="polite">
    <span class="toast-msg">{message}</span>
  </div>
{/if}

<style>
  :global(body) {
    margin: 0;
    font-family: -apple-system, system-ui, sans-serif;
    background: #06070f;
    color: #e8eaf2;
  }
  .container { max-width: 480px; margin: 0 auto; padding: 24px 16px 48px; }
  .title { font-size: 24px; margin: 0 0 4px; }
  .subtitle { color: #9aa0b4; font-size: 13px; margin: 0 0 16px; }
  .card {
    background: #12141f;
    border: 1px solid #23263a;
    border-radius: 10px;
    padding: 14px 16px;
    margin-bottom: 12px;
  }
  .card h2 { font-size: 14px; margin: 0 0 8px; color: #c6cbdf; }
  .card p { font-size: 13px; margin: 6px 0; }
  .row { display: flex; gap: 8px; margin-top: 8px; flex-wrap: wrap; }
  .input {
    flex: 1;
    min-width: 140px;
    background: #0a0c15;
    border: 1px solid #2c3049;
    color: inherit;
    border-radius: 8px;
    padding: 8px 10px;
    font-size: 13px;
  }
  .btn {
    background: #1d2133;
    border: 1px solid #2c3049;
    color: inherit;
    border-radius: 8px;
    padding: 8px 12px;
    font-size: 13px;
    cursor: pointer;
  }
  .btn:hover { background: #262b44; }
  .btn.primary { background: #3b5bdb; border-color: #3b5bdb; color: #fff; }
  .btn.primary:hover { background: #4c6ef5; }
  .btn.danger { background: transparent; border-color: #5c2b2b; color: #ffa8a8; }
  .ok { color: #69db7c; }
  .warn { color: #ffd43b; }
  .dim { color: #868e96; }
  .hint { color: #868e96; font-size: 12px; }
  code { background: #0a0c15; padding: 2px 6px; border-radius: 4px; }
  .preset {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
    padding: 8px 0;
    border-top: 1px solid #1d2133;
    font-size: 13px;
  }
  .toast {
    position: fixed;
    left: 50%;
    bottom: 24px;
    transform: translateX(-50%);
    background: #1d2133;
    border: 1px solid #2c3049;
    border-radius: 8px;
    padding: 10px 16px;
    font-size: 13px;
    max-width: 90vw;
  }
</style>
