// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// The agents panel: every pane with an agent or a status, most urgent first,
// a pane's children under it; then the lost panes no tab holds.
import type { LostPane, PaneState, StatusWord } from './api';
import type { Permission } from './notify';
import { ago } from './status';
import * as I from './icons';

export interface AgentRow {
  key: string;
  /** id is the session; tabId and paneId are empty when no tab holds it. */
  id: string;
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
  depth: number;
  labels: [string, string][];
  /** asking: Allow and Deny are offered; askNote: what an answer came back with. */
  asking: boolean;
  askNote?: string;
}

interface Props {
  rows: AgentRow[];
  lost: LostPane[];
  lostError?: string;
  now: number;
  permission: Permission;
  notify: boolean;
  sound: boolean;
  onPick: (row: AgentRow) => void;
  onAnswer: (row: AgentRow, decision: 'allow' | 'deny') => void;
  onLost: (lp: LostPane, action: LostAction) => void;
  onNotify: (on: boolean) => void;
  onSound: (on: boolean) => void;
  onClose: () => void;
}

export function Agents({ rows, lost, lostError, now, permission, notify, sound, onPick, onAnswer, onLost, onNotify, onSound, onClose }: Props) {
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
        {rows.map((r) => {
          const indent = Math.min(r.depth, 4) * 14;
          return (
            <div
              key={r.key}
              role="button"
              tabIndex={0}
              className={`agent-row ${r.current ? 'is-current' : ''} ${r.depth ? 'is-child' : ''}`}
              style={indent ? { marginLeft: `${indent}px`, width: `calc(100% - ${indent}px)` } : undefined}
              onClick={() => onPick(r)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && e.target === e.currentTarget) onPick(r);
              }}
              title={r.msg || undefined}
            >
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
              {r.labels.length > 0 && (
                <span className="agent-labels">
                  {r.labels.map(([k, v]) => (
                    <span key={k} className="label-chip" title={`${k}: ${v}`}>
                      {v || k}
                    </span>
                  ))}
                </span>
              )}
              {r.msg && <span className="agent-msg">{r.msg}</span>}
              {(r.asking || r.askNote) && <AskButtons asking={r.asking} note={r.askNote} onAnswer={(d) => onAnswer(r, d)} />}
            </div>
          );
        })}
        {rows.length === 0 && <p className="agents-empty">No agents yet. An agent pane shows here, and so does any pane whose program reports its status.</p>}
        {lost.length > 0 && (
          <section className="agents-lost">
            <div className="agents-sub">Lost panes</div>
            {lost.map((lp) => (
              <div key={lp.id} className="lost-row">
                <span className="agent-top">
                  <span className="agent-title">{lp.title}</span>
                  <span className="grow" />
                  <span className="agent-ago" title={`lost ${new Date(lp.lostAt).toLocaleString()}`}>
                    {ago(lp.lostAt, now)}
                  </span>
                </span>
                <span className="agent-where" title={lp.cwd}>
                  {lp.profile} · {lp.cwd}
                </span>
                <LostActions lp={lp} onAction={(a) => onLost(lp, a)} compact />
              </div>
            ))}
          </section>
        )}
        {lostError && <p className="lost-error">{lostError}</p>}
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

/** AskButtons answer what a pane's agent waits for; note replaces them once answered. */
export function AskButtons({ asking, note, onAnswer }: { asking: boolean; note?: string; onAnswer: (d: 'allow' | 'deny') => void }) {
  if (note) return <span className="ask-note">{note}</span>;
  if (!asking) return null;
  return (
    <span className="ask" onMouseDown={(e) => e.stopPropagation()} onClick={(e) => e.stopPropagation()}>
      <button className="ask-allow" title="Allow what the agent asks" onClick={() => onAnswer('allow')}>
        Allow
      </button>
      <button className="ask-deny" title="Deny it" onClick={() => onAnswer('deny')}>
        Deny
      </button>
    </span>
  );
}

export type LostAction = 'resume' | 'fresh' | 'dismiss';

/** LostActions offer a lost pane: resume it, start its profile fresh, or let it go. */
export function LostActions({ lp, onAction, compact = false }: { lp: LostPane; onAction: (a: LostAction) => void; compact?: boolean }) {
  return (
    <span className={`lost-actions ${compact ? 'is-compact' : ''}`}>
      <button className="lost-primary" title={lp.resumable ? "Start it again, continuing the agent's session" : 'Start it again in its folder'} onClick={() => onAction('resume')}>
        <I.Restart /> Resume
      </button>
      <button title="Start the same profile in the same folder, as a new session" onClick={() => onAction('fresh')}>
        Start fresh
      </button>
      <button title="Forget it and leave an empty pane" onClick={() => onAction('dismiss')}>
        Dismiss
      </button>
      {lp.resumable && <span className="lost-note">Resume continues the agent's session.</span>}
    </span>
  );
}
