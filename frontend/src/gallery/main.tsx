// Client entry of the gallery: hydrates the prerendered page so the demos
// respond (hover, menus, dialogs, toasts, theme switching).
import React from 'react';
import { hydrateRoot } from 'react-dom/client';
import '../styles/index.css';
import Gallery from './Gallery';

const root = document.getElementById('root')!;
hydrateRoot(
  root,
  <React.StrictMode>
    <Gallery generatedAt={root.dataset.generatedAt ?? ''} />
  </React.StrictMode>,
);
