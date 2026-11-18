// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// The forge's HTTP API. base is '' for the standalone client; a host that
// embeds the forge (the Outcome Arena) passes the forge's own origin.
import type { Collection, Offer } from './collections';
import type { InstallResult, Skin, SkinList } from './skins';

export interface Profile {
  name: string;
  label: string;
  description: string;
  command: string;
  agent: boolean;
  args?: string[];
  env?: string[];
  resume?: string[];
  available: boolean;
  reason?: string;
  /** builtin: every forge offers it; custom: saved in the forge's profiles.json (added, or changing a built-in). */
  builtin?: boolean;
  custom?: boolean;
}

export type PaneState = 'starting' | 'running' | 'idle' | 'complete' | 'failed';

/** A program's report about itself (OSC 7501 or a hook), in its own words. */
export type StatusWord = 'idle' | 'working' | 'done' | 'blocked' | 'error';

export interface Status {
  state: StatusWord;
  /** kind: what a blocked program waits for. */
  kind?: 'permission' | 'question' | 'auth';
  progress?: number;
  app?: string;
  title?: string;
  msg?: string;
  source?: 'hook';
}

export interface Session {
  id: string;
  command: string;
  state: PaneState;
  exitCode?: number;
  lastError?: string;
  stopped: boolean;
  startedAt: string;
  lastOutputAt: string;
  endedAt?: string;
  columns: number;
  rows: number;
  bytesOut: number;
  pid?: number;
  profile: string;
  title: string;
  cwd: string;
  /** after: the agent profile this pane ran before it became a shell; agentExit: its exit code. */
  after?: string;
  agentExit?: number;
  /** status: the program's own report; absent when it reports nothing. records: its child records. */
  status?: Status;
  records?: Record<string, Status>;
  /** statusSince: when the root state last changed. */
  statusSince?: string;
  turn?: number;
  reported?: boolean;
  /** seen: a viewer has looked since the status last changed. */
  seen?: boolean;
  /** agentSession: the agent's own session, what resuming continues. */
  agentSession?: AgentSession;
  /** held: the pane runs in a holder and keeps running if the forge restarts. */
  held?: boolean;
  /** parent: the pane this one was started from (la run); labels: its starter's names for it. */
  parent?: string;
  outlive?: boolean;
  notes?: boolean;
  labels?: Record<string, string>;
  /** asking: the permission its agent waits for a person to answer (answer). */
  asking?: { msg: string; since: string };
}

export interface AgentSession {
  agent: string;
  sessionId: string;
}

/** LostPane is a pane whose program is gone (la down, a reboot), offered to resume. */
export interface LostPane {
  id: string;
  profile: string;
  title: string;
  cwd: string;
  agentSession?: AgentSession;
  startedAt: string;
  lostAt: string;
  /** resumable: starting it again continues the agent's own session. */
  resumable: boolean;
}

/** Who is the viewer: the forge's full token, or a read-only share of one pane. */
export type Whoami = { mode: 'full' } | { mode: 'view'; pane: string };

/** Config is GET /api/config: the forge's settings and, read-only, what it is. An older forge sends cwd, host, user and version only. */
export interface Config {
  cwd: string;
  host: string;
  user: string;
  version: string;
  /** cwdSaved: cwd is saved and survives a restart; false: it is where la up ran. */
  cwdSaved?: boolean;
  skin?: string;
  scenes?: boolean;
  /** spaces: where the spaces show, "top" or "rail"; "" (or absent, from an older forge) lets the skin suggest. */
  spaces?: string;
  profiles?: Profile[];
  port?: number;
  home?: string;
  readOnly?: string[];
}

/** ConfigPatch is a PUT /api/config: each key left out is kept. */
export interface ConfigPatch {
  cwd?: string;
  skin?: string;
  scenes?: boolean;
  spaces?: '' | 'top' | 'rail';
}

/** Share is one read-only link to a pane. */
export interface Share {
  token: string;
  pane: string;
  created: string;
  expires: string;
}

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    reason: string,
  ) {
    super(reason);
  }
}

export class ForgeApi {
  constructor(public base = '') {}

  private async call<T>(method: string, path: string, body?: unknown): Promise<T> {
    const res = await fetch(this.base + path, {
      method,
      credentials: 'include',
      headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    if (!res.ok) {
      let code = 'error';
      let reason = res.statusText;
      try {
        const j = await res.json();
        code = j.error ?? code;
        reason = j.reason ?? reason;
      } catch {
        /* not JSON */
      }
      throw new ApiError(res.status, code, reason);
    }
    if (res.status === 204 || res.status === 202) return undefined as T;
    return (await res.json()) as T;
  }

  profiles = () => this.call<Profile[]>('GET', '/api/profiles');
  sessions = () => this.call<Session[]>('GET', '/api/sessions');
  config = () => this.call<Config>('GET', '/api/config');
  setConfig = (patch: ConfigPatch) => this.call<Config>('PUT', '/api/config', patch);
  saveProfile = (p: Profile) => this.call<Profile>('PUT', `/api/profiles/${encodeURIComponent(p.name)}`, p);
  removeProfile = (name: string) => this.call<void>('DELETE', `/api/profiles/${encodeURIComponent(name)}`);
  shares = () => this.call<Share[]>('GET', '/api/shares');
  revokeShare = (token: string) => this.call<void>('DELETE', `/api/shares/${encodeURIComponent(token)}`);
  /** restart asks the forge to restart itself, panes kept; it answers before it goes. */
  restart = () => this.call<void>('POST', '/api/restart?respawn=1');
  health = () => this.call<{ ok: boolean; version: string; pid: number; startedAt: string }>('GET', '/api/health');
  start = (req: { profile: string; cwd?: string; title?: string; cols?: number; rows?: number; resume?: string; labels?: Record<string, string> }) => this.call<Session>('POST', '/api/sessions', req);
  rename = (id: string, title: string) => this.call<Session>('PATCH', `/api/sessions/${encodeURIComponent(id)}`, { title });
  close = (id: string, force = false) => this.call<void>('DELETE', `/api/sessions/${encodeURIComponent(id)}${force ? '?force=1' : ''}`);
  seen = (id: string) => this.call<Session>('POST', `/api/sessions/${encodeURIComponent(id)}/seen`);
  whoami = () => this.call<Whoami>('GET', '/api/whoami');
  lost = () => this.call<LostPane[]>('GET', '/api/lost');
  dismissLost = (id: string) => this.call<void>('DELETE', `/api/lost/${encodeURIComponent(id)}`);
  answer = (id: string, decision: 'allow' | 'deny') => this.call<{ decision: string }>('POST', `/api/sessions/${encodeURIComponent(id)}/answer`, { decision });
  skins = () => this.call<SkinList>('GET', '/api/skins');
  setDefaultSkin = (name: string) => this.call<{ default: string }>('PUT', '/api/skins/default', { name });
  installSkins = (source: string) => this.call<InstallResult>('POST', '/api/skins/install', { source });
  allowSkinCode = (name: string, allow: boolean) => this.call<Skin>('POST', `/api/skins/${encodeURIComponent(name)}/code`, { allow });
  setScenes = (on: boolean) => this.call<{ scenes: boolean }>('PUT', '/api/skins/scenes', { on });
  removeSkin = (name: string) => this.call<void>('DELETE', `/api/skins/${encodeURIComponent(name)}`);
  collections = () => this.call<Collection[]>('GET', '/api/skins/collections');
  addCollection = (source: string) => this.call<Collection>('POST', '/api/skins/collections', { source });
  removeCollection = (id: string) => this.call<void>('DELETE', `/api/skins/collections/${encodeURIComponent(id)}`);
  collectionSkins = (id: string, refresh = false) => this.call<Offer[]>('GET', `/api/skins/collections/${encodeURIComponent(id)}/skins${refresh ? '?refresh=1' : ''}`);
  /** installFrom installs skins from a collection by name; no names installs all. */
  installFrom = (collection: string, names: string[]) => this.call<InstallResult>('POST', '/api/skins/install', { collection, names });
  layout = () => this.call<unknown>('GET', '/api/layout');
  saveLayout = (layout: unknown) => this.call<void>('PUT', '/api/layout', layout);

  streamUrl(id: string): string {
    const origin = this.base || window.location.origin;
    return origin.replace(/^http/, 'ws') + `/api/sessions/${encodeURIComponent(id)}/stream`;
  }
}
