// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// Ant Colony scene: the nest, alive. Tunnels run round every pane. Ants
// fetch crumbs from busy panes to the granary in the header, where the queen
// sits and the store piles up. They crowd a blocked pane and patrol it, carry
// a seed home when a pane finishes, and doze beside an idle one. Work on
// other tabs shows as foragers along the surface.
//
// Drawn with PixiJS Graphics only, no images. Host contract: scene(PIXI, host)
// with host.app (a ready Pixi app the host paces), host.on(type, fn) and
// host.reducedMotion.

import { buildNest, track, pointAt } from './nest.js';

const PAL = {
  dark: {
    strata: [[0x3b2817, 0x33230f], [0x2c1e10, 0x2a1c0f], [0x24180d, 0x20150b], [0x1b1209, 0x160f07]],
    grit: [[0xc79a5e, 0.35], [0xc79a5e, 0.28], [0x5b4a3a, 0.6]],
    stone: 0x5b4a3a, root: 0x6b4f33, rootAlpha: 0.55,
    tunnel: 0x0d0804, rim: 0x4e3520, tunnelAlpha: 0.6, // drawn solid, faded as one layer
        ant: { working: 0xf2a93b, blocked: 0xe0604a, error: 0xe0604a, done: 0x9dbb5a, idle: 0xd8b98e, forager: 0xc98d55, crew: 0xe0b07a, queen: 0xffcf6e },
    crumb: 0xffcf6e, seed: 0xd9c08a, soil: 0x9a6a3e, pile: [0xffcf6e, 0xf2a93b, 0xd9c08a, 0xc79a5e],
    trail: { working: 0xffc76a, blocked: 0xff6a50, error: 0xff6a50, done: 0xb6d77a }, trailBlend: 'add',
  },
  light: {
    strata: [[0xe2d0bf, 0xeadac6], [0xe9dacc, 0xeadbce], [0xecdfd3, 0xeee1d6], [0xf0e5da, 0xf2e9de]],
    grit: [[0xac7c3c, 0.35], [0xac7c3c, 0.28], [0xa8998b, 0.6]],
    stone: 0xc2b3a5, root: 0x8a6a4a, rootAlpha: 0.45,
    tunnel: 0xc9b29a, rim: 0xdcc8b4, tunnelAlpha: 0.7,
        ant: { working: 0xb8690a, blocked: 0xb8432f, error: 0xb8432f, done: 0x5a7a26, idle: 0x6e4a2a, forager: 0x3a2614, crew: 0x4a2f18, queen: 0x8a5a10 },
    crumb: 0xe18d0f, seed: 0x8a6a2a, soil: 0x9a7a5a, pile: [0xe18d0f, 0xc97a12, 0x8a6a2a, 0xac7c3c],
    trail: { working: 0xe18d0f, blocked: 0xd0553f, error: 0xd0553f, done: 0x6e9030 }, trailBlend: 'normal',
  },
};

const BUDGET = 72; // most ants at once
const PILE_MAX = 110;

// a small seeded random, so the ground looks the same after every redraw
function rng(seed) {
  let s = seed >>> 0;
  return () => {
    s = (s + 0x6d2b79f5) >>> 0;
    let t = s;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}
const R = Math.random;
const rnd = (a, b) => a + R() * (b - a);
const pick = (xs) => xs[Math.floor(R() * xs.length)];
const lerpColor = (a, b, t) => {
  const ch = (s) => Math.round(((a >> s) & 255) + (((b >> s) & 255) - ((a >> s) & 255)) * t) << s;
  return ch(16) | ch(8) | ch(0);
};

export default function scene(PIXI, host) {
  const { Container, Graphics, Sprite } = PIXI;
  const app = host.app;
  const reduced = () => !!host.reducedMotion;

  let mode = 'dark', size = { w: app.screen.width, h: app.screen.height };
  let panes = [], chrome = null, nest = null, idle = false, layoutSig = '';
  const status = new Map(); // pane id -> status
  let time = 0;

  // layers, back to front
  const ground = new Graphics();
  const tunnels = new Graphics();
  const trailLayer = new Container();
  const pileG = new Graphics();
  const antLayer = new Container();
  app.stage.addChild(ground, tunnels, trailLayer, pileG, antLayer);

  // ---- textures: one ant in two strides, a queen, crumb and seed (drawn white, tinted)
  function antShape(stride, queen) {
    const g = new Graphics();
    const k = queen ? 1.5 : 1;
    for (const side of [-1, 1]) for (let j = 0; j < 3; j++) {
      const group = (j === 1) === (side === 1) ? 1 : -1; // tripod gait
      const bx = (-0.6 + j * 0.6) * k, reach = [-2.7, 0.2, 2.5][j] * k + group * stride * 0.9;
      g.moveTo(bx, side * 0.5)
        .lineTo(bx + reach * 0.45, side * 2.3 * k)
        .lineTo(bx + reach, side * 3.7 * k)
        .stroke({ width: 0.6, color: 0xb4b4b4, cap: 'round', join: 'round' });
    }
    g.moveTo(2.9 * k, -0.5).quadraticCurveTo(4.6 * k, -2.2, 6.1 * k, -1.6).stroke({ width: 0.5, color: 0xb4b4b4, cap: 'round' });
    g.moveTo(2.9 * k, 0.5).quadraticCurveTo(4.6 * k, 2.2, 6.1 * k, 1.6).stroke({ width: 0.5, color: 0xb4b4b4, cap: 'round' });
    const abX = queen ? -5.6 : -3.4, abRx = queen ? 4.4 : 2.5, abRy = queen ? 2.5 : 1.8;
    g.ellipse(abX, 0, abRx, abRy).fill(0xd6d6d6);
    g.ellipse(abX - abRx * 0.25, -abRy * 0.35, abRx * 0.42, abRy * 0.3).fill({ color: 0xffffff, alpha: 0.9 });
    if (queen) {
      g.ellipse(-3.2, -2.6, 3.6, 1.3).fill({ color: 0xffffff, alpha: 0.35 });
      g.ellipse(-3.2, 2.6, 3.6, 1.3).fill({ color: 0xffffff, alpha: 0.35 });
    }
    g.circle(-1.15 * k, 0, 0.55 * k).fill(0xcfcfcf);
    g.ellipse(0, 0, 1.5 * k, 0.95 * k).fill(0xe2e2e2);
    g.circle(2.35 * k, 0, 1.25 * k).fill(0xeeeeee);
    return g;
  }
  const tex = (g, res = 4) => {
    const t = app.renderer.generateTexture({ target: g, resolution: res, antialias: true });
    g.destroy();
    return t;
  };
  const T = {};
  function makeTextures() {
    T.ant = [tex(antShape(1)), tex(antShape(-1))];
    T.queen = tex(antShape(0.5, true));
    T.crumb = tex(new Graphics().circle(0, 0, 1.4).fill(0xffffff));
    T.seed = tex(new Graphics().ellipse(0, 0, 3.1, 2).fill(0xffffff));
  }
  makeTextures();

  // ---- ants
  const ants = [];
  const pool = [];
  const tmp = { x: 0, y: 0, a: 0 };

  function spawn(kind, paneId, script, at) {
    if (ants.length >= BUDGET) return null;
    const a = pool.pop() || makeAnt();
    Object.assign(a, { kind, pane: paneId, tr: null, s: 0, v: 30, loopT: 0, wait: 0, fade: 0, leaving: false, cargo: null, stride: 0, frame: 0, phase: R() * 6.28 });
    const p = nest.nodes[at];
    a.x = p.x; a.y = p.y; a.heading = R() * 6.28; a.target_a = a.heading;
    a.spr.alpha = reduced() ? 1 : 0;
    a.spr.visible = true;
    a.spr.scale.set(kind === 'nurse' ? 1.2 : 1.3);
    a.spr.tint = PAL[mode].ant[tintKey(a)];
    a.script = script(a);
    antLayer.addChild(a.cargoSpr, a.spr);
    ants.push(a);
    advance(a);
    if (reduced()) settle(a);
    else place(a);
    return a;
  }
  function makeAnt() {
    const spr = new Sprite(T.ant[0]);
    spr.anchor.set(0.5);
    const cargoSpr = new Sprite(T.crumb);
    cargoSpr.anchor.set(0.5);
    cargoSpr.visible = false;
    return { spr, cargoSpr };
  }
  const tintKey = (a) => (a.kind === 'forager' || a.kind === 'crew' || a.kind === 'nurse' ? (a.kind === 'nurse' ? 'crew' : a.kind) : statusTint(a.pane, a.kind));
  function statusTint(paneId, kind) {
    if (kind === 'carrier' || kind === 'rester') return 'done';
    if (kind === 'guard' || kind === 'messenger') return status.get(paneId) === 'error' ? 'error' : 'blocked';
    if (kind === 'ambler') return 'idle';
    return 'working';
  }
  function kill(a) {
    const i = ants.indexOf(a);
    if (i >= 0) ants.splice(i, 1);
    antLayer.removeChild(a.spr, a.cargoSpr);
    a.script = null;
    pool.push(a);
  }
  function carry(a, what) {
    a.cargo = what;
    a.cargoSpr.visible = !!what;
    if (what) {
      a.cargoSpr.texture = what === 'seed' ? T.seed : T.crumb;
      a.cargoSpr.tint = what === 'seed' ? PAL[mode].seed : what === 'soil' ? PAL[mode].soil : PAL[mode].crumb;
    }
  }

  // script commands
  const go = (to, v) => ({ go: to, v });
  const wait = (t) => ({ wait: t });
  const patrol = (ringId, t, v, dir) => ({ patrol: ringId, t, v, dir });

  function advance(a) {
    while (a.script) {
      const r = a.script.next();
      if (r.done) { a.leaving = true; a.script = null; return; }
      const c = r.value;
      if (c.go !== undefined) {
        if (c.go < 0 || c.go >= nest.nodes.length) continue;
        const from = nest.nearest(a.x, a.y);
        const pts = [{ x: a.x, y: a.y }, ...nest.route(from, c.go)];
        a.tr = track(pts); a.s = 0; a.v = c.v; a.loopT = 0; a.target = c.go;
        if (a.tr.len < 1) { a.tr = null; continue; }
        return;
      }
      if (c.wait !== undefined) { a.tr = null; a.wait = c.wait; return; }
      if (c.patrol !== undefined) {
        const g = nest.ringOf.get(c.patrol);
        if (!g) continue;
        const corners = [{ x: g.x0, y: g.y0 }, { x: g.x1, y: g.y0 }, { x: g.x1, y: g.y1 }, { x: g.x0, y: g.y1 }];
        if (c.dir < 0) corners.reverse();
        a.tr = track(corners, true, nest.rad);
        // start where the ant stands
        let best = 0, bd = Infinity;
        for (let i = 0; i < a.tr.P.length; i++) {
          const d = (a.tr.P[i].x - a.x) ** 2 + (a.tr.P[i].y - a.y) ** 2;
          if (d < bd) { bd = d; best = i; }
        }
        a.s = a.tr.cum[best]; a.v = c.v; a.loopT = c.t;
        return;
      }
    }
  }

  // put an ant somewhere along its first leg, for the still frame
  function settle(a) {
    if (a.tr) { a.s = a.tr.len * rnd(0.15, 0.85); }
    place(a);
  }
  function place(a) {
    if (a.tr) {
      pointAt(a.tr, a.s, tmp);
      const wob = Math.sin(a.s * 0.4 + a.phase) * 0.7;
      a.x = tmp.x - Math.sin(tmp.a) * wob;
      a.y = tmp.y + Math.cos(tmp.a) * wob;
      a.target_a = tmp.a;
    }
    a.spr.position.set(a.x, a.y);
    a.spr.rotation = a.heading;
    if (a.cargo) {
      a.cargoSpr.position.set(a.x + Math.cos(a.heading) * 6.6, a.y + Math.sin(a.heading) * 6.6);
      a.cargoSpr.alpha = a.spr.alpha;
    }
  }

  function step(a, dt) {
    // fade in, fade out when leaving
    if (a.leaving) {
      a.spr.alpha -= dt * 1.6;
      if (a.spr.alpha <= 0) { kill(a); return; }
    } else if (a.spr.alpha < 1) a.spr.alpha = Math.min(1, a.spr.alpha + dt * 2.5);

    const mul = idle ? 0.85 : 1;
    if (a.wait > 0) {
      a.wait -= dt;
      a.spr.rotation = a.heading + Math.sin(time * 9 + a.phase) * 0.12; // antennae work
      if (a.cargo) place(a);
      if (a.wait <= 0) advance(a);
      return;
    }
    if (!a.tr) { if (!a.leaving) advance(a); return; }
    const d = a.v * mul * dt;
    a.s += d;
    if (a.tr.closed) {
      a.s %= a.tr.len;
      a.loopT -= dt;
      if (a.loopT <= 0) { a.tr = null; place(a); advance(a); return; }
    } else if (a.s >= a.tr.len) {
      a.s = a.tr.len; place(a); a.tr = null; advance(a); return;
    }
    place(a);
    // turn smoothly toward the way ahead
    let da = a.target_a - a.heading;
    da = Math.atan2(Math.sin(da), Math.cos(da));
    a.heading += da * Math.min(1, dt * 16);
    a.spr.rotation = a.heading;
    a.stride += d;
    if (a.stride > 2.2) { a.stride = 0; a.frame ^= 1; a.spr.texture = T.ant[a.frame]; }
  }

  // the host's status words, folded into what the colony knows
  const BUSY = new Set(['working', 'running']);
  const kindOf = (st) => (BUSY.has(st) ? 'working' : st === 'blocked' || st === 'error' || st === 'done' ? st : 'idle');

  // ---- behaviour, one generator per kind of ant
  const live = (id, ...want) => status.has(id) && want.includes(kindOf(status.get(id)));
  const ringNode = (id) => { const g = nest.ringOf.get(id); return g ? pick(g.nodes) : -1; };
  const granaryNode = () => pick(nest.granaryNodes);

  function* worker(a, id) {
    while (live(id, 'working')) {
      yield go(ringNode(id), rnd(46, 62));
      yield wait(rnd(0.4, 1.1));
      carry(a, 'crumb');
      yield go(granaryNode(), rnd(40, 52));
      carry(a, null);
      deposit(a, 'crumb');
      yield wait(rnd(0.15, 0.5));
    }
  }
  function* guard(a, id) {
    yield go(ringNode(id), rnd(70, 95));
    while (live(id, 'blocked', 'error')) {
      yield patrol(id, rnd(2.5, 5), rnd(48, 78), R() < 0.5 ? 1 : -1);
      yield wait(rnd(0.25, 0.8));
    }
  }
  function* messenger(a, id) {
    yield go(granaryNode(), rnd(85, 105));
    yield wait(0.5);
    yield go(ringNode(id), rnd(85, 105));
  }
  function* carrier(a, id, delay) {
    carry(a, 'seed');
    yield wait(delay);
    yield go(granaryNode(), 20);
    carry(a, null);
    deposit(a, 'seed');
    yield wait(0.4);
  }
  function* rester(a, id) {
    const g = nest.ringOf.get(id);
    const corner = g ? nest.nearest(g.x1, g.y1, g.nodes) : -1;
    yield go(corner, 18);
    while (live(id, 'done')) yield wait(rnd(2, 5));
  }
  function* ambler(a, id) {
    while (live(id, 'idle')) {
      yield patrol(id, rnd(3, 7), rnd(14, 24), R() < 0.5 ? 1 : -1);
      yield wait(rnd(2, 6)); // dozing, in the gutter where it can be seen
    }
  }
  function* forager(a) {
    for (let n = 0; n < 3 && nest.ends.length; n++) {
      if (offTabWork() > 0 && R() < 0.6) {
        carry(a, 'crumb');
        yield go(granaryNode(), rnd(26, 34));
        carry(a, null);
        deposit(a, 'crumb');
        yield wait(0.3);
      }
      yield go(pick(nest.ends), rnd(18, 30));
    }
  }

  // the nest's own crew: chores in the header band and the gutters, whatever
  // the panes are doing, so the colony never looks empty
  const shown = () => nest.nodes.map((p, i) => i).filter((i) => nest.nodes[i].y <= nest.T + 0.5);
  function* crew(a) {
    const near = shown();
    for (let n = 0; n < 6; n++) {
      if (R() < 0.35 && nest.surfaceNodes.length) { // dig: carry soil up and out
        yield go(pick(near), rnd(34, 48));
        carry(a, 'soil');
        yield go(pick(nest.surfaceNodes.length ? nest.surfaceNodes : near), rnd(30, 42));
        carry(a, null);
      } else {
        yield go(R() < 0.75 ? pick(near) : pick([...nest.ringOf.values()].flatMap((g) => g.nodes)), rnd(32, 50));
      }
      yield wait(rnd(0.6, 2.5));
    }
  }
  function* nurse(a) { // tends the queen
    const g = nest.granary;
    if (!g) return;
    const spot = nest.nearest(g.x - g.w * 0.22, g.floorY, nest.granaryNodes);
    yield go(spot, 26);
    for (let n = 0; n < 8; n++) yield wait(rnd(1.5, 4));
  }

  const SCRIPTS = { worker, guard, messenger, carrier, rester, ambler, forager, crew, nurse };
  const count = (kind, id) => ants.reduce((n, a) => n + (a.kind === kind && a.pane === id && !a.leaving ? 1 : 0), 0);
  const offTabWork = () => panes.filter((p) => !nest.ringOf.has(p.id) && kindOf(p.status) === 'working').length;

  // how many of each kind every pane should have
  function wanted() {
    const shown = panes.filter((p) => nest.ringOf.has(p.id));
    const per = Math.max(2, Math.floor((BUDGET - 8) / Math.max(1, shown.length)));
    const out = [];
    for (const p of shown) {
      const k = kindOf(p.status);
      if (k === 'working') out.push(['worker', p.id, Math.min(6, per)]);
      else if (k === 'blocked') out.push(['guard', p.id, Math.min(6, per)]);
      else if (k === 'error') out.push(['guard', p.id, Math.min(4, per)]);
      else if (k === 'done') out.push(['rester', p.id, 1]);
      else out.push(['ambler', p.id, Math.min(2, per)]);
    }
    if (nest.ends.length) out.push(['forager', null, 2 + Math.min(6, offTabWork() * 2)]);
    out.push(['crew', null, nest.surfaceNodes.length ? 7 : 4]);
    if (nest.granary) out.push(['nurse', null, 2]);
    return out;
  }

  let spawnClock = 0;
  function reconcile(all) {
    for (const [kind, id, n] of wanted()) {
      const have = count(kind, id);
      const add = all ? n - have : Math.min(1, n - have);
      for (let i = 0; i < add; i++) {
        let at;
        if (kind === 'forager') at = pick(nest.ends);
        else if (kind === 'crew' || kind === 'nurse') at = all ? pick(shown()) : granaryNode();
        else if (kind === 'worker') at = all ? ringNode(id) : granaryNode();
        else at = all ? ringNode(id) : granaryNode();
        if (at === undefined || at < 0) continue;
        spawn(kind, id, (a) => SCRIPTS[kind](a, id), at);
      }
    }
  }

  // ---- the granary's store
  const pile = [];
  const pileRand = rng(7);
  let seeded = false;
  function deposit(a, what) {
    const g = nest.granary;
    if (!g) return;
    const n = what === 'seed' ? 1 : 1;
    for (let i = 0; i < n; i++) {
      if (pile.length >= PILE_MAX) pile.splice(0, 30); // the colony eats
      const u = (pileRand() + pileRand() + pileRand()) / 3 - 0.5; // heaped in the middle
      const height = Math.min(1, pile.length / PILE_MAX) * (g.h * 0.42) * (1 - Math.abs(u) * 1.6);
      pile.push({ u, h: height * pileRand(), seed: what === 'seed', c: Math.floor(pileRand() * 4) });
    }
    drawPile();
  }
  function drawPile() {
    pileG.clear();
    const g = nest && nest.granary;
    if (!g) return;
    const P = PAL[mode];
    for (const c of pile) {
      const x = g.x + c.u * g.w * 0.62, y = g.floorY + g.h * 0.2 - 1.5 - c.h;
      if (c.seed) pileG.ellipse(x, y, 3, 1.9).fill(P.seed);
      else pileG.circle(x, y, 1.5).fill(P.pile[c.c]);
    }
  }

  // ---- static drawing: ground, tunnels, granary, trails
  let queen = null;
  function drawGround() {
    const P = PAL[mode], { w, h } = size;
    ground.clear();
    const stops = [[0, 0.22], [0.224, 0.48], [0.484, 0.74], [0.744, 1]];
    stops.forEach(([a, b], i) => {
      const strips = 14, y0 = a * h, y1 = b * h, band = (y1 - y0) / strips;
      for (let k = 0; k < strips; k++) {
        ground.rect(0, y0 + k * band, w, band + 1).fill(lerpColor(P.strata[i][0], P.strata[i][1], k / (strips - 1)));
      }
      if (i < 3) ground.rect(0, y1, w, stops[i + 1][0] * h - y1 + 0.5).fill(P.strata[i][1]);
    });
    const r = rng(42);
    const grit = Math.round((w * h) / 2600);
    for (let i = 0; i < grit; i++) {
      const [c, al] = P.grit[i % 3];
      ground.ellipse(r() * w, r() * h, 0.8 + r() * 1.6, 0.6 + r() * 1.1).fill({ color: c, alpha: al * (0.5 + r() * 0.5) });
    }
    const stones = Math.round((w * h) / 60000);
    for (let i = 0; i < stones; i++) {
      const x = r() * w, y = 40 + r() * (h - 40), rx = 3 + r() * 6;
      ground.ellipse(x, y, rx, rx * (0.55 + r() * 0.3)).fill({ color: P.stone, alpha: 0.35 + r() * 0.25 });
    }
    if (nest && nest.surfaceNodes.length) { // roots hanging from the surface, header band only
      const { L, R: Rx, surfaceY, T } = nest;
      for (let i = 0; i < 7; i++) {
        const x = L + 30 + r() * (Rx - L - 60), len = (T - surfaceY) * (0.35 + r() * 0.5);
        const bend = (r() - 0.5) * 30;
        ground.moveTo(x, 12).bezierCurveTo(x + bend, 12 + len * 0.4, x - bend, 12 + len * 0.7, x + bend * 0.5, 12 + len)
          .stroke({ width: 1.6, color: P.root, alpha: P.rootAlpha, cap: 'round' });
        const bx = x + bend * 0.3, by = 12 + len * 0.45;
        ground.moveTo(bx, by).quadraticCurveTo(bx + 10, by + 6, bx + 14 + r() * 8, by + 16)
          .stroke({ width: 0.9, color: P.root, alpha: P.rootAlpha * 0.8, cap: 'round' });
      }
    }
  }

  function drawTunnels() {
    const P = PAL[mode];
    tunnels.cacheAsTexture(false);
    tunnels.clear();
    tunnels.alpha = P.tunnelAlpha;
    if (!nest) return;
    const wide = Math.max(7, Math.min(16, nest.o * 2 - 3));
    const width = (tag) => (tag === 'trunk' ? wide : tag === 'surface' ? 8 : 6);
    // the carved edge first, then the dark of the tunnel
    for (const pass of [0, 1]) {
      const style = (w) => (pass === 0
        ? { width: w + 3, color: P.rim, cap: 'round', join: 'round' }
        : { width: w, color: P.tunnel, cap: 'round', join: 'round' });
      for (const g of nest.ringOf.values()) {
        tunnels.roundRect(g.x0, g.y0, g.x1 - g.x0, g.y1 - g.y0, nest.rad).stroke(style(wide));
      }
      for (const e of nest.edges) {
        if (e.tag === 'ring') continue;
        const a = nest.nodes[e.a], b = nest.nodes[e.b];
        tunnels.moveTo(a.x, a.y).lineTo(b.x, b.y).stroke(style(width(e.tag)));
      }
    }
    if (nest.synthetic) {
      for (const g of nest.ringOf.values()) {
        const r = g.rect;
        tunnels.roundRect(r.x, r.y, r.w, r.h, 22).fill(P.tunnel);
      }
    }
    const g = nest.granary;
    if (g) {
      tunnels.ellipse(g.x, g.y, g.w / 2 + 2, g.h / 2 + 2).fill(P.rim);
      tunnels.ellipse(g.x, g.y, g.w / 2, g.h / 2).fill(P.tunnel);
      // a floor of packed sand
      tunnels.ellipse(g.x, g.y + g.h * 0.32, g.w * 0.44, g.h * 0.16).fill(P.rim);
    }
    tunnels.cacheAsTexture({ resolution: Math.min(2, app.renderer.resolution) });
    // the queen holds court at the granary's left end
    if (!queen) { queen = new Sprite(T.queen); queen.anchor.set(0.5); antLayer.addChildAt(queen, 0); }
    queen.baseScale = 1.5;
    queen.scale.set(1.5);
    queen.visible = !!g;
    if (g) {
      queen.position.set(g.x - g.w * 0.3, g.floorY - 1);
      queen.rotation = -0.12;
      queen.tint = P.ant.queen;
    }
  }

  const trails = new Map(); // pane id -> {g, kind}
  function drawTrails() {
    for (const t of trails.values()) t.g.destroy();
    trails.clear();
    trailLayer.removeChildren();
    if (!nest) return;
    const P = PAL[mode];
    for (const p of panes) {
      const ring = nest.ringOf.get(p.id);
      if (!ring) continue;
      const kind = kindOf(p.status);
      const color = P.trail[kind];
      if (!color) continue;
      const g = new Graphics();
      g.blendMode = P.trailBlend;
      if (kind === 'working' && nest.granary) {
        // a scent trail of dots from the granary to the pane
        const tr = track(nest.route(nest.granaryNodes[0], ring.topMid));
        const pts = tr.P;
        g.moveTo(pts[0].x, pts[0].y);
        for (const q of pts.slice(1)) g.lineTo(q.x, q.y);
        g.stroke({ width: 7, color, alpha: 0.16, cap: 'round', join: 'round' });
        const at = { x: 0, y: 0 };
        for (let s = 0; s < tr.len; s += 7) { pointAt(tr, s, at); g.circle(at.x, at.y, 1.1).fill({ color, alpha: 0.85 }); }
      } else {
        g.roundRect(ring.x0, ring.y0, ring.x1 - ring.x0, ring.y1 - ring.y0, nest.rad);
        const strong = kind === 'blocked' || kind === 'error';
        g.stroke({ width: strong ? 8 : 5, color, alpha: strong ? 0.22 : 0.12, join: 'round' });
        g.stroke({ width: strong ? 1.6 : 1.1, color, alpha: strong ? 0.8 : 0.45, join: 'round' });
      }
      trailLayer.addChild(g);
      trails.set(p.id, { g, kind });
    }
  }

  // ---- events
  function rebuild(force) {
    const sig = JSON.stringify([size, chrome, panes.map((p) => [p.id, p.rect && [p.rect.x, p.rect.y, p.rect.w, p.rect.h]])]);
    const geometry = force || sig !== layoutSig;
    layoutSig = sig;
    if (geometry) {
      nest = buildNest(panes, chrome, size);
      for (const a of [...ants]) kill(a); // ants re-enter on the new tunnels
      drawGround();
      drawTunnels();
      if (!seeded && nest.granary) { seeded = true; for (let i = 0; i < 14; i++) deposit(null, i % 5 ? 'crumb' : 'seed'); }
      drawPile();
    }
    drawTrails();
    reconcile(reduced() || geometry);
  }

  host.on('init', (ev) => {
    mode = ev.mode === 'light' ? 'light' : 'dark';
    if (ev.size) size = { w: ev.size.w, h: ev.size.h };
  });
  host.on('mode', (ev) => {
    mode = ev.mode === 'light' ? 'light' : 'dark';
    if (!nest) return;
    drawGround(); drawTunnels(); drawTrails(); drawPile();
    for (const a of ants) {
      a.spr.tint = PAL[mode].ant[tintKey(a)];
      if (a.cargo) carry(a, a.cargo);
    }
  });
  host.on('resize', (ev) => { size = { w: ev.size.w, h: ev.size.h }; if (nest) rebuild(); });
  host.on('idle', (ev) => { idle = !!ev.idle; });
  host.on('panes', (ev) => {
    const before = new Map(status);
    panes = (ev.panes || []).map((p) => ({ id: String(p.id), status: String(p.status || 'idle'), rect: p.rect }));
    if (ev.chrome) chrome = ev.chrome;
    status.clear();
    for (const p of panes) status.set(p.id, p.status);
    size = { w: app.screen.width || size.w, h: app.screen.height || size.h };
    rebuild();
    // news travels: a finished pane sends a seed home, a newly blocked one a runner
    for (const p of panes) {
      const was = before.get(p.id);
      if (!was || was === p.status || !nest.ringOf.has(p.id)) continue;
      const k = kindOf(p.status);
      if (kindOf(was) === k) continue;
      if (k === 'done') {
        for (let i = 0; i < 2; i++) spawn('carrier', p.id, (a) => carrier(a, p.id, i * 0.35), nest.ringOf.get(p.id).topMid);
      } else if (k === 'blocked' || k === 'error') {
        spawn('messenger', p.id, (a) => messenger(a, p.id), nest.ringOf.get(p.id).topMid);
      }
    }
  });

  // A lost WebGL context (a GPU switch on a Mac) loses generated textures.
  // When it comes back, make them again and redraw.
  app.canvas.addEventListener('webglcontextlost', (e) => e.preventDefault());
  app.canvas.addEventListener('webglcontextrestored', () => {
    setTimeout(() => {
      makeTextures();
      for (const a of [...ants, ...pool]) {
        a.spr.texture = T.ant[a.frame || 0];
        if (a.cargo) carry(a, a.cargo); else a.cargoSpr.texture = T.crumb;
      }
      if (queen) queen.texture = T.queen;
      if (nest) { drawGround(); drawTunnels(); drawTrails(); drawPile(); }
      app.ticker.update();
    }, 0);
  });

  // for tests over CDP: what the colony is doing (stays inside the sandbox)
  globalThis.__antColony = () => {
    const hidden = (a) => panes.some((p) => p.rect && a.x > p.rect.x && a.x < p.rect.x + p.rect.w && a.y > p.rect.y && a.y < p.rect.y + p.rect.h);
    const kinds = {};
    for (const a of ants) kinds[a.kind] = (kinds[a.kind] || 0) + 1;
    return { t: Math.round(time), n: ants.length, shown: ants.filter((a) => !hidden(a) && a.spr.alpha > 0.5).length, kinds,
      statuses: panes.map((p) => p.status).join(','), started: app.ticker.started, fps: Math.round(app.ticker.FPS), lost: !!app.renderer.gl?.isContextLost?.() };
  };

  // first layout before any event, so the frame is never empty
  rebuild(true);

  let pulseClock = 0;
  app.ticker.add((tk) => {
    if (reduced()) return; // the host renders one still frame per event
    const dt = Math.min(0.12, tk.deltaMS / 1000);
    time += dt;
    for (let i = ants.length - 1; i >= 0; i--) step(ants[i], dt);
    spawnClock -= dt;
    if (spawnClock <= 0) { spawnClock = 0.45; reconcile(false); }
    pulseClock += dt;
    for (const t of trails.values()) {
      if (t.kind === 'working') t.g.alpha = 0.7 + 0.3 * Math.sin(pulseClock * 2.2);
      else if (t.kind === 'blocked' || t.kind === 'error') t.g.alpha = 0.55 + 0.45 * Math.sin(pulseClock * 4.2);
    }
    if (queen && queen.visible) queen.scale.set(queen.baseScale * (1 + Math.sin(time * 1.4) * 0.025));
  });
}
