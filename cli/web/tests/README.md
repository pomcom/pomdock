# UI verification

Run from `cli/web`:

- `npm test`: selection, search, export, curation and palette contrast regressions.
- `npm run build`: Svelte/TypeScript checks and locally bundled production assets.
- `CHROMIUM_PATH=/path/to/chromium npm run test:browser`: populated UI workflows, computed text contrast, menus/focus, both themes and densities, 1280/1440px views, zoom-equivalent viewports, 200% text resizing, local preference reloads and 320px reflow. Uses synthetic API fixtures and a local file document; no real engagement or Docker mutations. Chromium is also detected from a Playwright cache or standard system paths.
- `npm run test:native-terminal`: optional Docker/tmux integration. Requires Python 3, Go, Docker, tmux and the local `pcm-kali-pentest` image (`POMDOCK_TEST_IMAGE` overrides it). Builds a temporary CLI and tests a disposable offline container using a private tmux socket. Verifies target identity, per-pane recording, nested bash/tmux, rapid/multiline command boundaries, Atuin coexistence, indexed search/output/replay, shell keys, copy mode, ANSI output and PTY resize, then cleans up. The browser test also loads the bundled asciinema player against synthetic casts.

Browser screenshots and DOM-readiness timing results go to `/tmp/pomdock-ui-review` (`POMDOCK_BROWSER_ARTIFACTS` overrides it). The history fixture has 1,003 executions, 100 rendered per page, and a 600-line output. Tests do not benchmark arbitrarily large expanded transcripts, OS desktop integration, or browsers other than Chromium. Zoom checks emulate reduced CSS viewport dimensions and pixel ratios; also check native browser zoom manually before a release.
