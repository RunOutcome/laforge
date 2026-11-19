// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// Scene shows a skin's animated scene behind the forge: a frame the forge
// serves sandboxed (opaque origin: no cookies, storage, API or network past
// its own files) and the page sandboxes again. The page tells it what it
// may know — the mode, the layout, and each visible pane's status, focus
// and box, under ids that mean nothing outside the scene — never titles,
// output or paths. A scene that errs or stops beating is removed and the
// skin's stylesheet draws on its own.
import { useEffect, useRef, useState } from 'react';
import type { Mode } from './theme';

export interface ScenePane {
  id: string;
  status: string;
  active: boolean;
  tab: number;
  rect: { x: number; y: number; w: number; h: number };
}

export type SceneEvent =
  | { type: 'init'; mode: Mode; layout: 'classic' | 'rail'; reducedMotion: boolean; size: { w: number; h: number } }
  | { type: 'panes'; panes: ScenePane[]; chrome: { top: number; left: number } }
  | { type: 'mode'; mode: Mode }
  | { type: 'resize'; size: { w: number; h: number } }
  | { type: 'visibility'; visible: boolean }
  | { type: 'idle'; idle: boolean };

/** How long without a status change before the scene is told it is idle. */
export const IDLE_MS = 30_000;
/** The page pings a ready scene this often while the page is visible. */
export const PING_MS = 2_000;
/** A visible, ready scene that answers no ping for this long is dropped. */
export const DEAD_MS = 12_000;
/** A scene that is not ready by then is dropped. */
export const READY_MS = 30_000;

const ids = new WeakMap<Element, string>();
let nextId = 1;

/**
 * readPanes reads the visible panes from the page: geometry and status only.
 * In a tall tab a pane's box is cut to the part on screen, and a pane
 * scrolled out of view is left out.
 */
export function readPanes(root: ParentNode): { panes: ScenePane[]; chrome: { top: number; left: number } } {
  const panes: ScenePane[] = [];
  root.querySelectorAll<HTMLElement>('.pane[data-status]').forEach((el) => {
    const b = el.getBoundingClientRect();
    const clip = el.closest('.tab-scroll')?.getBoundingClientRect();
    const r = clip ? { left: Math.max(b.left, clip.left), top: Math.max(b.top, clip.top), right: Math.min(b.right, clip.right), bottom: Math.min(b.bottom, clip.bottom) } : b;
    const width = r.right - r.left;
    const height = r.bottom - r.top;
    if (width < 1 || height < 1) return;
    if (el.checkVisibility && !el.checkVisibility({ visibilityProperty: true } as CheckVisibilityOptions)) return;
    let id = ids.get(el);
    if (!id) {
      id = 'p' + nextId++;
      ids.set(el, id);
    }
    panes.push({
      id,
      status: el.dataset.status ?? 'idle',
      active: el.dataset.active === 'true',
      tab: 0,
      rect: { x: Math.round(r.left), y: Math.round(r.top), w: Math.round(width), h: Math.round(height) },
    });
  });
  const top = panes.length ? Math.min(...panes.map((p) => p.rect.y)) : 0;
  const left = panes.length ? Math.min(...panes.map((p) => p.rect.x)) : 0;
  return { panes, chrome: { top, left } };
}

interface Props {
  url: string;
  mode: Mode;
  layout: 'classic' | 'rail';
  /** a zoomed pane covers the scene: it pauses */
  covered?: boolean;
}

export function Scene({ url, mode, layout, covered = false }: Props) {
  const frame = useRef<HTMLIFrameElement>(null);
  const [failed, setFailed] = useState<{ url: string; why: string } | null>(null);
  const [attempt, setAttempt] = useState(0);
  const now = useRef({ mode, layout, covered });
  now.current = { mode, layout, covered };
  const post = useRef<(ev: SceneEvent) => void>(() => undefined);

  useEffect(() => {
    if (failed?.url === url) return;
    const root = document.documentElement;
    const motion = window.matchMedia?.('(prefers-reduced-motion: reduce)');
    let ready = false;
    let lastPong = Date.now();
    let ping = 0;
    const started = Date.now();
    let lastChange = Date.now();
    let idle = false;
    let lastStatus = '';
    let lastSent = '';
    let pending: number | undefined;
    let dead = false;

    const send = (ev: SceneEvent) => frame.current?.contentWindow?.postMessage(ev, '*');
    post.current = send;
    const size = () => ({ w: window.innerWidth, h: window.innerHeight });
    const visible = () => document.visibilityState === 'visible' && !now.current.covered;

    const sendPanes = (force = false) => {
      const forge = document.querySelector('.forge') ?? document;
      const p = readPanes(forge);
      const status = p.panes.map((x) => x.id + x.status + x.active).join();
      if (status !== lastStatus) {
        lastStatus = status;
        lastChange = Date.now();
        if (idle) {
          idle = false;
          send({ type: 'idle', idle: false });
        }
      }
      const key = JSON.stringify(p);
      if (force || key !== lastSent) {
        lastSent = key;
        send({ type: 'panes', ...p });
      }
    };
    // at most ten updates a second
    const soon = () => {
      if (pending === undefined) pending = window.setTimeout(() => ((pending = undefined), sendPanes()), 100);
    };

    const fail = (why: string) => {
      if (dead) return;
      dead = true;
      delete root.dataset.scene;
      console.warn('[laforge] scene removed: ' + why);
      setFailed({ url, why });
    };

    const onMessage = (e: MessageEvent) => {
      if (!frame.current || e.source !== frame.current.contentWindow) return;
      const m = e.data as { laforgeScene?: number; type?: string; message?: string };
      if (!m || m.laforgeScene !== 1) return;
      lastPong = Date.now();
      if (m.type === 'hello') {
        send({ type: 'init', mode: now.current.mode, layout: now.current.layout, reducedMotion: !!motion?.matches, size: size() });
        sendPanes(true);
        send({ type: 'visibility', visible: visible() });
        send({ type: 'idle', idle });
      } else if (m.type === 'ready') {
        ready = true;
        root.dataset.scene = 'on';
      } else if (m.type === 'error') {
        fail(String(m.message ?? 'error').slice(0, 300));
      } else if (m.type === 'warn') {
        console.warn('[laforge] scene: ' + String(m.message ?? '').slice(0, 300));
      }
    };
    const onResize = () => {
      send({ type: 'resize', size: size() });
      soon();
    };
    const onVisibility = () => {
      // a hidden page's frame may have slept: start counting again
      if (document.visibilityState === 'visible') lastPong = Date.now();
      send({ type: 'visibility', visible: visible() });
    };

    const watch = window.setInterval(() => {
      const t = Date.now();
      if (document.visibilityState !== 'visible') return;
      if (!ready && t - started > READY_MS) fail('it did not start within ' + READY_MS / 1000 + ' s');
      else if (ready && t - lastPong > DEAD_MS) fail('it stopped answering');
      else if (ready) frame.current?.contentWindow?.postMessage({ type: 'ping', n: ++ping }, '*');
    }, PING_MS);

    const idleWatch = window.setInterval(() => {
      if (!idle && Date.now() - lastChange > IDLE_MS) {
        idle = true;
        send({ type: 'idle', idle: true });
      }
    }, 1000);

    const observer = new MutationObserver(soon);
    const forge = document.querySelector('.forge');
    if (forge) observer.observe(forge, { subtree: true, childList: true, attributes: true, attributeFilter: ['data-status', 'data-active', 'style', 'class'] });
    window.addEventListener('message', onMessage);
    window.addEventListener('resize', onResize);
    document.addEventListener('scroll', soon, true); // a tall tab scrolling moves its panes
    document.addEventListener('visibilitychange', onVisibility);
    return () => {
      dead = true;
      observer.disconnect();
      window.clearInterval(watch);
      window.clearInterval(idleWatch);
      window.clearTimeout(pending);
      window.removeEventListener('message', onMessage);
      window.removeEventListener('resize', onResize);
      document.removeEventListener('scroll', soon, true);
      document.removeEventListener('visibilitychange', onVisibility);
      delete root.dataset.scene;
      post.current = () => undefined;
    };
  }, [url, failed, attempt]);

  useEffect(() => post.current({ type: 'mode', mode }), [mode]);
  useEffect(() => post.current({ type: 'visibility', visible: document.visibilityState === 'visible' && !covered }), [covered]);

  if (failed?.url === url && !failed.why) return null; // dismissed
  if (failed?.url === url)
    return (
      <div className="scene-stopped" role="status">
        <span>Animation stopped: {failed.why}.</span>
        <button
          onClick={() => {
            setFailed(null);
            setAttempt((a) => a + 1);
          }}
        >
          Retry
        </button>
        <button aria-label="Dismiss" onClick={() => setFailed({ ...failed, why: '' })}>
          ×
        </button>
      </div>
    );
  return (
    <iframe
      key={attempt}
      ref={frame}
      className="scene-frame"
      src={url}
      sandbox="allow-scripts"
      referrerPolicy="no-referrer"
      title="Theme scene"
      aria-hidden="true"
      tabIndex={-1}
    />
  );
}
