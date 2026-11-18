// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// The scene frame's host. It runs inside a sandboxed frame with an opaque
// origin (no cookies, no storage, no API, no network past its own files),
// loads the bundled library, makes the drawing surface, then runs the skin's
// scene. It passes on the forge page's events and paces the scene: stopped
// while hidden, slow while nothing changes, one frame per event under
// reduced motion. It says ready, a heartbeat each second, and errors; the
// page drops a scene that stops beating.

const root = document.documentElement;
const parentWin = window.parent;
const send = (m) => parentWin.postMessage({ laforgeScene: 1, ...m }, '*');

const handlers = {};
const last = {};
const host = {
  canvasParent: document.body,
  reducedMotion: false,
  mode: 'dark',
  app: null,
  on(type, fn) {
    (handlers[type] ||= []).push(fn);
    if (last[type]) call(fn, last[type]);
  },
};

let failed = false;
function fail(e) {
  if (failed) return;
  failed = true;
  send({ type: 'error', message: String((e && (e.stack || e.message)) || e).slice(0, 500) });
}
function call(fn, ev) {
  try {
    fn(ev);
  } catch (e) {
    fail(e);
  }
}
window.addEventListener('error', (e) => fail(e.error || e.message));
window.addEventListener('unhandledrejection', (e) => fail(e.reason));

let visible = true;
let idle = false;
function pace() {
  const t = host.app && host.app.ticker;
  if (!t) return;
  if (host.reducedMotion || !visible) {
    t.stop();
    if (host.reducedMotion && visible) t.update(performance.now());
    return;
  }
  t.maxFPS = idle ? 12 : 60;
  if (!t.started) t.start();
}

window.addEventListener('message', (e) => {
  if (e.source !== parentWin) return;
  const ev = e.data;
  if (!ev || typeof ev !== 'object' || typeof ev.type !== 'string') return;
  last[ev.type] = ev;
  if (ev.type === 'init') {
    host.reducedMotion = !!ev.reducedMotion;
    host.mode = ev.mode === 'light' ? 'light' : 'dark';
  }
  if (ev.type === 'mode') host.mode = ev.mode === 'light' ? 'light' : 'dark';
  if (ev.type === 'visibility') visible = !!ev.visible;
  if (ev.type === 'idle') idle = !!ev.idle;
  for (const fn of handlers[ev.type] || []) call(fn, ev);
  pace();
});

function classicScript(src) {
  return new Promise((resolve, reject) => {
    const s = document.createElement('script');
    s.src = src;
    s.onload = resolve;
    s.onerror = () => reject(new Error('could not load ' + src));
    document.head.append(s);
  });
}

async function start() {
  const library = root.dataset.library;
  const entry = root.dataset.entry;
  if (library !== 'pixi@8') throw new Error('unsupported library ' + library);
  const mod = await import('./_lib/pixi.mjs');
  // The unsafe-eval patch replaces PixiJS's generated code (new Function)
  // with plain functions, so the frame needs no 'unsafe-eval'.
  globalThis.PIXI = { ...mod };
  await classicScript('./_lib/pixi-unsafe-eval.js');
  const lib = mod;
  try {
    lib.Assets.setPreferences({ preferWorkers: false });
  } catch {
    /* older Assets */
  }
  const app = new lib.Application();
  await app.init({
    resizeTo: window,
    backgroundAlpha: 0,
    antialias: true,
    autoDensity: true,
    resolution: Math.min(window.devicePixelRatio || 1, 2),
  });
  document.body.append(app.canvas);
  host.app = app;
  const scene = await import('./' + entry);
  if (typeof scene.default !== 'function') throw new Error(entry + ' has no default export');
  await scene.default(lib, host);
  pace();
  send({ type: 'ready' });
  setInterval(() => send({ type: 'heartbeat' }), 1000);
}

send({ type: 'hello' });
start().catch(fail);
