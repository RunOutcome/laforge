// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// Terminal colours drawn from Outcome's tokens (outcome-ui kit/ui themes/outcome.css).
import type { ITheme } from '@xterm/xterm';

export type Mode = 'light' | 'dark';
export type ModePref = Mode | 'system';

export const darkTerminal: ITheme = {
  background: '#0e1115',
  foreground: '#e8ebf1',
  cursor: '#4db3de',
  cursorAccent: '#0e1115',
  selectionBackground: 'rgba(77, 179, 222, 0.28)',
  black: '#20242b',
  red: '#e5484d',
  green: '#3fb67a',
  yellow: '#e5a13a',
  blue: '#6f8ff0',
  magenta: '#9a85ea',
  cyan: '#4db3de',
  white: '#c9ced8',
  brightBlack: '#5b6272',
  brightRed: '#ec6266',
  brightGreen: '#5fcf95',
  brightYellow: '#f1bb5c',
  brightBlue: '#94acf6',
  brightMagenta: '#ab99ef',
  brightCyan: '#82cae8',
  brightWhite: '#f4f6fa',
};

export const lightTerminal: ITheme = {
  background: '#fbfcfd',
  foreground: '#161b26',
  cursor: '#337693',
  cursorAccent: '#fbfcfd',
  selectionBackground: 'rgba(51, 118, 147, 0.2)',
  black: '#161b26',
  red: '#c43c41',
  green: '#177e43',
  yellow: '#ac5d04',
  blue: '#2356c9',
  magenta: '#725dca',
  cyan: '#337693',
  white: '#697181',
  brightBlack: '#4b5264',
  brightRed: '#e5484d',
  brightGreen: '#1f9d55',
  brightYellow: '#d97706',
  brightBlue: '#3b6fe0',
  brightMagenta: '#9a85ea',
  brightCyan: '#367d9b',
  brightWhite: '#888f9c',
};

export function terminalTheme(mode: Mode): ITheme {
  return mode === 'dark' ? darkTerminal : lightTerminal;
}

export function systemMode(): Mode {
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

export function loadPref(): ModePref {
  try {
    const v = localStorage.getItem('laforge.mode');
    if (v === 'light' || v === 'dark' || v === 'system') return v;
  } catch {
    /* storage blocked */
  }
  return 'dark';
}

export function savePref(p: ModePref): void {
  try {
    localStorage.setItem('laforge.mode', p);
  } catch {
    /* storage blocked */
  }
}
