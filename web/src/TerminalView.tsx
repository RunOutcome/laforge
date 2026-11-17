// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// TerminalView draws one forge session with xterm.js over its WebSocket:
// the replay buffer first, then live output. Input and resizes go back as
// JSON text frames. A dropped connection reconnects and redraws from the
// buffer; a normal close with "ended" means the session is over.
import { useEffect, useRef } from 'react';
import { Terminal, type ITheme } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { WebLinksAddon } from '@xterm/addon-web-links';
import '@xterm/xterm/css/xterm.css';
import type { ForgeApi } from './api';
import { terminalTheme, type Mode } from './theme';

export interface TerminalHandle {
  focus(): void;
  send(data: string): void;
}

interface Props {
  api: ForgeApi;
  session: string;
  mode: Mode;
  focused: boolean;
  fontSize: number;
  /** onKey sees every keydown first; true means the app took it. */
  onKey: (e: KeyboardEvent) => boolean;
  onFocus: () => void;
  onEnded: () => void;
  register: (h: TerminalHandle | null) => void;
  /** readOnly watches only: no input, no resize; the terminal keeps the pane's own size (cols, rows). */
  readOnly?: boolean;
  cols?: number;
  rows?: number;
  /** theme is the skin's terminal palette; without it, the mode's own. */
  theme?: ITheme;
}

export function TerminalView({ api, session, mode, focused, fontSize, onKey, onFocus, onEnded, register, readOnly = false, cols, rows, theme }: Props) {
  const host = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const refitRef = useRef<() => void>(() => undefined);
  const handlers = useRef({ onKey, onFocus, onEnded });
  handlers.current = { onKey, onFocus, onEnded };
  const focusedRef = useRef(focused);
  focusedRef.current = focused;
  const fixed = useRef({ readOnly, cols, rows });
  fixed.current = { readOnly, cols, rows };

  useEffect(() => {
    const el = host.current;
    if (!el) return;
    const term = new Terminal({
      fontFamily: '"IBM Plex Mono", ui-monospace, SFMono-Regular, Menlo, monospace',
      fontSize,
      lineHeight: 1.15,
      cursorBlink: true,
      scrollback: 10000,
      allowProposedApi: true,
      macOptionIsMeta: true,
      theme: theme ?? terminalTheme(mode),
      disableStdin: readOnly,
      cursorInactiveStyle: readOnly ? 'none' : undefined,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.loadAddon(new WebLinksAddon());
    term.open(el);
    termRef.current = term;
    const focusLater = setTimeout(() => focusedRef.current && term.focus(), 30);

    let swallowed = false;
    term.attachCustomKeyEventHandler((e) => {
      if (e.type === 'keydown') {
        swallowed = handlers.current.onKey(e);
        if (swallowed) e.preventDefault();
        return !swallowed;
      }
      return !swallowed;
    });

    let socket: WebSocket | null = null;
    let closed = false;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let size = { cols: 0, rows: 0 };

    const sendResize = () => {
      if (fixed.current.readOnly) return;
      if (socket?.readyState === WebSocket.OPEN && term.cols > 0) {
        socket.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
      }
    };
    const refit = () => {
      const f = fixed.current;
      if (f.readOnly) {
        // a viewer draws the pane at its own size: fitting would rewrap it
        if (f.cols && f.rows && (term.cols !== f.cols || term.rows !== f.rows)) term.resize(f.cols, f.rows);
        return;
      }
      if (el.offsetWidth < 20 || el.offsetHeight < 20) return;
      try {
        fit.fit();
      } catch {
        return;
      }
      if (term.cols !== size.cols || term.rows !== size.rows) {
        size = { cols: term.cols, rows: term.rows };
        sendResize();
      }
    };

    refitRef.current = refit;
    let failures = 0;
    const connect = () => {
      const ws = new WebSocket(api.streamUrl(session));
      ws.binaryType = 'arraybuffer';
      socket = ws;
      let first = true;
      ws.onopen = () => {
        failures = 0;
        size = { cols: 0, rows: 0 };
        refit();
        sendResize();
      };
      ws.onmessage = (ev) => {
        if (first) {
          term.reset();
          first = false;
        }
        if (ev.data instanceof ArrayBuffer) term.write(new Uint8Array(ev.data));
        else term.write(String(ev.data));
      };
      ws.onclose = (ev) => {
        if (closed) return;
        if (ev.code === 1000 && ev.reason === 'ended') {
          handlers.current.onEnded();
          return;
        }
        // A refused upgrade (stale token, session gone) shows only as 1006;
        // ask the API which it was before trying again, and back off.
        failures++;
        api
          .sessions()
          .then((list) => {
            if (closed) return;
            if (!list.some((x) => x.id === session)) {
              handlers.current.onEnded();
              return;
            }
            retry = setTimeout(connect, ev.code === 4408 ? 0 : Math.min(1000 * failures, 10000));
          })
          .catch((err) => {
            if (closed) return;
            if (err?.status === 401) {
              handlers.current.onEnded(); // the forge shows the expired-link notice
              return;
            }
            retry = setTimeout(connect, Math.min(1000 * failures, 10000));
          });
      };
    };
    connect();

    const send = (data: string) => {
      if (!fixed.current.readOnly && socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: 'input', data }));
    };
    const input = term.onData((data) => {
      if (fixed.current.readOnly) return;
      if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: 'input', data }));
    });
    const binary = term.onBinary((data) => {
      if (fixed.current.readOnly) return;
      if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: 'input', data }));
    });
    const focusIn = () => handlers.current.onFocus();
    el.addEventListener('focusin', focusIn);

    const observer = new ResizeObserver(() => refit());
    observer.observe(el);

    register({
      focus: () => term.focus(),
      send,
    });

    return () => {
      closed = true;
      clearTimeout(retry);
      clearTimeout(focusLater);
      observer.disconnect();
      el.removeEventListener('focusin', focusIn);
      input.dispose();
      binary.dispose();
      socket?.close();
      register(null);
      term.dispose();
      termRef.current = null;
    };
    // a new session is a new terminal; mode and font are applied below
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [api, session]);

  useEffect(() => {
    if (termRef.current) termRef.current.options.theme = theme ?? terminalTheme(mode);
  }, [mode, theme]);

  useEffect(() => {
    if (!termRef.current) return;
    termRef.current.options.fontSize = fontSize;
    refitRef.current(); // new glyph size: new cols/rows, and the pty told
  }, [fontSize]);

  useEffect(() => {
    if (focused) termRef.current?.focus();
  }, [focused]);

  useEffect(() => {
    if (readOnly) refitRef.current(); // the pane was resized by its owner
  }, [readOnly, cols, rows]);

  return <div className={`term-host ${readOnly ? 'is-read-only' : ''}`} ref={host} />;
}
