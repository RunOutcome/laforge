// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// Skin collections: where the theme panel's "Get more" finds skins. The
// forge lists its built-in collections (official, then library) and the
// ones people added; each offers skins to install one at a time or all at
// once. These helpers turn the forge's answers into the panel's states.
import { ApiError } from './api';
import type { InstallResult } from './skins';

export interface Collection {
  id: string;
  name: string;
  source: string;
  description?: string;
  builtin: boolean;
  addedAt?: string;
}

/** Offer is a skin a collection offers. problem: it fails the checks and would not install. */
export interface Offer {
  name: string;
  label: string;
  description?: string;
  author?: string;
  layout?: string;
  path: string;
  installed: boolean;
  problem?: string;
}

/** CollectionsApi is the part of the forge's API the "Get more" section uses. */
export interface CollectionsApi {
  collections: () => Promise<Collection[]>;
  addCollection: (source: string) => Promise<Collection>;
  removeCollection: (id: string) => Promise<void>;
  collectionSkins: (id: string, refresh?: boolean) => Promise<Offer[]>;
  installFrom: (collection: string, names: string[]) => Promise<InstallResult>;
}

export type CollectionsState = { kind: 'loading' } | { kind: 'unsupported' } | { kind: 'error'; reason: string } | { kind: 'ready'; list: Collection[] };

/** OffersState: retry is true when the forge could not reach the source (502) and trying again may work. */
export type OffersState = { kind: 'loading' } | { kind: 'error'; reason: string; retry: boolean } | { kind: 'ready'; offers: Offer[] };

const reasonOf = (err: unknown, fallback: string) => (err as Error)?.message || fallback;

/** unsupported: a forge without the collection routes answers 404 or 405 (or 501). */
export function unsupported(err: unknown): boolean {
  const e = err as { status?: number };
  return e?.status === 404 || e?.status === 405 || e?.status === 501;
}

/** ordered puts the built-ins first, official then library, then the user's in the order the forge gave. */
export function ordered(list: Collection[]): Collection[] {
  const rank = (c: Collection) => (c.id === 'official' ? 0 : c.id === 'library' ? 1 : c.builtin ? 2 : 3);
  return list
    .map((c, i) => ({ c, i }))
    .sort((a, b) => rank(a.c) - rank(b.c) || a.i - b.i)
    .map((x) => x.c);
}

export async function loadCollections(api: Pick<CollectionsApi, 'collections'>): Promise<CollectionsState> {
  try {
    return { kind: 'ready', list: ordered((await api.collections()) ?? []) };
  } catch (err) {
    if (unsupported(err)) return { kind: 'unsupported' };
    return { kind: 'error', reason: reasonOf(err, 'The forge did not list its collections.') };
  }
}

export async function loadOffers(api: Pick<CollectionsApi, 'collectionSkins'>, id: string, refresh = false): Promise<OffersState> {
  try {
    return { kind: 'ready', offers: (await api.collectionSkins(id, refresh)) ?? [] };
  } catch (err) {
    const e = err as ApiError;
    return { kind: 'error', reason: reasonOf(err, 'The forge did not list the skins.'), retry: e?.status === 502 || !(err instanceof ApiError) };
  }
}

/** installable is every offer Install all takes: not installed, no problem. */
export const installable = (offers: Offer[]): Offer[] => offers.filter((o) => !o.installed && !o.problem);

/** InstallOutcome is how installing offers went: the offers with the installed ones marked, and each failure by skin name. */
export interface InstallOutcome {
  offers: Offer[];
  installed: string[];
  failed: Record<string, string>;
  /** error: the whole request failed. */
  error?: string;
}

/** installOffers installs the named offers from a collection, marks the ones that installed, and then refreshes the installed list. */
export async function installOffers(
  api: Pick<CollectionsApi, 'installFrom'>,
  collection: string,
  offers: Offer[],
  names: string[],
  refresh?: () => Promise<unknown> | unknown,
): Promise<InstallOutcome> {
  if (names.length === 0) return { offers, installed: [], failed: {} };
  try {
    const r = await api.installFrom(collection, names);
    const installed = (r?.installed ?? []).map((k) => k.name);
    const failed: Record<string, string> = {};
    for (const f of r?.failed ?? []) {
      const o = offers.find((x) => x.path === f.path || x.name === f.path);
      failed[o?.name ?? f.path] = f.reason;
    }
    if (installed.length && refresh) await refresh();
    return { offers: offers.map((o) => (installed.includes(o.name) ? { ...o, installed: true } : o)), installed, failed };
  } catch (err) {
    return { offers, installed: [], failed: {}, error: reasonOf(err, 'The install failed.') };
  }
}

export type AddOutcome = { kind: 'added'; collection: Collection } | { kind: 'error'; reason: string };

/** addCollection adds a collection from a GitHub link or a folder; the forge's reason on a refusal (400 source, 409 exists). */
export async function addCollection(api: Pick<CollectionsApi, 'addCollection'>, source: string): Promise<AddOutcome> {
  try {
    return { kind: 'added', collection: await api.addCollection(source) };
  } catch (err) {
    const e = err as ApiError;
    if (e?.status === 409 && e.code === 'exists') return { kind: 'error', reason: e.message || 'That collection is already added.' };
    return { kind: 'error', reason: reasonOf(err, 'The forge could not add that collection.') };
  }
}
