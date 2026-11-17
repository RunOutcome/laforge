// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

const base = { width: 14, height: 14, viewBox: '0 0 16 16', fill: 'none', stroke: 'currentColor', strokeWidth: 1.5, strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const };

export const SplitRight = () => (
  <svg {...base} aria-hidden>
    <rect x="2" y="2.5" width="12" height="11" rx="2" />
    <path d="M8 2.5v11" />
  </svg>
);
export const SplitDown = () => (
  <svg {...base} aria-hidden>
    <rect x="2" y="2.5" width="12" height="11" rx="2" />
    <path d="M2 8h12" />
  </svg>
);
export const Zoom = () => (
  <svg {...base} aria-hidden>
    <path d="M9.5 2.5h4v4M6.5 13.5h-4v-4M13.5 2.5 9 7M2.5 13.5 7 9" />
  </svg>
);
export const Unzoom = () => (
  <svg {...base} aria-hidden>
    <path d="M13.5 6.5h-4v-4M2.5 9.5h4v4M9.5 6.5 14 2M6.5 9.5 2 14" />
  </svg>
);
export const Close = () => (
  <svg {...base} aria-hidden>
    <path d="M4 4l8 8M12 4l-8 8" />
  </svg>
);
export const Plus = () => (
  <svg {...base} aria-hidden>
    <path d="M8 3v10M3 8h10" />
  </svg>
);
export const Grid = () => (
  <svg {...base} aria-hidden>
    <rect x="2" y="2" width="5" height="5" rx="1.2" />
    <rect x="9" y="2" width="5" height="5" rx="1.2" />
    <rect x="2" y="9" width="5" height="5" rx="1.2" />
    <rect x="9" y="9" width="5" height="5" rx="1.2" />
  </svg>
);
export const MainStack = () => (
  <svg {...base} aria-hidden>
    <rect x="2" y="2.5" width="12" height="11" rx="2" />
    <path d="M9 2.5v11M9 8h5" />
  </svg>
);
export const Sun = () => (
  <svg {...base} aria-hidden>
    <circle cx="8" cy="8" r="3" />
    <path d="M8 1.5v1.5M8 13v1.5M1.5 8H3M13 8h1.5M3.4 3.4l1 1M11.6 11.6l1 1M3.4 12.6l1-1M11.6 4.4l1-1" />
  </svg>
);
export const Moon = () => (
  <svg {...base} aria-hidden>
    <path d="M13 9.5A5.5 5.5 0 0 1 6.5 3a5.5 5.5 0 1 0 6.5 6.5z" />
  </svg>
);
export const Monitor = () => (
  <svg {...base} aria-hidden>
    <rect x="2" y="2.5" width="12" height="8.5" rx="1.5" />
    <path d="M6 14h4M8 11v3" />
  </svg>
);
export const Command = () => (
  <svg {...base} aria-hidden>
    <path d="M6 6h4v4H6zM6 6V4.5A1.5 1.5 0 1 0 4.5 6H6M10 6V4.5A1.5 1.5 0 1 1 11.5 6H10M6 10v1.5A1.5 1.5 0 1 1 4.5 10H6M10 10v1.5a1.5 1.5 0 1 0 1.5-1.5H10" />
  </svg>
);
export const Restart = () => (
  <svg {...base} aria-hidden>
    <path d="M2.5 8a5.5 5.5 0 1 0 1.8-4.1M2.5 2.5v3h3" />
  </svg>
);

/** Mark is LaForge's mark: a prompt struck on a tile, in Outcome's accent and AI colours. */
export const Mark = ({ size = 20 }: { size?: number }) => (
  <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden>
    <defs>
      <linearGradient id="lf-g" x1="0" y1="0" x2="1" y2="1">
        <stop offset="0" stopColor="var(--acc)" />
        <stop offset="1" stopColor="var(--ai)" />
      </linearGradient>
    </defs>
    <rect x="1" y="1" width="22" height="22" rx="6" fill="url(#lf-g)" />
    <path d="M7 8l4 4-4 4" stroke="#fff" strokeWidth="2.2" fill="none" strokeLinecap="round" strokeLinejoin="round" />
    <path d="M13 16h4.5" stroke="#fff" strokeWidth="2.2" strokeLinecap="round" />
  </svg>
);

export const Pencil = () => (
  <svg {...base} width={12} height={12} aria-hidden>
    <path d="M10.5 2.5l3 3L6 13H3v-3z" />
  </svg>
);
export const Chevron = () => (
  <svg {...base} width={11} height={11} aria-hidden>
    <path d="M4 6l4 4 4-4" />
  </svg>
);
export const SmallClose = () => (
  <svg {...base} width={11} height={11} aria-hidden>
    <path d="M4 4l8 8M12 4l-8 8" />
  </svg>
);
export const Bell = () => (
  <svg {...base} aria-hidden>
    <path d="M4 11.5V7a4 4 0 0 1 8 0v4.5l1 1H3l1-1ZM6.5 14h3" />
  </svg>
);
