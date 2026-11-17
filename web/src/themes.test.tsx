// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC
import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { ApiError, ForgeApi } from './api';
import * as K from './skins';
import { modeLabel, nextPref, resolveMode } from './theme';
import { InstallStatus, Themes } from './Themes';

const skin = (name: string, more: Partial<K.Skin> = {}): K.Skin => ({ name, label: name, builtin: false, ...more });
const html = (state: K.InstallState) => renderToStaticMarkup(<InstallStatus state={state} />);

describe('light/dark', () => {
  it('cycles system, light, dark, then system again', () => {
    expect(nextPref('system')).toBe('light');
    expect(nextPref('light')).toBe('dark');
    expect(nextPref('dark')).toBe('system');
  });

  it('resolves system to the system mode, and keeps a chosen mode', () => {
    expect(resolveMode('system', 'dark')).toBe('dark');
    expect(resolveMode('system', 'light')).toBe('light');
    expect(resolveMode('light', 'dark')).toBe('light');
    expect(resolveMode('dark', 'light')).toBe('dark');
  });

  it('says the current state in the tooltip', () => {
    expect(modeLabel('system', 'dark')).toBe('Light/dark: system (dark now)');
    expect(modeLabel('light', 'dark')).toBe('Light/dark: light');
  });
});

describe('install', () => {
  it('returns what was installed and what failed', async () => {
    const r = await K.installState(async () => ({ installed: [skin('harbour', { label: 'Harbour' })], failed: [{ path: 'skins/bad', reason: 'a skin needs a skin.json' }] }), 'me/skins');
    expect(r).toEqual({ kind: 'done', result: { installed: [skin('harbour', { label: 'Harbour' })], failed: [{ path: 'skins/bad', reason: 'a skin needs a skin.json' }] } });
  });

  it("says the forge can't install when the route is missing (405, or a plain 404)", async () => {
    expect(await K.installState(() => Promise.reject(new ApiError(405, 'error', 'Method Not Allowed')), 'x')).toEqual({ kind: 'unsupported' });
    expect(await K.installState(() => Promise.reject(new ApiError(404, 'error', 'Not Found')), 'x')).toEqual({ kind: 'unsupported' });
  });

  it("gives the forge's reason for a refusal", async () => {
    expect(await K.installState(() => Promise.reject(new ApiError(404, 'no_source', 'There is no folder /x.')), '/x')).toEqual({ kind: 'error', reason: 'There is no folder /x.' });
    expect(await K.installState(() => Promise.reject(new ApiError(400, 'input', 'Give a folder or a repo.')), '')).toEqual({ kind: 'error', reason: 'Give a folder or a repo.' });
  });

  it('posts the source to /api/skins/install, and reads a 405 from a forge without the route', async () => {
    const calls: [string, RequestInit | undefined][] = [];
    const real = globalThis.fetch;
    globalThis.fetch = (async (url: string, init?: RequestInit) => {
      calls.push([url, init]);
      return new Response('Method Not Allowed\n', { status: 405, statusText: 'Method Not Allowed' });
    }) as typeof fetch;
    try {
      const api = new ForgeApi('http://forge');
      expect(await K.installState(api.installSkins, 'owner/repo')).toEqual({ kind: 'unsupported' });
    } finally {
      globalThis.fetch = real;
    }
    expect(calls[0][0]).toBe('http://forge/api/skins/install');
    expect(calls[0][1]?.method).toBe('POST');
    expect(JSON.parse(String(calls[0][1]?.body))).toEqual({ source: 'owner/repo' });
  });

  it('renders each state', () => {
    expect(html({ kind: 'idle' })).toBe('');
    expect(html({ kind: 'busy', source: 'me/skins' })).toContain('spinner');
    expect(html({ kind: 'busy', source: 'me/skins' })).toContain('Installing from me/skins');
    expect(html({ kind: 'unsupported' })).toContain('This forge can&#x27;t install themes; update LaForge.');
    expect(html({ kind: 'error', reason: 'There is no folder /x.' })).toContain('There is no folder /x.');
    const done = html({ kind: 'done', result: { installed: [skin('harbour', { label: 'Harbour' }), skin('dusk', { label: 'Dusk' })], failed: [{ path: 'skins/bad', reason: 'no skin.json' }] } });
    expect(done).toContain('Installed Harbour');
    expect(done).toContain('Installed Dusk');
    expect(done).toContain('<code>skins/bad</code>: no skin.json');
    expect(html({ kind: 'done', result: { installed: [], failed: [] } })).toContain('No skins were found there.');
  });
});

describe('themes panel', () => {
  const list: K.SkinList = {
    default: 'classic',
    skins: [skin('classic', { label: 'Classic', builtin: true, author: 'LaForge' }), skin('console', { label: 'Console', builtin: true, layout: 'rail' }), skin('harbour', { label: 'Harbour', source: 'me/skins', author: 'You' })],
  };
  const noop = () => Promise.resolve();
  const out = renderToStaticMarkup(<Themes list={list} active="console" onPick={() => undefined} onDefault={noop} onRemove={noop} onInstall={() => Promise.resolve({ installed: [], failed: [] })} onClose={() => undefined} />);

  it('lists every skin with its tag, layout, author and source', () => {
    expect(out.match(/class="theme-row/g)?.length).toBe(3);
    expect(out).toContain('built in');
    expect(out).toContain('installed');
    expect(out).toContain('LaForge · classic');
    expect(out).toContain('You · classic');
    expect(out).toContain('me/skins');
  });

  it('marks the active skin and the default, offers Remove only for installed skins', () => {
    expect(out).toMatch(/theme-row is-active" data-skin-name="console"/);
    expect(out.match(/forge default<\/span>/g)?.length).toBe(1);
    expect(out.match(/Make forge default/g)?.length).toBe(2);
    expect(out.match(/class="theme-remove"/g)?.length).toBe(1);
  });

  it('draws a light and a dark strip per skin', () => {
    expect(out.match(/class="theme-strip"/g)?.length).toBe(6);
  });
});
