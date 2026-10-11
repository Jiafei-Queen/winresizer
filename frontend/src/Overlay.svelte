<script lang="ts">
  import {onMount} from 'svelte';
  import {Events} from "@wailsio/runtime";
  import {ResizerService} from "../bindings/winresizer";

  interface OverlayRect { id: string; app: string; title: string; x: number; y: number; w: number; h: number; isRecorded: boolean; }
  interface OverlayData { placement?: unknown; windows: OverlayRect[]; }

  let rects: OverlayRect[] = $state([]);
  let hoverId: string | null = $state(null);
  let selectedId: string | null = $state(null);
  let message: string = $state('');
  let loadError: string = $state('');
  let pollTimer: ReturnType<typeof setInterval> | undefined;

  function errText(e: unknown): string {
    if (e instanceof Error) return e.message;
    if (typeof e === 'string') return e;
    try { return JSON.stringify(e); } catch { return String(e); }
  }

  function apply(data: OverlayData): void {
    rects = data.windows ?? [];
    // Keep the selection if the window survived; drop it if it closed.
    if (selectedId && !rects.some((r) => r.id === selectedId)) selectedId = null;
    if (hoverId && !rects.some((r) => r.id === hoverId)) hoverId = null;
    loadError = '';
  }

  async function reload(): Promise<void> {
    try {
      apply(await ResizerService.GetOverlayData() as OverlayData);
    } catch (e) {
      loadError = errText(e);
    }
  }

  // The overlay page is reused across Show/Hide cycles. Keep the window
  // outlines fresh while it is up: windows move, close, and open behind
  // the picker, and a stale snapshot means clicking the wrong rect.
  function startPolling(): void {
    stopPolling();
    pollTimer = setInterval(() => { void reload(); }, 800);
  }

  function stopPolling(): void {
    if (pollTimer !== undefined) {
      clearInterval(pollTimer);
      pollTimer = undefined;
    }
  }

  function hitTest(px: number, py: number): OverlayRect | null {
    // Iterate back-to-front so the topmost outline wins.
    for (let i = rects.length - 1; i >= 0; i--) {
      const r = rects[i];
      if (px >= r.x && px <= r.x + r.w && py >= r.y && py <= r.y + r.h) return r;
    }
    return null;
  }

  function onMove(e: MouseEvent): void {
    if (selectedId) return;
    hoverId = hitTest(e.clientX, e.clientY)?.id ?? null;
  }

  function onClick(e: MouseEvent): void {
    if (selectedId) return; // confirm bar handles its own buttons
    // A result or error panel owns the screen while it is up: clicking its
    // body should not read as "clicked empty space, dismiss".
    if (message || loadError) return;
    const hit = hitTest(e.clientX, e.clientY);
    // A window already carrying this size has nothing to record, so the
    // click is inert — not a close. Its outline says why.
    if (hit?.isRecorded) return;
    if (hit) {
      selectedId = hit.id;
      hoverId = null;
    } else {
      doClose();
    }
  }

  function onKey(e: KeyboardEvent): void {
    if (e.key !== 'Escape') return;
    // Unwind one step at a time so no state can trap the user: selection,
    // then a result panel, then out. A failed record leaves no timer to
    // close the overlay, so the middle step is the only way out.
    if (selectedId) selectedId = null;
    else if (message) message = '';
    else doClose();
  }

  async function doClose(): Promise<void> {
    try { await ResizerService.CloseOverlay(); } catch { /* window hides regardless */ }
  }

  async function doRecord(): Promise<void> {
    if (!selectedId) return;
    try {
      const msg = await ResizerService.RecordWindow(selectedId);
      message = String(msg);
      // Brief success flash, then hand control back.
      setTimeout(() => { void doClose(); }, 900);
    } catch (e) {
      message = errText(e);
    }
  }

  const selected: OverlayRect | null = $derived(rects.find((r) => r.id === selectedId) ?? null);
  const hovered: OverlayRect | null = $derived(rects.find((r) => r.id === hoverId) ?? null);

  // Fixed measure so the tag neither jitters between windows of different
  // name lengths nor has to guess its own width when flipping sides.
  // Keep in step with .tag in the stylesheet below.
  const TAG_W = 300;
  const TAG_H = 32;
  const EDGE = 8;

  function tagStyle(r: OverlayRect): string {
    // Prefer above the window, flip below when it hugs the top of the desktop.
    const above = r.y - TAG_H - EDGE;
    const top = above >= EDGE
      ? above
      : Math.min(r.y + r.h + EDGE, window.innerHeight - TAG_H - EDGE);
    // Left-align with the window, or line up with its trailing edge when that
    // would run past the desktop.
    const left = r.x + TAG_W <= window.innerWidth - EDGE
      ? r.x
      : Math.max(EDGE, r.x + r.w - TAG_W);
    return `left:${left}px; top:${top}px;`;
  }

  onMount(() => {
    // Belt-and-braces: render immediately from a self-fetched snapshot,
    // then treat the backend's overlay-open event (which carries a fresh
    // OverlayData payload) as the authoritative update and keep polling
    // while visible.
    void reload();
    startPolling();
    const off = Events.On('overlay-open', (e: any) => {
      // This page is reused across show/hide cycles (the window is hidden,
      // never destroyed), so the previous pick's result and selection would
      // still be live on reopen. Because both the confirm bar and the click
      // handler key off `message`, a leftover one locks the picker up.
      // Reset here rather than in apply(): apply() also runs on the poll and
      // would wipe the success flash before its close timer fires.
      message = '';
      selectedId = null;
      hoverId = null;
      if (e?.data && Array.isArray(e.data.windows)) {
        apply(e.data as OverlayData);
      } else {
        void reload();
      }
    });
    return () => { off(); stopPolling(); };
  });
</script>

<svelte:window onkeydown={onKey} />

<div class="overlay-root" onmousemove={onMove} onclick={onClick} role="presentation">
  {#snippet dims(w: number, h: number)}
    <span class="dims">
      <span>{w}</span><span class="dim-sep" aria-hidden="true">×</span><span>{h}</span><small>px</small>
    </span>
  {/snippet}

  {#each rects as r (r.id)}
    <div
      class="outline"
      class:hovered={r.id === hoverId}
      class:selected={r.id === selectedId}
      class:recorded={r.isRecorded}
      class:dimmed={selectedId !== null && r.id !== selectedId}
      style="left:{r.x}px; top:{r.y}px; width:{r.w}px; height:{r.h}px;"
    ></div>
  {/each}

  {#if hovered && !selectedId}
    <div class="tag" class:recorded={hovered.isRecorded} style={tagStyle(hovered)}>
      <strong>{hovered.app}</strong>
      {#if hovered.isRecorded}
        {@render dims(hovered.w, hovered.h)}
        <span class="tag-note">already recorded</span>
      {:else}
        {#if hovered.title && hovered.title !== hovered.app}
          <span class="tag-title">{hovered.title}</span>
        {/if}
        {@render dims(hovered.w, hovered.h)}
      {/if}
    </div>
  {/if}

  {#if loadError}
    <div class="center-msg error" role="alert" aria-label={loadError}>
      <span class="msg-icon" aria-hidden="true">
        <svg viewBox="0 0 20 20" fill="none">
          <path d="M10 2.75 18 17H2L10 2.75Z" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
          <path d="M10 7v4.5M10 14.2v.1" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" />
        </svg>
      </span>
      <p>{loadError}</p>
      <button class="btn ghost" onclick={() => { void doClose(); }}>Close</button>
    </div>
  {/if}

  <!-- Hidden once the result lands: the overlay stays open for a beat after
       Record, and both panels at once stacked two surfaces on the desktop. -->
  {#if selected && !message}
    <div class="confirm" onclick={(e) => e.stopPropagation()} role="dialog" aria-label="Confirm record">
      <div class="confirm-glyph" aria-hidden="true">
        <svg viewBox="0 0 24 24" fill="none">
          <rect x="3.5" y="4" width="17" height="16" stroke="currentColor" stroke-width="1.6" />
          <path d="M3.8 9h16.4" stroke="currentColor" stroke-width="1.6" />
          <circle cx="7" cy="6.5" r=".7" fill="currentColor" />
          <circle cx="9.5" cy="6.5" r=".7" fill="currentColor" />
        </svg>
      </div>
      <div class="confirm-copy">
        <p class="eyebrow">Target window</p>
        <div class="confirm-app">{selected.app}</div>
        {#if selected.title && selected.title !== selected.app}
          <div class="confirm-title">{selected.title}</div>
        {/if}
      </div>
      {@render dims(selected.w, selected.h)}
      <div class="confirm-actions">
        <button class="btn ghost" onclick={() => { selectedId = null; }}>Cancel</button>
        <button class="btn primary" onclick={() => { void doRecord(); }}>Record</button>
      </div>
    </div>
  {/if}

  {#if message}
    <div class="center-msg success" role="status" aria-live="polite" aria-label={message}>
      <span class="msg-icon" aria-hidden="true">
        <svg viewBox="0 0 20 20" fill="none">
          <path d="M4.5 10.5 8 14l7.5-8" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </span>
      <p>{message}</p>
    </div>
  {/if}

  {#if !selected && !message && !loadError}
    <div class="hint">
      <span>Click a window to record its size</span>
      <kbd class="keycap">Esc</kbd>
    </div>
  {/if}
</div>

<style>
  /* These rules target <html>/<body>, so they MUST stay scoped to the
     overlay page. Bare :global(html, body) rules leak into every page of
     the bundle — that once broke the console by setting overflow:hidden
     there too (content taller than the window, no way to scroll). */
  :global(html.overlay-page),
  :global(html.overlay-page body) {
    margin: 0;
    padding: 0;
    background: transparent !important;
    backdrop-filter: none !important;
    -webkit-backdrop-filter: none !important;
    overflow: hidden;
    user-select: none;
    -webkit-user-select: none;
    cursor: crosshair;
    /* The overlay is pinned natively (overlay_pin_darwin.m); never let
       the runtime's drag machinery also treat the mask as a drag area. */
    --wails-draggable: no-drag;
  }
  .overlay-root {
    position: fixed;
    inset: 0;
    background: var(--ov-scrim);
    /* The picker floats above the Dock, so anything anchored to the bottom
       edge lands on top of it. Both bottom-anchored panels clear it by the
       same amount, which also means the hint and the confirm bar swap in
       place instead of jumping. */
    --ov-bottom-inset: max(88px, env(safe-area-inset-bottom));
  }

  /* Outlines sit on the dimmed desktop, never on a panel, so their colours
     come from the fixed --ov-* set rather than the page theme. */
  .outline {
    position: absolute;
    border: 1px solid var(--ov-stroke);
    background: var(--ov-fill-rest);
    pointer-events: none;
    transition: border-color var(--duration-fast) var(--ease-out),
                background var(--duration-fast) var(--ease-out),
                box-shadow var(--duration-fast) var(--ease-out);
  }
  .outline.hovered {
    border-color: var(--ov-stroke-active);
    background: var(--ov-fill-active);
    box-shadow: 0 0 0 3px var(--ov-ring-active);
  }
  .outline.selected {
    border-color: var(--ov-stroke-active);
    background: var(--ov-fill-active);
    box-shadow: 0 0 0 4px var(--ov-ring-active);
  }
  .outline.dimmed { opacity: 0.35; }

  /* Already carrying a saved size: shown in the danger hue so the picker
     reads as "nothing to do here" before the user clicks anything. */
  .outline.recorded {
    border-color: var(--ov-stroke-recorded);
    background: var(--ov-fill-recorded);
  }

  /* Width is fixed, not max-width: tagStyle() flips the tag against the
     window's trailing edge, which needs a known measure. */
  .tag {
    position: absolute;
    display: flex;
    align-items: center;
    gap: 8px;
    width: 300px;
    padding: 6px 10px;
    color: var(--ov-text-primary);
    background: var(--ov-panel);
    border: 1px solid var(--ov-panel-border);
    box-shadow: var(--ov-panel-shadow);
    font-size: 12px;
    pointer-events: none;
    white-space: nowrap;
    overflow: hidden;
  }
  .tag strong {
    flex: 0 0 auto;
    font-weight: 600;
  }
  .tag-title {
    overflow: hidden;
    color: var(--ov-text-secondary);
    font-size: 11px;
    text-overflow: ellipsis;
  }
  .tag.recorded {
    border-color: var(--ov-stroke-recorded);
    background: var(--ov-panel-recorded);
  }
  .tag-note { color: var(--ov-text-recorded); font-size: 11px; }

  /* The console reads sizes as the one piece of display type on a page;
     the picker keeps that pairing so the same numbers look the same in
     both windows. */
  .dims {
    display: flex;
    align-items: baseline;
    gap: 4px;
    margin-left: auto;
    padding-left: 8px;
    color: var(--ov-text-primary);
    font-family: var(--font-serif);
    font-size: 17px;
    font-variant-numeric: tabular-nums;
    font-weight: 600;
    letter-spacing: -0.02em;
    line-height: 1;
  }
  .dims .dim-sep {
    color: var(--ov-text-tertiary);
    font-size: 13px;
    font-weight: 400;
  }
  .dims small {
    color: var(--ov-text-tertiary);
    font-family: var(--font-sans);
    font-size: 10px;
    font-weight: 500;
  }

  .confirm {
    position: absolute;
    left: 50%;
    bottom: var(--ov-bottom-inset);
    transform: translateX(-50%);
    display: flex;
    align-items: center;
    gap: 16px;
    width: max-content;
    max-width: min(92vw, 620px);
    padding: 14px 16px;
    color: var(--ov-text-primary);
    background: var(--ov-panel);
    border: 1px solid var(--ov-panel-border);
    box-shadow: var(--ov-panel-shadow);
    cursor: default;
  }
  .confirm-glyph {
    display: grid;
    place-items: center;
    width: 38px;
    height: 38px;
    flex: 0 0 auto;
    color: var(--ov-accent);
    background: var(--ov-chip);
    border: 1px solid var(--ov-panel-border);
  }
  .confirm-glyph svg { width: 21px; height: 21px; }
  .confirm-copy { flex: 1 1 auto; min-width: 0; }

  .eyebrow {
    margin: 0 0 4px;
    color: var(--ov-text-tertiary);
    font-size: 10px;
    font-weight: 550;
    letter-spacing: 0.02em;
  }
  .confirm-app {
    overflow: hidden;
    font-size: 13px;
    font-weight: 600;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .confirm-title {
    overflow: hidden;
    margin-top: 2px;
    color: var(--ov-text-secondary);
    font-size: 11px;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .confirm .dims { flex: 0 0 auto; margin-left: 0; }
  .confirm-actions { display: flex; gap: 8px; flex: 0 0 auto; }

  .btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-height: 40px;
    padding: 0 14px;
    border: 1px solid transparent;
    border-radius: var(--radius);
    font-size: 12px;
    font-weight: 600;
    cursor: pointer;
    white-space: nowrap;
    transition: background var(--duration-fast) var(--ease-out),
                border-color var(--duration-fast) var(--ease-out),
                color var(--duration-fast) var(--ease-out);
  }
  .btn.primary { color: var(--ov-accent-fg); background: var(--ov-accent); }
  .btn.primary:hover { background: var(--ov-accent-hover); }

  .btn.ghost {
    color: var(--ov-text-primary);
    background: transparent;
    border-color: var(--ov-panel-border);
  }
  .btn.ghost:hover {
    color: var(--ov-accent);
    background: var(--ov-chip);
    border-color: rgba(255, 255, 255, 0.24);
  }
  .btn:focus-visible {
    outline: 2px solid var(--ov-focus-ring);
    outline-offset: 2px;
  }

  .center-msg {
    position: absolute;
    left: 50%;
    top: 40%;
    transform: translate(-50%, -50%);
    display: flex;
    align-items: center;
    gap: 12px;
    width: max-content;
    max-width: min(92vw, 460px);
    padding: 12px 16px;
    color: var(--ov-text-primary);
    background: var(--ov-panel);
    border: 1px solid var(--ov-panel-border);
    box-shadow: var(--ov-panel-shadow);
    font-size: 12px;
    line-height: 1.45;
    cursor: default;
  }
  .center-msg p { margin: 0; flex: 1 1 auto; }
  .center-msg.error {
    background: var(--ov-danger-soft);
    border-color: var(--ov-danger-border);
  }
  .msg-icon {
    display: grid;
    place-items: center;
    width: 20px;
    height: 20px;
    flex: 0 0 auto;
  }
  .msg-icon svg { width: 20px; height: 20px; }
  .center-msg.error .msg-icon { color: var(--ov-danger); }
  .center-msg.success .msg-icon { color: var(--ov-success); }

  .hint {
    position: absolute;
    left: 50%;
    bottom: var(--ov-bottom-inset);
    transform: translateX(-50%);
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 7px 12px;
    color: var(--ov-text-secondary);
    background: var(--ov-panel);
    border: 1px solid var(--ov-panel-border);
    font-size: 12px;
    pointer-events: none;
    white-space: nowrap;
  }
  .keycap {
    padding: 3px 6px;
    color: var(--ov-text-primary);
    background: var(--ov-chip);
    border: 1px solid var(--ov-panel-border);
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 10px;
  }
</style>
