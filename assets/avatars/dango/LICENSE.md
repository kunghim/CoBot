# Dango (团子) avatar series — sources and licenses

## Gaze by DiceBear

`gaze.json` is the Gaze avatar style by [DiceBear](https://www.dicebear.com),
copied unchanged from `src/gaze.json` of `@dicebear/styles` 10.6.0
(<https://github.com/dicebear/styles>). It is dedicated to the public domain
under [CC0 1.0](https://creativecommons.org/publicdomain/zero/1.0/).

The series uses Gaze's bodies, eyes, spacing and animation. Its pentagon,
hexagon and octagon shapes are left out because they look like circles at
small sizes.

## CoBot's own parts

The star, cloud, drop, flower and bean shapes (defined in `source.mjs`), the
palette and the role defaults are part of CoBot and are covered by this
repository's license (Apache License 2.0, see `/LICENSE`).

## What CoBot downloads

`dist/dango.json` combines both. It carries Gaze's license text in its
`license` field.
