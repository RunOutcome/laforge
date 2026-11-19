// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// The scene frame's host. It runs inside a sandboxed frame with an opaque
// origin (no cookies, no storage, no API, no network past its own files),
// loads the bundled library, makes the drawing surface, then runs the skin's
// scene. It passes on the forge page's events and paces the scene: stopped
// while hidden, slow while nothing changes, one frame per event under
// reduced motion. It says ready, answers the page's pings from its message
// handler (timers here may be throttled; messages are not), and reports
// errors: one before ready ends the scene, later ones are only warnings,
// since a scene that still draws should stay. The
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
let isReady = false;
let warned = 0;
const text = (e) => String((e && (e.stack || e.message)) || e).slice(0, 500);
function fail(e) {
  if (isReady) {
    // the scene is up: say so, keep drawing
    if (warned++ < 5) send({ type: 'warn', message: text(e) });
    return;
  }
  if (failed) return;
  failed = true;
  send({ type: 'error', message: text(e) });
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
  t.maxFPS = idle ? 12 : 30;
  if (!t.started) t.start();
}

window.addEventListener('message', (e) => {
  if (e.source !== parentWin) return;
  const ev = e.data;
  if (!ev || typeof ev !== 'object' || typeof ev.type !== 'string') return;
  if (ev.type === 'ping') {
    send({ type: 'pong', n: ev.n });
    return;
  }
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
    preference: 'webgl', // the tested path; WebGPU probes vary by machine
  });
  document.body.append(app.canvas);
  host.app = app;
  const scene = await import('./' + entry);
  if (typeof scene.default !== 'function') throw new Error(entry + ' has no default export');
  await scene.default(lib, host);
  pace();
  isReady = true;
  send({ type: 'ready' });
}

send({ type: 'hello' });
start().catch(fail);
