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

  it('accepts only layouts of its own shape', () => {
    expect(L.valid(L.initialLayout())).toBe(true);
    expect(L.valid(null)).toBe(false);
    expect(L.valid({ version: 2, tabs: [], active: 'x' })).toBe(false);
  });
});
