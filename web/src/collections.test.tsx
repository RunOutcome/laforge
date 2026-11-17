// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { ForgeApi } from './api';
import * as C from './collections';
import { CollectionView, GetMore } from './GetMore';
import * as K from './skins';
import { Themes } from './Themes';

type Call = { method: string; url: string; body?: unknown };
type Reply = { status: number; body?: unknown };

// a fetch mock: each route answers from the table; every call is recorded
let calls: Call[] = [];
let routes: Record<string, Reply | ((c: Call) => Reply)> = {};
const real = globalThis.fetch;
beforeEach(() => {
  calls = [];
  routes = {};
  globalThis.fetch = (async (url: string, init?: RequestInit) => {
    const c: Call = { method: init?.method ?? 'GET', url: url.replace('http://forge', ''), body: init?.body ? JSON.parse(String(init.body)) : undefined };
    calls.push(c);
    const route = routes[`${c.method} ${c.url}`];
    const r = typeof route === 'function' ? route(c) : (route ?? { status: 404, body: undefined });
    if (r.body === undefined) return new Response(r.status === 204 ? null : 'not here\n', { status: r.status, statusText: r.status === 404 ? 'Not Found' : 'Status' });
    return new Response(JSON.stringify(r.body), { status: r.status, headers: { 'Content-Type': 'application/json' } });
  }) as typeof fetch;
});
afterEach(() => {
  globalThis.fetch = real;
});

const api = new ForgeApi('http://forge');
const official: C.Collection = { id: 'official', name: 'LaForge official', source: 'RunOutcome/laforge/skins', builtin: true, description: 'The skins released with LaForge.' };
const library: C.Collection = { id: 'library', name: 'LaForge library', source: 'RunOutcome/laforge-skins', builtin: true };
const mine: C.Collection = { id: 'me-skins', name: 'me/skins', source: 'https://github.com/me/skins', builtin: false, addedAt: '2026-11-16T10:00:00Z' };
const offer = (name: string, more: Partial<C.Offer> = {}): C.Offer => ({ name, label: name[0].toUpperCase() + name.slice(1), path: `skins/${name}`, installed: false, ...more });
const offers = [
  offer('harbour', { author: 'You', layout: 'classic', description: 'Slate and brass.' }),
  offer('dusk', { installed: true, layout: 'rail' }),
  offer('broken', { problem: 'skin.json: unknown field "mode"' }),
  offer('ember', { author: 'Ann' }),
];
const harbour: K.Skin = { name: 'harbour', label: 'Harbour', builtin: false };
const ember: K.Skin = { name: 'ember', label: 'Ember', builtin: false };

describe('collections: listing', () => {
  it('lists the collections, official first, then library, then the user’s', async () => {
    routes['GET /api/skins/collections'] = { status: 200, body: [mine, library, official] };
    expect(await C.loadCollections(api)).toEqual({ kind: 'ready', list: [official, library, mine] });
    expect(calls).toEqual([{ method: 'GET', url: '/api/skins/collections', body: undefined }]);
  });

  it('says a forge without the routes cannot browse (404, 405)', async () => {
    expect(await C.loadCollections(api)).toEqual({ kind: 'unsupported' });
    routes['GET /api/skins/collections'] = { status: 405 };
    expect(await C.loadCollections(api)).toEqual({ kind: 'unsupported' });
  });

  it('gives any other failure as an error', async () => {
    routes['GET /api/skins/collections'] = { status: 500, body: { error: 'internal', reason: 'The forge broke.' } };
    expect(await C.loadCollections(api)).toEqual({ kind: 'error', reason: 'The forge broke.' });
  });
});

describe('collections: browsing', () => {
  it('lists the skins a collection offers, and refreshes with ?refresh=1', async () => {
    routes['GET /api/skins/collections/official/skins'] = { status: 200, body: offers };
    routes['GET /api/skins/collections/official/skins?refresh=1'] = { status: 200, body: offers.slice(0, 1) };
    expect(await C.loadOffers(api, 'official')).toEqual({ kind: 'ready', offers });
    expect(await C.loadOffers(api, 'official', true)).toEqual({ kind: 'ready', offers: offers.slice(0, 1) });
    expect(calls.map((c) => c.url)).toEqual(['/api/skins/collections/official/skins', '/api/skins/collections/official/skins?refresh=1']);
  });

  it('gives a 502 with its reason and a retry, and the retry can succeed', async () => {
    let n = 0;
    routes['GET /api/skins/collections/me-skins/skins'] = () => (n++ === 0 ? { status: 502, body: { error: 'source', reason: 'GitHub could not be reached.' } } : { status: 200, body: offers });
    expect(await C.loadOffers(api, 'me-skins')).toEqual({ kind: 'error', reason: 'GitHub could not be reached.', retry: true });
    expect(await C.loadOffers(api, 'me-skins')).toEqual({ kind: 'ready', offers });
  });

  it('offers no retry for a refusal that will not change', async () => {
    routes['GET /api/skins/collections/gone/skins'] = { status: 404, body: { error: 'not_found', reason: 'No collection gone.' } };
    expect(await C.loadOffers(api, 'gone')).toEqual({ kind: 'error', reason: 'No collection gone.', retry: false });
  });
});

describe('collections: install', () => {
  it('installs one skin by name and marks it installed', async () => {
    routes['POST /api/skins/install'] = { status: 200, body: { installed: [harbour], failed: [] } };
    routes['GET /api/skins'] = { status: 200, body: { default: 'classic', skins: [K.classic, harbour] } };
    const r = await C.installOffers(api, 'official', offers, ['harbour'], api.skins);
    expect(calls[0]).toEqual({ method: 'POST', url: '/api/skins/install', body: { collection: 'official', names: ['harbour'] } });
    expect(calls[1]).toEqual({ method: 'GET', url: '/api/skins', body: undefined });
    expect(r.installed).toEqual(['harbour']);
    expect(r.offers.find((o) => o.name === 'harbour')?.installed).toBe(true);
    expect(r.failed).toEqual({});
  });

  it('Install all takes every offer not installed and with no problem', async () => {
    expect(C.installable(offers).map((o) => o.name)).toEqual(['harbour', 'ember']);
    routes['POST /api/skins/install'] = { status: 200, body: { installed: [harbour], failed: [{ path: 'skins/ember', reason: 'ember: a font is 20 MB' }] } };
    const r = await C.installOffers(
      api,
      'official',
      offers,
      C.installable(offers).map((o) => o.name),
    );
    expect(calls[0].body).toEqual({ collection: 'official', names: ['harbour', 'ember'] });
    expect(r.installed).toEqual(['harbour']);
    expect(r.failed).toEqual({ ember: 'ember: a font is 20 MB' });
    expect(r.offers.filter((o) => o.installed).map((o) => o.name)).toEqual(['harbour', 'dusk']);
  });

  it('sends nothing when there is nothing to install', async () => {
    const r = await C.installOffers(api, 'official', offers, []);
    expect(calls).toEqual([]);
    expect(r.installed).toEqual([]);
  });

  it('gives the reason when the whole install fails', async () => {
    routes['POST /api/skins/install'] = { status: 502, body: { error: 'source', reason: 'GitHub could not be reached.' } };
    const r = await C.installOffers(api, 'official', offers, ['harbour']);
    expect(r.error).toBe('GitHub could not be reached.');
    expect(r.offers).toBe(offers);
  });
});

describe('collections: add and remove', () => {
  it('adds a collection (201)', async () => {
    routes['POST /api/skins/collections'] = { status: 201, body: mine };
    expect(await C.addCollection(api, 'me/skins')).toEqual({ kind: 'added', collection: mine });
    expect(calls[0]).toEqual({ method: 'POST', url: '/api/skins/collections', body: { source: 'me/skins' } });
  });

  it('gives the reason for a bad source (400) and one already added (409)', async () => {
    routes['POST /api/skins/collections'] = { status: 400, body: { error: 'source', reason: 'Give a GitHub link or a folder.' } };
    expect(await C.addCollection(api, 'nope nope')).toEqual({ kind: 'error', reason: 'Give a GitHub link or a folder.' });
    routes['POST /api/skins/collections'] = { status: 409, body: { error: 'exists', reason: 'me/skins is already a collection.' } };
    expect(await C.addCollection(api, 'me/skins')).toEqual({ kind: 'error', reason: 'me/skins is already a collection.' });
  });

  it('removes a collection with DELETE, and a built-in is refused (409)', async () => {
    routes['DELETE /api/skins/collections/me-skins'] = { status: 204 };
    routes['DELETE /api/skins/collections/official'] = { status: 409, body: { error: 'builtin', reason: 'A built-in collection stays.' } };
    await expect(api.removeCollection('me-skins')).resolves.toBeUndefined();
    await expect(api.removeCollection('official')).rejects.toMatchObject({ status: 409, code: 'builtin' });
  });
});

describe('get more: drawn', () => {
  const draw = (initial: Parameters<typeof GetMore>[0]['initial'], installedNames = ['classic', 'dusk']) =>
    renderToStaticMarkup(<GetMore api={api} installedNames={installedNames} active="classic" onPick={() => undefined} onInstalled={() => undefined} initial={initial} />);

  it('draws the collection tabs in order with the selected one', () => {
    const out = draw({ collections: { kind: 'ready', list: [official, library, mine] }, selected: 'official', offers: { official: { kind: 'ready', offers } } });
    const tabs = [...out.matchAll(/role="tab" aria-selected="(\w+)"[^>]*>([^<]+)</g)].map((m) => [m[2], m[1]]);
    expect(tabs).toEqual([
      ['LaForge official', 'true'],
      ['LaForge library', 'false'],
      ['me/skins', 'false'],
    ]);
  });

  it('draws each offer: label, author, layout, description, Install or Installed, problems disabled with the reason', () => {
    const out = draw({ collections: { kind: 'ready', list: [official] }, selected: 'official', offers: { official: { kind: 'ready', offers } } });
    expect(out.match(/class="more-row/g)?.length).toBe(4);
    expect(out).toContain('You · classic');
    expect(out).toContain('Slate and brass.');
    expect(out).toContain('Install all (2)');
    expect(out).toMatch(/data-offer="dusk"[\s\S]*?Installed <svg/);
    expect(out).toMatch(/data-offer="dusk"[\s\S]*?>Use</);
    expect(out).toMatch(/data-offer="broken" aria-disabled="true"[\s\S]*?Won&#x27;t install: skin.json: unknown field &quot;mode&quot;[\s\S]*?disabled="">Install</);
    expect(out).not.toContain('>Remove<');
  });

  it('gives a user collection Remove; built-ins have none', () => {
    const out = draw({ collections: { kind: 'ready', list: [official, mine] }, selected: 'me-skins', offers: { 'me-skins': { kind: 'ready', offers: [] } } });
    expect(out).toContain('>Remove<');
    expect(out).toContain('https://github.com/me/skins');
    expect(out).toContain('This collection offers no skins.');
  });

  it('asks inline before removing a collection', () => {
    const out = renderToStaticMarkup(
      <CollectionView
        collection={mine}
        state={{ kind: 'ready', offers: [] }}
        busy={new Set()}
        failed={{}}
        note={null}
        installedNames={[]}
        active="classic"
        confirming
        removing={false}
        removeError={null}
        onRefresh={() => undefined}
        onRetry={() => undefined}
        onInstall={() => undefined}
        onPick={() => undefined}
        onAskRemove={() => undefined}
        onCancelRemove={() => undefined}
        onRemove={() => undefined}
      />,
    );
    expect(out).toContain('Remove the collection me/skins? Installed skins stay.');
    expect(out).toContain('>Cancel<');
  });

  it('draws loading, a 502 with Retry, and the unsupported forge', () => {
    expect(draw({ collections: { kind: 'loading' } })).toContain('Loading collections');
    const err = draw({ collections: { kind: 'ready', list: [mine] }, selected: 'me-skins', offers: { 'me-skins': { kind: 'error', reason: 'GitHub could not be reached.', retry: true } } });
    expect(err).toContain('GitHub could not be reached.');
    expect(err).toContain('>Retry<');
    const no = draw({ collections: { kind: 'unsupported' } });
    expect(no).toContain('This forge can&#x27;t browse collections; update LaForge.');
    expect(no).not.toContain('Add a collection');
  });

  it('keeps the rest of the panel on a forge without collections', () => {
    const list: K.SkinList = { default: 'classic', skins: [K.classic, ember] };
    const out = renderToStaticMarkup(
      <Themes
        list={list}
        active="classic"
        onPick={() => undefined}
        onDefault={() => Promise.resolve()}
        onRemove={() => Promise.resolve()}
        onInstall={() => Promise.resolve({ installed: [], failed: [] })}
        onClose={() => undefined}
        collections={api}
        moreInitial={{ collections: { kind: 'unsupported' } }}
      />,
    );
    expect(out).toContain('update LaForge');
    expect(out.match(/class="theme-row/g)?.length).toBe(2);
    expect(out).toContain('Install from a link');
    expect(out).toContain('Make forge default');
  });
});
