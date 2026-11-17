// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// The agents panel: every pane with an agent or a status, most urgent first.
import type { PaneState, StatusWord } from './api';
import type { Permission } from './notify';
import { ago } from './status';
import * as I from './icons';

export interface AgentRow {
  key: string;
  tabId: string;
  paneId: string;
  tab: string;
  title: string;
  app: string;
  word: StatusWord | null;
  state: PaneState;
  msg: string;
  since?: string;
  current: boolean;
}

interface Props {
  rows: AgentRow[];
  now: number;
  permission: Permission;
  notify: boolean;
  sound: boolean;
  onPick: (row: AgentRow) => void;
  onNotify: (on: boolean) => void;
  onSound: (on: boolean) => void;
  onClose: () => void;
}

export function Agents({ rows, now, permission, notify, sound, onPick, onNotify, onSound, onClose }: Props) {
  return (
    <aside className="agents" aria-label="Agents">
      <header className="agents-head">
        <span className="agents-name">Agents</span>
        {rows.length > 0 && <span className="tab-count">{rows.length}</span>}
        <span className="grow" />
        <button className="agents-x" title="Hide (^b a)" onClick={onClose}>
          <I.SmallClose />
        </button>
      </header>
      <div className="agents-list">
        {rows.map((r) => (
          <button key={r.key} className={`agent-row ${r.current ? 'is-current' : ''}`} onClick={() => onPick(r)} title={r.msg || undefined}>
            <span className="agent-top">
              {r.word ? <Badge word={r.word} /> : <span className={`dot dot-${r.state}`} title={r.state} />}
              <span className="agent-title">{r.title}</span>
              <span className="grow" />
              <span className="agent-ago">{ago(r.since, now)}</span>
            </span>
            <span className="agent-where">
              {r.tab}
              {r.app ? ` · ${r.app}` : ''}
            </span>
            {r.msg && <span className="agent-msg">{r.msg}</span>}
          </button>
        ))}
        {rows.length === 0 && <p className="agents-empty">No agents yet. An agent pane shows here, and so does any pane whose program reports its status.</p>}
      </div>
      <footer className="agents-foot">
        {permission === 'unsupported' ? (
          <span className="agents-note">Notifications aren't available on this page.</span>
        ) : permission === 'denied' ? (
          <span className="agents-note">Notifications are blocked for this page in the browser's settings.</span>
        ) : notify && permission === 'granted' ? (
          <label className="agents-toggle">
            <input type="checkbox" checked onChange={() => onNotify(false)} />
            Notifications
          </label>
        ) : (
          <button className="agents-on" onClick={() => onNotify(true)}>
            Turn on notifications
          </button>
        )}
        <label className="agents-toggle">
          <input type="checkbox" checked={sound} onChange={(e) => onSound(e.target.checked)} />
          Sound
        </label>
      </footer>
    </aside>
  );
}

export function Badge({ word, progress }: { word: StatusWord; progress?: number }) {
  return (
    <span className={`badge badge-${word}`} title={word === 'done' ? 'done since you last looked' : word}>
      {word}
      {progress !== undefined ? ` ${progress}%` : ''}
    </span>
  );
}
