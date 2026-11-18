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
});
