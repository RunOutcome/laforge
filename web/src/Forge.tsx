// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// Forge is the whole client: tabs of split panes, each a shell or an agent.
// It is one component so a host (the Outcome Arena) can mount it with its
// own api base; the standalone client is this and nothing else.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Agents, AskButtons, Badge, LostActions, type AgentRow, type LostAction } from './Agents';
import { ApiError, ForgeApi, type Config, type LostPane, type Profile, type Session, type Whoami } from './api';
import * as L from './layout';
import { Help, Palette, type Action } from './Palette';
import { Picker, monogram } from './Picker';
import { TerminalView, type TerminalHandle } from './TerminalView';
import { loadPref, savePref, systemMode, terminalTheme, type Mode, type ModePref } from './theme';
import * as K from './skins';
import * as R from './rail';
import * as N from './notify';
import * as S from './status';
import * as I from './icons';

interface Props {
  api?: ForgeApi;
  /** embedded hides the top brand, for hosts that draw their own chrome. */
  embedded?: boolean;
}

const live = (s?: Session) => !!s && (s.state === 'running' || s.state === 'idle' || s.state === 'starting');

export function Forge({ api: given, embedded = false }: Props) {
  const api = useMemo(() => given ?? new ForgeApi(''), [given]);
  const [layout, setLayout] = useState<L.Layout | null>(null);
  const [sessions, setSessions] = useState<Map<string, Session>>(new Map());
  const [profiles, setProfiles] = useState<Profile[]>([]);
  const [config, setConfig] = useState<Config | null>(null);
  const [connected, setConnected] = useState(true);
  const [authLost, setAuthLost] = useState(false);
  const [pref, setPref] = useState<ModePref>(loadPref);
  const [sysMode, setSysMode] = useState<Mode>(systemMode);
  const [prefix, setPrefix] = useState(false);
  const [palette, setPalette] = useState(false);
  const [help, setHelp] = useState(false);
  const [confirm, setConfirm] = useState<string | null>(null);
  const [renaming, setRenaming] = useState<string | null>(null);
  const [paneMenu, setPaneMenu] = useState(false);
  const [closingTab, setClosingTab] = useState<string | null>(null);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [fontSize, setFontSize] = useState(13);
  const [clock, setClock] = useState(() => new Date());
  const [agentsOpen, setAgentsOpen] = useState(() => N.loadFlag('laforge.agents', false));
  const [notifyOn, setNotifyOn] = useState(() => N.loadFlag('laforge.notify', false));
  const [soundOn, setSoundOn] = useState(() => N.loadFlag('laforge.sound', false));
  const [permission, setPermission] = useState<N.Permission>(N.permission);
  const [visible, setVisible] = useState(() => document.visibilityState === 'visible');
  const [who, setWho] = useState<Whoami | null>(null);
  const [lost, setLost] = useState<LostPane[]>([]);
  const [lostError, setLostError] = useState('');
  const [askHidden, setAskHidden] = useState<Set<string>>(new Set());
  const [askNotes, setAskNotes] = useState<Record<string, string>>({});
  const viewingRef = useRef(false);
  viewingRef.current = who?.mode === 'view';
  const [skinList, setSkinList] = useState<K.SkinList | null>(null);
  const [skin, setSkin] = useState<K.Skin>(K.classic);
  const [railSpace, setRailSpace] = useState<string | null>(null);
  const [railOpen, setRailOpen] = useState(false);
  const [newSpace, setNewSpace] = useState<{ name: string; cwd: string; error: string } | null>(null);
  const terms = useRef(new Map<string, TerminalHandle>());
  // a skin that draws one mode forces it; otherwise the viewer's choice
  const mode: Mode = K.modeOf(skin, pref === 'system' ? sysMode : pref);
  const termTheme = useMemo(() => K.terminalOf(skin, terminalTheme(mode)), [skin, mode]);
  const layoutKind = K.layoutOf(skin);

  useEffect(() => {
    document.documentElement.dataset.mode = mode;
  }, [mode]);

  // skins: the browser's choice if the forge still has it, else the forge's
  // default; one that fails to load falls back to classic
  const skinSeq = useRef(0);
  const showSkin = useCallback(
    (next: K.Skin) => {
      const seq = ++skinSeq.current;
      K.apply(next, api.base)
        .then(() => seq === skinSeq.current && setSkin(next))
        .catch(() => {
          if (seq !== skinSeq.current) return;
          K.apply(K.classic, api.base).catch(() => undefined);
          setSkin(K.classic);
        });
    },
    [api],
  );
  const loadSkins = useCallback(
    () =>
      api
        .skins()
        .then((list) => {
          setSkinList(list);
          return list;
        })
        .catch(() => null),
    [api],
  );
  useEffect(() => {
    let stop = false;
    loadSkins().then((list) => !stop && showSkin(K.pick(list, K.loadChoice())));
    return () => {
      stop = true;
    };
  }, [loadSkins, showSkin]);
  const chooseSkin = useCallback(
    (name: string) => {
      K.saveChoice(name);
      showSkin(K.pick(skinList, name));
    },
    [skinList, showSkin],
  );
  const makeDefaultSkin = useCallback(
    (name: string) => {
      api
        .setDefaultSkin(name)
        .then(loadSkins)
        .catch(() => undefined);
    },
    [api, loadSkins],
  );
  useEffect(() => {
    const mq = window.matchMedia?.('(prefers-color-scheme: dark)');
    const on = () => setSysMode(mq.matches ? 'dark' : 'light');
    mq?.addEventListener('change', on);
    const t = setInterval(() => setClock(new Date()), 15000);
    return () => {
      mq?.removeEventListener('change', on);
      clearInterval(t);
    };
  }, []);

  const refresh = useCallback(async () => {
    try {
      const list = await api.sessions();
      setSessions(new Map(list.map((s) => [s.id, s])));
      setConnected(true);
      setAuthLost(false);
      return list;
    } catch (err) {
      setConnected(false);
      if (err instanceof ApiError && err.status === 401) setAuthLost(true);
      return null;
    }
  }, [api]);

  // load: the saved layout, reconciled with the sessions that are running
  useEffect(() => {
    let stop = false;
    (async () => {
      // Never reconcile against a failed fetch: an empty session list or a
      // missing layout from a hiccup would clear every pane and be saved.
      let raw: unknown;
      let list: Session[] | null = null;
      let profs: Profile[] = [];
      let cfg: Config | null = null;
      let me: Whoami = { mode: 'full' };
      let gone: LostPane[] = [];
      for (let attempt = 0; !stop; attempt++) {
        const failed = Symbol('failed');
        // whoami first: a read-only share may not ask for the lost panes
        const w = await api.whoami().catch((err) => (err instanceof ApiError && err.status === 404 ? ({ mode: 'full' } as Whoami) : failed));
        const [r, l, p, c, g] = await Promise.all([
          api.layout().catch(() => failed),
          refresh(),
          api.profiles().catch(() => failed),
          api.config().catch(() => null),
          w !== failed && w.mode === 'full' ? api.lost().catch(() => [] as LostPane[]) : Promise.resolve([] as LostPane[]),
        ]);
        if (w !== failed && r !== failed && l !== null && p !== failed) {
          me = w;
          raw = r;
          list = l;
          profs = p as Profile[];
          cfg = c;
          gone = g;
          break;
        }
        await new Promise((ok) => setTimeout(ok, Math.min(1000 * (attempt + 1), 5000)));
      }
      if (stop) return;
      setProfiles(profs);
      setConfig(cfg);
      setWho(me);
      if (me.mode === 'view') return; // one pane, no layout
      setLost(gone);
      let next = L.valid(raw) ? raw : L.initialLayout();
      const ids = new Set((list ?? []).map((s) => s.id));
      const lostIds = new Set(gone.map((x) => x.id));
      const placed = new Set<string>();
      next = {
        ...next,
        tabs: next.tabs.map((t) => {
          let root = t.root;
          for (const p of L.panes(root)) {
            // a lost pane keeps its slot, to resume there
            if (p.session && !ids.has(p.session) && !lostIds.has(p.session)) root = L.setSession(root, p.id, undefined);
            else if (p.session) placed.add(p.session);
          }
          return { ...t, root };
        }),
      };
      const orphans = (list ?? []).filter((s) => !placed.has(s.id) && live(s));
      if (orphans.length) {
        const tab = { ...L.newTab('Found', orphans[0].id), custom: true };
        let root: L.Node = tab.root;
        for (const s of orphans.slice(1)) root = L.split(root, L.panes(root).at(-1)!.id, 'row', L.newPane(s.id));
        next = { ...next, tabs: [...next.tabs, { ...tab, root: L.tiled(root) }] };
      }
      if (!next.tabs.some((t) => t.id === next.active)) next = { ...next, active: next.tabs[0].id };
      setLayout(next);
    })();
    const poll = setInterval(refresh, 1500);
    return () => {
      stop = true;
      clearInterval(poll);
    };
  }, [api, refresh]);

  // save the layout shortly after it changes
  useEffect(() => {
    if (!layout) return;
    const t = setTimeout(() => api.saveLayout(layout).catch(() => undefined), 400);
    return () => clearTimeout(t);
  }, [api, layout]);

  const tab = layout?.tabs.find((t) => t.id === layout.active);
  const layoutRef = useRef(layout);
  layoutRef.current = layout;
  const sessionsRef = useRef(sessions);
  sessionsRef.current = sessions;
  const visibleRef = useRef(visible);
  visibleRef.current = visible;

  const updateTab = useCallback((id: string, fn: (t: L.Tab) => L.Tab) => {
    setLayout((l) => (l ? { ...l, tabs: l.tabs.map((t) => (t.id === id ? fn(t) : t)) } : l));
  }, []);

  const focusPane = useCallback(
    (tabId: string, paneId: string) => updateTab(tabId, (t) => (t.focus === paneId ? t : { ...t, focus: paneId })),
    [updateTab],
  );

  // goTo activates a tab and focuses one of its panes (zoomed onto it when the tab is zoomed).
  const goTo = useCallback((tabId: string, paneId: string) => {
    setLayout((l) => (l ? { ...l, active: tabId, tabs: l.tabs.map((t) => (t.id === tabId ? { ...t, focus: paneId, zoom: t.zoom ? paneId : null } : t)) } : l));
  }, []);

  // markSeen tells the forge a viewer looked at the pane, once per status change.
  const seenSent = useRef(new Set<string>());
  const markSeen = useCallback(
    (s: Session | undefined) => {
      if (!s || s.seen !== false || viewingRef.current) return;
      const key = `${s.id}|${s.statusSince ?? ''}`;
      if (seenSent.current.has(key)) return;
      seenSent.current.add(key);
      api
        .seen(s.id)
        .then((p) => setSessions((m) => (m.has(p.id) ? new Map(m).set(p.id, p) : m)))
        .catch(() => seenSent.current.delete(key));
    },
    [api],
  );

  // locate finds the tab and pane a session is in.
  const locate = useCallback((id: string): { tab: L.Tab; pane: L.PaneNode } | undefined => {
    for (const t of layoutRef.current?.tabs ?? []) {
      const pane = L.panes(t.root).find((p) => p.session === id);
      if (pane) return { tab: t, pane };
    }
    return undefined;
  }, []);

  // watched: the session is in the focused pane of the active tab, and the page is visible.
  const watched = useCallback((id: string) => {
    const l = layoutRef.current;
    const t = l?.tabs.find((x) => x.id === l.active);
    return visibleRef.current && !!t && L.findPane(t.root, t.focus)?.session === id;
  }, []);

  const startIn = useCallback(
    async (tabId: string, paneId: string, profile: string, cwd?: string) => {
      setErrors((e) => ({ ...e, [paneId]: '' }));
      try {
        const s = await api.start({ profile, cwd, cols: 100, rows: 30 });
        setSessions((m) => new Map(m).set(s.id, s));
        updateTab(tabId, (t) => ({ ...t, root: L.setSession(t.root, paneId, s.id), focus: paneId }));
      } catch (err) {
        setErrors((e) => ({ ...e, [paneId]: (err as Error).message }));
      }
    },
    [api, updateTab],
  );

  const splitPane = useCallback(
    (dir: L.Dir) => {
      if (!tab) return;
      const pane = L.newPane();
      updateTab(tab.id, (t) => ({ ...t, root: L.split(t.root, t.focus, dir, pane), focus: pane.id, zoom: null }));
    },
    [tab, updateTab],
  );

  const closePane = useCallback(
    (tabId: string, paneId: string) => {
      setConfirm(null);
      const session = L.findPane(layoutRef.current?.tabs.find((x) => x.id === tabId)?.root ?? L.newPane(), paneId)?.session;
      if (session) api.close(session).catch(() => undefined);
      setLayout((l) => {
        if (!l) return l;
        const t = l.tabs.find((x) => x.id === tabId);
        if (!t) return l;
        const root = L.remove(t.root, paneId);
        if (!root) {
          if (l.tabs.length > 1) {
            const tabs = l.tabs.filter((x) => x.id !== tabId);
            const idx = l.tabs.findIndex((x) => x.id === tabId);
            return { ...l, tabs, active: l.active === tabId ? tabs[Math.max(0, idx - 1)].id : l.active };
          }
          const fresh = L.newPane();
          return { ...l, tabs: [{ ...t, root: fresh, focus: fresh.id, zoom: null }] };
        }
        const rest = L.panes(root);
        const focus = t.focus === paneId ? (L.neighbor(t.root, paneId, 'left') ?? L.neighbor(t.root, paneId, 'up') ?? rest[0].id) : t.focus;
        return { ...l, tabs: l.tabs.map((x) => (x.id === tabId ? { ...x, root, focus: rest.some((p) => p.id === focus) ? focus : rest[0].id, zoom: null } : x)) };
      });
    },
    [api],
  );

  const restart = useCallback(
    (tabId: string, paneId: string, s: Session) => {
      api.close(s.id).catch(() => undefined);
      updateTab(tabId, (t) => ({ ...t, root: L.setSession(t.root, paneId, undefined) }));
      startIn(tabId, paneId, s.after ?? s.profile, s.cwd);
    },
    [api, startIn, updateTab],
  );

  const newTabAction = useCallback(() => {
    setLayout((l) => {
      if (!l) return l;
      const t = L.newTab();
      return { ...l, tabs: [...l.tabs, t], active: t.id };
    });
  }, []);

  const selectTab = useCallback((i: number) => setLayout((l) => (l && l.tabs[i] ? { ...l, active: l.tabs[i].id } : l)), []);
  const stepTab = useCallback(
    (d: number) =>
      setLayout((l) => {
        if (!l) return l;
        const i = l.tabs.findIndex((t) => t.id === l.active);
        return { ...l, active: l.tabs[(i + d + l.tabs.length) % l.tabs.length].id };
      }),
    [],
  );

  const moveFocus = useCallback(
    (m: L.Move) => {
      if (!tab) return;
      const n = L.neighbor(tab.root, tab.focus, m);
      if (n) updateTab(tab.id, (t) => ({ ...t, focus: n, zoom: t.zoom ? n : null }));
    },
    [tab, updateTab],
  );
  const cyclePane = useCallback(() => {
    if (!tab) return;
    const list = L.panes(tab.root);
    const i = list.findIndex((p) => p.id === tab.focus);
    updateTab(tab.id, (t) => ({ ...t, focus: list[(i + 1) % list.length].id }));
  }, [tab, updateTab]);
  const toggleZoom = useCallback(() => tab && updateTab(tab.id, (t) => ({ ...t, zoom: t.zoom ? null : t.focus })), [tab, updateTab]);
  const preset = useCallback(
    (kind: 'tiled' | 'main') => tab && updateTab(tab.id, (t) => ({ ...t, zoom: null, root: kind === 'tiled' ? L.tiled(t.root) : L.mainStack(t.root, t.focus) })),
    [tab, updateTab],
  );
  const cycleMode = useCallback(() => {
    setPref((p) => {
      const next: ModePref = p === 'dark' ? 'light' : p === 'light' ? 'system' : 'dark';
      savePref(next);
      return next;
    });
  }, []);

  useEffect(() => {
    const on = () => setVisible(document.visibilityState === 'visible');
    document.addEventListener('visibilitychange', on);
    return () => document.removeEventListener('visibilitychange', on);
  }, []);

  // seen: the focused pane of the active tab, while the page is visible
  const focusedSession = tab ? L.findPane(tab.root, tab.focus)?.session : undefined;
  useEffect(() => {
    if (visible && focusedSession) markSeen(sessions.get(focusedSession));
  }, [visible, focusedSession, sessions, markSeen]);

  // notifications: a pane newly blocked, error or done, a second later if it
  // still is and nobody is looking at it
  const alertRef = useRef<(t: S.Transition) => void>(() => undefined);
  alertRef.current = (t: S.Transition) => {
    const s = sessionsRef.current.get(t.id);
    if (viewingRef.current || !s || S.shown(s) !== t.word || s.statusSince !== t.since || watched(t.id)) return;
    if (soundOn) N.chime(t.word);
    if (!notifyOn) return;
    const name = s.status?.app || s.title;
    const what = t.word === 'blocked' ? 'is waiting for you' : t.word === 'error' ? 'hit an error' : 'is done';
    let body = s.status?.msg || (t.word === 'blocked' && s.status?.kind ? `Waiting for ${s.status.kind}` : s.title);
    if (t.word === 'blocked' && s.asking) body = `${s.asking.msg || body} · answer in LaForge`;
    N.show(`${name} ${what}`, body, `laforge-${t.id}`, () => {
      const at = locate(t.id);
      if (at) goTo(at.tab.id, at.pane.id);
      markSeen(sessionsRef.current.get(t.id));
    });
  };
  const prevSessions = useRef<Session[]>([]);
  const alertTimers = useRef(new Set<ReturnType<typeof setTimeout>>());
  useEffect(() => {
    const list = [...sessions.values()];
    const prev = prevSessions.current;
    prevSessions.current = list;
    for (const t of S.transitions(prev, list)) {
      const timer = setTimeout(() => {
        alertTimers.current.delete(timer);
        alertRef.current(t);
      }, 1000);
      alertTimers.current.add(timer);
    }
  }, [sessions]);
  useEffect(() => {
    const timers = alertTimers.current;
    return () => timers.forEach(clearTimeout);
  }, []);

  // the page title leads with how many panes are blocked
  const blocked = [...sessions.values()].filter((s) => S.shown(s) === 'blocked').length;
  const baseTitle = useRef(document.title);
  useEffect(() => {
    document.title = blocked ? `(${blocked}) ${baseTitle.current}` : baseTitle.current;
  }, [blocked]);
  useEffect(() => {
    const base = baseTitle.current;
    return () => {
      document.title = base;
    };
  }, []);

  // asks: Allow or Deny what a pane's agent waits for. A 409 means it gave
  // up waiting and asked in its terminal.
  const answer = useCallback(
    async (s: Session, decision: 'allow' | 'deny') => {
      const key = `${s.id}|${s.asking?.since ?? ''}`;
      const note = (text: string) => {
        setAskNotes((n) => ({ ...n, [s.id]: text }));
        setTimeout(
          () =>
            setAskNotes((n) => {
              const { [s.id]: _, ...rest } = n;
              return rest;
            }),
          4000,
        );
      };
      setAskHidden((h) => new Set(h).add(key));
      try {
        await api.answer(s.id, decision);
        note(decision === 'allow' ? 'Allowed' : 'Denied');
        refresh();
      } catch (err) {
        if (err instanceof ApiError && err.status === 409) {
          note('It already asked in its terminal');
          return;
        }
        setAskHidden((h) => {
          const next = new Set(h);
          next.delete(key);
          return next;
        });
        note((err as Error).message);
      }
    },
    [api, refresh],
  );

  // lost panes: resume (continuing the agent's session when it can), start
  // the profile fresh, or dismiss; in their slot, or else in a new tab
  const lostAction = useCallback(
    async (lp: LostPane, action: LostAction, slot?: { tabId: string; paneId: string }) => {
      const fail = (msg: string) => (slot ? setErrors((e) => ({ ...e, [slot.paneId]: msg })) : setLostError(msg));
      fail('');
      try {
        if (action === 'dismiss') {
          await api.dismissLost(lp.id).catch((err) => {
            if (!(err instanceof ApiError && err.status === 404)) throw err;
          });
          setLost((l) => l.filter((x) => x.id !== lp.id));
          if (slot) updateTab(slot.tabId, (t) => ({ ...t, root: L.setSession(t.root, slot.paneId, undefined), focus: slot.paneId }));
          return;
        }
        const s = await api.start(action === 'resume' ? { profile: lp.profile, resume: lp.id, cols: 100, rows: 30 } : { profile: lp.profile, cwd: lp.cwd, cols: 100, rows: 30 });
        if (action === 'fresh') api.dismissLost(lp.id).catch(() => undefined);
        setLost((l) => l.filter((x) => x.id !== lp.id));
        setSessions((m) => new Map(m).set(s.id, s));
        if (slot) updateTab(slot.tabId, (t) => ({ ...t, root: L.setSession(t.root, slot.paneId, s.id), focus: slot.paneId }));
        else
          setLayout((l) => {
            if (!l) return l;
            const t = L.newTab('', s.id);
            return { ...l, tabs: [...l.tabs, t], active: t.id };
          });
      } catch (err) {
        fail((err as Error).message);
        api
          .lost()
          .then(setLost)
          .catch(() => undefined);
      }
    },
    [api, updateTab],
  );

  // openUnplaced shows a pane no tab holds (one an agent started) in a new tab.
  const openUnplaced = useCallback((id: string) => {
    setLayout((l) => {
      if (!l) return l;
      const t = L.newTab('', id);
      return { ...l, tabs: [...l.tabs, t], active: t.id };
    });
  }, []);

  const toggleAgents = useCallback(() => {
    setAgentsOpen((o) => {
      N.saveFlag('laforge.agents', !o);
      return !o;
    });
  }, []);
  const setNotify = useCallback(async (on: boolean) => {
    if (on) {
      const p = N.permission() === 'granted' ? 'granted' : await N.ask();
      setPermission(p);
      if (p !== 'granted') return;
    }
    setNotifyOn(on);
    N.saveFlag('laforge.notify', on);
  }, []);
  const setSound = useCallback((on: boolean) => {
    setSoundOn(on);
    N.saveFlag('laforge.sound', on);
    if (on) {
      N.unlock();
      setTimeout(() => N.chime('done'), 50);
    }
  }, []);

  useEffect(() => {
    if (!agentsOpen || who?.mode !== 'full') return;
    api
      .lost()
      .then(setLost)
      .catch(() => undefined);
  }, [agentsOpen, who, api]);

  // the agents panel's clock: finer than the status bar's while it is open
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!agentsOpen) return;
    setNow(Date.now());
    const t = setInterval(() => setNow(Date.now()), 5000);
    return () => clearInterval(t);
  }, [agentsOpen]);

  const actions: Action[] = useMemo(() => {
    const list: Action[] = [
      { id: 'split-right', group: 'Pane', label: 'Split right', hint: '^b %', run: () => splitPane('row') },
      { id: 'split-down', group: 'Pane', label: 'Split down', hint: '^b "', run: () => splitPane('col') },
      { id: 'zoom', group: 'Pane', label: 'Zoom or unzoom the pane', hint: '^b z', run: toggleZoom },
      { id: 'close', group: 'Pane', label: 'Close the pane', hint: '^b x', run: () => tab && setConfirm(tab.focus) },
      { id: 'new-tab', group: 'Tab', label: 'New tab', hint: '^b c', run: newTabAction },
      { id: 'rename-tab', group: 'Tab', label: 'Rename the tab', hint: '^b ,', run: () => tab && setRenaming(tab.id) },
      { id: 'next-tab', group: 'Tab', label: 'Next tab', hint: '^b n', run: () => stepTab(1) },
      { id: 'prev-tab', group: 'Tab', label: 'Previous tab', hint: '^b p', run: () => stepTab(-1) },
      { id: 'tiled', group: 'Layout', label: 'Tiled', hint: '^b space', run: () => preset('tiled') },
      { id: 'main', group: 'Layout', label: 'Main and stack', hint: '^b m', run: () => preset('main') },
      { id: 'agents', group: 'View', label: agentsOpen ? 'Hide the agents panel' : 'Show the agents panel', hint: '^b a', run: toggleAgents },
      { id: 'mode', group: 'View', label: `Theme: ${pref} (switch)`, run: cycleMode },
      { id: 'bigger', group: 'View', label: 'Bigger text', hint: '^b +', run: () => setFontSize((f) => Math.min(f + 1, 22)) },
      { id: 'smaller', group: 'View', label: 'Smaller text', hint: '^b _', run: () => setFontSize((f) => Math.max(f - 1, 9)) },
      { id: 'help', group: 'Help', label: 'Keys', hint: '^b ?', run: () => setHelp(true) },
    ];
    for (const k of skinList?.skins ?? []) {
      list.push({ id: `skin-${k.name}`, group: 'Skin', label: `Skin: ${k.label}${k.name === skin.name ? ' (in use)' : ''}`, run: () => chooseSkin(k.name) });
    }
    for (const k of skinList?.skins ?? []) {
      if (k.name !== skinList?.default) list.push({ id: `skin-default-${k.name}`, group: 'Skin', label: `Make ${k.label} the forge default`, run: () => makeDefaultSkin(k.name) });
    }
    for (const p of profiles.filter((x) => x.available)) {
      list.push({
        id: `new-${p.name}`,
        group: 'Start',
        label: `${p.label} in a new pane`,
        run: () => {
          if (!tab) return;
          const pane = L.newPane();
          updateTab(tab.id, (t) => ({ ...t, root: L.split(t.root, t.focus, 'row', pane), focus: pane.id, zoom: null }));
          startIn(tab.id, pane.id, p.name, config?.cwd);
        },
      });
    }
    return list;
  }, [profiles, tab, pref, config, agentsOpen, skinList, skin, chooseSkin, makeDefaultSkin, splitPane, toggleZoom, newTabAction, stepTab, preset, cycleMode, toggleAgents, startIn, updateTab]);

  // keys: the prefix (Ctrl-b) then one key, as in tmux
  const keys = useRef<(e: KeyboardEvent) => boolean>(() => false);
  const prefixRef = useRef(false);
  prefixRef.current = prefix;
  keys.current = (e: KeyboardEvent) => {
    if (palette || help || renaming || viewingRef.current) return false;
    if (paneMenu && e.key === 'Escape') {
      setPaneMenu(false);
      return true;
    }
    if (confirm) {
      if (['Shift', 'Control', 'Alt', 'Meta'].includes(e.key)) return true;
      if (e.key === 'y' || e.key === 'Y' || e.key === 'Enter') closePane(tab!.id, confirm);
      setConfirm(null);
      return true;
    }
    if (e.metaKey && !e.ctrlKey && e.key.toLowerCase() === 'k') {
      setPalette(true);
      return true;
    }
    if (!prefixRef.current) {
      if (e.ctrlKey && !e.metaKey && !e.altKey && e.key.toLowerCase() === 'b') {
        setPrefix(true);
        return true;
      }
      return false;
    }
    if (['Shift', 'Control', 'Alt', 'Meta'].includes(e.key)) return true;
    setPrefix(false);
    if (e.ctrlKey && e.key.toLowerCase() === 'b') {
      if (tab) terms.current.get(tab.focus)?.send('\x02');
      return true;
    }
    switch (shifted(e)) {
      case '%':
      case '|':
        splitPane('row');
        break;
      case '"':
      case "'":
      case '-':
        splitPane('col');
        break;
      case 'ArrowLeft':
      case 'h':
        moveFocus('left');
        break;
      case 'ArrowRight':
      case 'l':
        moveFocus('right');
        break;
      case 'ArrowUp':
      case 'k':
        moveFocus('up');
        break;
      case 'ArrowDown':
      case 'j':
        moveFocus('down');
        break;
      case 'o':
        cyclePane();
        break;
      case 'z':
        toggleZoom();
        break;
      case 'x':
        if (tab) setConfirm(tab.focus);
        break;
      case 'c':
        newTabAction();
        break;
      case 'n':
        stepTab(1);
        break;
      case 'p':
        stepTab(-1);
        break;
      case ',':
        if (tab) setRenaming(tab.id);
        break;
      case ' ':
        preset('tiled');
        break;
      case 'm':
        preset('main');
        break;
      case 'a':
        toggleAgents();
        break;
      case '+':
      case '=':
        setFontSize((f) => Math.min(f + 1, 22));
        break;
      case '_':
        setFontSize((f) => Math.max(f - 1, 9));
        break;
      case ':':
        setPalette(true);
        break;
      case '?':
        setHelp(true);
        break;
      default:
        if (/^[1-9]$/.test(e.key) && !e.shiftKey) selectTab(Number(e.key) - 1);
    }
    return true;
  };

  // keys typed outside a terminal (pickers, the chrome)
  useEffect(() => {
    const onDown = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement | null;
      if (t?.closest?.('.xterm')) return; // the terminal's own handler saw it
      if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA') && !prefixRef.current && !(e.ctrlKey && e.key.toLowerCase() === 'b')) return;
      if (keys.current(e)) {
        e.preventDefault();
        e.stopPropagation();
      }
    };
    window.addEventListener('keydown', onDown);
    return () => window.removeEventListener('keydown', onDown);
  }, []);

  const lostById = new Map(lost.map((x) => [x.id, x]));

  if (who?.mode === 'view') {
    const s = sessions.get(who.pane);
    const word = S.shown(s);
    const msg = s?.status?.msg ?? '';
    return (
      <div className={`forge is-view ${embedded ? 'is-embedded' : ''}`}>
        <div className="view-banner">
          <I.Eye />
          <span>Read-only view</span>
          <span className="view-sep">·</span>
          <span className="view-title">{s?.title ?? 'pane'}</span>
          <span className="view-sep">·</span>
          {word ? <Badge word={word} progress={s?.status?.progress} /> : <span className={`dot dot-${s ? s.state : 'empty'}`} />}
          {!word && <span>{s ? s.state : 'gone'}</span>}
          {msg && (
            <span className="pane-msg" title={msg}>
              {msg}
            </span>
          )}
        </div>
        <main className="view-body">
          {s && !authLost ? (
            <div className="pane view-pane">
              <div className="pane-body">
                <TerminalView api={api} session={s.id} mode={mode} theme={termTheme} focused fontSize={fontSize} readOnly cols={s.columns} rows={s.rows} onKey={() => false} onFocus={() => undefined} onEnded={refresh} register={() => undefined} />
              </div>
            </div>
          ) : (
            <div className="loading">{authLost ? 'This share link has expired or was revoked.' : 'This pane has ended.'}</div>
          )}
        </main>
      </div>
    );
  }

  if (!layout || !tab) {
    return (
      <div className={`forge ${embedded ? 'is-embedded' : ''}`}>
        <div className="loading">
          <I.Mark size={28} />
          <span>Opening the forge…</span>
        </div>
      </div>
    );
  }

  const placedSessions = new Set(layout.tabs.flatMap((t) => L.panes(t.root).map((p) => p.session)).filter(Boolean) as string[]);

  const counts = { running: 0, idle: 0, ended: 0 };
  for (const s of sessions.values()) {
    if (s.state === 'running' || s.state === 'starting') counts.running++;
    else if (s.state === 'idle') counts.idle++;
    else counts.ended++;
  }

  const tabSessions = (t: L.Tab) =>
    L.panes(t.root)
      .map((p) => (p.session ? sessions.get(p.session) : undefined))
      .filter(Boolean) as Session[];

  const tabState = (t: L.Tab): string => {
    const ss = tabSessions(t);
    if (ss.some((s) => s.state === 'failed')) return 'failed';
    if (ss.some((s) => s.state === 'running' || s.state === 'starting')) return 'running';
    if (ss.some((s) => s.state === 'idle')) return 'idle';
    return ss.length ? 'complete' : 'empty';
  };

  const pct = (v: number) => `${(v * 100).toFixed(4)}%`;

  // Every pane of a tab is drawn in one flat layer at the rect the layout
  // gives it, so a split, a zoom or a preset never remounts a terminal.
  const renderTab = (t: L.Tab): React.ReactNode => {
    const boxes = L.rects(t.root);
    const list = L.panes(t.root);
    return (
      <>
        {list.map((pane) => {
          const r = t.zoom === pane.id ? { x: 0, y: 0, w: 1, h: 1 } : boxes.get(pane.id)!;
          return (
            <div key={pane.id} className="pane-slot" style={{ left: pct(r.x), top: pct(r.y), width: pct(r.w), height: pct(r.h), zIndex: t.zoom === pane.id ? 3 : 1, visibility: t.zoom && t.zoom !== pane.id ? 'hidden' : undefined }}>
              {PaneView({ t, pane })}
            </div>
          );
        })}
        {!t.zoom &&
          L.gutters(t.root).map((g) => {
            const row = g.split.dir === 'row';
            const style = row
              ? { left: `calc(${pct(g.rect.x + g.at * g.rect.w)} - 4px)`, top: pct(g.rect.y), height: pct(g.rect.h), width: '8px' }
              : { top: `calc(${pct(g.rect.y + g.at * g.rect.h)} - 4px)`, left: pct(g.rect.x), width: pct(g.rect.w), height: '8px' };
            return (
              <div
                key={`${g.split.id}-${g.index}`}
                className={`gutter gutter-${g.split.dir}`}
                style={style}
                onMouseDown={(e) => {
                  e.preventDefault();
                  const body = (e.currentTarget as HTMLElement).parentElement!.getBoundingClientRect();
                  const total = row ? body.width * g.rect.w : body.height * g.rect.h;
                  const start = row ? e.clientX : e.clientY;
                  const a = g.split.sizes[g.index - 1];
                  const b = g.split.sizes[g.index];
                  document.body.classList.add(`dragging-${g.split.dir}`);
                  const move = (ev: MouseEvent) => {
                    const d = ((row ? ev.clientX : ev.clientY) - start) / total;
                    const na = Math.max(0.08, Math.min(a + b - 0.08, a + d));
                    const sizes = [...g.split.sizes];
                    sizes[g.index - 1] = na;
                    sizes[g.index] = a + b - na;
                    updateTab(t.id, (x) => ({ ...x, root: L.resize(x.root, g.split.id, sizes) }));
                  };
                  const up = () => {
                    document.body.classList.remove(`dragging-${g.split.dir}`);
                    window.removeEventListener('mousemove', move);
                    window.removeEventListener('mouseup', up);
                  };
                  window.addEventListener('mousemove', move);
                  window.addEventListener('mouseup', up);
                }}
                onDoubleClick={() => updateTab(t.id, (x) => ({ ...x, root: L.resize(x.root, g.split.id, g.split.sizes.map(() => 1 / g.split.sizes.length)) }))}
              />
            );
          })}
      </>
    );
  };

  function PaneView({ t, pane }: { t: L.Tab; pane: L.PaneNode }) {
    const s = pane.session ? sessions.get(pane.session) : undefined;
    const focused = t.id === layout!.active && t.focus === pane.id;
    const zoomed = t.zoom === pane.id;
    const only = L.panes(t.root).length === 1;
    const ended = s && !live(s);
    const state = s ? s.state : 'empty';
    const word = S.shown(s);
    const msg = word ? s?.status?.msg || (word === 'blocked' && s?.status?.kind ? `waiting for ${s.status.kind}` : '') : '';
    const lp = !s && pane.session ? lostById.get(pane.session) : undefined;
    return (
      <section
        className={`pane ${focused ? 'is-focused' : ''} ${zoomed ? 'is-zoomed' : ''}`}
        data-status={word ?? (lp ? 'lost' : state)}
        data-active={focused ? 'true' : 'false'}
        onMouseDown={() => focusPane(t.id, pane.id)}
      >
        <header className="pane-head">
          {!word && <span className={`dot dot-${state}`} title={state} />}
          {s ? (
            <>
              <span className={`chip ${profiles.find((p) => p.name === s.profile)?.agent ? 'chip-agent' : ''}`}>{monogram(profiles.find((p) => p.name === s.profile) ?? { name: s.profile, label: s.profile })}</span>
              <span
                className="pane-title"
                title="Double-click to rename"
                onDoubleClick={() => {
                  const title = window.prompt('Pane name', s.title);
                  if (title) api.rename(s.id, title).then(refresh).catch(() => undefined);
                }}
              >
                {s.title}
              </span>
              {word && <Badge word={word} progress={s.status?.progress} />}
              {msg && (
                <span className={`pane-msg msg-${word}`} title={msg}>
                  {msg}
                </span>
              )}
              <AskButtons asking={!!s.asking && !askHidden.has(`${s.id}|${s.asking.since}`)} note={askNotes[s.id]} onAnswer={(d) => answer(s, d)} />
              {s.held && (
                <span className="pane-held" title="Keeps running if LaForge restarts">
                  <I.Held />
                </span>
              )}
              {s.after && (
                <span className="pane-after" title={`${profiles.find((p) => p.name === s.after)?.label ?? s.after} exited${s.agentExit !== undefined ? ` (${s.agentExit})` : ''}; this pane is a shell now`}>
                  after {profiles.find((p) => p.name === s.after)?.label ?? s.after}
                  {s.agentExit ? ` · exit ${s.agentExit}` : ''}
                </span>
              )}
              <span className="pane-cwd" title={s.cwd}>
                {shortPath(s.cwd, config?.cwd)}
              </span>
            </>
          ) : lp ? (
            <>
              <span className="pane-title is-muted">{lp.title}</span>
              <span className="pane-lost">lost</span>
              <span className="pane-cwd" title={lp.cwd}>
                {shortPath(lp.cwd, config?.cwd)}
              </span>
            </>
          ) : (
            <span className="pane-title is-muted">New pane</span>
          )}
          <span className="grow" />
          {confirm === pane.id ? (
            <span className="confirm">
              Close{s && live(s) ? ` and stop ${s.title}` : ''}? <kbd>y</kbd> / <kbd>n</kbd>
            </span>
          ) : (
            <span className="pane-tools">
              <button title="Split right (^b %)" onClick={() => (focusPane(t.id, pane.id), setTimeout(() => splitPane('row')))}>
                <I.SplitRight />
              </button>
              <button title="Split down (^b &quot;)" onClick={() => (focusPane(t.id, pane.id), setTimeout(() => splitPane('col')))}>
                <I.SplitDown />
              </button>
              {!only && (
                <button title="Zoom (^b z)" onClick={() => updateTab(t.id, (x) => ({ ...x, focus: pane.id, zoom: x.zoom ? null : pane.id }))}>
                  {zoomed ? <I.Unzoom /> : <I.Zoom />}
                </button>
              )}
              <button title="Close (^b x)" onClick={() => (s && live(s) ? (focusPane(t.id, pane.id), setConfirm(pane.id)) : closePane(t.id, pane.id))}>
                <I.Close />
              </button>
            </span>
          )}
        </header>
        <div className="pane-body">
          {s ? (
            <TerminalView
              api={api}
              session={s.id}
              mode={mode}
              theme={termTheme}
              focused={focused && !palette && !help}
              fontSize={fontSize}
              onKey={(e) => keys.current(e)}
              onFocus={() => focusPane(t.id, pane.id)}
              onEnded={refresh}
              register={(h) => (h ? terms.current.set(pane.id, h) : terms.current.delete(pane.id))}
            />
          ) : lp ? (
            <div className="lost-pane">
              <div className="lost-inner">
                <h2>{lp.title}</h2>
                <p className="picker-note">
                  Its program stopped {S.ago(lp.lostAt, Date.now()) === 'now' ? 'just now' : `${S.ago(lp.lostAt, Date.now())} ago`} (LaForge was stopped, or the machine restarted). It ran {profiles.find((x) => x.name === lp.profile)?.label ?? lp.profile} in <code>{shortPath(lp.cwd)}</code>.
                </p>
                <LostActions lp={lp} onAction={(a) => lostAction(lp, a, { tabId: t.id, paneId: pane.id })} />
                {errors[pane.id] && <p className="picker-error">{errors[pane.id]}</p>}
              </div>
            </div>
          ) : (
            <Picker
              profiles={profiles}
              cwd={config?.cwd ?? ''}
              focused={focused && !palette && !help}
              error={errors[pane.id]}
              onStart={(profile, cwd) => startIn(t.id, pane.id, profile, cwd)}
              onCancel={only && layout!.tabs.length === 1 ? undefined : () => closePane(t.id, pane.id)}
              onFocus={() => focusPane(t.id, pane.id)}
            />
          )}
          {ended && s && (
            <div className="ended">
              <span>
                {s.state === 'failed' ? 'Failed' : 'Ended'}
                {s.exitCode !== undefined ? ` · exit ${s.exitCode}` : ''}
                {s.lastError && s.lastError !== 'stopped' ? ` · ${s.lastError}` : ''}
              </span>
              <button onClick={() => restart(t.id, pane.id, s)}>
                <I.Restart /> Restart
              </button>
              <button onClick={() => closePane(t.id, pane.id)}>
                <I.Close /> Close
              </button>
            </div>
          )}
        </div>
      </section>
    );
  }

  const tabLabel = (t: L.Tab): string => {
    if (!L.autoNamed(t)) return t.name;
    const focused = L.findPane(t.root, t.focus);
    const pick = [focused, ...L.panes(t.root)].find((p) => p?.session && sessions.get(p.session));
    return pick?.session ? sessions.get(pick.session)!.title : 'New tab';
  };

  const closeTab = (id: string) => {
    setClosingTab(null);
    const closing = layoutRef.current?.tabs.find((x) => x.id === id);
    for (const p of closing ? L.panes(closing.root) : []) if (p.session) api.close(p.session).catch(() => undefined);
    setLayout((l) => {
      if (!l) return l;
      const t = l.tabs.find((x) => x.id === id);
      if (!t) return l;
      if (l.tabs.length === 1) {
        const fresh = L.newTab();
        return { ...l, tabs: [fresh], active: fresh.id };
      }
      const idx = l.tabs.findIndex((x) => x.id === id);
      const tabs = l.tabs.filter((x) => x.id !== id);
      return { ...l, tabs, active: l.active === id ? tabs[Math.max(0, idx - 1)].id : l.active };
    });
  };

  const startNewPane = (profile: string | null, dir: L.Dir = 'row') => {
    setPaneMenu(false);
    if (!tab) return;
    const pane = L.newPane();
    updateTab(tab.id, (t) => ({ ...t, root: L.split(t.root, t.focus, dir, pane), focus: pane.id, zoom: null }));
    if (profile) startIn(tab.id, pane.id, profile, config?.cwd);
  };

  // agentRows: every pane with a status, an agent profile, a parent or
  // children, most urgent first, children under their parent; panes no tab
  // holds (an agent started them) too, so they can be opened
  const agentRows = (): AgentRow[] => {
    const where = new Map<string, { tab: L.Tab; pane: L.PaneNode }>();
    for (const t of layout.tabs) for (const p of L.panes(t.root)) if (p.session) where.set(p.session, { tab: t, pane: p });
    const parents = new Set([...sessions.values()].map((x) => x.parent).filter(Boolean) as string[]);
    const items: (AgentRow & { s: Session; parent?: string })[] = [];
    for (const s of sessions.values()) {
      const at = where.get(s.id);
      if (!at && !live(s)) continue;
      const prof = profiles.find((x) => x.name === s.profile);
      if (!s.status && !prof?.agent && !s.parent && !parents.has(s.id)) continue;
      const word = S.shown(s);
      items.push({
        s,
        parent: s.parent,
        key: s.id,
        id: s.id,
        tabId: at?.tab.id ?? '',
        paneId: at?.pane.id ?? '',
        tab: at ? tabLabel(at.tab) : 'not in a tab',
        title: s.title,
        app: s.status?.app || prof?.label || s.profile,
        word,
        state: s.state,
        msg: s.status?.msg || (word === 'blocked' && s.status?.kind ? `waiting for ${s.status.kind}` : ''),
        since: s.statusSince,
        current: !!at && at.tab.id === layout.active && at.tab.focus === at.pane.id,
        depth: 0,
        labels: Object.entries(s.labels ?? {}),
        asking: !!s.asking && !askHidden.has(`${s.id}|${s.asking.since}`),
        askNote: askNotes[s.id],
      });
    }
    return S.nest(items, (a, b) => S.byUrgency(a.s, b.s)).map(({ s: _, parent: __, ...r }) => r);
  };

  const ModeIcon = pref === 'dark' ? I.Moon : pref === 'light' ? I.Sun : I.Monitor;

  // the pieces both layouts draw
  const tabBar = (shownTabs: Set<string> | null) => (
    <div className="tabs" role="tablist">
      {layout.tabs.map((t, i) => {
        if (shownTabs && !shownTabs.has(t.id)) return null;
        const count = L.panes(t.root).length;
        const liveCount = L.panes(t.root).filter((p) => p.session && live(sessions.get(p.session))).length;
        return (
          <div
            key={t.id}
            role="tab"
            aria-selected={t.id === layout.active}
            className={`tab ${t.id === layout.active ? 'is-active' : ''}`}
            data-status={S.tabStatus(tabSessions(t)) ?? tabState(t)}
            title={`Tab ${i + 1} · double-click to rename`}
            onMouseDown={() => setLayout((l) => (l ? { ...l, active: t.id } : l))}
            onDoubleClick={() => setRenaming(t.id)}
          >
            {(() => {
              const w = S.tabStatus(tabSessions(t));
              return w ? <span className={`dot st-${w}`} title={`Most urgent: ${w}`} /> : <span className={`dot dot-${tabState(t)}`} />;
            })()}
            {renaming === t.id ? (
              <input
                className="tab-rename"
                autoFocus
                defaultValue={L.autoNamed(t) ? '' : t.name}
                placeholder={tabLabel(t)}
                onFocus={(e) => e.target.select()}
                onBlur={(e) => {
                  const name = e.target.value.trim();
                  updateTab(t.id, (x) => (name ? { ...x, name, custom: true } : { ...x, name: '', custom: false }));
                  setRenaming(null);
                }}
                onKeyDown={(e) => {
                  e.stopPropagation();
                  if (e.key === 'Enter') (e.target as HTMLInputElement).blur();
                  if (e.key === 'Escape') setRenaming(null);
                }}
              />
            ) : (
              <span className={`tab-name ${L.autoNamed(t) ? 'is-auto' : ''}`}>{tabLabel(t)}</span>
            )}
            {count > 1 && (
              <span className="tab-count" title={`${count} panes`}>
                {count}
              </span>
            )}
            {closingTab === t.id ? (
              <span className="tab-confirm" onMouseDown={(e) => e.stopPropagation()}>
                Close{liveCount ? ` ${liveCount} running` : ''}?
                <button onClick={() => closeTab(t.id)}>Yes</button>
                <button onClick={() => setClosingTab(null)}>No</button>
              </span>
            ) : (
              renaming !== t.id && (
                <span className="tab-actions">
                  <button
                    title="Rename tab (^b ,)"
                    onMouseDown={(e) => e.stopPropagation()}
                    onClick={() => {
                      setLayout((l) => (l ? { ...l, active: t.id } : l));
                      setRenaming(t.id);
                    }}
                  >
                    <I.Pencil />
                  </button>
                  <button title="Close tab" onMouseDown={(e) => e.stopPropagation()} onClick={() => (liveCount ? setClosingTab(t.id) : closeTab(t.id))}>
                    <I.SmallClose />
                  </button>
                </span>
              )
            )}
          </div>
        );
      })}
      <button className="icon-btn" title="New tab (^b c)" onClick={newTabAction}>
        <I.Plus />
      </button>
    </div>
  );
  const paneMenuButton = () => (
    <div className="pane-menu-wrap">
      <button className={`new-pane-btn ${paneMenu ? 'is-open' : ''}`} title="Add a pane to this tab" onClick={() => setPaneMenu((o) => !o)}>
        <I.SplitRight />
        <span>New pane</span>
        <I.Chevron />
      </button>
      {paneMenu && (
        <>
          <div className="menu-scrim" onMouseDown={() => setPaneMenu(false)} />
          <div className="menu" role="menu">
            <div className="menu-label">Start in a new pane</div>
            {profiles
              .filter((p) => p.available)
              .map((p) => (
                <button key={p.name} role="menuitem" className="menu-item" onClick={() => startNewPane(p.name)}>
                  <span className={`menu-mono ${p.agent ? 'is-agent' : 'is-shell'}`}>{monogram(p)}</span>
                  <span className="menu-text">{p.label}</span>
                </button>
              ))}
            <div className="menu-sep" />
            <button role="menuitem" className="menu-item" onClick={() => startNewPane(null, 'row')}>
              <span className="menu-mono">
                <I.SplitRight />
              </span>
              <span className="menu-text">Empty pane, right</span>
              <kbd>^b %</kbd>
            </button>
            <button role="menuitem" className="menu-item" onClick={() => startNewPane(null, 'col')}>
              <span className="menu-mono">
                <I.SplitDown />
              </span>
              <span className="menu-text">Empty pane, below</span>
              <kbd>^b &quot;</kbd>
            </button>
          </div>
        </>
      )}
    </div>
  );
  const toolButtons = () => (
    <div className="tools">
      <button className={`icon-btn agents-btn ${agentsOpen ? 'is-open' : ''}`} title="Agents (^b a)" onClick={toggleAgents}>
        <I.Bell />
        {blocked > 0 && <span className="agents-btn-count">{blocked}</span>}
      </button>
      <button className="icon-btn" title="Tiled (^b space)" onClick={() => preset('tiled')}>
        <I.Grid />
      </button>
      <button className="icon-btn" title="Main and stack (^b m)" onClick={() => preset('main')}>
        <I.MainStack />
      </button>
      <button className="icon-btn" title={`Theme: ${pref}`} onClick={cycleMode}>
        <ModeIcon />
      </button>
      <button className="cmd-btn" title="Commands" onClick={() => setPalette(true)}>
        <I.Command />
        <span>Commands</span>
        <kbd>⌘K</kbd>
      </button>
    </div>
  );
  const workArea = () => (
    <div className="work">
      <main className="tab-bodies">
        {layout.tabs.map((t) => (
          <div key={t.id} className={`tab-body ${t.id === layout.active ? 'is-active' : ''}`}>
            {renderTab(t)}
          </div>
        ))}
      </main>
      {agentsOpen && (
        <Agents
          rows={agentRows()}
          lost={lost.filter((x) => !placedSessions.has(x.id))}
          lostError={lostError}
          onAnswer={(r, d) => {
            const s = sessions.get(r.id);
            if (s) answer(s, d);
          }}
          onLost={(lp, a) => lostAction(lp, a)}
          now={now}
          permission={permission}
          notify={notifyOn}
          sound={soundOn}
          onPick={(r) => {
            if (r.tabId) goTo(r.tabId, r.paneId);
            else openUnplaced(r.id);
            markSeen(sessions.get(r.id));
          }}
          onNotify={setNotify}
          onSound={setSound}
          onClose={toggleAgents}
        />
      )}
    </div>
  );
  const overlays = () => (
    <>
      {authLost && (
        <div className="scrim">
          <div className="help">
            <h2>This link has expired</h2>
            <p className="help-sub">
              The forge was restarted, so it has a new token. Run <code>la open</code> (or <code>la status</code>) for a fresh link. Your panes are still running unless the forge was stopped.
            </p>
          </div>
        </div>
      )}
      {palette && <Palette actions={actions} onClose={() => setPalette(false)} />}
      {help && <Help onClose={() => setHelp(false)} />}
    </>
  );

  // the rail layout's model: spaces, crew and counts
  const pool = [...sessions.values()].filter((x) => placedSessions.has(x.id) || live(x));
  const spaceList = R.spaces(pool);
  const crewList = R.crew(pool, profiles);
  const c = R.counts(pool);
  const tabPanes = layout!.tabs.map((t) => ({ id: t.id, sessions: L.panes(t.root).map((p) => p.session).filter(Boolean) as string[] }));
  const shown = railSpace === null ? null : R.tabsIn(tabPanes, sessions, railSpace).add(layout!.active);
  const focusedPane = focusedSession ? sessions.get(focusedSession) : undefined;
  const spaceName = railSpace ?? (focusedPane ? R.spaceOf(focusedPane) : 'All spaces');
  const pickSpace = (name: string | null) => {
    setRailSpace(name);
    setRailOpen(false);
    if (name === null) return;
    const ids = R.tabsIn(tabPanes, sessions, name);
    const first = layout!.tabs.find((t) => ids.has(t.id));
    if (!first || first.id === layout!.active) return;
    const pane = L.panes(first.root).find((p) => p.session && sessions.get(p.session) && R.spaceOf(sessions.get(p.session)!) === name);
    goTo(first.id, pane?.id ?? first.focus);
  };
  const goToSession = (s: Session) => {
    const at = locate(s.id);
    if (at) goTo(at.tab.id, at.pane.id);
    else openUnplaced(s.id);
    markSeen(s);
    setRailOpen(false);
  };
  const startSpace = async () => {
    if (!newSpace) return;
    const name = newSpace.name.trim();
    if (!name) return setNewSpace({ ...newSpace, error: 'Name the space.' });
    const shell = profiles.find((p) => !p.agent && p.available)?.name ?? 'shell';
    try {
      const s = await api.start({ profile: shell, cwd: newSpace.cwd.trim() || undefined, labels: { space: name }, cols: 100, rows: 30 });
      setSessions((m) => new Map(m).set(s.id, s));
      setLayout((l) => {
        if (!l) return l;
        const t = { ...L.newTab(name, s.id), custom: true };
        return { ...l, tabs: [...l.tabs, t], active: t.id };
      });
      setNewSpace(null);
      setRailSpace(name);
      setRailOpen(false);
    } catch (err) {
      setNewSpace({ ...newSpace, error: (err as Error).message });
    }
  };

  const rail = layoutKind === 'rail';

  // Both layouts share one tree: the panes sit at the same place in each, so
  // switching skins never remounts a terminal. In classic the rail parts are
  // absent and .forge-main is display: contents.
  return (
    <div className={`forge ${embedded ? 'is-embedded' : ''} ${rail ? 'layout-rail' : ''} ${rail && railOpen ? 'is-rail-open' : ''}`} data-layout={layoutKind}>
      {rail ? (
        <aside className="rail" aria-label="Spaces and crew">
          <div className="rail-machine">
            <span className="rail-label">Machine</span>
            <span className="rail-host">{config?.host?.replace(/\.local(domain)?$/, '') ?? 'forge'}</span>
          </div>
          <section className="rail-section rail-spaces">
            <div className="rail-title">Spaces</div>
            <button className={`rail-space ${railSpace === null ? 'is-active' : ''}`} data-space="" onClick={() => pickSpace(null)}>
              <span className="rail-space-name">All</span>
              <span className="rail-count">{pool.length}</span>
            </button>
            {spaceList.map((sp, i) => (
              <button key={sp.name} className={`rail-space ${railSpace === sp.name ? 'is-active' : ''}`} data-space={sp.name} data-index={i % 8} onClick={() => pickSpace(sp.name)} title={sp.name}>
                <span className="rail-space-name">{sp.name}</span>
                <span className="rail-count">{sp.count}</span>
              </button>
            ))}
            {newSpace ? (
              <form
                className="rail-new-space"
                onSubmit={(e) => {
                  e.preventDefault();
                  startSpace();
                }}
              >
                <input autoFocus placeholder="Space name" value={newSpace.name} onChange={(e) => setNewSpace({ ...newSpace, name: e.target.value, error: '' })} onKeyDown={(e) => e.key === 'Escape' && setNewSpace(null)} />
                <input placeholder="Folder" value={newSpace.cwd} spellCheck={false} onChange={(e) => setNewSpace({ ...newSpace, cwd: e.target.value, error: '' })} onKeyDown={(e) => e.key === 'Escape' && setNewSpace(null)} />
                <span className="rail-form-actions">
                  <button type="submit">Start a shell</button>
                  <button type="button" onClick={() => setNewSpace(null)}>
                    Cancel
                  </button>
                </span>
                {newSpace.error && <span className="rail-error">{newSpace.error}</span>}
              </form>
            ) : (
              <button className="rail-add" onClick={() => setNewSpace({ name: '', cwd: config?.cwd ?? '', error: '' })}>
                + New space
              </button>
            )}
          </section>
          <section className="rail-section rail-crew">
            <div className="rail-title">
              Crew <span className="rail-count">{crewList.length}</span>
            </div>
            {crewList.map((s) => {
              const word = S.shown(s);
              const prof = profiles.find((p) => p.name === s.profile);
              const msg = s.status?.msg || (word === 'blocked' && s.status?.kind ? `waiting for ${s.status.kind}` : '');
              const current = s.id === focusedSession;
              return (
                <div
                  key={s.id}
                  role="button"
                  tabIndex={0}
                  className={`crew-row ${current ? 'is-current' : ''}`}
                  data-status={word ?? s.state}
                  title={msg || undefined}
                  onClick={() => goToSession(s)}
                  onKeyDown={(e) => e.key === 'Enter' && e.target === e.currentTarget && goToSession(s)}
                >
                  <span className="crew-bar" />
                  <span className="crew-body">
                    <span className="crew-name">
                      {s.status?.app || prof?.label || s.profile} · {s.title}
                    </span>
                    <span className="crew-sub">
                      {word ?? s.state} · {R.spaceOf(s)}
                    </span>
                    {msg && <span className="crew-msg">{msg}</span>}
                    <AskButtons asking={!!s.asking && !askHidden.has(`${s.id}|${s.asking.since}`)} note={askNotes[s.id]} onAnswer={(d) => answer(s, d)} />
                  </span>
                </div>
              );
            })}
            {crewList.length === 0 && <p className="rail-empty">No agents yet.</p>}
          </section>
        </aside>
      ) : null}
      {rail ? <div className="rail-scrim" onMouseDown={() => setRailOpen(false)} /> : null}
      <div className={rail ? 'rail-main' : 'forge-main'}>
        {rail ? (
          <header className="rail-header">
            <button className="icon-btn rail-toggle" title="Spaces and crew" onClick={() => setRailOpen((o) => !o)}>
              <I.Menu />
            </button>
            <span className="rail-when">
              <span className="rail-date">{clock.toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric' })}</span>
              <span className="rail-time">{clock.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</span>
            </span>
            <span className="rail-space-title">{spaceName}</span>
            <span className="grow" />
            {toolButtons()}
          </header>
        ) : null}
        <nav className={rail ? 'topbar rail-tabs' : 'topbar'}>
          {!rail && !embedded && (
            <div className="brand">
              <I.Mark />
              <span className="brand-name">
                La<b>Forge</b>
              </span>
            </div>
          )}
          {tabBar(rail ? shown : null)}
          {paneMenuButton()}
          {!rail && <span className="grow" />}
          {!rail && toolButtons()}
        </nav>

        {workArea()}

        {rail ? (
          <footer className="statusbar rail-footer">
            <span className="rf-counts">
              <span className="rf-count" data-status="working">
                {c.working} working
              </span>
              <span className="rf-sep">·</span>
              <span className="rf-count" data-status="blocked">
                {c.needYou} need you
              </span>
              <span className="rf-sep">·</span>
              <span className="rf-count" data-status="done">
                {c.done} done
              </span>
            </span>
            <span className="grow" />
            {prefix && <span className="sb-prefix">PREFIX</span>}
            {!connected && !authLost && <span className="sb-down">disconnected — retrying</span>}
            <button className="sb-help" onClick={() => setHelp(true)}>
              ^b ? keys
            </button>
            <span className="sb-clock">{clock.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</span>
          </footer>
        ) : (
          <footer className="statusbar">
            <span className="sb-session">forge</span>
            {layout.tabs.map((t, i) => (
              <span key={t.id} className={`sb-tab ${t.id === layout.active ? 'is-active' : ''}`}>
                {i + 1}:{tabLabel(t)}
                {t.id === layout.active ? '*' : ''}
                {t.zoom ? 'Z' : ''}
              </span>
            ))}
            <span className="grow" />
            {prefix && <span className="sb-prefix">PREFIX</span>}
            {!connected && !authLost && <span className="sb-down">disconnected — retrying</span>}
            <span className="sb-counts">
              {blocked > 0 && (
                <>
                  <span className="dot st-blocked" /> {blocked} blocked{' '}
                </>
              )}
              <span className="dot dot-running" /> {counts.running} running <span className="dot dot-idle" /> {counts.idle} idle
              {counts.ended ? (
                <>
                  {' '}
                  <span className="dot dot-complete" /> {counts.ended} ended
                </>
              ) : null}
            </span>
            <button className="sb-help" onClick={() => setHelp(true)}>
              ^b ? keys
            </button>
            {config && (
              <span className="sb-host">
                {config.user}@{config.host.replace(/\.local$/, '')}
              </span>
            )}
            <span className="sb-clock">{clock.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</span>
          </footer>
        )}
      </div>
      {overlays()}
    </div>
  );
}

// shifted is the character a key makes, from its physical key when a
// synthetic or unusual layout reports the unshifted one.
const shiftedCodes: Record<string, string> = { Digit5: '%', Quote: '"', Slash: '?', Semicolon: ':', Equal: '+', Minus: '_', Backslash: '|' };
function shifted(e: KeyboardEvent): string {
  if (e.shiftKey && e.key.length === 1 && !/[%"?:+_|A-Z]/.test(e.key) && shiftedCodes[e.code]) return shiftedCodes[e.code];
  return e.key;
}

function shortPath(p: string, base?: string): string {
  const home = p.match(/^\/Users\/[^/]+|^\/home\/[^/]+/)?.[0];
  let out = home ? '~' + p.slice(home.length) : p;
  if (base && p === base) out = out || '~';
  const parts = out.split('/');
  return parts.length > 3 ? '…/' + parts.slice(-2).join('/') : out;
}
