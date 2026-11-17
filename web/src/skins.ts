// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// Skins: how the forge looks, installable (docs/skins.md). A skin is a
// manifest, stylesheets, fonts and images; never code. The client picks the
// browser's own choice if the forge still has it, else the forge's default,
// adds the skin's stylesheets, marks the root with data-skin and
// data-layout, and gives the terminals the skin's palette.
import type { ITheme } from '@xterm/xterm';
import type { Mode } from './theme';

export type SkinLayout = 'classic' | 'rail';

export type TerminalColours = Partial<Record<Exclude<keyof ITheme, 'extendedAnsi'>, string>>;

export interface Skin {
  name: string;
  label: string;
  description?: string;
  version?: string;
  author?: string;
  license?: string;
  layout?: SkinLayout;
  css?: string[];
  /** terminal is the xterm palette for each mode; every skin draws both. */
  terminal?: { dark?: TerminalColours; light?: TerminalColours };
  builtin: boolean;
}

export interface SkinList {
  default: string;
  skins: Skin[];
}

/** classic is the client's own look; it needs nothing from the forge. */
export const classic: Skin = { name: 'classic', label: 'Classic', layout: 'classic', builtin: true };

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

/** modeOf is the mode to draw. Every skin draws light and dark, so it is the viewer's. */
export const modeOf = (_skin: Skin, viewer: Mode): Mode => viewer;

/** terminalOf is the skin's terminal palette for the mode, over the base one. */
export function terminalOf(skin: Skin, base: ITheme, mode: Mode): ITheme {
  const own = skin.terminal?.[mode];
  if (!own) return base;
  return { ...base, ...own } as ITheme;
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
