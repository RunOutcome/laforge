// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// The Settings panel's model: what it shows and sends, kept apart from the
// panel so it can be tested without a DOM.
import type { Profile } from './api';

/** A profile as the panel's form holds it: args and env as text. */
export interface ProfileForm {
  name: string;
  label: string;
  description: string;
  command: string;
  args: string;
  env: string;
  agent: boolean;
  /** editing: the name is the profile's own, not chosen. */
  editing: boolean;
  resume?: string[];
}

export const emptyForm = (): ProfileForm => ({ name: '', label: '', description: '', command: '', args: '', env: '', agent: false, editing: false });

/** profileName is the forge's rule for a profile's name. */
export const profileName = /^[a-z0-9][a-z0-9._-]{0,39}$/;

/** splitArgs splits a command line's arguments as a shell would: on spaces, with '…' and "…" holding them together and \ escaping one character. */
export function splitArgs(s: string): string[] {
  const out: string[] = [];
  let cur = '';
  let any = false;
  let quote: '"' | "'" | null = null;
  for (let i = 0; i < s.length; i++) {
    const c = s[i];
    if (quote) {
      if (c === quote) quote = null;
      else if (c === '\\' && quote === '"' && i + 1 < s.length) cur += s[++i];
      else cur += c;
    } else if (c === '"' || c === "'") {
      quote = c;
      any = true;
    } else if (c === '\\' && i + 1 < s.length) {
      cur += s[++i];
      any = true;
    } else if (/\s/.test(c)) {
      if (any || cur) out.push(cur);
      cur = '';
      any = false;
    } else {
      cur += c;
    }
  }
  if (any || cur) out.push(cur);
  return out;
}

/** joinArgs is splitArgs backwards: each argument quoted when it needs to be. */
export function joinArgs(args: string[] = []): string {
  return args.map((a) => (a !== '' && /^[\w@%+=:,./{}-]+$/.test(a) ? a : `'${a.replace(/'/g, `'\\''`)}'`)).join(' ');
}

export function formOf(p: Profile): ProfileForm {
  return { name: p.name, label: p.label, description: p.description ?? '', command: p.command, args: joinArgs(p.args), env: (p.env ?? []).join('\n'), agent: p.agent, editing: true, resume: p.resume };
}

/** profileOf checks a form and gives the profile to send, or what is wrong with it. */
export function profileOf(f: ProfileForm): { profile: Profile; error?: undefined } | { error: string; profile?: undefined } {
  const name = f.name.trim();
  if (!profileName.test(name)) return { error: 'A name is lowercase letters, digits, ".", "_" or "-", starting with a letter or digit.' };
  const command = f.command.trim();
  if (!command) return { error: 'A profile needs a command.' };
  const env = f.env
    .split('\n')
    .map((x) => x.trim())
    .filter(Boolean);
  const bad = env.find((x) => !/^[^=\s]+=/.test(x));
  if (bad) return { error: `Each environment line is NAME=value: "${bad}" is not.` };
  const profile: Profile = { name, label: f.label.trim() || name, description: f.description.trim(), command, args: splitArgs(f.args), env, agent: f.agent, available: false };
  if (f.resume?.length) profile.resume = f.resume;
  return { profile };
}

/** commandLine is how a profile runs, for its row. */
export const commandLine = (p: Profile) => [p.command, joinArgs(p.args)].filter(Boolean).join(' ');

/** origin says where a profile comes from: built in, yours, or yours over a built-in. */
export function origin(p: Profile): 'built in' | 'yours' | 'changed' {
  if (p.custom && p.builtin) return 'changed';
  if (p.custom || !p.builtin) return 'yours';
  return 'built in';
}

export const fontMin = 9;
export const fontMax = 22;
export const fontDefault = 13;
export const clampFont = (n: number) => Math.min(fontMax, Math.max(fontMin, Math.round(Number.isFinite(n) ? n : fontDefault)));

const fontKey = 'laforge.fontSize';

/** loadFontSize is this browser's terminal text size. */
export function loadFontSize(): number {
  try {
    const v = localStorage.getItem(fontKey);
    return v === null ? fontDefault : clampFont(Number(v));
  } catch {
    return fontDefault;
  }
}

export function saveFontSize(n: number) {
  try {
    localStorage.setItem(fontKey, String(clampFont(n)));
  } catch {
    /* private mode: this session only */
  }
}

/** timeLeft is how long a share has, short: "2h 5m", "40m", "expired". */
export function timeLeft(expires: string, now: number): string {
  const ms = Date.parse(expires) - now;
  if (!Number.isFinite(ms)) return '';
  if (ms <= 0) return 'expired';
  const m = Math.ceil(ms / 60000);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 48) return m % 60 ? `${h}h ${m % 60}m` : `${h}h`;
  return `${Math.round(h / 24)}d`;
}

/** sameFolder: what the folder field holds is the folder saved (so Save has nothing to do). */
export const sameFolder = (typed: string, cwd: string) => typed.trim().replace(/\/+$/, '') === cwd.replace(/\/+$/, '');
