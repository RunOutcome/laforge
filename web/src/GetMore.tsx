// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// "Get more": the theme panel's browser of skin collections. Pick a
// collection (official, library, then your own), see the skins it offers,
// install one or all of them, use one once it is installed, and add or
// remove your own collections (a GitHub link or a folder).
import { useCallback, useEffect, useState } from 'react';
import * as C from './collections';
import * as I from './icons';

interface Props {
  api: C.CollectionsApi;
  /** installedNames: the skins the forge has now, so an installed offer can be used. */
  installedNames: string[];
  active: string;
  onPick: (name: string) => void;
  /** onInstalled refreshes the installed list (GET /api/skins). */
  onInstalled: () => Promise<unknown> | unknown;
  /** initial: a starting state, for tests and screenshots; the section loads from the forge when it is left out. */
  initial?: { collections: C.CollectionsState; selected?: string; offers?: Record<string, C.OffersState> };
}

type AddState = { kind: 'idle' } | { kind: 'busy' } | { kind: 'error'; reason: string };

export function GetMore({ api, installedNames, active, onPick, onInstalled, initial }: Props) {
  const [cols, setCols] = useState<C.CollectionsState>(initial?.collections ?? { kind: 'loading' });
  const [selected, setSelected] = useState<string | null>(initial?.selected ?? null);
  const [offers, setOffers] = useState<Record<string, C.OffersState>>(initial?.offers ?? {});
  const [busy, setBusy] = useState<Set<string>>(new Set());
  const [failed, setFailed] = useState<Record<string, string>>({});
  const [note, setNote] = useState<{ kind: 'ok' | 'error'; text: string } | null>(null);
  const [source, setSource] = useState('');
  const [add, setAdd] = useState<AddState>({ kind: 'idle' });
  const [confirming, setConfirming] = useState<string | null>(null);
  const [removeError, setRemoveError] = useState<string | null>(null);
  const [removing, setRemoving] = useState(false);

  const fetchOffers = useCallback(
    async (id: string, refresh = false) => {
      setOffers((o) => ({ ...o, [id]: { kind: 'loading' } }));
      const next = await C.loadOffers(api, id, refresh);
      setOffers((o) => ({ ...o, [id]: next }));
    },
    [api],
  );

  const select = useCallback(
    (id: string) => {
      setSelected(id);
      setFailed({});
      setNote(null);
      setConfirming(null);
      setRemoveError(null);
      fetchOffers(id);
    },
    [fetchOffers],
  );

  const reloadCollections = useCallback(async () => {
    setCols({ kind: 'loading' });
    const next = await C.loadCollections(api);
    setCols(next);
    return next;
  }, [api]);

  useEffect(() => {
    if (initial) return;
    let stop = false;
    C.loadCollections(api).then((next) => {
      if (stop) return;
      setCols(next);
      if (next.kind === 'ready' && next.list.length) {
        setSelected(next.list[0].id);
        fetchOffers(next.list[0].id);
      }
    });
    return () => {
      stop = true;
    };
  }, [api, fetchOffers, initial]);

  const install = async (id: string, names: string[]) => {
    const state = offers[id];
    if (state?.kind !== 'ready' || names.length === 0) return;
    setBusy((b) => new Set([...b, ...names]));
    setNote(null);
    setFailed((f) => {
      const g = { ...f };
      for (const n of names) delete g[n];
      return g;
    });
    const r = await C.installOffers(api, id, state.offers, names, onInstalled);
    setOffers((o) => ({ ...o, [id]: { kind: 'ready', offers: r.offers } }));
    setFailed((f) => ({ ...f, ...r.failed }));
    setBusy((b) => new Set([...b].filter((n) => !names.includes(n))));
    if (r.error) setNote({ kind: 'error', text: r.error });
    else if (names.length > 1) {
      const nf = Object.keys(r.failed).length;
      setNote({ kind: nf ? 'error' : 'ok', text: `Installed ${r.installed.length} of ${names.length}${nf ? `; ${nf} failed` : ''}.` });
    }
  };

  const doAdd = async () => {
    const s = source.trim();
    if (!s || add.kind === 'busy') return;
    setAdd({ kind: 'busy' });
    const r = await C.addCollection(api, s);
    if (r.kind === 'error') {
      setAdd(r);
      return;
    }
    setAdd({ kind: 'idle' });
    setSource('');
    const next = await reloadCollections();
    const list = next.kind === 'ready' ? next.list : [];
    if (next.kind === 'ready' && !list.some((c) => c.id === r.collection.id)) setCols({ kind: 'ready', list: [...list, r.collection] });
    select(r.collection.id);
  };

  const doRemove = async (id: string) => {
    setRemoving(true);
    setRemoveError(null);
    try {
      await api.removeCollection(id);
      setConfirming(null);
      setOffers((o) => {
        const p = { ...o };
        delete p[id];
        return p;
      });
      const next = await reloadCollections();
      if (next.kind === 'ready' && next.list.length) select(next.list[0].id);
      else setSelected(null);
    } catch (err) {
      setRemoveError((err as Error).message || 'The forge did not remove that collection.');
    } finally {
      setRemoving(false);
    }
  };

  return (
    <section
      className="themes-more"
      aria-label="Get more"
      onKeyDown={(e) => {
        if (e.key === 'Escape' && confirming) {
          e.preventDefault();
          e.stopPropagation();
          setConfirming(null);
        }
      }}
    >
      <h2 className="agents-sub themes-sub">Get more</h2>
      {cols.kind === 'unsupported' ? (
        <p className="install-status is-error" role="status">
          This forge can't browse collections; update LaForge.
        </p>
      ) : cols.kind === 'loading' ? (
        <p className="install-status is-busy" role="status">
          <span className="spinner" aria-hidden /> Loading collections…
        </p>
      ) : cols.kind === 'error' ? (
        <p className="install-status is-error" role="status">
          {cols.reason}{' '}
          <button className="more-btn" onClick={reloadCollections}>
            Retry
          </button>
        </p>
      ) : (
        <>
          <div className="more-tabs" role="tablist" aria-label="Collections">
            {cols.list.map((c) => (
              <button key={c.id} role="tab" aria-selected={c.id === selected} className={`more-tab ${c.id === selected ? 'is-selected' : ''}`} title={c.source} onClick={() => select(c.id)}>
                {c.name}
              </button>
            ))}
          </div>
          {cols.list
            .filter((c) => c.id === selected)
            .map((c) => (
              <CollectionView
                key={c.id}
                collection={c}
                state={offers[c.id] ?? { kind: 'loading' }}
                busy={busy}
                failed={failed}
                note={note}
                installedNames={installedNames}
                active={active}
                confirming={confirming === c.id}
                removing={removing}
                removeError={removeError}
                onRefresh={() => fetchOffers(c.id, true)}
                onRetry={() => fetchOffers(c.id)}
                onInstall={(names) => install(c.id, names)}
                onPick={onPick}
                onAskRemove={() => {
                  setRemoveError(null);
                  setConfirming(c.id);
                }}
                onCancelRemove={() => setConfirming(null)}
                onRemove={() => doRemove(c.id)}
              />
            ))}
        </>
      )}
      {cols.kind !== 'unsupported' && (
        <div className="more-add">
          <label className="agents-sub themes-sub" htmlFor="themes-collection">
            Add a collection
          </label>
          <form
            className="themes-form"
            onSubmit={(e) => {
              e.preventDefault();
              doAdd();
            }}
          >
            <input value={source} spellCheck={false} id="themes-collection" placeholder="GitHub link or folder" aria-label="GitHub link or folder" onChange={(e) => setSource(e.target.value)} />
            <button type="submit" className="themes-install-btn" disabled={!source.trim() || add.kind === 'busy'}>
              Add
            </button>
          </form>
          {add.kind === 'busy' && (
            <p className="install-status is-busy" role="status">
              <span className="spinner" aria-hidden /> Adding…
            </p>
          )}
          {add.kind === 'error' && (
            <p className="install-status is-error" role="status">
              {add.reason}
            </p>
          )}
        </div>
      )}
    </section>
  );
}

interface CollectionViewProps {
  collection: C.Collection;
  state: C.OffersState;
  busy: Set<string>;
  failed: Record<string, string>;
  note: { kind: 'ok' | 'error'; text: string } | null;
  installedNames: string[];
  active: string;
  confirming: boolean;
  removing: boolean;
  removeError: string | null;
  onRefresh: () => void;
  onRetry: () => void;
  onInstall: (names: string[]) => void;
  onPick: (name: string) => void;
  onAskRemove: () => void;
  onCancelRemove: () => void;
  onRemove: () => void;
}

/** CollectionView is one collection: what it is, its skins, and Install all, Refresh and (for your own) Remove. */
export function CollectionView(p: CollectionViewProps) {
  const c = p.collection;
  const ready = p.state.kind === 'ready' ? p.state.offers : null;
  const todo = ready ? C.installable(ready) : [];
  const anyBusy = p.busy.size > 0;
  return (
    <div className="more-collection" role="tabpanel" aria-label={c.name} data-collection={c.id}>
      {(c.description || !c.builtin) && (
        <p className="themes-help more-desc">
          {c.description}
          {!c.builtin && (
            <span className="theme-source" title={c.source}>
              {c.description ? ' · ' : ''}
              {c.source}
            </span>
          )}
        </p>
      )}
      <div className="more-bar">
        <button className="more-btn is-primary" disabled={!ready || todo.length === 0 || anyBusy} onClick={() => p.onInstall(todo.map((o) => o.name))}>
          {todo.length ? `Install all (${todo.length})` : 'Install all'}
        </button>
        <button className="more-btn" title="Fetch the collection again" disabled={p.state.kind === 'loading'} onClick={p.onRefresh}>
          Refresh
        </button>
        <span className="grow" />
        {!c.builtin && !p.confirming && (
          <button className="more-btn theme-remove" onClick={p.onAskRemove}>
            Remove
          </button>
        )}
      </div>
      {!c.builtin && p.confirming && (
        <div className="more-confirm theme-confirm">
          <span className="theme-confirm-text">Remove the collection {c.name}? Installed skins stay.</span>
          <button className="more-btn theme-danger" disabled={p.removing} onClick={p.onRemove}>
            Remove
          </button>
          <button className="more-btn" onClick={p.onCancelRemove}>
            Cancel
          </button>
        </div>
      )}
      {p.removeError && <p className="theme-error more-error">{p.removeError}</p>}
      {p.note && (
        <p className={`install-status ${p.note.kind === 'error' ? 'is-error' : 'is-ok'}`} role="status">
          {p.note.text}
        </p>
      )}
      {p.state.kind === 'loading' && (
        <p className="install-status is-busy" role="status">
          <span className="spinner" aria-hidden /> Loading skins…
        </p>
      )}
      {p.state.kind === 'error' && (
        <p className="install-status is-error" role="status">
          {p.state.reason}{' '}
          {p.state.retry && (
            <button className="more-btn" onClick={p.onRetry}>
              Retry
            </button>
          )}
        </p>
      )}
      {ready && ready.length === 0 && <p className="themes-help">This collection offers no skins.</p>}
      {ready && ready.length > 0 && (
        <div className="more-list" role="list">
          {ready.map((o) => (
            <OfferRow
              key={o.name}
              offer={o}
              busy={p.busy.has(o.name)}
              failure={p.failed[o.name]}
              canUse={o.installed && p.installedNames.includes(o.name) && o.name !== p.active}
              inUse={o.installed && o.name === p.active}
              onInstall={() => p.onInstall([o.name])}
              onUse={() => p.onPick(o.name)}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function OfferRow({
  offer: o,
  busy,
  failure,
  canUse,
  inUse,
  onInstall,
  onUse,
}: {
  offer: C.Offer;
  busy: boolean;
  failure?: string;
  canUse: boolean;
  inUse: boolean;
  onInstall: () => void;
  onUse: () => void;
}) {
  const meta = [o.author, o.layout].filter(Boolean).join(' · ');
  return (
    <div role="listitem" className={`more-row ${o.problem ? 'is-problem' : ''}`} data-offer={o.name} aria-disabled={o.problem ? true : undefined}>
      <div className="more-text">
        <span className="theme-top">
          <span className="theme-label">{o.label || o.name}</span>
          {inUse && <span className="theme-tag is-default">in use</span>}
        </span>
        {meta && <span className="theme-meta">{meta}</span>}
        {o.description && <span className="more-about">{o.description}</span>}
        {o.problem && <span className="theme-error more-error">Won't install: {o.problem}</span>}
        {failure && <span className="theme-error more-error">{failure}</span>}
      </div>
      <div className="more-actions">
        {o.installed ? (
          <>
            <span className="more-installed">
              Installed <I.Check />
            </span>
            {canUse && (
              <button className="more-btn" onClick={onUse}>
                Use
              </button>
            )}
          </>
        ) : (
          <button className="more-btn is-primary" disabled={busy || !!o.problem} onClick={onInstall}>
            {busy ? <span className="spinner" aria-label="Installing" /> : 'Install'}
          </button>
        )}
      </div>
    </div>
  );
}
