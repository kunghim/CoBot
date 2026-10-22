// Wails otherwise retains an existing Windows ICO, including its default W.
// Always stage the tracked icons before building; no external image tools needed.
import { copyFileSync, mkdirSync, readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const source = resolve(root, 'assets/branding');
const target = resolve(root, '.tmp/wails');
const png = readFileSync(resolve(source, 'appicon-macos.png'));
if (png.subarray(0, 8).toString('hex') !== '89504e470d0a1a0a' ||
    png.readUInt32BE(16) !== 1024 || png.readUInt32BE(20) !== 1024) {
  throw new Error('macOS app icon must be a 1024px PNG');
}
const ico = readFileSync(resolve(source, 'appicon-windows.ico'));
if (ico.readUInt16LE(0) !== 0 || ico.readUInt16LE(2) !== 1 || ico.readUInt16LE(4) !== 7) {
  throw new Error('Windows app icon must contain all seven exported sizes');
}
mkdirSync(resolve(target, 'windows'), { recursive: true });
copyFileSync(resolve(source, 'appicon-macos.png'), resolve(target, 'appicon.png'));
copyFileSync(resolve(source, 'appicon-windows.ico'), resolve(target, 'windows/icon.ico'));
console.log('Staged CoBot metallic app icons (macOS PNG and Windows ICO).');
