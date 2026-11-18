// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC
import { describe, expect, it } from 'vitest';
import * as L from './layout';
import { dropTarget, paneAt, startsDrag } from './paneDrag';

// closest stands in for the DOM's: the element matches when any of its own selectors are asked for.
const el = (own: boolean) => ({ closest: () => (own ? {} : null) }) as unknown as Element;

describe('pane drag', () => {
  it('starts from the header but not from its buttons or the close confirm', () => {
    expect(startsDrag(el(false))).toBe(true);
    expect(startsDrag(el(true))).toBe(false);
    expect(startsDrag(null)).toBe(false);
  });

  it('finds the pane under the pointer, and nothing outside the tab', () => {
    const a = L.newPane();
    const b = L.newPane();
    const c = L.newPane();
    let root: L.Node = L.split(a, a.id, 'row', b); // a | b
    root = L.split(root, b.id, 'col', c); // a | (b / c)
    expect(paneAt(root, 0.2, 0.5)).toBe(a.id);
    expect(paneAt(root, 0.8, 0.2)).toBe(b.id);
    expect(paneAt(root, 0.8, 0.8)).toBe(c.id);
    expect(paneAt(root, 1.2, 0.5)).toBeUndefined();
    expect(paneAt(root, 0.5, -0.1)).toBeUndefined();
  });

  it('never targets the dragged pane itself', () => {
    const a = L.newPane();
    const b = L.newPane();
    const root = L.split(a, a.id, 'row', b);
    expect(dropTarget(root, a.id, 0.2, 0.5)).toBeUndefined();
    expect(dropTarget(root, a.id, 0.7, 0.5)).toBe(b.id);
  });
});
