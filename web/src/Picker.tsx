// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// Picker is an empty pane: choose what runs in it.
import { useEffect, useRef, useState } from 'react';
import type { Profile } from './api';

interface Props {
  profiles: Profile[];
  cwd: string;
  focused: boolean;
  note?: string;
  error?: string;
  onStart: (profile: string, cwd: string) => void;
  onCancel?: () => void;
  onFocus: () => void;
}

export function monogram(p: { name: string; label: string }): string {
  if (p.name === 'shell') return '>_';
  return p.label.slice(0, 2);
}

export function Picker({ profiles, cwd, focused, note, error, onStart, onCancel, onFocus }: Props) {
  const box = useRef<HTMLDivElement>(null);
  const [folder, setFolder] = useState(cwd);
  const [active, setActive] = useState(0);
  const [editing, setEditing] = useState(false);
  useEffect(() => setFolder(cwd), [cwd]);
  useEffect(() => {
    if (focused && !editing) box.current?.focus();
  }, [focused, editing]);

  const usable = profiles;
  const choose = (i: number) => {
    const p = usable[i];
    if (p?.available) onStart(p.name, folder);
  };

  return (
    <div
      className="picker"
      ref={box}
      tabIndex={0}
      onFocus={onFocus}
      onKeyDown={(e) => {
        if (editing) return;
        if (e.ctrlKey || e.metaKey || e.altKey) return;
        if (/^[1-9]$/.test(e.key)) {
          e.preventDefault();
          choose(Number(e.key) - 1);
        } else if (e.key === 'Enter') {
          e.preventDefault();
          choose(active);
        } else if (e.key === 'ArrowRight' || e.key === 'ArrowDown' || e.key === 'Tab') {
          e.preventDefault();
          setActive((a) => (a + 1) % usable.length);
        } else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
          e.preventDefault();
          setActive((a) => (a - 1 + usable.length) % usable.length);
        } else if (e.key === 'Escape' && onCancel) {
          e.preventDefault();
          onCancel();
        }
      }}
    >
      <div className="picker-inner">
        <div className="picker-head">
          <h2>Start something here</h2>
          {note && <p className="picker-note">{note}</p>}
        </div>
        <div className="picker-grid">
          {usable.map((p, i) => (
            <button
              key={p.name}
              className={`profile ${p.agent ? 'is-agent' : 'is-shell'} ${i === active ? 'is-active' : ''}`}
              disabled={!p.available}
              title={p.available ? `${p.command}` : p.reason}
              onMouseEnter={() => setActive(i)}
              onClick={() => choose(i)}
            >
              <span className="profile-mono">{monogram(p)}</span>
              <span className="profile-text">
                <span className="profile-label">{p.label}</span>
                <span className="profile-desc">{p.available ? p.description : 'Not installed'}</span>
              </span>
              {i < 9 && <kbd>{i + 1}</kbd>}
            </button>
          ))}
        </div>
        <label className="picker-cwd">
          <span>in</span>
          <input
            value={folder}
            spellCheck={false}
            onFocus={() => setEditing(true)}
            onBlur={() => setEditing(false)}
            onChange={(e) => setFolder(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                choose(active);
              } else if (e.key === 'Escape') {
                (e.target as HTMLInputElement).blur();
              }
            }}
          />
        </label>
        {error && <p className="picker-error">{error}</p>}
        <p className="picker-hint">
          <kbd>1</kbd>–<kbd>9</kbd> choose · <kbd>Enter</kbd> start{onCancel ? <> · <kbd>Esc</kbd> close pane</> : null}
        </p>
      </div>
    </div>
  );
}
