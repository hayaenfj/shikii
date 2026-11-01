# Configuration merger for the Svelte build plugin

This private, development-only compatibility package replaces the plugin's deepmerge dependency. It is derived from deepmerge 4.3.1's CommonJS distribution, with its MIT license retained in LICENSE. It is not an upstream deepmerge release.

GHSA-gcqx-w48m-w2h4 has no patched upstream release. Both merge inputs now reject __proto__, constructor and prototype keys before accessing their values. Every destination property is defined as an own data property. Nested cloning and custom array replacement keep the plugin's configuration behavior.

The root npm override selects this local package for the actual plugin import. scripts/test-config-merge.mjs resolves that import from the plugin and checks prototype poisoning, nested options, replacement arrays, symbols and special values. npm audit still checks every registry dependency at every severity. Remove this compatibility package and override when the plugin adopts a verified upstream fix.

Source: https://github.com/TehShrike/deepmerge/tree/v4.3.1
Advisory: https://github.com/advisories/GHSA-gcqx-w48m-w2h4
