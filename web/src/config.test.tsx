// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC
import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { ForgeApi, type Config, type Profile } from './api';
import * as G from './config';
import { Settings, type SettingsProps } from './Settings';

const prof = (name: string, more: Partial<Profile> = {}): Profile => ({ name, label: name, description: '', command: name, agent: false, available: true, ...more });

describe('arguments', () => {
  it('splits as a shell would, quotes holding spaces', () => {
    expect(G.splitArgs(`run  llama3.2 --opt "a b" 'c d' e\\ f ""`)).toEqual(['run', 'llama3.2', '--opt', 'a b', 'c d', 'e f', '']);
    expect(G.splitArgs('   ')).toEqual([]);
  });

  it('joins back what it splits', () => {
    for (const args of [['--model', 'sonnet'], ['a b', "it's", '', '{session}', '--x=1']]) {
      expect(G.splitArgs(G.joinArgs(args))).toEqual(args);
    }
    expect(G.joinArgs(['--resume', '{session}'])).toBe('--resume {session}');
  });
});

describe('the profile form', () => {
  it('gives the profile to send, label defaulting to the name', () => {
    const r = G.profileOf({ ...G.emptyForm(), name: 'aider', command: ' aider ', args: '--model sonnet', env: 'A=1\n\n B=two ' });
    expect(r.profile).toEqual({ name: 'aider', label: 'aider', description: '', command: 'aider', args: ['--model', 'sonnet'], env: ['A=1', 'B=two'], agent: false, available: false });
  });

  it('refuses what the forge would', () => {
    expect(G.profileOf({ ...G.emptyForm(), name: 'Bad Name', command: 'x' }).error).toMatch(/lowercase/);
    expect(G.profileOf({ ...G.emptyForm(), name: 'ok', command: '  ' }).error).toMatch(/command/);
    expect(G.profileOf({ ...G.emptyForm(), name: 'ok', command: 'x', env: 'NOEQUALS' }).error).toMatch(/NAME=value/);
  });

  it('round-trips a profile through the form, resume kept', () => {
    const p = prof('codex', { label: 'Codex', args: ['--full-auto'], env: ['X=1'], agent: true, resume: ['resume', '{session}'], builtin: true });
    const back = G.profileOf(G.formOf(p)).profile!;
    expect(back).toMatchObject({ name: 'codex', label: 'Codex', args: ['--full-auto'], env: ['X=1'], agent: true, resume: ['resume', '{session}'] });
    expect(G.formOf(p).editing).toBe(true);
  });

  it('says where a profile comes from', () => {
    expect(G.origin(prof('shell', { builtin: true }))).toBe('built in');
    expect(G.origin(prof('codex', { builtin: true, custom: true }))).toBe('changed');
    expect(G.origin(prof('mine', { custom: true }))).toBe('yours');
  });
});

describe('small things', () => {
  it('keeps the text size in range', () => {
    expect(G.clampFont(30)).toBe(22);
    expect(G.clampFont(2)).toBe(9);
    expect(G.clampFont(Number.NaN)).toBe(13);
    expect(G.clampFont(14.4)).toBe(14);
  });

  it('says how long a share has', () => {
    const now = Date.parse('2026-11-17T10:00:00Z');
    expect(G.timeLeft('2026-11-17T12:05:00Z', now)).toBe('2h 5m');
    expect(G.timeLeft('2026-11-17T10:40:00Z', now)).toBe('40m');
    expect(G.timeLeft('2026-11-17T09:00:00Z', now)).toBe('expired');
    expect(G.timeLeft('2026-11-21T10:00:00Z', now)).toBe('4d');
  });

  it('compares folders without a trailing slash', () => {
    expect(G.sameFolder(' /a/b/ ', '/a/b')).toBe(true);
    expect(G.sameFolder('/a/c', '/a/b')).toBe(false);
  });
});

const config: Config = { cwd: '/Users/me/src', cwdSaved: true, skin: 'console', scenes: false, host: 'mac.local', user: 'me', version: '0.9.0', port: 47612, home: '/Users/me/.laforge/forge', profiles: [prof('shell', { builtin: true }), prof('later', { custom: true, available: false, reason: 'later is not installed' })] };
const props = (more: Partial<SettingsProps> = {}): SettingsProps => ({
  api: new ForgeApi(''),
  config,
  skins: [{ name: 'classic', label: 'Classic', builtin: true }, { name: 'console', label: 'Console', builtin: true }],
  sessions: new Map(),
  onChanged: async () => undefined,
  pref: 'dark',
  sysMode: 'light',
  onPref: () => undefined,
  notify: false,
  sound: true,
  permission: 'default',
  onNotify: () => undefined,
  onSound: () => undefined,
  fontSize: 15,
  onFontSize: () => undefined,
  now: Date.parse('2026-11-17T10:00:00Z'),
  onClose: () => undefined,
  shares: [{ token: 'abcdef0123456789', pane: 'p1', created: '2026-11-17T09:00:00Z', expires: '2026-11-17T11:00:00Z' }],
  ...more,
});

describe('the settings panel', () => {
  it('draws every section from the config', () => {
    const html = renderToStaticMarkup(<Settings {...props()} />);
    for (const want of ['>Forge<', '>Profiles', '>This browser<', '>Shares', '>About<', 'value="/Users/me/src"', 'Use where la up runs', '<option value="console" selected="">Console</option>', 'not installed', '15px', '1h left', '47612', '/Users/me/.laforge/forge', 'Restart the forge', '/notices.txt']) {
      expect(html).toContain(want);
    }
    expect(html).toMatch(/aria-pressed="true"[^>]*>.*Dark/);
    // scenes off: unchecked
    expect(html).toMatch(/<input type="checkbox"\/>Animated scenes/);
  });

  it('says so when the forge is older than the panel', () => {
    const html = renderToStaticMarkup(<Settings {...props({ config: { cwd: '/x', host: 'h', user: 'u', version: '0.1' } })} />);
    expect(html).toContain('older than the Settings panel');
    expect(html).not.toContain('>Profiles');
    expect(html).toContain('Where la up ran');
  });
});
