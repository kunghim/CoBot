# CoBot branding

All application marks use the approved graphite/silver **metallic bird**, including
small favicons. No monochrome substitution, new bird shape or recoloring is used.
The source is `Cobot_Brand_Assets_V1.0/01_Bird_Mark/cobot_bird_metallic_1024.png`
from the user-supplied 2026-10-18 brand package. `bird-metallic-1024.png` is an
unchanged copy. The upstream source is raster artwork, not an original vector.

`export.py` derives the committed exports with Pillow 12.3.0. It only scales the
bird and adds a rounded white plate for contrast against light/dark backgrounds:

- `appicon-macos.png`: 1024px, transparent outer Dock margin. Wails converts this
  PNG to the bundle's `iconfile.icns`.
- `appicon-windows.ico`: 16, 24, 32, 48, 64, 128 and 256px, embedded in the EXE
  and reused by the MSI installer and shortcut.
- `frontend/src/assets/branding/brandmark-128.png`: the same metal/white treatment
  for the interface, without theme-dependent tinting.
- `frontend/src/assets/branding/favicon.ico`: 16, 32 and 48px metallic marks.

Run `python3 assets/branding/export.py` after replacing the source. Builds need
no Python: `node scripts/prepare-branding.mjs` copies the committed app icons into
the ignored Wails build directory on every build, replacing any old default icon.
The frontend imports its committed exports, so Vite bundles them for both Web and
desktop. These resources ship with the app and are not downloaded at runtime.
