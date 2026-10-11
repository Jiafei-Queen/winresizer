<script lang="ts">
  import {onMount} from 'svelte';
  import {Events} from "@wailsio/runtime";
  import {ResizerService} from "../bindings/winresizer";

  interface OverlayRect { id: string; app: string; title: string; x: number; y: number; w: number; h: number; }
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
    const hit = hitTest(e.clientX, e.clientY);
    if (hit) {
      selectedId = hit.id;
      hoverId = null;
    } else {
      doClose();
    }
  }

  function onKey(e: KeyboardEvent): void {
    if (e.key !== 'Escape') return;
    if (selectedId) selectedId = null;
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

  onMount(() => {
    // Belt-and-braces: render immediately from a self-fetched snapshot,
    // then treat the backend's overlay-open event (which carries a fresh
    // OverlayData payload) as the authoritative update and keep polling
    // while visible.
    void reload();
    startPolling();
    const off = Events.On('overlay-open', (e: any) => {
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
  {#each rects as r (r.id)}
    <div
      class="outline"
      class:hovered={r.id === hoverId}
      class:selected={r.id === selectedId}
      class:dimmed={selectedId !== null && r.id !== selectedId}
      style="left:{r.x}px; top:{r.y}px; width:{r.w}px; height:{r.h}px;"
    ></div>
  {/each}

  {#if hovered && !selectedId}
    <div
      class="tag"
      style="left:{Math.min(hovered.x, Math.max(0, window.innerWidth - 320))}px; top:{Math.max(4, hovered.y - 30)}px;"
    >
      <strong>{hovered.app}</strong>
      {#if hovered.title && hovered.title !== hovered.app}
        <span class="tag-title">{hovered.title}</span>
      {/if}
      <span class="tag-dims">{hovered.w}×{hovered.h}</span>
    </div>
  {/if}

  {#if loadError}
    <div class="center-msg error">
      <p>{loadError}</p>
      <button class="btn" onclick={(e) => { e.stopPropagation(); void doClose(); }}>Close</button>
    </div>
  {/if}

  {#if selected}
    <div class="confirm" onclick={(e) => e.stopPropagation()} role="dialog" aria-label="Confirm record">
      <div class="confirm-info">
        <strong>{selected.app}</strong>
        {#if selected.title && selected.title !== selected.app}
          <span class="confirm-title">{selected.title}</span>
        {/if}
        <span class="tag-dims">{selected.w}×{selected.h}</span>
      </div>
      <div class="confirm-actions">
        <button class="btn ghost" onclick={() => { selectedId = null; }}>Cancel</button>
        <button class="btn primary" onclick={() => { void doRecord(); }}>Record this size</button>
      </div>
    </div>
  {/if}

  {#if message}
    <div class="center-msg" role="status" aria-live="polite">
      <p>{message}</p>
    </div>
  {/if}

  {#if !selected && !message && !loadError}
    <div class="hint">Click a window to record its size · Esc to cancel</div>
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
    background: rgba(4, 6, 12, 0.45);
  }
  .outline {
    position: absolute;
    border: 1.5px solid rgba(255, 255, 255, 0.35);
    border-radius: 6px;
    background: rgba(255, 255, 255, 0.04);
    pointer-events: none;
    transition: border-color .1s ease, background .1s ease, box-shadow .1s ease;
  }
  .outline.hovered {
    border-color: #4c9aff;
    background: rgba(76, 154, 255, 0.12);
    box-shadow: 0 0 0 3px rgba(76, 154, 255, 0.25);
  }
  .outline.selected {
    border-color: #3b5bdb;
    background: rgba(59, 91, 219, 0.14);
    box-shadow: 0 0 0 3px rgba(59, 91, 219, 0.3);
  }
  .outline.dimmed { opacity: 0.35; }
  .tag {
    position: absolute;
    display: flex;
    align-items: center;
    gap: 8px;
    max-width: 320px;
    padding: 5px 10px;
    color: #e8eaf2;
    background: rgba(10, 12, 20, 0.92);
    border: 1px solid rgba(255, 255, 255, 0.18);
    border-radius: 7px;
    font-size: 12px;
    pointer-events: none;
    white-space: nowrap;
    overflow: hidden;
  }
  .tag-title {
    overflow: hidden;
    color: #9aa0b4;
    text-overflow: ellipsis;
  }
  .tag-dims {
    color: #8ec5ff;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 11px;
  }
  .confirm {
    position: absolute;
    left: 50%;
    bottom: 48px;
    transform: translateX(-50%);
    display: flex;
    align-items: center;
    gap: 16px;
    max-width: min(92vw, 560px);
    padding: 12px 14px;
    color: #e8eaf2;
    background: rgba(12, 14, 24, 0.96);
    border: 1px solid rgba(255, 255, 255, 0.2);
    border-radius: 12px;
    box-shadow: 0 12px 40px rgba(0, 0, 0, 0.5);
    cursor: default;
  }
  .confirm-info {
    display: flex;
    align-items: baseline;
    gap: 8px;
    min-width: 0;
    font-size: 13px;
  }
  .confirm-title {
    overflow: hidden;
    color: #9aa0b4;
    font-size: 12px;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .confirm-actions { display: flex; gap: 8px; flex: 0 0 auto; }
  .btn {
    min-height: 36px;
    padding: 0 14px;
    color: #fff;
    background: #3b5bdb;
    border: 1px solid transparent;
    border-radius: 9px;
    font-size: 12px;
    font-weight: 650;
    cursor: pointer;
    white-space: nowrap;
  }
  .btn:hover { background: #4c6ef5; }
  .btn.ghost { color: #c6cbdf; background: transparent; border-color: rgba(255, 255, 255, 0.25); }
  .btn.ghost:hover { background: rgba(255, 255, 255, 0.08); }
  .center-msg {
    position: absolute;
    left: 50%;
    top: 40%;
    transform: translate(-50%, -50%);
    padding: 14px 20px;
    color: #e8eaf2;
    background: rgba(12, 14, 24, 0.95);
    border: 1px solid rgba(255, 255, 255, 0.2);
    border-radius: 12px;
    font-size: 13px;
    text-align: center;
    cursor: default;
  }
  .center-msg.error { border-color: #e03131; }
  .center-msg p { margin: 0 0 10px; }
  .center-msg p:last-child { margin: 0; }
  .hint {
    position: absolute;
    left: 50%;
    bottom: 20px;
    transform: translateX(-50%);
    padding: 6px 14px;
    color: rgba(232, 234, 242, 0.75);
    background: rgba(10, 12, 20, 0.7);
    border-radius: 999px;
    font-size: 12px;
    pointer-events: none;
    white-space: nowrap;
  }
</style>
