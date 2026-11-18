// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// The settings panel: the forge's settings (saved through /api/config, the
// same keys as la config), this browser's own (light/dark, notifications,
// sound, text size), the forge's shares, and what the forge is.
import { useEffect, useState, type ReactElement } from 'react';
import type { Config, ConfigPatch, ForgeApi, Profile, Session, Share } from './api';
import type { Permission } from './notify';
import type { Mode, ModePref } from './theme';
import * as K from './skins';
import * as G from './config';
import * as I from './icons';
import * as R from './rail';

export interface SettingsProps {
  api: ForgeApi;
  config: Config | null;
  skins: K.Skin[];
  sessions: Map<string, Session>;
  /** onChanged reloads what the forge's settings change elsewhere: the config, profiles and skins. */
  onChanged: () => Promise<unknown>;
  pref: ModePref;
  sysMode: Mode;
  onPref: (p: ModePref) => void;
  notify: boolean;
  sound: boolean;
  permission: Permission;
  onNotify: (on: boolean) => void;
  onSound: (on: boolean) => void;
  fontSize: number;
  onFontSize: (n: number) => void;
  now: number;
  /** spaces: where the spaces show now, and what the skin in use suggests. */
  spaces?: { now: R.Arrangement; suggested: R.Arrangement; browser: R.Arrangement | null };
  /** onSpaces sets where the spaces show, for the forge or for this browser ('' follows the next one down). */
  onSpaces?: (where: R.Arrangement | '', scope: 'forge' | 'browser') => Promise<unknown> | void;
  onClose: () => void;
  /** shares: a starting list, for tests and screenshots (else it is fetched). */
  shares?: Share[];
}

type Restart = { kind: 'idle' } | { kind: 'asking' } | { kind: 'busy' } | { kind: 'done' } | { kind: 'error'; reason: string };

const reason = (err: unknown) => (err as Error)?.message || 'That did not work.';

export function Settings(props: SettingsProps) {
  const { api, config, onChanged, onClose } = props;
  const [confirming, setConfirming] = useState<string | null>(null);
  const [form, setForm] = useState<G.ProfileForm | null>(null);
  const [restart, setRestart] = useState<Restart>({ kind: 'idle' });

  return (
    <aside
      className="themes settings"
      aria-label="Settings"
      onKeyDown={(e) => {
        if (e.key === 'Escape') {
          e.preventDefault();
          e.stopPropagation();
          if (confirming) setConfirming(null);
          else if (form) setForm(null);
          else if (restart.kind === 'asking') setRestart({ kind: 'idle' });
          else onClose();
        }
      }}
    >
      <header className="agents-head themes-head">
        <span className="agents-name">Settings</span>
        <span className="grow" />
        <button className="agents-x" title="Close (Esc)" onClick={onClose}>
          <I.SmallClose />
        </button>
      </header>
      <div className="themes-body settings-body">
        {config ? <ForgeSection {...props} config={config} /> : <p className="settings-note">Loading the forge's settings…</p>}
        {config && <ProfilesSection api={api} profiles={config.profiles} onChanged={onChanged} form={form} setForm={setForm} confirming={confirming} setConfirming={setConfirming} />}
        <BrowserSection {...props} />
        <SharesSection {...props} confirming={confirming} setConfirming={setConfirming} />
        <AboutSection {...props} restart={restart} setRestart={setRestart} />
      </div>
    </aside>
  );
}

/** useSave runs one save at a time and keeps its refusal to show inline. */
function useSave() {
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<{ key: string; reason: string } | null>(null);
  const run = async (key: string, what: () => Promise<unknown>) => {
    setBusy(key);
    setError(null);
    try {
      await what();
      return true;
    } catch (err) {
      setError({ key, reason: reason(err) });
      return false;
    } finally {
      setBusy(null);
    }
  };
  return { busy, error, run, clear: () => setError(null) };
}

const spaceChoices: { value: '' | R.Arrangement; label: string; hint: string }[] = [
  { value: 'top', label: 'Top', hint: 'tabs on the title line' },
  { value: 'rail', label: 'Rail', hint: 'a list at the left, with the crew' },
  { value: '', label: 'As the skin suggests', hint: '' },
];

function ForgeSection({ api, config, skins, onChanged, spaces, onSpaces }: SettingsProps & { config: Config }) {
  const [folder, setFolder] = useState(config.cwd);
  const save = useSave();
  useEffect(() => setFolder(config.cwd), [config.cwd]);
  const put = (key: string, patch: ConfigPatch) => save.run(key, () => api.setConfig(patch).then(onChanged));
  const older = config.skin === undefined;
  const err = (key: string) => (save.error?.key === key ? <span className="theme-error settings-error" role="alert">{save.error.reason}</span> : null);
  return (
    <section className="settings-section" aria-labelledby="settings-forge">
      <h3 className="agents-sub" id="settings-forge">
        Forge
      </h3>
      <p className="settings-note">Saved in the forge, for every browser and for la config.</p>
      <div className="settings-field">
        <label htmlFor="settings-cwd">New panes start in</label>
        <form
          className="themes-form"
          onSubmit={(e) => {
            e.preventDefault();
            put('cwd', { cwd: folder.trim() });
          }}
        >
          <input id="settings-cwd" value={folder} spellCheck={false} placeholder="~/projects" onChange={(e) => (setFolder(e.target.value), save.clear())} />
          <button type="submit" className="themes-install-btn" disabled={save.busy === 'cwd' || !folder.trim() || (config.cwdSaved !== false && G.sameFolder(folder, config.cwd))}>
            Save
          </button>
        </form>
        <span className="settings-hint">
          {config.cwdSaved ? (
            <>
              Saved.{' '}
              <button className="settings-link" disabled={save.busy === 'cwd'} onClick={() => put('cwd', { cwd: '' })}>
                Use where la up runs
              </button>
            </>
          ) : (
            'Where la up ran; save it to keep it after a restart.'
          )}
        </span>
        {err('cwd')}
      </div>
      {!older && (
        <>
          <div className="settings-field settings-inline">
            <label htmlFor="settings-skin">Default skin</label>
            <select id="settings-skin" value={config.skin} disabled={save.busy === 'skin'} onChange={(e) => put('skin', { skin: e.target.value })}>
              {(skins.length ? skins : [K.classic]).map((k) => (
                <option key={k.name} value={k.name}>
                  {k.label}
                </option>
              ))}
            </select>
          </div>
          {err('skin')}
          <label className="settings-check">
            <input type="checkbox" checked={config.scenes !== false} disabled={save.busy === 'scenes'} onChange={(e) => put('scenes', { scenes: e.target.checked })} />
            Animated scenes in this forge
          </label>
          {err('scenes')}
          {onSpaces && (
            <fieldset className="settings-field settings-radios" role="radiogroup" aria-labelledby="settings-spaces">
              <legend id="settings-spaces">Spaces</legend>
              {spaceChoices.map((ch) => (
                <label key={ch.value || 'skin'} className="settings-check settings-radio">
                  <input
                    type="radio"
                    name="settings-spaces"
                    value={ch.value || 'skin'}
                    checked={(config.spaces ?? '') === ch.value}
                    disabled={save.busy === 'spaces'}
                    onChange={() => save.run('spaces', async () => onSpaces(ch.value, 'forge'))}
                  />
                  {ch.label}
                  <span className="settings-hint">{ch.value ? ch.hint : spaces ? `now ${spaces.suggested}` : ''}</span>
                </label>
              ))}
              {spaces?.browser && <span className="settings-hint">This browser shows them {spaces.browser === 'top' ? 'on top' : 'in the rail'} (below).</span>}
            </fieldset>
          )}
          {err('spaces')}
        </>
      )}
      {older && <p className="settings-note">This forge is older than the Settings panel: update LaForge to change its skin, scenes and profiles here.</p>}
    </section>
  );
}

function ProfilesSection({
  api,
  profiles,
  onChanged,
  form,
  setForm,
  confirming,
  setConfirming,
}: {
  api: ForgeApi;
  profiles?: Profile[];
  onChanged: () => Promise<unknown>;
  form: G.ProfileForm | null;
  setForm: (f: G.ProfileForm | null) => void;
  confirming: string | null;
  setConfirming: (k: string | null) => void;
}) {
  const save = useSave();
  const [warning, setWarning] = useState<string | null>(null);
  if (!profiles) return null;
  const submit = async () => {
    if (!form) return;
    const checked = G.profileOf(form);
    if (checked.error !== undefined) {
      save.run('form', () => Promise.reject(new Error(checked.error)));
      return;
    }
    let saved: Profile | undefined;
    const ok = await save.run('form', async () => {
      saved = await api.saveProfile(checked.profile);
      await onChanged();
    });
    if (ok) {
      setForm(null);
      setWarning(saved && !saved.available ? `${saved.label}: ${saved.reason ?? 'its command is not installed.'}` : null);
    }
  };
  const editingName = form?.editing ? form.name : null;
  return (
    <section className="settings-section" aria-labelledby="settings-profiles">
      <h3 className="agents-sub" id="settings-profiles">
        Profiles <span className="themes-count">{profiles.length}</span>
      </h3>
      <p className="settings-note">The shells and agents a pane can start.</p>
      <div className="themes-list settings-profiles" role="list">
        {profiles.map((p) => {
          const from = G.origin(p);
          const key = `profile:${p.name}`;
          return (
            <div key={p.name} role="listitem" className="theme-row settings-profile" data-profile={p.name}>
              <div className="settings-profile-top">
                <span className="theme-label">{p.label}</span>
                <span className={`theme-tag ${from === 'built in' ? 'is-builtin' : 'is-installed'}`}>{from === 'changed' ? 'built in, changed' : from}</span>
                {p.agent && <span className="theme-tag">agent</span>}
                {!p.available && <span className="theme-tag settings-missing">not installed</span>}
                <span className="grow" />
                {confirming !== key && editingName !== p.name && (
                  <span className="settings-row-actions">
                    <button className="settings-btn" onClick={() => setForm(G.formOf(p))}>
                      Edit
                    </button>
                    {p.custom && (
                      <button className="settings-btn" onClick={() => setConfirming(key)}>
                        {from === 'changed' ? 'Restore' : 'Remove'}
                      </button>
                    )}
                  </span>
                )}
              </div>
              <code className="settings-cmd" title={G.commandLine(p)}>
                {G.commandLine(p)}
              </code>
              {confirming === key && (
                <span className="theme-actions theme-confirm settings-confirm">
                  <span className="theme-confirm-text">{from === 'changed' ? `Give ${p.label} back its built-in settings?` : `Remove ${p.label}?`}</span>
                  <button
                    className="theme-danger"
                    disabled={save.busy === key}
                    onClick={() =>
                      save.run(key, async () => {
                        await api.removeProfile(p.name);
                        setConfirming(null);
                        await onChanged();
                      })
                    }
                  >
                    {from === 'changed' ? 'Restore' : 'Remove'}
                  </button>
                  <button onClick={() => setConfirming(null)}>Cancel</button>
                </span>
              )}
              {save.error?.key === key && <span className="theme-error settings-error">{save.error.reason}</span>}
              {editingName === p.name && form && <ProfileEditor form={form} setForm={setForm} busy={save.busy === 'form'} error={save.error?.key === 'form' ? save.error.reason : null} onSubmit={submit} />}
            </div>
          );
        })}
      </div>
      {warning && (
        <p className="settings-warning" role="status">
          {warning}
        </p>
      )}
      {form && !form.editing ? (
        <ProfileEditor form={form} setForm={setForm} busy={save.busy === 'form'} error={save.error?.key === 'form' ? save.error.reason : null} onSubmit={submit} />
      ) : (
        !form && (
          <button className="settings-btn settings-add" onClick={() => (setForm(G.emptyForm()), save.clear(), setWarning(null))}>
            <I.Plus /> Add a profile
          </button>
        )
      )}
    </section>
  );
}

function ProfileEditor({ form, setForm, busy, error, onSubmit }: { form: G.ProfileForm; setForm: (f: G.ProfileForm | null) => void; busy: boolean; error: string | null; onSubmit: () => void }) {
  const set = (patch: Partial<G.ProfileForm>) => setForm({ ...form, ...patch });
  return (
    <form
      className="settings-editor"
      aria-label={form.editing ? `Edit ${form.name}` : 'Add a profile'}
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit();
      }}
    >
      <label>
        Name
        <input value={form.name} disabled={form.editing} spellCheck={false} placeholder="aider" autoFocus={!form.editing} onChange={(e) => set({ name: e.target.value })} />
      </label>
      <label>
        Label
        <input value={form.label} placeholder="Aider" autoFocus={form.editing} onChange={(e) => set({ label: e.target.value })} />
      </label>
      <label>
        Command
        <input value={form.command} spellCheck={false} placeholder="aider" onChange={(e) => set({ command: e.target.value })} />
      </label>
      <label>
        Arguments
        <input value={form.args} spellCheck={false} placeholder="--model sonnet" onChange={(e) => set({ args: e.target.value })} />
      </label>
      <label>
        Environment <span className="settings-hint">one NAME=value a line</span>
        <textarea value={form.env} rows={2} spellCheck={false} onChange={(e) => set({ env: e.target.value })} />
      </label>
      <label className="settings-check">
        <input type="checkbox" checked={form.agent} onChange={(e) => set({ agent: e.target.checked })} />
        An agent (its pane becomes a shell when it exits)
      </label>
      {error && (
        <span className="theme-error settings-error" role="alert">
          {error}
        </span>
      )}
      <span className="settings-editor-actions">
        <button type="submit" className="themes-install-btn" disabled={busy}>
          {form.editing ? 'Save' : 'Add'}
        </button>
        <button type="button" className="settings-btn" onClick={() => setForm(null)}>
          Cancel
        </button>
      </span>
    </form>
  );
}

const prefs: { value: ModePref; label: string; icon: () => ReactElement }[] = [
  { value: 'system', label: 'System', icon: I.Monitor },
  { value: 'light', label: 'Light', icon: I.Sun },
  { value: 'dark', label: 'Dark', icon: I.Moon },
];

function BrowserSection({ pref, sysMode, onPref, notify, sound, permission, onNotify, onSound, fontSize, onFontSize, spaces, onSpaces }: SettingsProps) {
  return (
    <section className="settings-section" aria-labelledby="settings-browser">
      <h3 className="agents-sub" id="settings-browser">
        This browser
      </h3>
      <p className="settings-note">Kept in this browser only.</p>
      <div className="settings-field settings-inline">
        <span id="settings-mode">Light/dark</span>
        <span className="settings-seg" role="group" aria-labelledby="settings-mode">
          {prefs.map((p) => (
            <button key={p.value} className={`settings-seg-btn ${pref === p.value ? 'is-on' : ''}`} aria-pressed={pref === p.value} title={p.value === 'system' ? `System (${sysMode} now)` : p.label} onClick={() => onPref(p.value)}>
              <p.icon /> {p.label}
            </button>
          ))}
        </span>
      </div>
      {spaces && onSpaces && (
        <div className="settings-field settings-inline">
          <span id="settings-browser-spaces">Spaces here</span>
          <span className="settings-seg" role="group" aria-labelledby="settings-browser-spaces">
            {(
              [
                [null, 'As the forge'],
                ['top', 'Top'],
                ['rail', 'Rail'],
              ] as const
            ).map(([v, label]) => (
              <button key={label} className={`settings-seg-btn ${spaces.browser === v ? 'is-on' : ''}`} aria-pressed={spaces.browser === v} onClick={() => onSpaces(v ?? '', 'browser')}>
                {label}
              </button>
            ))}
          </span>
        </div>
      )}
      <label className="settings-check">
        <input type="checkbox" checked={notify} disabled={permission === 'unsupported'} onChange={(e) => onNotify(e.target.checked)} />
        Notify me when an agent needs me or finishes
      </label>
      {permission === 'denied' && <span className="settings-hint settings-indent">This browser blocks notifications for the forge; allow them in its site settings.</span>}
      {permission === 'unsupported' && <span className="settings-hint settings-indent">This browser cannot show notifications here.</span>}
      <label className="settings-check">
        <input type="checkbox" checked={sound} onChange={(e) => onSound(e.target.checked)} />
        Play a sound too
      </label>
      <div className="settings-field settings-inline">
        <span id="settings-font">Terminal text</span>
        <span className="settings-stepper" role="group" aria-labelledby="settings-font">
          <button className="settings-btn" aria-label="Smaller text" disabled={fontSize <= G.fontMin} onClick={() => onFontSize(fontSize - 1)}>
            −
          </button>
          <span className="settings-font-size" aria-live="polite">
            {fontSize}px
          </span>
          <button className="settings-btn" aria-label="Bigger text" disabled={fontSize >= G.fontMax} onClick={() => onFontSize(fontSize + 1)}>
            +
          </button>
          {fontSize !== G.fontDefault && (
            <button className="settings-link" onClick={() => onFontSize(G.fontDefault)}>
              Reset
            </button>
          )}
        </span>
      </div>
    </section>
  );
}

function SharesSection({ api, sessions, now, shares: initial, confirming, setConfirming }: SettingsProps & { confirming: string | null; setConfirming: (k: string | null) => void }) {
  const [shares, setShares] = useState<Share[] | null>(initial ?? null);
  const [loadError, setLoadError] = useState('');
  const save = useSave();
  useEffect(() => {
    if (initial) return;
    let stop = false;
    api
      .shares()
      .then((s) => !stop && setShares(s))
      .catch((err) => !stop && setLoadError(reason(err)));
    return () => {
      stop = true;
    };
  }, [api, initial]);
  return (
    <section className="settings-section" aria-labelledby="settings-shares">
      <h3 className="agents-sub" id="settings-shares">
        Shares {shares && shares.length > 0 && <span className="themes-count">{shares.length}</span>}
      </h3>
      {loadError && <span className="theme-error settings-error">{loadError}</span>}
      {shares && shares.length === 0 && (
        <p className="settings-note">
          No read-only links. Make one with <code>la share PANE</code>.
        </p>
      )}
      {shares && shares.length > 0 && (
        <div className="themes-list" role="list">
          {shares.map((sh) => {
            const key = `share:${sh.token}`;
            const title = sessions.get(sh.pane)?.title || sh.pane;
            return (
              <div key={sh.token} role="listitem" className="theme-row settings-share" data-share={sh.token}>
                <div className="settings-profile-top">
                  <span className="theme-label">{title}</span>
                  <span className="theme-tag">{G.timeLeft(sh.expires, now)} left</span>
                  <span className="grow" />
                  {confirming !== key && (
                    <button className="settings-btn" onClick={() => setConfirming(key)}>
                      Revoke
                    </button>
                  )}
                </div>
                <code className="settings-cmd">{sh.token.slice(0, 8)}…</code>
                {confirming === key && (
                  <span className="theme-actions theme-confirm settings-confirm">
                    <span className="theme-confirm-text">Revoke this link? Whoever has it loses the pane.</span>
                    <button
                      className="theme-danger"
                      disabled={save.busy === key}
                      onClick={() =>
                        save.run(key, async () => {
                          await api.revokeShare(sh.token);
                          setConfirming(null);
                          setShares((list) => (list ?? []).filter((x) => x.token !== sh.token));
                        })
                      }
                    >
                      Revoke
                    </button>
                    <button onClick={() => setConfirming(null)}>Cancel</button>
                  </span>
                )}
                {save.error?.key === key && <span className="theme-error settings-error">{save.error.reason}</span>}
              </div>
            );
          })}
        </div>
      )}
    </section>
  );
}

function AboutSection({ api, config, onChanged, restart, setRestart }: SettingsProps & { restart: Restart; setRestart: (r: Restart) => void }) {
  const doRestart = async () => {
    setRestart({ kind: 'busy' });
    try {
      const before = await api.health().catch(() => null);
      await api.restart();
      const deadline = Date.now() + 20000;
      while (Date.now() < deadline) {
        await new Promise((ok) => setTimeout(ok, 500));
        const h = await api.health().catch(() => null);
        if (h && (!before || h.startedAt !== before.startedAt)) {
          await onChanged().catch(() => undefined);
          setRestart({ kind: 'done' });
          return;
        }
      }
      setRestart({ kind: 'error', reason: 'The forge did not come back; run la up (or la status) on its machine.' });
    } catch (err) {
      setRestart({ kind: 'error', reason: reason(err) });
    }
  };
  const rows: [string, string | undefined][] = [
    ['Version', config?.version],
    ['Host', config ? `${config.user ? config.user + '@' : ''}${config.host}` : undefined],
    ['Port', config?.port ? String(config.port) : undefined],
    ['Home', config?.home],
  ];
  return (
    <section className="settings-section" aria-labelledby="settings-about">
      <h3 className="agents-sub" id="settings-about">
        About
      </h3>
      <dl className="settings-about">
        {rows
          .filter(([, v]) => v)
          .map(([k, v]) => (
            <div key={k}>
              <dt>{k}</dt>
              <dd title={v}>{v}</dd>
            </div>
          ))}
      </dl>
      {restart.kind === 'asking' ? (
        <span className="theme-actions theme-confirm settings-confirm">
          <span className="theme-confirm-text">Restart the forge? Panes it holds keep running; browsers reconnect.</span>
          <button className="theme-danger" onClick={doRestart}>
            Restart
          </button>
          <button onClick={() => setRestart({ kind: 'idle' })}>Cancel</button>
        </span>
      ) : (
        <span className="settings-about-actions">
          <button className="settings-btn" disabled={restart.kind === 'busy'} onClick={() => setRestart({ kind: 'asking' })}>
            <I.Restart /> {restart.kind === 'busy' ? 'Restarting…' : 'Restart the forge'}
          </button>
          <a className="settings-link" href={`${api.base}/notices.txt`} target="_blank" rel="noreferrer">
            Third-party notices
          </a>
        </span>
      )}
      {restart.kind === 'done' && (
        <p className="settings-note" role="status">
          Restarted.
        </p>
      )}
      {restart.kind === 'error' && (
        <span className="theme-error settings-error" role="alert">
          {restart.reason}
        </span>
      )}
    </section>
  );
}
