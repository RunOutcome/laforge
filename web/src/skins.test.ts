// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC
import { describe, expect, it } from 'vitest';
import type { Session, StatusWord } from './api';
import * as R from './rail';
import * as K from './skins';
import * as L from './layout';
import { darkTerminal, lightTerminal } from './theme';

const skin = (name: string, more: Partial<K.Skin> = {}): K.Skin => ({ name, label: name, builtin: false, ...more });

describe('skin loader', () => {
  const list: K.SkinList = { default: 'console', skins: [skin('classic', { builtin: true }), skin('console', { layout: 'rail' }), skin('paper')] };

  it("takes the browser's choice when the forge still has it", () => {
    expect(K.pick(list, 'paper').name).toBe('paper');
  });

  it("falls back to the forge's default, then classic", () => {
    expect(K.pick(list, 'gone').name).toBe('console');
    expect(K.pick(list, null).name).toBe('console');
    expect(K.pick({ default: 'gone', skins: list.skins }, 'also-gone').name).toBe('classic');
  });

  it('falls back to the built-in classic when the forge lists nothing', () => {
    expect(K.pick(null, 'console')).toBe(K.classic);
    expect(K.pick({ default: 'x', skins: [] }, null)).toBe(K.classic);
  });

  it('reads a skin layout', () => {
    expect(K.layoutOf(skin('a', { layout: 'rail' }))).toBe('rail');
    expect(K.layoutOf(skin('a', { layout: 'sideways' as K.SkinLayout }))).toBe('classic');
  });

  it("picks the skin's terminal palette by mode, over the client's palette for that mode", () => {
    const k = skin('a', { terminal: { dark: { background: '#000000', red: '#ff0000' }, light: { background: '#ffffff' } } });
    expect(K.terminalOf(k, { background: '#111111', foreground: '#eeeeee' }, 'dark')).toEqual({ background: '#000000', foreground: '#eeeeee', red: '#ff0000' });
    expect(K.terminalOf(k, { background: '#222222', foreground: '#111111' }, 'light')).toEqual({ background: '#ffffff', foreground: '#111111' });
  });

  it('falls back to the client palette for a mode the skin leaves out', () => {
    const base = { background: '#fbfcfd' };
    expect(K.terminalOf(skin('a', { terminal: { dark: { background: '#000000' } } }), base, 'light')).toBe(base);
    expect(K.terminalOf(skin('a'), base, 'dark')).toBe(base);
  });

  it('reads the old flat palette as the mode the skin declared, or both', () => {
    const flat = { background: '#000000' };
    expect(K.terminalOf(skin('a', { mode: 'dark', terminal: flat }), {}, 'dark').background).toBe('#000000');
    expect(K.terminalOf(skin('a', { mode: 'dark', terminal: flat }), { background: '#fff' }, 'light').background).toBe('#fff');
    expect(K.terminalOf(skin('a', { terminal: flat }), {}, 'light').background).toBe('#000000');
  });

  it('draws swatches from the skin terminal colours, else the client palette per mode', () => {
    const sw = K.swatches(skin('a', { terminal: { dark: { background: '#000000', foreground: '#f3e9dc', cursor: '#ffae42' } } }));
    expect(sw.dark).toEqual({ background: '#000000', foreground: '#f3e9dc', accent: '#ffae42' });
    expect(sw.light).toEqual({ background: lightTerminal.background, foreground: lightTerminal.foreground, accent: lightTerminal.cursor });
    const partial = K.swatches(skin('b', { terminal: { light: { background: '#eeeeee' } } }));
    expect(partial.light).toEqual({ background: '#eeeeee', foreground: lightTerminal.foreground, accent: lightTerminal.cursor });
    expect(K.swatches(K.classic).dark).toEqual({ background: darkTerminal.background, foreground: darkTerminal.foreground, accent: darkTerminal.cursor });
  });

  it("serves a skin's stylesheets from its folder", () => {
    expect(K.cssUrls(skin('console', { css: ['skin.css', 'parts/rail.css'] }))).toEqual(['/skins/console/skin.css', '/skins/console/parts/rail.css']);
    expect(K.cssUrls(skin('classic'))).toEqual([]);
  });

  it('applies a skin, and leaves the page as it was when a stylesheet fails', async () => {
    const doc = fakeDocument();
    const pending = K.apply(skin('ok', { css: ['a.css'], layout: 'rail' }), '', doc);
    doc.links()[0].onload!(new Event('load'));
    await pending;
    expect(doc.documentElement.dataset.skin).toBe('ok');
    // where the spaces show is the client's (rail.ts arrangement), not the skin's
    expect(doc.documentElement.dataset.layout).toBeUndefined();
    expect(doc.links().map((l) => l.href)).toEqual(['/skins/ok/a.css']);
    const failing = K.apply(skin('bad', { css: ['b.css'] }), '', doc);
    doc.links()[1].onerror!(new Event('error'));
    await expect(failing).rejects.toThrow('bad');
    expect(doc.links().map((l) => l.dataset.skinCss)).toEqual(['ok']);
    expect(doc.documentElement.dataset.skin).toBe('ok');
    const back = K.apply(K.classic, '', doc); // classic has no stylesheet: the old ones go
    await back;
    expect(doc.links()).toEqual([]);
    expect(doc.documentElement.dataset.skin).toBe('classic');
  });
});

// fakeDocument is as much of a document as apply uses (the tests run without a DOM).
function fakeDocument() {
  type FakeLink = { rel: string; href: string; dataset: Record<string, string>; onload: ((e: Event) => void) | null; onerror: ((e: Event) => void) | null; remove: () => void };
  const head: FakeLink[] = [];
  const doc = {
    documentElement: { dataset: {} as Record<string, string> },
    head: { appendChild: (l: FakeLink) => head.push(l) },
    createElement: () => {
      const l: FakeLink = { rel: '', href: '', dataset: {}, onload: null, onerror: null, remove: () => head.splice(head.indexOf(l), 1) };
      return l;
    },
    querySelectorAll: () => head.filter((l) => l.dataset.skinCss),
    links: () => [...head],
  };
  return doc as unknown as Document & { links: () => FakeLink[] };
}

type P = Pick<Session, 'id' | 'cwd' | 'labels' | 'status' | 'seen' | 'statusSince' | 'profile' | 'state'>;
const pane = (id: string, cwd: string, more: { space?: string; word?: StatusWord; seen?: boolean; profile?: string } = {}): P => ({
  id,
  cwd,
  labels: more.space ? { space: more.space } : undefined,
  status: more.word ? { state: more.word } : undefined,
  seen: more.seen ?? false,
  profile: more.profile ?? 'shell',
  state: 'running',
});

describe('rail', () => {
  it('names a pane by its space label, else its folder', () => {
    const list = [pane('a', '/Users/me/src/billing'), pane('b', '/Users/me/src/billing/'), pane('c', '/srv/site', { space: 'website' }), pane('d', '/'), pane('e', 'C:\\work\\billing')];
    expect(list.map(R.spaceOf)).toEqual(['billing', 'billing', 'website', '/', 'billing']);
    expect(R.folderOf(undefined)).toBe('/');
  });

  it('gives a tab its folder from its focused pane, else its first live one', () => {
    const sessions = new Map([
      ['a', pane('a', '/x/billing')],
      ['c', pane('c', '/y/site')],
    ]);
    const t = L.newTab('', 'a');
    const right = L.newPane('c');
    const two = { ...t, root: L.split(t.root, t.focus, 'row', right) };
    expect(R.tabFolder(two, sessions)).toBe('billing');
    expect(R.tabFolder({ ...two, focus: right.id }, sessions)).toBe('site');
    expect(R.tabFolder(L.newTab('', 'gone'), sessions)).toBe('');
    expect(R.tabOf([t, { ...L.newTab(), id: 'x' }], 'a')).toBe(t.id);
    expect(R.tabOf([t], 'nope')).toBeUndefined();
  });
});

describe('spaces are tabs, shown top or in the rail', () => {
  it('lets this browser decide, then the forge, then the skin, then the top', () => {
    expect(R.arrangement({})).toBe('top');
    expect(R.arrangement({ skin: 'classic' })).toBe('top');
    expect(R.arrangement({ skin: 'rail' })).toBe('rail');
    expect(R.arrangement({ skin: 'rail', forge: 'top' })).toBe('top');
    expect(R.arrangement({ skin: 'classic', forge: 'rail' })).toBe('rail');
    expect(R.arrangement({ skin: 'classic', forge: 'rail', browser: 'top' })).toBe('top');
    expect(R.arrangement({ skin: 'rail', forge: 'top', browser: 'rail' })).toBe('rail');
    // no choice, or one this client does not know, falls through
    expect(R.arrangement({ skin: 'rail', forge: '' })).toBe('rail');
    expect(R.arrangement({ skin: 'rail', forge: 'sideways', browser: null })).toBe('rail');
    expect(R.arrangement({ skin: 'sideways' })).toBe('top');
    expect(R.layoutName('top')).toBe('classic');
    expect(R.layoutName('rail')).toBe('rail');
  });

  it("follows a skin's layout through layoutOf", () => {
    expect(R.arrangement({ skin: K.layoutOf(skin('a', { layout: 'rail' })) })).toBe('rail');
    expect(R.arrangement({ skin: K.layoutOf(K.classic) })).toBe('top');
  });

  it('keeps existing tabs and puts a pane with a space label in the tab of that name', () => {
    const work = { ...L.newTab('work', 'w'), custom: true };
    const auto = L.newTab('', 'x');
    const make = (name: string, s: string) => L.newTab(name, s);
    const out = R.placeOrphans([work, auto], [{ id: 'a', labels: { space: 'work' } }, { id: 'b', labels: { space: 'billing ' } }, { id: 'c' }, { id: 'd', labels: { space: 'billing' } }, { id: 'e', labels: {} }], make);
    expect(out.slice(0, 2).map((t) => t.id)).toEqual([work.id, auto.id]);
    expect(L.panes(out[0].root).map((p) => p.session)).toEqual(['w', 'a']);
    expect(L.panes(out[1].root).map((p) => p.session)).toEqual(['x']);
    const billing = out.find((t) => t.name === 'billing')!;
    expect(billing.custom).toBe(true);
    expect(L.panes(billing.root).map((p) => p.session)).toEqual(['b', 'd']);
    const found = out.find((t) => t.name === 'Found')!;
    expect(L.panes(found.root).map((p) => p.session)).toEqual(['c', 'e']);
    expect(out).toHaveLength(4);
  });

  it('fills an empty pane of the named tab before splitting it', () => {
    const empty = { ...L.newTab('ops'), custom: true };
    const out = R.placeOrphans([empty], [{ id: 'a', labels: { space: 'ops' } }], (n, s) => L.newTab(n, s));
    expect(out).toHaveLength(1);
    expect(L.panes(out[0].root).map((p) => p.session)).toEqual(['a']);
    expect(R.placeOrphans([empty], [], (n, s) => L.newTab(n, s))).toEqual([empty]);
  });

  it('puts every agent pane in the crew, most urgent first', () => {
    const profiles = [
      { name: 'shell', agent: false },
      { name: 'claude', agent: true },
    ];
    const list = [pane('shell', '/a'), pane('quiet', '/a', { profile: 'claude' }), pane('busy', '/a', { word: 'working' }), pane('stuck', '/a', { word: 'blocked' })];
    expect(R.crew(list, profiles).map((s) => s.id)).toEqual(['stuck', 'busy', 'quiet']);
  });

  it('counts working, need you and done for the footer', () => {
    const list = [pane('a', '/', { word: 'working' }), pane('b', '/', { word: 'blocked' }), pane('c', '/', { word: 'error' }), pane('d', '/', { word: 'done' }), pane('e', '/', { word: 'done', seen: true }), pane('f', '/')];
    expect(R.counts(list)).toEqual({ working: 1, needYou: 2, done: 1 });
  });

  it('pads counts and stamps dates for skins', () => {
    expect(R.pad2(4)).toBe('04');
    expect(R.pad2(12)).toBe('12');
    expect(R.dateStamp(new Date(2026, 0, 1, 0, 0))).toEqual({ year: '2026', day: '001', tenth: '0' });
    expect(R.dateStamp(new Date(2026, 10, 16, 23, 20))).toEqual({ year: '2026', day: '320', tenth: '9' });
    expect(R.dateStamp(new Date(2024, 11, 31, 12, 0))).toEqual({ year: '2024', day: '366', tenth: '5' });
  });
});
