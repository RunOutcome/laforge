// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// Dragging a pane by its header onto another pane of the same tab swaps the
// two. Pointer events, so a mouse and a finger work the same way. The pure
// parts (where a drag may start, which pane is under the pointer) are
// exported for the tests.
import { useCallback, useEffect, useRef, useState } from 'react';
import type React from 'react';
import * as L from './layout';

/** How far (px) the pointer moves before a press on a header becomes a drag. */
export const THRESHOLD = 5;

/** Parts of a header that keep their own press: buttons, inputs, links, the close confirm. */
const OWN_PRESS = 'button, input, select, textarea, a, kbd, .confirm, [data-no-drag]';

/** startsDrag reports whether a press on el (inside a pane header) may start a drag. */
export function startsDrag(el: Element | null): boolean {
  return !!el && !el.closest(OWN_PRESS);
}

/** paneAt is the pane at (x, y) in the tab's unit square, or undefined outside it. */
export function paneAt(root: L.Node, x: number, y: number): string | undefined {
  if (x < 0 || y < 0 || x > 1 || y > 1) return undefined;
  for (const [id, r] of L.rects(root)) {
    if (x >= r.x && x <= r.x + r.w && y >= r.y && y <= r.y + r.h) return id;
  }
  return undefined;
}

/** dropTarget is the pane a drag from source would swap with at (x, y): another pane, or undefined. */
export function dropTarget(root: L.Node, source: string, x: number, y: number): string | undefined {
  const id = paneAt(root, x, y);
  return id === source ? undefined : id;
}

export interface PaneDrag {
  tab: string;
  source: string;
  target?: string;
}

/**
 * usePaneDrag tracks one header drag at a time. drag is set once the pointer
 * has moved past THRESHOLD; onSwap runs on a drop over another pane. Escape,
 * a drop outside the tab's panes, or a cancelled pointer drops nothing.
 */
export function usePaneDrag(onSwap: (tab: string, a: string, b: string) => void) {
  const [drag, setDrag] = useState<PaneDrag | null>(null);
  const swapRef = useRef(onSwap);
  swapRef.current = onSwap;
  const stopRef = useRef<(() => void) | null>(null);
  useEffect(() => () => stopRef.current?.(), []);

  const start = useCallback((e: React.PointerEvent, t: L.Tab, paneId: string) => {
    if (e.button !== 0 || !e.isPrimary || t.zoom || L.panes(t.root).length < 2) return;
    if (!startsDrag(e.target as Element)) return;
    const body = (e.currentTarget as HTMLElement).closest('.pane-slot')?.parentElement;
    if (!body) return;
    stopRef.current?.();
    const box = body.getBoundingClientRect();
    const root = t.root;
    const x0 = e.clientX;
    const y0 = e.clientY;
    const pointer = e.pointerId;
    let active = false;
    let target: string | undefined;

    const stop = () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
      window.removeEventListener('pointercancel', stop);
      window.removeEventListener('keydown', key, true);
      window.removeEventListener('blur', stop);
      document.body.classList.remove('dragging-pane');
      stopRef.current = null;
      setDrag(null);
    };
    const move = (ev: PointerEvent) => {
      if (ev.pointerId !== pointer) return;
      if (!active) {
        if (Math.hypot(ev.clientX - x0, ev.clientY - y0) < THRESHOLD) return;
        active = true;
        document.body.classList.add('dragging-pane');
        window.getSelection?.()?.removeAllRanges();
        setDrag({ tab: t.id, source: paneId });
      }
      ev.preventDefault();
      const next = box.width && box.height ? dropTarget(root, paneId, (ev.clientX - box.left) / box.width, (ev.clientY - box.top) / box.height) : undefined;
      if (next !== target) {
        target = next;
        setDrag({ tab: t.id, source: paneId, target });
      }
    };
    const up = (ev: PointerEvent) => {
      if (ev.pointerId !== pointer) return;
      const swap = active && target ? target : undefined;
      stop();
      if (swap) swapRef.current(t.id, paneId, swap);
    };
    const key = (ev: KeyboardEvent) => {
      if (ev.key !== 'Escape') return;
      if (active) {
        ev.preventDefault();
        ev.stopPropagation();
      }
      stop();
    };
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
    window.addEventListener('pointercancel', stop);
    window.addEventListener('keydown', key, true);
    window.addEventListener('blur', stop);
    stopRef.current = stop;
  }, []);

  return { drag, start };
}
