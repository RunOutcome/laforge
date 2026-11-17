// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC
import { describe, expect, it } from 'vitest';
import type { StatusWord } from './api';
import * as S from './status';

const pane = (id: string, state?: StatusWord, seen = false, statusSince?: string) => ({ id, status: state ? { state } : undefined, seen, statusSince });

describe('status', () => {
  it('shows the reported word, done as idle once seen, and nothing when unreported', () => {
    expect(S.shown(pane('a', 'blocked'))).toBe('blocked');
    expect(S.shown(pane('a', 'done'))).toBe('done');
    expect(S.shown(pane('a', 'done', true))).toBe('idle');
    expect(S.shown(pane('a', 'blocked', true))).toBe('blocked');
    expect(S.shown(pane('a'))).toBeNull();
    expect(S.shown(undefined)).toBeNull();
  });

  it('orders blocked = error > done > working > idle > none', () => {
    expect(S.urgency('blocked')).toBe(S.urgency('error'));
    expect(S.urgency('error')).toBeGreaterThan(S.urgency('done'));
    expect(S.urgency('done')).toBeGreaterThan(S.urgency('working'));
    expect(S.urgency('working')).toBeGreaterThan(S.urgency('idle'));
    expect(S.urgency('idle')).toBeGreaterThan(S.urgency(null));
  });

  it('gives a tab its most urgent pane', () => {
    expect(S.tabStatus([pane('a', 'idle'), pane('b', 'working'), undefined])).toBe('working');
    expect(S.tabStatus([pane('a', 'done'), pane('b', 'blocked'), pane('c', 'working')])).toBe('blocked');
    expect(S.tabStatus([pane('a', 'done', true), pane('b', 'working')])).toBe('working');
    expect(S.tabStatus([pane('a'), undefined])).toBeNull();
    expect(S.tabStatus([])).toBeNull();
  });

  it('finds panes that newly became blocked, error or done', () => {
    const prev = [pane('a', 'working', false, 't1'), pane('b', 'working', false, 't1'), pane('c', 'blocked', false, 't1'), pane('d', 'working'), pane('e', 'working')];
    const next = [
      pane('a', 'blocked', false, 't2'), // newly blocked
      pane('b', 'done', false, 't2'), // newly done
      pane('c', 'blocked', false, 't1'), // still blocked: nothing new
      pane('d', 'done', true, 't2'), // done but already seen: shows idle
      pane('e', 'error', false, 't2'), // newly error
      pane('f', 'blocked', false, 't2'), // new pane: not a transition
    ];
    expect(S.transitions(prev, next)).toEqual([
      { id: 'a', word: 'blocked', since: 't2' },
      { id: 'b', word: 'done', since: 't2' },
      { id: 'e', word: 'error', since: 't2' },
    ]);
  });

  it('counts a pane again when it left and came back between two lists', () => {
    expect(S.transitions([pane('a', 'blocked', false, 't1')], [pane('a', 'blocked', false, 't3')])).toEqual([{ id: 'a', word: 'blocked', since: 't3' }]);
    expect(S.transitions([pane('a', 'done', true, 't1')], [pane('a', 'done', false, 't1')])).toEqual([{ id: 'a', word: 'done', since: 't1' }]);
    expect(S.transitions([pane('a', 'done', false, 't1')], [pane('a', 'done', true, 't1')])).toEqual([]);
  });

  it('sorts most urgent first, then the most recent', () => {
    const list = [pane('idle', 'idle', false, '2026-01-01T00:00:05Z'), pane('none'), pane('old', 'blocked', false, '2026-01-01T00:00:01Z'), pane('new', 'error', false, '2026-01-01T00:00:09Z'), pane('w', 'working')];
    expect(list.sort(S.byUrgency).map((p) => p.id)).toEqual(['new', 'old', 'w', 'idle', 'none']);
  });

  it('says how long ago', () => {
    const t = Date.parse('2026-01-01T00:00:00Z');
    expect(S.ago(undefined, t)).toBe('');
    expect(S.ago('2026-01-01T00:00:00Z', t + 2000)).toBe('now');
    expect(S.ago('2026-01-01T00:00:00Z', t + 42000)).toBe('42s');
    expect(S.ago('2026-01-01T00:00:00Z', t + 5 * 60000)).toBe('5m');
    expect(S.ago('2026-01-01T00:00:00Z', t + 3 * 3600000)).toBe('3h');
    expect(S.ago('2026-01-01T00:00:00Z', t + 49 * 3600000)).toBe('2d');
  });
});
