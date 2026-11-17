// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// Agent status as the client shows it: the program's own report, with
// Herdr's rule that "done" means finished while you weren't looking, so a
// done pane a viewer has seen shows as idle.
import type { Session, StatusWord } from './api';

type Pane = Pick<Session, 'id' | 'status' | 'seen' | 'statusSince'>;

/** shown is the status word a pane shows, or null when its program reports none. */
export function shown(s?: Pane | null): StatusWord | null {
  const word = s?.status?.state;
  if (!word) return null;
  return word === 'done' && s.seen ? 'idle' : word;
}

const rank: Record<StatusWord, number> = { blocked: 4, error: 4, done: 3, working: 2, idle: 1 };

/** urgency orders shown words: blocked = error > done (unseen) > working > idle > none (0). */
export function urgency(word: StatusWord | null): number {
  return word ? rank[word] : 0;
}

/** tabStatus is the most urgent word among the panes, or null when none reports. */
export function tabStatus(sessions: Iterable<Pane | undefined>): StatusWord | null {
  let best: StatusWord | null = null;
  for (const s of sessions) {
    const w = shown(s);
    if (urgency(w) > urgency(best)) best = w;
  }
  return best;
}

/** alerting words are the ones worth a notification. */
export const alerting = (w: StatusWord | null): w is 'blocked' | 'error' | 'done' => w === 'blocked' || w === 'error' || w === 'done';

export interface Transition {
  id: string;
  word: 'blocked' | 'error' | 'done';
  since?: string;
}

/**
 * transitions are the panes that newly became blocked, error or done (unseen)
 * between two lists. A pane only in next is new, not a transition; a pane
 * whose word stayed the same but whose statusSince moved went through another
 * state between the lists, so it counts again.
 */
export function transitions(prev: Iterable<Pane>, next: Iterable<Pane>): Transition[] {
  const before = new Map<string, Pane>();
  for (const s of prev) before.set(s.id, s);
  const out: Transition[] = [];
  for (const s of next) {
    const w = shown(s);
    if (!alerting(w)) continue;
    const p = before.get(s.id);
    if (!p) continue;
    if (shown(p) === w && p.statusSince === s.statusSince) continue;
    out.push({ id: s.id, word: w, since: s.statusSince });
  }
  return out;
}

/** byUrgency sorts most urgent first, then the most recent statusSince. */
export function byUrgency(a: Pane, b: Pane): number {
  const d = urgency(shown(b)) - urgency(shown(a));
  if (d) return d;
  return (b.statusSince ? Date.parse(b.statusSince) : 0) - (a.statusSince ? Date.parse(a.statusSince) : 0);
}

/** ago is a short time since iso, as of now: "now", "42s", "5m", "3h", "2d". */
export function ago(iso: string | undefined, now: number): string {
  if (!iso) return '';
  const s = Math.max(0, Math.floor((now - Date.parse(iso)) / 1000));
  if (Number.isNaN(s)) return '';
  if (s < 5) return 'now';
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  if (s < 86400) return `${Math.floor(s / 3600)}h`;
  return `${Math.floor(s / 86400)}d`;
}
