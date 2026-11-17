// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// The rail layout's model: spaces (what a pane is about), the crew (every
// agent pane) and the footer's counts. Pure, so it is tested on its own.
import type { Profile, Session } from './api';
import { byUrgency, shown } from './status';

type Pane = Pick<Session, 'id' | 'cwd' | 'labels' | 'status' | 'seen' | 'statusSince' | 'profile' | 'state'>;

/** spaceOf is the pane's space: its label "space", else the last part of its folder. */
export function spaceOf(s: Pick<Session, 'cwd' | 'labels'>): string {
  const label = s.labels?.space?.trim();
  if (label) return label;
  const parts = (s.cwd ?? '').split(/[\\/]+/).filter(Boolean);
  return parts.at(-1) ?? '/';
}

export interface Space {
  name: string;
  count: number;
}

/** spaces are the spaces of the panes, each with how many panes it has, by name. */
export function spaces(panes: Iterable<Pick<Session, 'cwd' | 'labels'>>): Space[] {
  const counts = new Map<string, number>();
  for (const s of panes) {
    const n = spaceOf(s);
    counts.set(n, (counts.get(n) ?? 0) + 1);
  }
  return [...counts].map(([name, count]) => ({ name, count })).sort((a, b) => a.name.localeCompare(b.name));
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

/** tabsIn are the ids of the tabs holding a pane of the space (all of them when space is null). */
export function tabsIn(
  tabs: { id: string; sessions: string[] }[],
  sessions: Map<string, Pick<Session, 'cwd' | 'labels'>>,
  space: string | null,
): Set<string> {
  const out = new Set<string>();
  for (const t of tabs) {
    if (space === null || t.sessions.some((id) => sessions.has(id) && spaceOf(sessions.get(id)!) === space)) out.add(t.id);
  }
  return out;
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
