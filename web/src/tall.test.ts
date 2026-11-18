// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC
import { describe, expect, it } from 'vitest';
import * as L from './layout';
import { dropTarget, paneAt } from './paneDrag';
import { readPanes } from './Scene';
import { nextSize } from './TallTab';

describe('tall tabs in the client', () => {
  it('steps through the sizes and wraps', () => {
    expect(L.SIZES.map(nextSize)).toEqual([2, 3, 4, 1]);
  });

  it('finds the pane under the pointer in a tall area', () => {
    const a = L.newPane();
    const b = L.newPane();
    const root = L.split(a, a.id, 'col', b); // a over b
    const boxes = L.tall(root, 2).rects; // each one screen of two
    expect(paneAt(root, 0.5, 0.4, boxes)).toBe(a.id);
    expect(paneAt(root, 0.5, 0.6, boxes)).toBe(b.id);
    expect(dropTarget(root, a.id, 0.5, 0.9, boxes)).toBe(b.id);
    expect(dropTarget(root, a.id, 0.5, 0.1, boxes)).toBeUndefined();
  });

  it('gives a scene only the part of a pane on screen, and no pane scrolled away', () => {
    const box = (left: number, top: number, w: number, h: number) => ({ left, top, right: left + w, bottom: top + h, width: w, height: h });
    const scroller = { getBoundingClientRect: () => box(0, 40, 800, 500) };
    const pane = (r: ReturnType<typeof box>) => ({ dataset: { status: 'idle', active: 'false' }, getBoundingClientRect: () => r, closest: () => scroller });
    const root = { querySelectorAll: () => [pane(box(0, 20, 400, 300)), pane(box(400, 600, 400, 300))] } as unknown as ParentNode;
    const { panes } = readPanes(root);
    expect(panes).toHaveLength(1);
    expect(panes[0].rect).toEqual({ x: 0, y: 40, w: 400, h: 280 });
  });
});
