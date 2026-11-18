// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// The spaces' model: where they show (top or rail), what a space is (a
// tab), the crew (every agent pane) and the footer's counts. Pure, so it is
// tested on its own.
import type { Profile, Session } from './api';
import * as L from './layout';
import { byUrgency, shown } from './status';

type Pane = Pick<Session, 'id' | 'cwd' | 'labels' | 'status' | 'seen' | 'statusSince' | 'profile' | 'state'>;

/** folderOf is the last part of a folder, for a short name ("/" for the root). */
export function folderOf(cwd: string | undefined): string {
  const parts = (cwd ?? '').split(/[\\/]+/).filter(Boolean);
  return parts.at(-1) ?? '/';
}

/** spaceOf is the space a pane asks for: its label "space", else the last part of its folder. */
export function spaceOf(s: Pick<Session, 'cwd' | 'labels'>): string {
  return s.labels?.space?.trim() || folderOf(s.cwd);
}

// ---- spaces are tabs ----
//
// A space is a tab: one list, shown in one place at a time, on the title
// line (top) or in the rail at the left (rail). Where is the user's choice;
// a skin's layout only suggests one.

export type Arrangement = 'top' | 'rail';

/** asArrangement is v when it is an arrangement, else null. */
export const asArrangement = (v: unknown): Arrangement | null => (v === 'top' || v === 'rail' ? v : null);

/**
 * arrangement is where the spaces show: this browser's own choice, else the
 * forge's (spaces in /api/config), else what the skin suggests (its layout:
 * rail suggests the rail, anything else the top), else the top.
 */
export function arrangement(o: { browser?: unknown; forge?: unknown; skin?: unknown }): Arrangement {
  return asArrangement(o.browser) ?? asArrangement(o.forge) ?? (o.skin === 'rail' ? 'rail' : 'top');
}

/** layoutName is an arrangement as the older data-layout hook names it: classic (top) or rail. */
export const layoutName = (a: Arrangement): 'classic' | 'rail' => (a === 'rail' ? 'rail' : 'classic');

const spacesKey = 'laforge.spaces';

/** loadBrowserSpaces is this browser's own arrangement, or null to follow the forge. */
export function loadBrowserSpaces(): Arrangement | null {
  try {
    return asArrangement(localStorage.getItem(spacesKey));
  } catch {
    return null;
  }
}

export function saveBrowserSpaces(a: Arrangement | null) {
  try {
    if (a) localStorage.setItem(spacesKey, a);
    else localStorage.removeItem(spacesKey);
  } catch {
    /* private mode: this session only */
  }
}

interface TabLike {
  id: string;
  name: string;
  custom?: boolean;
  root: L.Node;
  focus: string;
}

/** tabFolder is the short folder of a tab: its focused pane's, else its first pane's with a session ('' for none). */
export function tabFolder(t: Pick<TabLike, 'root' | 'focus'>, sessions: Map<string, Pick<Session, 'cwd'>>): string {
  const list = L.panes(t.root);
  const pick = [L.findPane(t.root, t.focus), ...list].find((p) => p?.session && sessions.has(p.session));
  return pick?.session ? folderOf(sessions.get(pick.session)!.cwd) : '';
}

/** tabOf is the id of the tab holding a session, if one does. */
export function tabOf(tabs: Pick<TabLike, 'id' | 'root'>[], session: string): string | undefined {
  return tabs.find((t) => L.panes(t.root).some((p) => p.session === session))?.id;
}

/**
 * placeOrphans puts panes no tab holds (an agent or la run started them)
 * into tabs: a pane with a "space" label joins the tab of that name, made
 * when there is none; the rest go to one new "Found" tab. Existing tabs keep
 * their arrangement; a pane joining one goes to its right.
 */
export function placeOrphans<T extends TabLike>(tabs: T[], orphans: Pick<Session, 'id' | 'labels'>[], make: (name: string, session: string) => T): T[] {
  const out = [...tabs];
  const loose: string[] = [];
  const made = new Set<string>();
  for (const s of orphans) {
    const want = s.labels?.space?.trim();
    if (!want) {
      loose.push(s.id);
      continue;
    }
    const i = out.findIndex((t) => t.name === want);
    if (i < 0) {
      out.push({ ...make(want, s.id), custom: true });
      made.add(want);
      continue;
    }
    const t = out[i];
    const last = L.panes(t.root).at(-1)!;
    let root: L.Node;
    if (!last.session) root = L.setSession(t.root, last.id, s.id);
    else root = L.split(t.root, last.id, 'row', L.newPane(s.id));
    out[i] = { ...t, root: made.has(want) ? L.tiled(root) : root };
  }
  if (loose.length) {
    const found = { ...make('Found', loose[0]), custom: true };
    let root: L.Node = found.root;
    for (const id of loose.slice(1)) root = L.split(root, L.panes(root).at(-1)!.id, 'row', L.newPane(id));
    out.push({ ...found, root: L.tiled(root) });
  }
  return out;
}

/** isAgent: a pane that reports a status or runs an agent profile. */
export const isAgent = (s: Pick<Session, 'status' | 'profile'>, profiles: Pick<Profile, 'name' | 'agent'>[]) =>
  !!s.status || !!profiles.find((p) => p.name === s.profile)?.agent;

/** crew are the agent panes, most urgent first, then the most recent. */
export function crew<T extends Pane>(panes: Iterable<T>, profiles: Pick<Profile, 'name' | 'agent'>[]): T[] {
  return [...panes].filter((s) => isAgent(s, profiles)).sort(byUrgency);
}

export interface Counts {
  working: number;
  needYou: number;
  done: number;
}

/** counts are the footer's: working, need you (blocked or error) and done (unseen). */
export function counts(panes: Iterable<Pick<Session, 'status' | 'seen'>>): Counts {
  const c: Counts = { working: 0, needYou: 0, done: 0 };
  for (const s of panes) {
    const w = shown(s as Pane);
    if (w === 'working') c.working++;
    else if (w === 'blocked' || w === 'error') c.needYou++;
    else if (w === 'done') c.done++;
  }
  return c;
}

/** pad2 is a count with at least two digits ("04"), for skins that number things. */
export const pad2 = (n: number): string => String(n).padStart(2, '0');

/** dateStamp is a compact numeric date for skins: the year, the day of the year (3 digits) and the tenth of the day. */
export function dateStamp(d: Date): { year: string; day: string; tenth: string } {
  const start = new Date(d.getFullYear(), 0, 1);
  const day = Math.floor((new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime() - start.getTime()) / 86400000) + 1;
  const tenth = Math.floor(((d.getHours() * 60 + d.getMinutes()) / 1440) * 10);
  return { year: String(d.getFullYear()), day: String(day).padStart(3, '0'), tenth: String(tenth) };
}
