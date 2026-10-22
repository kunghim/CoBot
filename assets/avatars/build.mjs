#!/usr/bin/env node
// Builds the avatar catalog CoBot downloads: each series' dist file and the
// index.json that lists them with their size and sha256.
//
//   node assets/avatars/build.mjs           write dist files and index.json
//   node assets/avatars/build.mjs --check   fail if they differ from the sources
//
// Hashes are taken over the LF form of each file, which is what the
// repository stores and serves, so a CRLF checkout still checks clean.
import { createHash } from 'node:crypto';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import * as dango from './dango/source.mjs';

const root = dirname(fileURLToPath(import.meta.url));
const check = process.argv.includes('--check');

const HEX = /^#[0-9a-f]{6}$/;

/** The dango series file: Gaze plus our shapes, parts, states and defaults. */
function buildDango() {
  const gaze = JSON.parse(readFileSync(join(root, 'dango', 'gaze.json'), 'utf8'));
  if (gaze.meta?.license?.name !== 'CC0 1.0') throw new Error('gaze.json is expected to be CC0 1.0');
  const definition = structuredClone(gaze);
  const shapes = definition.components.shape.variants;
  for (const name of dango.droppedShapes) delete shapes[name];
  for (const [name, elements] of Object.entries(dango.ownShapes)) {
    if (shapes[name]) throw new Error(`shape ${name} already exists in Gaze`);
    shapes[name] = { elements };
  }
  const eyes = definition.components.eyes.variants;
  const animations = definition.components.animation.variants;

  // Everything the picker offers or a default names must exist.
  for (const shape of dango.parts.shapes) if (!shapes[shape]) throw new Error(`unknown shape ${shape}`);
  for (const eye of dango.parts.eyes) if (!eyes[eye]) throw new Error(`unknown eyes ${eye}`);
  for (const color of dango.parts.palette) if (!HEX.test(color)) throw new Error(`palette color ${color} is not #rrggbb`);
  if (new Set(dango.parts.palette).size !== dango.parts.palette.length) throw new Error('palette has duplicates');
  for (const [role, avatar] of Object.entries(dango.roleDefaults)) {
    if (!dango.parts.shapes.includes(avatar.shape)) throw new Error(`${role}: shape ${avatar.shape} is not offered`);
    if (!dango.parts.eyes.includes(avatar.eyes)) throw new Error(`${role}: eyes ${avatar.eyes} are not offered`);
    if (!dango.parts.palette.includes(avatar.color)) throw new Error(`${role}: color ${avatar.color} is not in the palette`);
  }
  for (const [state, change] of Object.entries(dango.states)) {
    if (change.eyes && !eyes[change.eyes]) throw new Error(`${state}: unknown eyes ${change.eyes}`);
    if (change.animation && !animations[change.animation]) throw new Error(`${state}: unknown animation ${change.animation}`);
  }

  return {
    schemaVersion: 1,
    id: dango.series.id,
    version: dango.series.version,
    name: dango.series.name,
    license: {
      name: gaze.meta.license.name,
      url: gaze.meta.license.url,
      text: gaze.meta.license.text,
      notes: `Shapes ${Object.keys(dango.ownShapes).join(', ')} are CoBot's own; ${dango.droppedShapes.join(', ')} are left out.`,
    },
    render: dango.render,
    parts: dango.parts,
    states: dango.states,
    roleDefaults: dango.roleDefaults,
    definition,
  };
}

const stringify = (value) => JSON.stringify(value, null, 2) + '\n';
const sha256 = (text) => createHash('sha256').update(text).digest('hex');
const readLF = (path) => (existsSync(path) ? readFileSync(path, 'utf8').replace(/\r\n/g, '\n') : null);

const outputs = [];
const index = { schemaVersion: 1, series: [] };
for (const build of [buildDango]) {
  const file = build();
  const text = stringify(file);
  const path = `${file.id}/dist/${file.id}.json`;
  outputs.push({ path, text });
  index.series.push({
    id: file.id,
    name: file.name,
    kind: 'composed',
    version: file.version,
    file: path,
    sha256: sha256(text),
    size: Buffer.byteLength(text),
  });
}
outputs.push({ path: 'index.json', text: stringify(index) });

let drift = 0;
for (const { path, text } of outputs) {
  const target = join(root, path);
  if (check) {
    if (readLF(target) !== text) {
      console.error(`${relative(process.cwd(), target)} is out of date`);
      drift++;
    }
    continue;
  }
  mkdirSync(dirname(target), { recursive: true });
  writeFileSync(target, text);
  console.log(`wrote ${relative(process.cwd(), target)}`);
}
if (drift) {
  console.error('run node assets/avatars/build.mjs and commit the result');
  process.exit(1);
}
if (check) console.log('avatar catalog is up to date');
