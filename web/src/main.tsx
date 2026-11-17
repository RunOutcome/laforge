// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import '@fontsource/ibm-plex-sans/latin-400.css';
import '@fontsource/ibm-plex-sans/latin-500.css';
import '@fontsource/ibm-plex-sans/latin-600.css';
import '@fontsource/ibm-plex-mono/latin-400.css';
import '@fontsource/ibm-plex-mono/latin-500.css';
import '@fontsource/ibm-plex-mono/latin-600.css';
import './styles.css';
import { Forge } from './Forge';
import { loadPref, resolveMode, systemMode } from './theme';

// the resolved light/dark mode before the first render, so nothing flashes
document.documentElement.dataset.mode = resolveMode(loadPref(), systemMode());

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <Forge />
  </StrictMode>,
);
