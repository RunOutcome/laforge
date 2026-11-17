// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// The themes panel: every skin the forge has, with its light and dark
// colours; pick one for this browser, make one the forge's default, remove
// an installed one, get more from the forge's collections, or install from
// a folder or a GitHub repo.
import { useState } from 'react';
import type { CollectionsApi } from './collections';
import { GetMore } from './GetMore';
import * as K from './skins';
import * as I from './icons';

interface Props {
  list: K.SkinList | null;
  active: string;
  onPick: (name: string) => void;
  onDefault: (name: string) => Promise<unknown>;
  onRemove: (name: string) => Promise<unknown>;
  onInstall: (source: string) => Promise<K.InstallResult>;
  onClose: () => void;
  /** collections: the forge's collection routes, for "Get more"; left out, the section is not drawn. */
  collections?: CollectionsApi;
  /** onInstalled refreshes the installed list after "Get more" installs. */
  onInstalled?: () => Promise<unknown> | unknown;
  /** moreInitial: a starting state for "Get more", for tests and screenshots. */
  moreInitial?: Parameters<typeof GetMore>[0]['initial'];
}

export function Themes({ list, active, onPick, onDefault, onRemove, onInstall, onClose, collections, onInstalled, moreInitial }: Props) {
  const [confirming, setConfirming] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [rowError, setRowError] = useState<{ name: string; reason: string } | null>(null);
  const [source, setSource] = useState('');
  const [install, setInstall] = useState<K.InstallState>({ kind: 'idle' });
  const skins = list?.skins ?? [K.classic];

  const run = async (name: string, what: () => Promise<unknown>) => {
    setBusy(name);
    setRowError(null);
    try {
      await what();
    } catch (err) {
      setRowError({ name, reason: (err as Error).message || 'That did not work.' });
    } finally {
      setBusy(null);
    }
  };
  const doInstall = async () => {
    const s = source.trim();
    if (!s || install.kind === 'busy') return;
    setInstall({ kind: 'busy', source: s });
    const next = await K.installState(onInstall, s);
    setInstall(next);
    if (next.kind === 'done' && next.result.installed.length) setSource('');
  };

  return (
    <aside
      className="themes"
      aria-label="Themes"
      onKeyDown={(e) => {
        if (e.key === 'Escape') {
          e.preventDefault();
          e.stopPropagation();
          if (confirming) setConfirming(null);
          else onClose();
        }
      }}
    >
      <header className="agents-head themes-head">
        <span className="agents-name">Themes</span>
        <span className="themes-count">{skins.length}</span>
        <span className="grow" />
        <button className="agents-x" title="Close (Esc)" onClick={onClose}>
          <I.SmallClose />
        </button>
      </header>
      <div className="themes-body">
        <div className="themes-list" role="list">
          {skins.map((k) => {
            const sw = K.swatches(k);
            const isActive = k.name === active;
            const isDefault = k.name === (list?.default ?? 'classic');
            return (
              <div key={k.name} role="listitem" className={`theme-row ${isActive ? 'is-active' : ''}`} data-skin-name={k.name}>
                <button className="theme-pick" title={`Use ${k.label} in this browser`} aria-pressed={isActive} onClick={() => onPick(k.name)}>
                  <span className="theme-strips" aria-hidden>
                    <Strip s={sw.light} />
                    <Strip s={sw.dark} />
                  </span>
                  <span className="theme-text">
                    <span className="theme-top">
                      <span className="theme-label">{k.label}</span>
                      <span className={`theme-tag ${k.builtin ? 'is-builtin' : 'is-installed'}`}>{k.builtin ? 'built in' : 'installed'}</span>
                      {isDefault && <span className="theme-tag is-default">forge default</span>}
                    </span>
                    <span className="theme-meta">
                      {[k.author, K.layoutOf(k)].filter(Boolean).join(' · ')}
                      {k.source ? <span className="theme-source" title={k.source}>{` · ${k.source}`}</span> : null}
                    </span>
                  </span>
                  <span className="theme-check">{isActive && <I.Check />}</span>
                </button>
                {confirming === k.name ? (
                  <span className="theme-actions theme-confirm">
                    <span className="theme-confirm-text">Remove {k.label} from the forge?</span>
                    <button
                      className="theme-danger"
                      disabled={busy === k.name}
                      onClick={() =>
                        run(k.name, async () => {
                          await onRemove(k.name);
                          setConfirming(null);
                        })
                      }
                    >
                      Remove
                    </button>
                    <button onClick={() => setConfirming(null)}>Cancel</button>
                  </span>
                ) : (
                  <span className="theme-actions">
                    {!isDefault && (
                      <button disabled={busy === k.name} onClick={() => run(k.name, () => onDefault(k.name))}>
                        Make forge default
                      </button>
                    )}
                    {!k.builtin && (
                      <button className="theme-remove" disabled={busy === k.name} onClick={() => setConfirming(k.name)}>
                        Remove
                      </button>
                    )}
                  </span>
                )}
                {rowError?.name === k.name && <span className="theme-error">{rowError.reason}</span>}
              </div>
            );
          })}
        </div>
        {collections && <GetMore api={collections} installedNames={skins.map((k) => k.name)} active={active} onPick={onPick} onInstalled={onInstalled ?? (() => undefined)} initial={moreInitial} />}
        <section className="themes-install">
          <label className="agents-sub" htmlFor="themes-source">
            Install from a link
          </label>
          <p className="themes-help themes-lead">From a folder path or GitHub repo (owner/repo, a github.com URL).</p>
          <form
            className="themes-form"
            onSubmit={(e) => {
              e.preventDefault();
              doInstall();
            }}
          >
            <input value={source} spellCheck={false} id="themes-source" placeholder="~/my-skin or owner/repo" onChange={(e) => setSource(e.target.value)} />
            <button type="submit" className="themes-install-btn" disabled={!source.trim() || install.kind === 'busy'}>
              Install
            </button>
          </form>
          <p className="themes-help">A repo or folder may hold one skin (a skin.json at its top), or many in subfolders or under skins/.</p>
          <InstallStatus state={install} />
        </section>
      </div>
    </aside>
  );
}

function Strip({ s }: { s: K.Swatch }) {
  return (
    <span className="theme-strip" style={{ background: s.background }}>
      <span style={{ background: s.foreground }} />
      <span style={{ background: s.accent }} />
    </span>
  );
}

/** InstallStatus shows how an install went: a spinner, then each skin installed and each failure with its reason. */
export function InstallStatus({ state }: { state: K.InstallState }) {
  switch (state.kind) {
    case 'idle':
      return null;
    case 'busy':
      return (
        <p className="install-status is-busy" role="status">
          <span className="spinner" aria-hidden /> Installing from {state.source}…
        </p>
      );
    case 'unsupported':
      return (
        <p className="install-status is-error" role="status">
          This forge can't install themes; update LaForge.
        </p>
      );
    case 'error':
      return (
        <p className="install-status is-error" role="status">
          {state.reason}
        </p>
      );
    case 'done': {
      const { installed, failed } = state.result;
      return (
        <div className="install-status" role="status">
          {installed.length === 0 && failed.length === 0 && <p>No skins were found there.</p>}
          {installed.length > 0 && (
            <ul className="install-ok">
              {installed.map((k) => (
                <li key={k.name}>
                  <I.Check /> Installed {k.label}
                </li>
              ))}
            </ul>
          )}
          {failed.length > 0 && (
            <ul className="install-failed">
              {failed.map((f) => (
                <li key={f.path}>
                  <code>{f.path}</code>: {f.reason}
                </li>
              ))}
            </ul>
          )}
        </div>
      );
    }
  }
}
