# should-build

Decides whether a component needs to be rebuilt based on dependency-graph analysis.

Given a base ref and a head ref, `should-build` walks the changes in between, projects them through a declarative config (`should-build.yaml`) and a language-specific dependency-graph analyzer, and answers a single question per target: rebuild, or skip.

Currently private. Design lives in [prassoai/murmuration#1812](https://github.com/prassoai/murmuration/pull/1812). Will be open-sourced once the v1 implementation stabilizes.

## Status

Pre-implementation. Schema and design are documented in the linked design doc.

## License

Apache-2.0.
