// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC
import { describe, expect, it } from 'vitest';
import type { Session, StatusWord } from './api';
import * as R from './rail';
import * as K from './skins';
import { darkTerminal, lightTerminal } from './theme';

const skin = (name: string, more: Partial<K.Skin> = {}): K.Skin => ({ name, label: name, builtin: false, ...more });

describe('skin loader', () => {
  const list: K.SkinList = { default: 'console', skins: [skin('classic', { builtin: true }), skin('console', { layout: 'rail', mode: 'dark' }), skin('paper')] };

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
    expect(doc.documentElement.dataset.layout).toBe('rail');
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
  it('groups panes into spaces by label, else by folder', () => {
    const list = [pane('a', '/Users/me/src/billing'), pane('b', '/Users/me/src/billing/'), pane('c', '/srv/site', { space: 'website' }), pane('d', '/'), pane('e', 'C:\\work\\billing')];
    expect(R.spaceOf(list[2])).toBe('website');
    expect(R.spaceOf(list[3])).toBe('/');
    expect(R.spaces(list)).toEqual([
      { name: '/', count: 1 },
      { name: 'billing', count: 3 },
      { name: 'website', count: 1 },
    ]);
  });

  it('filters tabs to the ones holding a space', () => {
    const sessions = new Map([
      ['a', pane('a', '/x/billing')],
      ['c', pane('c', '/y', { space: 'website' })],
    ]);
    const tabs = [
      { id: 't1', sessions: ['a'] },
      { id: 't2', sessions: ['c', 'gone'] },
      { id: 't3', sessions: [] },
    ];
    expect([...R.tabsIn(tabs, sessions, 'website')]).toEqual(['t2']);
    expect([...R.tabsIn(tabs, sessions, null)]).toEqual(['t1', 't2', 't3']);
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
});
