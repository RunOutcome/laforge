// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// The forge's layout: tabs, each a tree of splits whose leaves are panes.
// Pure functions only, so the Arena and the standalone client share them.

export type Dir = 'row' | 'col'; // row: side by side; col: stacked

export interface PaneNode {
  kind: 'pane';
  id: string;
  session?: string;
}

export interface SplitNode {
  kind: 'split';
  id: string;
  dir: Dir;
  children: Node[];
  sizes: number[]; // fractions, summing to 1
}

export type Node = PaneNode | SplitNode;

export interface Tab {
  id: string;
  /** name is what the user called the tab; '' (or an old default) means it is named after its focused pane. */
  name: string;
  custom?: boolean;
  root: Node;
  focus: string;
  zoom?: string | null;
}

export interface Layout {
  version: 1;
  tabs: Tab[];
  active: string;
}

let counter = 0;
export function uid(prefix: string): string {
  counter += 1;
  return `${prefix}${Date.now().toString(36)}${counter.toString(36)}${Math.random().toString(36).slice(2, 6)}`;
}

export function newPane(session?: string): PaneNode {
  return { kind: 'pane', id: uid('p'), session };
}

export function newTab(name = '', session?: string): Tab {
  const pane = newPane(session);
  return { id: uid('t'), name, root: pane, focus: pane.id, zoom: null };
}

export function initialLayout(): Layout {
  const tab = newTab();
  return { version: 1, tabs: [tab], active: tab.id };
}

/** panes lists the tree's panes in reading order. */
export function panes(node: Node): PaneNode[] {
  return node.kind === 'pane' ? [node] : node.children.flatMap(panes);
}

export function findPane(node: Node, id: string): PaneNode | undefined {
  return panes(node).find((p) => p.id === id);
}

function mapNode(node: Node, fn: (n: Node) => Node): Node {
  const mapped = fn(node);
  if (mapped.kind === 'split') {
    return { ...mapped, children: mapped.children.map((c) => mapNode(c, fn)) };
  }
  return mapped;
}

/** setSession points a pane at a session (or clears it). */
export function setSession(root: Node, paneId: string, session: string | undefined): Node {
  return mapNode(root, (n) => (n.kind === 'pane' && n.id === paneId ? { ...n, session } : n));
}

/**
 * split puts a new pane beside paneId: after it in a split of the same
 * direction, or in a new split that replaces it.
 */
export function split(root: Node, paneId: string, dir: Dir, pane: PaneNode): Node {
  const visit = (node: Node): Node => {
    if (node.kind === 'pane') {
      if (node.id !== paneId) return node;
      return { kind: 'split', id: uid('s'), dir, children: [node, pane], sizes: [0.5, 0.5] };
    }
    const i = node.children.findIndex((c) => c.kind === 'pane' && c.id === paneId);
    if (i >= 0 && node.dir === dir) {
      const children = [...node.children];
      children.splice(i + 1, 0, pane);
      const sizes = [...node.sizes];
      const half = sizes[i] / 2;
      sizes.splice(i, 1, half, half);
      return { ...node, children, sizes };
    }
    return { ...node, children: node.children.map(visit) };
  };
  return visit(root);
}

/** remove takes paneId out, collapsing splits left with one child; null when nothing is left. */
export function remove(root: Node, paneId: string): Node | null {
  if (root.kind === 'pane') return root.id === paneId ? null : root;
  const children: Node[] = [];
  const sizes: number[] = [];
  let freed = 0;
  root.children.forEach((child, i) => {
    const next = remove(child, paneId);
    if (next) {
      children.push(next);
      sizes.push(root.sizes[i]);
    } else {
      freed += root.sizes[i];
    }
  });
  if (children.length === 0) return null;
  if (children.length === 1) return children[0];
  const total = sizes.reduce((a, b) => a + b, 0) || 1;
  void freed;
  return { ...root, children, sizes: sizes.map((s) => s / total) };
}

/** resize sets the sizes of split splitId. */
export function resize(root: Node, splitId: string, sizes: number[]): Node {
  return mapNode(root, (n) => (n.kind === 'split' && n.id === splitId ? { ...n, sizes } : n));
}

export interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
}

/** rects places every pane in the unit square. */
export function rects(node: Node, r: Rect = { x: 0, y: 0, w: 1, h: 1 }, out = new Map<string, Rect>()): Map<string, Rect> {
  if (node.kind === 'pane') {
    out.set(node.id, r);
    return out;
  }
  let offset = 0;
  node.children.forEach((child, i) => {
    const s = node.sizes[i];
    const cr = node.dir === 'row' ? { x: r.x + offset * r.w, y: r.y, w: s * r.w, h: r.h } : { x: r.x, y: r.y + offset * r.h, w: r.w, h: s * r.h };
    offset += s;
    rects(child, cr, out);
  });
  return out;
}

export type Move = 'left' | 'right' | 'up' | 'down';

/** neighbor is the pane next to paneId in direction move, or undefined. */
export function neighbor(root: Node, paneId: string, move: Move): string | undefined {
  const all = rects(root);
  const from = all.get(paneId);
  if (!from) return undefined;
  const cx = from.x + from.w / 2;
  const cy = from.y + from.h / 2;
  const eps = 1e-6;
  let best: { id: string; score: number } | undefined;
  for (const [id, r] of all) {
    if (id === paneId) continue;
    let gap: number;
    let overlap: number;
    switch (move) {
      case 'left':
        gap = from.x - (r.x + r.w);
        overlap = Math.min(from.y + from.h, r.y + r.h) - Math.max(from.y, r.y);
        break;
      case 'right':
        gap = r.x - (from.x + from.w);
        overlap = Math.min(from.y + from.h, r.y + r.h) - Math.max(from.y, r.y);
        break;
      case 'up':
        gap = from.y - (r.y + r.h);
        overlap = Math.min(from.x + from.w, r.x + r.w) - Math.max(from.x, r.x);
        break;
      case 'down':
        gap = r.y - (from.y + from.h);
        overlap = Math.min(from.x + from.w, r.x + r.w) - Math.max(from.x, r.x);
        break;
    }
    if (gap < -eps || overlap <= eps) continue;
    const dist = move === 'left' || move === 'right' ? Math.abs(r.y + r.h / 2 - cy) : Math.abs(r.x + r.w / 2 - cx);
    const score = gap * 10 + dist;
    if (!best || score < best.score) best = { id, score };
  }
  return best?.id;
}

/** tiled rebuilds a tab's panes as an even grid. */
export function tiled(root: Node): Node {
  const list = panes(root);
  if (list.length < 2) return root;
  const cols = Math.ceil(Math.sqrt(list.length));
  const rowsN = Math.ceil(list.length / cols);
  const rowsOut: Node[] = [];
  for (let r = 0; r < rowsN; r++) {
    const items = list.slice(r * cols, (r + 1) * cols);
    rowsOut.push(items.length === 1 ? items[0] : { kind: 'split', id: uid('s'), dir: 'row', children: items, sizes: items.map(() => 1 / items.length) });
  }
  return rowsOut.length === 1 ? rowsOut[0] : { kind: 'split', id: uid('s'), dir: 'col', children: rowsOut, sizes: rowsOut.map(() => 1 / rowsOut.length) };
}

/** mainStack puts the focused pane on the left and the rest stacked on the right. */
export function mainStack(root: Node, focus: string): Node {
  const list = panes(root);
  if (list.length < 2) return root;
  const main = list.find((p) => p.id === focus) ?? list[0];
  const rest = list.filter((p) => p !== main);
  const stack: Node = rest.length === 1 ? rest[0] : { kind: 'split', id: uid('s'), dir: 'col', children: rest, sizes: rest.map(() => 1 / rest.length) };
  return { kind: 'split', id: uid('s'), dir: 'row', children: [main, stack], sizes: [0.6, 0.4] };
}

/** valid reports whether a stored layout has the shape this client reads. */
export function valid(raw: unknown): raw is Layout {
  if (!raw || typeof raw !== 'object') return false;
  const l = raw as Layout;
  return l.version === 1 && Array.isArray(l.tabs) && l.tabs.length > 0 && typeof l.active === 'string';
}

export interface Gutter {
  split: SplitNode;
  index: number; // between children index-1 and index
  rect: Rect; // the split's rect
  at: number; // the boundary's offset within the split, 0..1
}

/** gutters lists every boundary between split children, for drag handles. */
export function gutters(node: Node, r: Rect = { x: 0, y: 0, w: 1, h: 1 }, out: Gutter[] = []): Gutter[] {
  if (node.kind === 'pane') return out;
  let offset = 0;
  node.children.forEach((child, i) => {
    if (i > 0) out.push({ split: node, index: i, rect: r, at: offset });
    const s = node.sizes[i];
    const cr = node.dir === 'row' ? { x: r.x + offset * r.w, y: r.y, w: s * r.w, h: r.h } : { x: r.x, y: r.y + offset * r.h, w: r.w, h: s * r.h };
    offset += s;
    gutters(child, cr, out);
  });
  return out;
}

/** autoNamed reports whether a tab takes its name from its panes. */
export function autoNamed(t: Tab): boolean {
  return !t.custom && (t.name === '' || t.name === 'main' || /^\d+$/.test(t.name));
}
