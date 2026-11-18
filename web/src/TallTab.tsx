// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// Tall tabs: a tab may be 1x to 4x screens tall. Its body scrolls; the
// panes are laid out across the tall area (layout.ts's tall) and keep their
// slots, so a size change only resizes their boxes and never remounts a
// terminal. The wheel scrolls the area over headers and gutters; inside a
// terminal it stays the terminal's own scrollback.
import { useEffect, useRef, useState } from 'react';
import type React from 'react';
import * as L from './layout';
import './tall.css';

const smooth = (): ScrollBehavior => (window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth');

/** activeScroller is the active tab's scrolling body, if any. */
export function activeScroller(): HTMLElement | null {
  return document.querySelector<HTMLElement>('.tab-body.is-active > .tab-scroll');
}

/** pageBy scrolls the active tab one screen down (1) or up (-1). */
export function pageBy(dir: 1 | -1) {
  const el = activeScroller();
  if (!el || !el.classList.contains('is-tall')) return;
  const page = Math.round(el.scrollTop / el.clientHeight) + dir;
  el.scrollTo({ top: Math.max(0, page) * el.clientHeight, behavior: smooth() });
}

/** nextSize is the size after s, wrapping 4x back to 1x. */
export function nextSize(s: L.Size): L.Size {
  return L.SIZES[(L.SIZES.indexOf(s) + 1) % L.SIZES.length];
}

/**
 * edgeScroll scrolls el while the pointer is near its top or bottom edge,
 * for drags that reach panes scrolled off-screen. onScroll runs after each
 * step, so the drag can re-read what is under the still pointer.
 */
export function edgeScroll(el: HTMLElement | null, onScroll: () => void) {
  let y = -1;
  let frame = 0;
  const EDGE = 36;
  const tick = () => {
    frame = 0;
    if (!el || y < 0 || el.scrollHeight <= el.clientHeight) return;
    const box = el.getBoundingClientRect();
    const speed = y < box.top + EDGE ? -(box.top + EDGE - y) : y > box.bottom - EDGE ? y - (box.bottom - EDGE) : 0;
    if (!speed) return;
    const before = el.scrollTop;
    el.scrollTop += Math.max(-24, Math.min(24, speed / 2 + Math.sign(speed) * 2));
    if (el.scrollTop !== before) onScroll();
    frame = requestAnimationFrame(tick);
  };
  return {
    at(clientY: number) {
      y = clientY;
      if (!frame) frame = requestAnimationFrame(tick);
    },
    stop() {
      y = -1;
      if (frame) cancelAnimationFrame(frame);
      frame = 0;
    },
  };
}

/**
 * startGutterDrag resizes split g of tab t as the mouse moves, measuring
 * against the tall area as it is now (it may scroll under the drag).
 */
export function startGutterDrag(e: React.MouseEvent, g: L.TallGutter, update: (fn: (root: L.Node) => L.Node) => void) {
  e.preventDefault();
  const row = g.split.dir === 'row';
  const canvas = (e.currentTarget as HTMLElement).parentElement!;
  const at = (x: number, y: number) => {
    const box = canvas.getBoundingClientRect();
    return row ? (x - box.left) / box.width : (y - box.top) / box.height;
  };
  const span = row ? g.rect.w : g.rect.h;
  const start = at(e.clientX, e.clientY);
  const a = g.fracs[g.index - 1];
  const b = g.fracs[g.index];
  let last = { x: e.clientX, y: e.clientY };
  document.body.classList.add(`dragging-${g.split.dir}`);
  const apply = () => {
    const d = (at(last.x, last.y) - start) / span;
    const na = Math.max(0.08, Math.min(a + b - 0.08, a + d));
    const sizes = [...g.fracs];
    sizes[g.index - 1] = na;
    sizes[g.index] = a + b - na;
    update((root) => L.resize(root, g.split.id, sizes));
  };
  const edges = edgeScroll(row ? null : canvas.closest<HTMLElement>('.tab-scroll'), apply);
  const move = (ev: MouseEvent) => {
    last = { x: ev.clientX, y: ev.clientY };
    edges.at(ev.clientY);
    apply();
  };
  const up = () => {
    edges.stop();
    document.body.classList.remove(`dragging-${g.split.dir}`);
    window.removeEventListener('mousemove', move);
    window.removeEventListener('mouseup', up);
  };
  window.addEventListener('mousemove', move);
  window.addEventListener('mouseup', up);
}

interface BodyProps {
  tab: L.Tab;
  active: boolean;
  /** the tab laid out at its size (tall(tab.root, size)) */
  lay: L.Tall;
  children: React.ReactNode;
}

/**
 * TabBody is one tab's pane area: a scrolling box holding the tall area the
 * panes are placed in, with page dots when it is taller than a screen. A
 * zoomed tab shows its first screen, the zoomed pane drawn over it; the area
 * keeps its height, so the hidden panes keep their sizes and are not refit.
 */
export function TabBody({ tab, active, lay, children }: BodyProps) {
  const scroller = useRef<HTMLDivElement>(null);
  const zoomed = !!tab.zoom;
  const tallNow = lay.height > 1 && !zoomed;
  const pages = tallNow ? Math.ceil(lay.height - 1e-6) : 1;
  const [page, setPage] = useState(0);

  // the page in view, for the dots
  useEffect(() => {
    const el = scroller.current;
    if (!el) return;
    const read = () => setPage(el.clientHeight ? Math.min(pages - 1, Math.round(el.scrollTop / el.clientHeight)) : 0);
    read();
    el.addEventListener('scroll', read, { passive: true });
    return () => el.removeEventListener('scroll', read);
  }, [pages]);

  // the wheel inside a terminal scrolls its scrollback only, never the tall area past it
  useEffect(() => {
    const el = scroller.current;
    if (!el) return;
    const wheel = (e: WheelEvent) => {
      if ((e.target as Element | null)?.closest?.('.pane-body .xterm') && !e.ctrlKey) e.preventDefault();
    };
    el.addEventListener('wheel', wheel, { passive: false });
    return () => el.removeEventListener('wheel', wheel);
  }, []);

  // a focused pane is scrolled into view; a zoomed tab shows its first screen
  const box = lay.rects.get(tab.focus);
  const boxKey = box ? `${box.y}:${box.h}` : '';
  useEffect(() => {
    const el = scroller.current;
    if (!el || !active) return;
    if (zoomed || !tallNow) {
      if (el.scrollTop) el.scrollTop = 0;
      return;
    }
    if (!box) return;
    const full = el.scrollHeight;
    const top = box.y * full;
    const bottom = (box.y + box.h) * full;
    const view = el.clientHeight;
    let to: number | null = null;
    if (top < el.scrollTop - 1) to = top;
    else if (bottom > el.scrollTop + view + 1) to = Math.min(top, bottom - view);
    if (to !== null) el.scrollTo({ top: to, behavior: smooth() });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab.focus, boxKey, active, zoomed, tallNow]);

  return (
    <div className={`tab-body ${active ? 'is-active' : ''}`} data-size={tallNow ? lay.height : undefined}>
      <div ref={scroller} className={`tab-scroll ${tallNow ? 'is-tall' : ''}`}>
        <div className="tab-canvas" style={{ height: `${lay.height * 100}%` }}>
          {children}
        </div>
      </div>
      {pages > 1 && (
        <div className="tab-pages" role="group" aria-label="Screens">
          {Array.from({ length: pages }, (_, i) => (
            <button
              key={i}
              className={i === page ? 'is-current' : ''}
              title={`Screen ${i + 1} (^b PgUp / PgDn)`}
              aria-label={`Screen ${i + 1}`}
              aria-current={i === page ? 'true' : undefined}
              onClick={() => {
                const el = scroller.current;
                if (el) el.scrollTo({ top: i * el.clientHeight, behavior: smooth() });
              }}
            >
              {i + 1}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

const narrow = () => !!window.matchMedia?.('(max-width: 640px)').matches;

/** SizeControl picks the active tab's size. On a phone it is one button that steps through them. */
export function SizeControl({ size, onSize }: { size: L.Size; onSize: (s: L.Size) => void }) {
  return (
    <div className="tab-size" role="group" aria-label="Tab size" title="Tab size: screens tall (^b s)">
      {L.SIZES.map((s) => (
        <button key={s} className={s === size ? 'is-on' : ''} aria-pressed={s === size} title={`${s}x: ${s === 1 ? 'one screen' : `${s} screens tall`}`} onClick={() => onSize(s !== size ? s : narrow() ? nextSize(s) : s)}>
          {s}x
        </button>
      ))}
    </div>
  );
}
