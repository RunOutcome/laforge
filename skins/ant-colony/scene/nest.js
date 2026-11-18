// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// The nest's tunnels, worked out from where the panes sit. Every pane gets a
// tunnel running round it, down the middle of the gutter. A trunk runs along
// the top of the panes, two shafts climb to the surface at the edges, a
// tunnel runs just under the grass, and the granary (the colony's store) sits
// in the header band. All tunnels are straight and axis-aligned, so the
// network is a set of segments split wherever they meet, read as a graph.

const SNAP = 2.5; // px: lines closer than this are the same tunnel
const STEP = 90; // px: extra stops along long tunnels, so ants spread out

const clamp = (v, a, b) => Math.max(a, Math.min(b, v));

function snapper(values) {
  const sorted = [...new Set(values)].sort((a, b) => a - b);
  const map = new Map();
  let group = [];
  const flush = () => {
    const mean = group.reduce((s, v) => s + v, 0) / group.length;
    for (const v of group) map.set(v, Math.round(mean * 2) / 2);
    group = [];
  };
  for (const v of sorted) {
    if (group.length && v - group[group.length - 1] > SNAP) flush();
    group.push(v);
  }
  if (group.length) flush();
  return (v) => map.get(v) ?? v;
}

// Pretend chambers when the host sends no geometry: a grid under the header.
function synthetic(count, size, chrome) {
  const left = (chrome && chrome.left) || 0;
  const top = Math.max((chrome && chrome.top) || 0, 120);
  const w = size.w - left - 40, h = size.h - top - 50;
  const n = Math.max(1, Math.min(count, 6));
  const cols = n <= 2 ? n : Math.ceil(n / 2), rows = Math.ceil(n / cols);
  const cw = w / cols, ch = h / rows;
  const out = [];
  for (let i = 0; i < n; i++) {
    const c = i % cols, r = Math.floor(i / cols);
    out.push({ x: left + 20 + c * cw + cw * 0.18, y: top + r * ch + ch * 0.2, w: cw * 0.64, h: ch * 0.6 });
  }
  return out;
}

export function buildNest(panes, chrome, size, radius = 26) {
  const shown = panes.filter((p) => p.rect && p.rect.w > 4 && p.rect.h > 4);
  const isSynthetic = shown.length === 0;
  const rects = isSynthetic
    ? synthetic(panes.length || 2, size, chrome).map((r, i) => ({ id: panes[i] ? panes[i].id : 'syn' + i, r }))
    : shown.map((p) => ({ id: p.id, r: p.rect }));

  // half the narrowest gutter: the tunnel runs down its middle
  let gap = Infinity;
  for (const a of rects) for (const b of rects) {
    if (a === b) continue;
    const A = a.r, B = b.r;
    const oy = Math.min(A.y + A.h, B.y + B.h) - Math.max(A.y, B.y);
    const ox = Math.min(A.x + A.w, B.x + B.w) - Math.max(A.x, B.x);
    const gx = B.x - (A.x + A.w), gy = B.y - (A.y + A.h);
    if (oy > 0 && gx > 0) gap = Math.min(gap, gx);
    if (ox > 0 && gy > 0) gap = Math.min(gap, gy);
  }
  const o = Number.isFinite(gap) ? clamp(gap / 2, 4, 14) : 9;

  const W = size.w, H = size.h;
  const rings = rects.map(({ id, r }) => ({
    id,
    rect: r,
    x0: clamp(r.x - o, 3, W - 3), y0: clamp(r.y - o, 3, H - 3),
    x1: clamp(r.x + r.w + o, 3, W - 3), y1: clamp(r.y + r.h + o, 3, H - 3),
  }));
  const L = Math.min(...rings.map((g) => g.x0)), R = Math.max(...rings.map((g) => g.x1));
  const T = Math.min(...rings.map((g) => g.y0)), B = Math.max(...rings.map((g) => g.y1));
  const surfaceY = Math.min(22, Math.max(8, T - 30)); // just under the grass
  const band = T - surfaceY;

  const segs = []; // {x1,y1,x2,y2}
  const seg = (x1, y1, x2, y2, tag) => segs.push({ x1, y1, x2, y2, tag });
  for (const g of rings) {
    seg(g.x0, g.y0, g.x1, g.y0, 'ring');
    seg(g.x1, g.y0, g.x1, g.y1, 'ring');
    seg(g.x0, g.y1, g.x1, g.y1, 'ring');
    seg(g.x0, g.y0, g.x0, g.y1, 'ring');
  }
  // tunnels round a pane follow its rounded corners
  const rad = Math.min(radius + o, ...rings.map((g) => Math.min(g.x1 - g.x0, g.y1 - g.y0) / 2));
  seg(L + rad, T, R - rad, T, 'trunk');

  let granary = null;
  const surface = band >= 30;
  // The title line holds the spaces or the title at the left and the tools
  // (about 600px) at the right. The granary digs in the gap between them,
  // and is left out when there is no gap.
  const free0 = L + (R - L) * 0.3, free1 = R - 600;
  if (surface) {
    seg(L, surfaceY, L, T + rad, 'shaft');
    seg(R, surfaceY, R, T + rad, 'shaft');
    seg(L, surfaceY, R, surfaceY, 'surface');
  }
  if (surface && free1 - free0 >= 110) {
    const gw = clamp(free1 - free0 - 30, 90, 170), gh = clamp(band * 0.5, 14, 34);
    const gx = Math.round((free0 + free1) / 2), gy = Math.round(surfaceY + band * 0.5);
    const floorY = Math.round(gy + gh * 0.22);
    granary = { x: gx, y: gy, w: gw, h: gh, floorY };
    seg(gx - gw * 0.36, floorY, gx + gw * 0.36, floorY, 'granary');
    seg(gx, floorY, gx, T, 'shaft');
    const sx = Math.round(gx + gw * 0.3);
    seg(sx, surfaceY, sx, floorY, 'shaft');
  }

  // snap nearly-equal lines together (neighbouring panes share a gutter)
  const sx = snapper(segs.flatMap((s) => [s.x1, s.x2]));
  const sy = snapper(segs.flatMap((s) => [s.y1, s.y2]));
  for (const s of segs) {
    s.x1 = sx(s.x1); s.x2 = sx(s.x2); s.y1 = sy(s.y1); s.y2 = sy(s.y2);
    if (s.x1 > s.x2) [s.x1, s.x2] = [s.x2, s.x1];
    if (s.y1 > s.y2) [s.y1, s.y2] = [s.y2, s.y1];
  }
  const ringsSnapped = rings.map((g) => ({ ...g, x0: sx(g.x0), x1: sx(g.x1), y0: sy(g.y0), y1: sy(g.y1) }));
  if (granary) granary.floorY = sy(granary.floorY);

  // points: ends, crossings and stops along long tunnels
  const nodes = [], index = new Map();
  const key = (x, y) => `${Math.round(x * 2)},${Math.round(y * 2)}`;
  const node = (x, y) => {
    const k = key(x, y);
    if (!index.has(k)) { index.set(k, nodes.length); nodes.push({ x, y }); }
    return index.get(k);
  };
  const horiz = segs.filter((s) => s.y1 === s.y2), vert = segs.filter((s) => s.x1 === s.x2);
  for (const s of segs) {
    node(s.x1, s.y1); node(s.x2, s.y2);
    const len = Math.max(s.x2 - s.x1, s.y2 - s.y1);
    const n = Math.floor(len / STEP);
    for (let i = 1; i < n; i++) node(s.x1 + ((s.x2 - s.x1) * i) / n, s.y1 + ((s.y2 - s.y1) * i) / n);
  }
  for (const h of horiz) for (const v of vert) {
    if (v.x1 >= h.x1 - 0.5 && v.x1 <= h.x2 + 0.5 && h.y1 >= v.y1 - 0.5 && h.y1 <= v.y2 + 0.5) node(v.x1, h.y1);
  }

  // edges: consecutive points along each segment
  const adj = nodes.map(() => []), edges = [], seen = new Set();
  for (const s of segs) {
    const h = s.y1 === s.y2;
    const on = [];
    nodes.forEach((p, i) => {
      if (h ? Math.abs(p.y - s.y1) < 0.5 && p.x >= s.x1 - 0.5 && p.x <= s.x2 + 0.5
            : Math.abs(p.x - s.x1) < 0.5 && p.y >= s.y1 - 0.5 && p.y <= s.y2 + 0.5) on.push(i);
    });
    on.sort((a, b) => (h ? nodes[a].x - nodes[b].x : nodes[a].y - nodes[b].y));
    for (let i = 1; i < on.length; i++) {
      const a = on[i - 1], b = on[i];
      const k = a < b ? `${a}-${b}` : `${b}-${a}`;
      if (a === b || seen.has(k)) continue;
      seen.add(k);
      const d = Math.hypot(nodes[a].x - nodes[b].x, nodes[a].y - nodes[b].y);
      adj[a].push([b, d]); adj[b].push([a, d]);
      edges.push({ a, b, tag: s.tag });
    }
  }

  const onRing = (p, g) =>
    ((Math.abs(p.y - g.y0) < 0.5 || Math.abs(p.y - g.y1) < 0.5) && p.x >= g.x0 - 0.5 && p.x <= g.x1 + 0.5) ||
    ((Math.abs(p.x - g.x0) < 0.5 || Math.abs(p.x - g.x1) < 0.5) && p.y >= g.y0 - 0.5 && p.y <= g.y1 + 0.5);
  const ringOf = new Map();
  for (const g of ringsSnapped) {
    const ids = [];
    nodes.forEach((p, i) => { if (onRing(p, g)) ids.push(i); });
    const top = ids.filter((i) => Math.abs(nodes[i].y - g.y0) < 0.5);
    const mid = (g.x0 + g.x1) / 2;
    const topMid = top.sort((a, b) => Math.abs(nodes[a].x - mid) - Math.abs(nodes[b].x - mid))[0] ?? ids[0];
    ringOf.set(g.id, { ...g, nodes: ids, topMid });
  }

  const nearest = (x, y, among) => {
    let best = -1, bd = Infinity;
    for (const i of among || nodes.keys()) {
      const d = (nodes[i].x - x) ** 2 + (nodes[i].y - y) ** 2;
      if (d < bd) { bd = d; best = i; }
    }
    return best;
  };

  const cache = new Map();
  function route(from, to) {
    const k = from + '>' + to;
    if (cache.has(k)) return cache.get(k);
    const dist = new Float64Array(nodes.length).fill(Infinity), prev = new Int32Array(nodes.length).fill(-1);
    const done = new Uint8Array(nodes.length);
    dist[from] = 0;
    for (;;) {
      let u = -1, best = Infinity;
      for (let i = 0; i < nodes.length; i++) if (!done[i] && dist[i] < best) { best = dist[i]; u = i; }
      if (u < 0 || u === to) break;
      done[u] = 1;
      for (const [v, d] of adj[u]) if (dist[u] + d < dist[v]) { dist[v] = dist[u] + d; prev[v] = u; }
    }
    const out = [];
    if (dist[to] === Infinity) { out.push(nodes[from], nodes[to]); }
    else for (let v = to; v >= 0; v = prev[v]) out.unshift(nodes[v]);
    if (cache.size > 400) cache.clear();
    cache.set(k, out);
    return out;
  }

  const granaryNodes = granary
    ? nodes.map((p, i) => i).filter((i) => Math.abs(nodes[i].y - granary.floorY) < 0.5 && Math.abs(nodes[i].x - granary.x) <= granary.w * 0.37)
    : [nearest(L, T)];
  const surfaceNodes = surface ? nodes.map((p, i) => i).filter((i) => Math.abs(nodes[i].y - surfaceY) < 0.5) : [];
  const ends = surface ? [nearest(L, surfaceY), nearest(R, surfaceY)] : [];

  return { o, rad, L, R, T, B, surfaceY, granary, nodes, edges, ringOf, granaryNodes, surfaceNodes, ends, route, nearest, synthetic: isSynthetic };
}

// Round the corners of a polyline, so ants turn instead of pivoting.
export function smooth(points, r = 22) {
  const p = points.filter((q, i) => i === 0 || Math.hypot(q.x - points[i - 1].x, q.y - points[i - 1].y) > 0.25);
  if (p.length < 3) return p.slice();
  const out = [p[0]];
  for (let i = 1; i < p.length - 1; i++) {
    const a = p[i - 1], b = p[i], c = p[i + 1];
    const d1 = Math.hypot(b.x - a.x, b.y - a.y), d2 = Math.hypot(c.x - b.x, c.y - b.y);
    const u1 = { x: (b.x - a.x) / d1, y: (b.y - a.y) / d1 }, u2 = { x: (c.x - b.x) / d2, y: (c.y - b.y) / d2 };
    if (Math.abs(u1.x * u2.y - u1.y * u2.x) < 0.01 && u1.x * u2.x + u1.y * u2.y > 0) { out.push(b); continue; }
    const rr = Math.min(r, d1 / 2, d2 / 2);
    const s = { x: b.x - u1.x * rr, y: b.y - u1.y * rr }, e = { x: b.x + u2.x * rr, y: b.y + u2.y * rr };
    for (let k = 0; k <= 4; k++) {
      const t = k / 4, m = 1 - t;
      out.push({ x: m * m * s.x + 2 * m * t * b.x + t * t * e.x, y: m * m * s.y + 2 * m * t * b.y + t * t * e.y });
    }
  }
  out.push(p[p.length - 1]);
  return out;
}

// A walkable track: points plus running distance.
export function track(points, closed = false, r) {
  // a loop starts and ends halfway along its first side, so every corner rounds
  const m = closed && { x: (points[0].x + points[1].x) / 2, y: (points[0].y + points[1].y) / 2 };
  const P = smooth(closed ? [m, ...points.slice(1), points[0], m] : points, r);
  const cum = [0];
  for (let i = 1; i < P.length; i++) cum.push(cum[i - 1] + Math.hypot(P[i].x - P[i - 1].x, P[i].y - P[i - 1].y));
  return { P, cum, len: cum[cum.length - 1] || 0, closed, hint: 0 };
}

export function pointAt(tr, s, out) {
  const { P, cum } = tr;
  if (P.length === 1) { out.x = P[0].x; out.y = P[0].y; return out; }
  let i = tr.hint;
  if (i >= cum.length - 1 || cum[i] > s) i = 0;
  while (i < cum.length - 2 && cum[i + 1] < s) i++;
  tr.hint = i;
  const a = P[i], b = P[i + 1], span = cum[i + 1] - cum[i] || 1;
  const t = Math.max(0, Math.min(1, (s - cum[i]) / span));
  out.x = a.x + (b.x - a.x) * t;
  out.y = a.y + (b.y - a.y) * t;
  out.a = Math.atan2(b.y - a.y, b.x - a.x);
  return out;
}
