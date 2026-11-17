// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

import { useEffect, useMemo, useRef, useState } from 'react';

export interface Action {
  id: string;
  label: string;
  hint?: string;
  group: string;
  run: () => void;
}

export function Palette({ actions, onClose }: { actions: Action[]; onClose: () => void }) {
  const [q, setQ] = useState('');
  const [i, setI] = useState(0);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => input.current?.focus(), []);
  const shown = useMemo(() => {
    const words = q.toLowerCase().split(/\s+/).filter(Boolean);
    return actions.filter((a) => words.every((w) => (a.label + ' ' + a.group).toLowerCase().includes(w)));
  }, [q, actions]);
  useEffect(() => setI(0), [q]);
  const run = (a?: Action) => {
    if (!a) return;
    onClose();
    a.run();
  };
  return (
    <div className="scrim" onMouseDown={onClose}>
      <div className="palette" onMouseDown={(e) => e.stopPropagation()} role="dialog" aria-label="Commands">
        <input
          ref={input}
          value={q}
          placeholder="Type a command…"
          spellCheck={false}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Escape') {
              e.preventDefault();
              onClose();
            } else if (e.key === 'ArrowDown') {
              e.preventDefault();
              setI((x) => Math.min(x + 1, shown.length - 1));
            } else if (e.key === 'ArrowUp') {
              e.preventDefault();
              setI((x) => Math.max(x - 1, 0));
            } else if (e.key === 'Enter') {
              e.preventDefault();
              run(shown[i]);
            }
            e.stopPropagation();
          }}
        />
        <ul>
          {shown.map((a, k) => (
            <li key={a.id} className={k === i ? 'is-active' : ''} onMouseEnter={() => setI(k)} onClick={() => run(a)}>
              <span className="pal-group">{a.group}</span>
              <span className="pal-label">{a.label}</span>
              {a.hint && <kbd>{a.hint}</kbd>}
            </li>
          ))}
          {shown.length === 0 && <li className="pal-empty">No command matches.</li>}
        </ul>
      </div>
    </div>
  );
}

export const keyHelp: [string, string][] = [
  ['Ctrl-b % or |', 'Split right'],
  ['Ctrl-b " or -', 'Split down'],
  ['Ctrl-b ← ↑ → ↓', 'Move between panes (or h j k l)'],
  ['Ctrl-b o', 'Next pane'],
  ['Ctrl-b z', 'Zoom the pane'],
  ['Ctrl-b x', 'Close the pane'],
  ['Ctrl-b c', 'New tab'],
  ['Ctrl-b n / p', 'Next / previous tab'],
  ['Ctrl-b 1…9', 'Go to tab'],
  ['Ctrl-b ,', 'Rename the tab'],
  ['Ctrl-b space', 'Tiled layout'],
  ['Ctrl-b m', 'Main + stack layout'],
  ['Ctrl-b + / _', 'Bigger / smaller text'],
  ['Ctrl-b a', 'Agents panel (show or hide)'],
  ['Ctrl-b :  or  ⌘K', 'Command palette'],
  ['Ctrl-b Ctrl-b', 'Send Ctrl-b to the pane'],
  ['Ctrl-b ?', 'This help'],
];

export function Help({ onClose }: { onClose: () => void }) {
  useEffect(() => {
    const k = (e: KeyboardEvent) => {
      e.preventDefault();
      e.stopPropagation();
      onClose();
    };
    window.addEventListener('keydown', k, true);
    return () => window.removeEventListener('keydown', k, true);
  }, [onClose]);
  return (
    <div className="scrim" onMouseDown={onClose}>
      <div className="help" onMouseDown={(e) => e.stopPropagation()}>
        <h2>Keys</h2>
        <p className="help-sub">
          Press the prefix, Ctrl-b, then a key — as in tmux. ·{' '}
          <a href="/notices.txt" target="_blank" rel="noreferrer" onMouseDown={(e) => e.stopPropagation()}>
            Licences
          </a>
        </p>
        <dl>
          {keyHelp.map(([k, v]) => (
            <div key={k}>
              <dt>
                <kbd>{k}</kbd>
              </dt>
              <dd>{v}</dd>
            </div>
          ))}
        </dl>
      </div>
    </div>
  );
}
