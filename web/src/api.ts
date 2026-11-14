// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// The forge's HTTP API. base is '' for the standalone client; a host that
// embeds the forge (the Outcome Arena) passes the forge's own origin.

export interface Profile {
  name: string;
  label: string;
  description: string;
  command: string;
  agent: boolean;
  available: boolean;
  reason?: string;
}

export type PaneState = 'starting' | 'running' | 'idle' | 'complete' | 'failed';

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
}

export interface Config {
  cwd: string;
  host: string;
  user: string;
  version: string;
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
    if (res.status === 204) return undefined as T;
    return (await res.json()) as T;
  }

  profiles = () => this.call<Profile[]>('GET', '/api/profiles');
  sessions = () => this.call<Session[]>('GET', '/api/sessions');
  config = () => this.call<Config>('GET', '/api/config');
  start = (req: { profile: string; cwd?: string; title?: string; cols?: number; rows?: number }) => this.call<Session>('POST', '/api/sessions', req);
  rename = (id: string, title: string) => this.call<Session>('PATCH', `/api/sessions/${encodeURIComponent(id)}`, { title });
  close = (id: string, force = false) => this.call<void>('DELETE', `/api/sessions/${encodeURIComponent(id)}${force ? '?force=1' : ''}`);
  layout = () => this.call<unknown>('GET', '/api/layout');
  saveLayout = (layout: unknown) => this.call<void>('PUT', '/api/layout', layout);

  streamUrl(id: string): string {
    const origin = this.base || window.location.origin;
    return origin.replace(/^http/, 'ws') + `/api/sessions/${encodeURIComponent(id)}/stream`;
  }
}
