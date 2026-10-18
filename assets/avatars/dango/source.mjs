// What the dango (团子) series is made of. build.mjs turns this, together with
// the vendored Gaze definition (gaze.json, DiceBear, CC0 1.0), into the one
// file CoBot downloads: dist/dango.json.
//
// Raise `version` whenever anything here or in gaze.json changes; CoBot only
// downloads a series whose version is newer than the copy it has.

export const series = {
  id: 'dango',
  version: 1,
  name: { 'zh-CN': '团子', en: 'Dango' },
};

// ---- shapes of our own, in Gaze's definition format ----------------------
const body = { type: 'color', name: 'body' };
const eyesAt = (transform) => ({
  name: 'g',
  type: 'element',
  attributes: { class: 'dbga-look' },
  children: [{ name: 'spacing', type: 'component', attributes: { transform } }],
});
const filled = (name, attributes) => ({ name, type: 'element', attributes: { ...attributes, fill: body } });

// A five-point star with rounded tips: a polygon stroked in its own color.
function starPoints(cx, cy, outer, inner) {
  const points = [];
  for (let i = 0; i < 10; i++) {
    const radius = i % 2 ? inner : outer;
    const angle = (Math.PI / 5) * i - Math.PI / 2;
    points.push(`${(cx + radius * Math.cos(angle)).toFixed(1)},${(cy + radius * Math.sin(angle)).toFixed(1)}`);
  }
  return points.join(' ');
}

export const ownShapes = {
  star: [
    { name: 'polygon', type: 'element', attributes: { points: starPoints(50, 54, 40, 21), fill: body, stroke: body, 'stroke-width': '9', 'stroke-linejoin': 'round' } },
    eyesAt('matrix(.72 0 0 .72 28.4 50)'),
  ],
  cloud: [
    filled('circle', { cx: '34', cy: '56', r: '20' }),
    filled('circle', { cx: '55', cy: '42', r: '25' }),
    filled('circle', { cx: '72', cy: '58', r: '17' }),
    filled('rect', { x: '14', y: '56', width: '74', height: '24', rx: '12' }),
    eyesAt('matrix(.9 0 0 .9 23 50)'),
  ],
  drop: [
    filled('path', { d: 'M50 10C46 18 20 42 20 62a30 30 0 0 0 60 0C80 42 54 18 50 10Z' }),
    eyesAt('matrix(.85 0 0 .85 24.5 56)'),
  ],
  flower: [
    ...Array.from({ length: 8 }, (_, i) => {
      const angle = (Math.PI / 4) * i;
      return filled('circle', { cx: (50 + 25 * Math.cos(angle)).toFixed(1), cy: (50 + 25 * Math.sin(angle)).toFixed(1), r: '14' });
    }),
    filled('circle', { cx: '50', cy: '50', r: '28' }),
    eyesAt('matrix(.85 0 0 .85 24.5 44)'),
  ],
  bean: [
    filled('path', { d: 'M28 20c12-6 24 0 32 4 9 4 24 4 27 20 4 22-14 40-38 40-21 0-37-12-37-32 0-15 7-26 16-32Z' }),
    eyesAt('matrix(.9 0 0 .9 22 44)'),
  ],
};

// Gaze shapes left out: at small sizes they read as circles.
export const droppedShapes = ['pentagon', 'hexagon', 'octagon'];

// What the avatar picker offers, in display order.
export const parts = {
  shapes: ['circle', 'square', 'triangle', 'diamond', 'pill', 'column', 'arch', 'egg', 'star', 'cloud', 'drop', 'flower', 'bean'],
  eyes: ['dots', 'big', 'small', 'shine', 'beans', 'bars', 'wide', 'tall', 'happy', 'grin', 'squint'],
  palette: [
    '#7d8c85', // graphite
    '#6d84b4', // slate blue
    '#a8b4fc', // periwinkle
    '#9cc8fb', // sky
    '#6fd6c8', // teal
    '#8ee3a8', // mint
    '#bef264', // lime
    '#f2d16b', // mustard
    '#fbb271', // orange
    '#f98b8b', // coral
    '#f9a8d4', // cherry blossom
    '#d8b1fb', // lavender
    '#b07cc6', // plum
    '#d6c3a3', // sand
  ],
};

// How a member's state changes its avatar.
export const states = {
  working: { animation: 'medium' },
  waiting: { eyes: 'big' },
  stopped: { eyes: 'bars' },
};

// The default avatar of each built-in role. New built-in roles get a line here.
export const roleDefaults = {
  leader: { shape: 'star', eyes: 'shine', color: '#7d8c85' },
  'product-manager': { shape: 'cloud', eyes: 'happy', color: '#d8b1fb' },
  'frontend-dev': { shape: 'circle', eyes: 'dots', color: '#9cc8fb' },
  'backend-dev': { shape: 'square', eyes: 'beans', color: '#6fd6c8' },
  'qa-engineer': { shape: 'arch', eyes: 'wide', color: '#fbb271' },
  'code-reviewer': { shape: 'diamond', eyes: 'tall', color: '#f2d16b' },
};

// How every avatar of the series is drawn, whatever its parts.
export const render = { scale: 1.1, translateX: 0, translateY: 0, rotate: 0 };
