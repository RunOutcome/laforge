// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC
import { describe, expect, it } from 'vitest';
import * as L from './layout';

const ids = (n: L.Node | null) => (n ? L.panes(n).map((p) => p.id) : []);

describe('layout', () => {
  it('splits beside a pane, then flattens same-direction splits', () => {
    const a = L.newPane();
    const b = L.newPane();
    const c = L.newPane();
    let root: L.Node = L.split(a, a.id, 'row', b);
    root = L.split(root, b.id, 'row', c);
    expect(root.kind).toBe('split');
    expect((root as L.SplitNode).children).toHaveLength(3);
    expect(ids(root)).toEqual([a.id, b.id, c.id]);
    const sizes = (root as L.SplitNode).sizes;
    expect(sizes.reduce((x, y) => x + y)).toBeCloseTo(1);
  });

  it('nests a split of the other direction', () => {
    const a = L.newPane();
    const b = L.newPane();
    const c = L.newPane();
    let root: L.Node = L.split(a, a.id, 'row', b);
    root = L.split(root, b.id, 'col', c);
    const s = root as L.SplitNode;
    expect(s.children[1].kind).toBe('split');
    expect((s.children[1] as L.SplitNode).dir).toBe('col');
  });

  it('removes a pane and collapses a split left with one child', () => {
    const a = L.newPane();
    const b = L.newPane();
    const root = L.split(a, a.id, 'row', b);
    expect(L.remove(root, b.id)).toEqual(a);
    expect(L.remove(a, a.id)).toBeNull();
  });

  it('finds neighbours by position', () => {
    const a = L.newPane();
    const b = L.newPane();
    const c = L.newPane();
    let root: L.Node = L.split(a, a.id, 'row', b); // a | b
    root = L.split(root, b.id, 'col', c); // a | (b / c)
    expect(L.neighbor(root, a.id, 'right')).toBe(b.id);
    expect(L.neighbor(root, b.id, 'down')).toBe(c.id);
    expect(L.neighbor(root, c.id, 'up')).toBe(b.id);
    expect(L.neighbor(root, c.id, 'left')).toBe(a.id);
    expect(L.neighbor(root, a.id, 'left')).toBeUndefined();
  });

  it('places panes and gutters in the unit square', () => {
    const a = L.newPane();
    const b = L.newPane();
    const root = L.split(a, a.id, 'row', b);
    const r = L.rects(root);
    expect(r.get(a.id)).toEqual({ x: 0, y: 0, w: 0.5, h: 1 });
    expect(r.get(b.id)).toEqual({ x: 0.5, y: 0, w: 0.5, h: 1 });
    const g = L.gutters(root);
    expect(g).toHaveLength(1);
    expect(g[0].at).toBeCloseTo(0.5);
  });

  it('tiles any number of panes and keeps them all', () => {
    let root: L.Node = L.newPane();
    for (let i = 0; i < 4; i++) root = L.split(root, L.panes(root).at(-1)!.id, 'row', L.newPane());
    const before = ids(root);
    const t = L.tiled(root);
    expect(ids(t).sort()).toEqual([...before].sort());
    const m = L.mainStack(root, before[2]);
    expect(ids(m)[0]).toBe(before[2]);
  });

  it('swaps two panes in place, keeping ids, sessions and the tree shape', () => {
    const a = L.newPane('sa');
    const b = L.newPane('sb');
    const c = L.newPane('sc');
    let root: L.Node = L.split(a, a.id, 'row', b); // a | b
    root = L.split(root, b.id, 'col', c); // a | (b / c)
    const before = L.rects(root);
    const s = L.swap(root, a.id, c.id); // c | (b / a)
    expect(ids(s)).toEqual([c.id, b.id, a.id]);
    expect(L.findPane(s, a.id)?.session).toBe('sa');
    expect(L.findPane(s, c.id)?.session).toBe('sc');
    const after = L.rects(s);
    expect(after.get(c.id)).toEqual(before.get(a.id));
    expect(after.get(a.id)).toEqual(before.get(c.id));
    expect(after.get(b.id)).toEqual(before.get(b.id));
    expect((s as L.SplitNode).sizes).toEqual((root as L.SplitNode).sizes);
    expect(L.swap(s, a.id, c.id)).toEqual(root); // swapping back restores it
  });

  it('leaves the tree alone for a pane swapped with itself or an unknown pane', () => {
    const a = L.newPane();
    const b = L.newPane();
    const root = L.split(a, a.id, 'row', b);
    expect(L.swap(root, a.id, a.id)).toBe(root);
    expect(L.swap(root, a.id, 'nope')).toBe(root);
  });

  it('swaps in a tab and focuses the dragged pane', () => {
    const t = L.newTab();
    const b = L.newPane();
    const tab = { ...t, root: L.split(t.root, t.focus, 'row', b), focus: b.id };
    const first = L.panes(tab.root)[0].id;
    const s = L.swapInTab(tab, first, b.id);
    expect(ids(s.root)).toEqual([b.id, first]);
    expect(s.focus).toBe(first);
    expect(L.swapInTab(tab, first, first)).toBe(tab);
  });

  it('accepts only layouts of its own shape', () => {
    expect(L.valid(L.initialLayout())).toBe(true);
    expect(L.valid(null)).toBe(false);
    expect(L.valid({ version: 2, tabs: [], active: 'x' })).toBe(false);
  });

  describe('tall tabs', () => {
    const grid = (n: number) => {
      let root: L.Node = L.newPane();
      for (let i = 1; i < n; i++) root = L.split(root, L.panes(root).at(-1)!.id, 'row', L.newPane());
      return L.tiled(root);
    };
    const stack = (n: number) => {
      let root: L.Node = L.newPane();
      for (let i = 1; i < n; i++) root = L.split(root, L.panes(root).at(-1)!.id, 'col', L.newPane());
      return root.kind === 'split' ? L.resize(root, root.id, root.sizes.map(() => 1 / n)) : root;
    };
    // a box's height in screens, from the tall area's unit square
    const screens = (t: L.Tall, id: string) => t.rects.get(id)!.h * t.height;

    it('is exactly rects and gutters at 1x', () => {
      for (const root of [grid(7), stack(4), L.mainStack(grid(6), L.panes(grid(6))[0].id)]) {
        const t = L.tall(root, 1);
        expect(t.height).toBe(1);
        expect(t.rects).toEqual(L.rects(root));
        expect(t.gutters.map(({ fracs: _, ...g }) => g)).toEqual(L.gutters(root));
      }
    });

    it('lays a stack across n screens, each pane one screen at most', () => {
      const root = stack(8);
      for (const n of [2, 3, 4] as const) {
        const t = L.tall(root, n);
        expect(t.height).toBe(n);
        const ids = L.panes(root).map((p) => p.id);
        for (const id of ids) expect(screens(t, id)).toBeCloseTo(n / 8);
        // still tiles the area: top to bottom with no gap
        const ys = ids.map((id) => t.rects.get(id)!);
        ys.forEach((r, i) => expect(r.y).toBeCloseTo(i / 8));
      }
    });

    it('caps a pane at one screen and gives the rest to its siblings', () => {
      const a = L.newPane();
      const b = L.newPane();
      const c = L.newPane();
      let root: L.Node = L.split(a, a.id, 'col', b);
      root = L.split(root, b.id, 'col', c); // a / b / c at 1/3 each
      root = L.resize(root, (root as L.SplitNode).id, [0.6, 0.2, 0.2]);
      const t = L.tall(root, 3);
      expect(t.height).toBe(3);
      expect(screens(t, a.id)).toBeCloseTo(1); // 0.6 * 3 = 1.8 capped to 1
      expect(screens(t, b.id)).toBeCloseTo(1);
      expect(screens(t, c.id)).toBeCloseTo(1);
      const g = t.gutters.map((x) => x.at);
      expect(g[0]).toBeCloseTo(1 / 3);
      expect(g[1]).toBeCloseTo(2 / 3);
      expect(t.gutters[0].fracs.map((f) => +f.toFixed(6))).toEqual([0.333333, 0.333333, 0.333333]);
    });

    it('no pane is ever taller than a screen, at any size', () => {
      const roots = [grid(2), grid(5), grid(8), stack(3), L.mainStack(stack(7), L.panes(stack(7))[0].id), L.newPane()];
      for (const root of roots) {
        for (const n of L.SIZES) {
          const t = L.tall(root, n);
          for (const p of L.panes(root)) expect(screens(t, p.id)).toBeLessThanOrEqual(1 + 1e-9);
          expect(t.height).toBeLessThanOrEqual(n);
        }
      }
    });

    it('is only as tall as the panes can use', () => {
      expect(L.tall(L.newPane(), 4).height).toBe(1);
      expect(L.tall(grid(2), 4).height).toBe(1); // side by side: one row
      expect(L.tall(grid(8), 4).height).toBe(3); // three rows
      expect(L.tall(stack(2), 4).height).toBe(2);
    });

    it('keeps a pane beside a taller stack one screen tall, at the top', () => {
      const s = stack(6);
      const main = L.panes(s)[0];
      const root = L.mainStack(s, main.id); // main | five stacked
      const t = L.tall(root, 4);
      expect(t.height).toBe(4);
      expect(t.rects.get(main.id)).toEqual({ x: 0, y: 0, w: 0.6, h: 0.25 });
      for (const p of L.panes(root).slice(1)) expect(screens(t, p.id)).toBeCloseTo(4 / 5);
    });

    it('fill caps and redistributes by size', () => {
      expect(L.fill(1, [0.5, 0.5], [1, 1])).toEqual([0.5, 0.5]);
      const out = L.fill(3, [0.7, 0.1, 0.2], [1, 2, 2]);
      expect(out[0]).toBe(1);
      expect(out[1]).toBeCloseTo(2 / 3);
      expect(out[2]).toBeCloseTo(4 / 3);
      expect(out.reduce((a, b) => a + b)).toBeCloseTo(3);
    });

    it('reads, sets and stores a tab size', () => {
      const t = L.newTab();
      expect(L.tabSize(t)).toBe(1);
      expect(L.tabSize({ ...t, size: 7 as L.Size })).toBe(1);
      const big = L.withSize(t, 3);
      expect(L.tabSize(big)).toBe(3);
      expect('size' in L.withSize(big, 1)).toBe(false); // a 1x tab is stored as before
      const layout: L.Layout = { version: 1, tabs: [big, t], active: big.id };
      const back = JSON.parse(JSON.stringify(layout));
      expect(L.valid(back)).toBe(true);
      expect(L.tabSize(back.tabs[0])).toBe(3);
      expect(L.tabSize(back.tabs[1])).toBe(1);
      expect(back).toEqual(layout);
    });
  });
});
