// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// Skins: how the forge looks, installable (docs/skins.md). A skin is a
// manifest, stylesheets, fonts and images; never code. The client picks the
// browser's own choice if the forge still has it, else the forge's default,
// adds the skin's stylesheets, marks the root with data-skin and
// data-layout, and gives the terminals the skin's palette.
import type { ITheme } from '@xterm/xterm';
import { terminalTheme, type Mode } from './theme';

export type SkinLayout = 'classic' | 'rail';

export interface Skin {
  name: string;
  label: string;
  description?: string;
  version?: string;
  author?: string;
  license?: string;
  layout?: SkinLayout;
  /** mode is legacy: every skin now draws both modes, so the client ignores it. */
  mode?: 'dark' | 'light' | 'both';
  css?: string[];
  /** terminal is the terminal palette per mode; a flat palette (the old shape) is the skin's one mode, or both. */
  terminal?: SkinTerminal | TerminalColours;
  /** source is where an installed skin came from (a folder or a GitHub repo). */
  source?: string;
  builtin: boolean;
}

export type TerminalColours = Partial<Record<Exclude<keyof ITheme, 'extendedAnsi'>, string>>;
export interface SkinTerminal {
  dark?: TerminalColours;
  light?: TerminalColours;
}

/** InstallResult is POST /api/skins/install's answer. */
export interface InstallResult {
  installed: Skin[];
  failed: { path: string; reason: string }[];
}

export interface SkinList {
  default: string;
  skins: Skin[];
}

/** classic is the client's own look; it needs nothing from the forge. */
export const classic: Skin = { name: 'classic', label: 'Classic', layout: 'classic', mode: 'both', builtin: true };

const key = 'laforge.skin';

export function loadChoice(): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null; // storage blocked
  }
}

export function saveChoice(name: string | null) {
  try {
    if (name) localStorage.setItem(key, name);
    else localStorage.removeItem(key);
  } catch {
    /* storage blocked */
  }
}

/** pick is the skin to show: the browser's choice if the forge has it, else the forge's default, else classic. */
export function pick(list: SkinList | null, choice: string | null): Skin {
  const skins = list?.skins ?? [];
  const find = (name: string | null | undefined) => (name ? skins.find((s) => s.name === name) : undefined);
  return find(choice) ?? find(list?.default) ?? find('classic') ?? classic;
}

/** layoutOf is the skin's layout; anything unknown is classic. */
export const layoutOf = (skin: Skin): SkinLayout => (skin.layout === 'rail' ? 'rail' : 'classic');

/** terminalFor is a skin's own terminal colours for a mode, if it has any. */
export function terminalFor(skin: Skin, mode: Mode): TerminalColours | undefined {
  const t = skin.terminal;
  if (!t) return undefined;
  if ('dark' in t || 'light' in t) {
    const v = (t as SkinTerminal)[mode];
    return v && typeof v === 'object' ? v : undefined;
  }
  // the old flat shape: one palette for the mode the skin declared, or both
  if (skin.mode === 'dark' || skin.mode === 'light') return skin.mode === mode ? (t as TerminalColours) : undefined;
  return t as TerminalColours;
}

/** terminalOf is the skin's terminal palette for the mode over the base (the client's palette for that mode). */
export function terminalOf(skin: Skin, base: ITheme, mode: Mode): ITheme {
  const own = terminalFor(skin, mode);
  return own ? ({ ...base, ...own } as ITheme) : base;
}

/** cssUrls are a skin's stylesheets, as the forge serves them. */
export function cssUrls(skin: Skin, base = ''): string[] {
  return (skin.css ?? []).map((c) => `${base}/skins/${encodeURIComponent(skin.name)}/${c.split('/').map(encodeURIComponent).join('/')}`);
}

/**
 * apply puts a skin on the page: its stylesheets (the new ones load before
 * the old ones go, so a switch does not flash) and the root's data-skin and
 * data-layout. It rejects when a stylesheet fails to load; the caller then
 * applies classic.
 */
export async function apply(skin: Skin, base = '', doc: Document = document): Promise<void> {
  const root = doc.documentElement;
  const old = [...doc.querySelectorAll<HTMLLinkElement>('link[data-skin-css]')];
  const links = cssUrls(skin, base).map((href) => {
    const l = doc.createElement('link');
    l.rel = 'stylesheet';
    l.href = href;
    l.dataset.skinCss = skin.name;
    return l;
  });
  const loaded = Promise.all(
    links.map(
      (l) =>
        new Promise<void>((ok, fail) => {
          l.onload = () => ok();
          l.onerror = () => fail(new Error(`${skin.name}: ${l.href} did not load`));
        }),
    ),
  );
  for (const l of links) doc.head.appendChild(l);
  try {
    await loaded;
  } catch (err) {
    for (const l of links) l.remove();
    throw err;
  }
  for (const l of old) l.remove();
  root.dataset.skin = skin.name;
  root.dataset.layout = layoutOf(skin);
}

export interface Swatch {
  background: string;
  foreground: string;
  accent: string;
}

/** swatches are a skin's light and dark strips: its terminal colours, else the client's palette for that mode. */
export function swatches(skin: Skin): Record<Mode, Swatch> {
  const one = (mode: Mode): Swatch => {
    const t = terminalOf(skin, terminalTheme(mode), mode);
    const base = terminalTheme(mode);
    return { background: t.background ?? base.background!, foreground: t.foreground ?? base.foreground!, accent: t.cursor ?? base.cursor! };
  };
  return { light: one('light'), dark: one('dark') };
}

/** InstallState is the theme panel's install section. */
export type InstallState =
  | { kind: 'idle' }
  | { kind: 'busy'; source: string }
  | { kind: 'done'; result: InstallResult }
  | { kind: 'unsupported' }
  | { kind: 'error'; reason: string };

/**
 * installState runs an install and says how it went. A forge without the
 * install route answers 405 (or a plain 404 with no JSON error code): it
 * cannot install themes.
 */
export async function installState(install: (source: string) => Promise<InstallResult>, source: string): Promise<InstallState> {
  try {
    const r = await install(source);
    return { kind: 'done', result: { installed: r?.installed ?? [], failed: r?.failed ?? [] } };
  } catch (err) {
    const e = err as { status?: number; code?: string; message?: string };
    if (e.status === 405 || e.status === 501 || (e.status === 404 && (!e.code || e.code === 'error'))) return { kind: 'unsupported' };
    return { kind: 'error', reason: e.message || 'The install failed.' };
  }
}
