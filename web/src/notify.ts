// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// Telling the viewer an agent needs them: saved on/off settings, the
// browser's Notification API, and a short chime made with WebAudio.

export function loadFlag(key: string, fallback: boolean): boolean {
  try {
    const v = localStorage.getItem(key);
    if (v === '1') return true;
    if (v === '0') return false;
  } catch {
    /* storage blocked */
  }
  return fallback;
}

export function saveFlag(key: string, on: boolean) {
  try {
    localStorage.setItem(key, on ? '1' : '0');
  } catch {
    /* storage blocked */
  }
}

export type Permission = NotificationPermission | 'unsupported';

/** permission is the page's notification permission, or unsupported (no API, or not a secure context). */
export function permission(): Permission {
  if (typeof window === 'undefined' || !('Notification' in window) || !window.isSecureContext) return 'unsupported';
  return Notification.permission;
}

/** ask asks for permission; call it only from a user gesture. */
export async function ask(): Promise<Permission> {
  if (permission() === 'unsupported') return 'unsupported';
  try {
    return await Notification.requestPermission();
  } catch {
    return permission();
  }
}

/** show posts a notification; onClick runs when it is clicked. tag replaces an earlier one for the same pane. */
export function show(title: string, body: string, tag: string, onClick: () => void) {
  if (permission() !== 'granted') return;
  try {
    const n = new Notification(title, { body, tag });
    n.onclick = () => {
      window.focus();
      onClick();
      n.close();
    };
  } catch {
    /* some browsers allow notifications only from a service worker */
  }
}

let audio: AudioContext | null = null;

/** unlock makes the audio context; call it from a user gesture so a later chime may play. */
export function unlock() {
  try {
    audio ??= new AudioContext();
    if (audio.state === 'suspended') void audio.resume();
  } catch {
    audio = null;
  }
}

/** chime plays two short soft tones: falling for blocked and error, rising for done. */
export function chime(word: 'blocked' | 'error' | 'done') {
  unlock();
  if (!audio || audio.state !== 'running') return;
  const notes = word === 'done' ? [660, 880] : [880, 587];
  const t0 = audio.currentTime + 0.01;
  notes.forEach((hz, i) => {
    const osc = audio!.createOscillator();
    const gain = audio!.createGain();
    const at = t0 + i * 0.14;
    osc.type = 'sine';
    osc.frequency.value = hz;
    gain.gain.setValueAtTime(0, at);
    gain.gain.linearRampToValueAtTime(0.08, at + 0.015);
    gain.gain.exponentialRampToValueAtTime(0.0001, at + 0.22);
    osc.connect(gain).connect(audio!.destination);
    osc.start(at);
    osc.stop(at + 0.24);
  });
}
