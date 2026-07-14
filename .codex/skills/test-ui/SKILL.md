---
name: test-ui
description: Exercise and verify AsterRouter's Vue user interfaces in a browser, including setup, login, admin, console, operator, portal, customer, account, localization, dark mode, and responsive behavior. Use for UI changes, browser regressions, end-to-end workflows, visual QA, forms, routing, or accessibility checks.
---

# AsterRouter UI Tests

Read the UI coverage and critical journeys in `docs/test/v1/README.md`. Use an available browser-control tool for exploratory checks and Playwright suites once the v1 harness exists.

## Start safely

- Prefer `./scripts/dev.sh --no-kill-occupied` so testing never terminates unrelated listeners.
- If ports are occupied, set `ASTER_DEV_BACKEND_PORT`, `ASTER_DEV_FRONTEND_PORT`, and `VITE_DEV_PROXY_TARGET` to isolated values.
- Use demo or dedicated test credentials and fake external services. Never expose production secrets in screenshots, traces, or logs.
- Confirm the expected profile and role before testing a surface.

## Exercise the workflow

1. Begin from a known setup, authentication, storage, locale, and theme state.
2. Test the primary path through visible controls, not direct internal state mutation.
3. Cover loading, empty, success, validation, authorization, server-error, and retry states.
4. Verify the browser URL, visible result, API request, persisted state after reload, and relevant audit evidence.
5. Check that forbidden surfaces redirect or return an authorization-safe state without leaking data.
6. Inspect console errors, failed network requests, unhandled promises, and unexpected page reloads.

Prioritize these journeys:

- first-run setup and local login;
- create provider, model route, policy, and Workspace Key, then inspect usage and Trace;
- identity binding, TOTP, password change, logout, and revoked-session behavior;
- admin, department-scoped, developer, operator, portal, and customer isolation;
- billing, notifications, export download, plugin actions, backup, and settings confirmations.

## Verify layout and accessibility

Check at least `1440x900`, `1280x800`, and `390x844` for changed workflows. Verify no overlap, clipping, unintended horizontal scrolling, layout shift, or inaccessible off-screen controls.

For changed controls, verify keyboard navigation, visible focus, accessible names, labels, dialog focus behavior, disabled state, and error association. Check English and Simplified Chinese, light and dark themes, and long content where the component displays user data.

Use screenshots as evidence, not as the only assertion. Prefer stable semantic locators and behavior assertions over CSS selectors or pixel-perfect snapshots.

## Report evidence

Record the URL, profile/role, viewport, locale/theme, actions, expected and actual result, console/network findings, and screenshot path. For failures, provide the shortest reproducible sequence and the request ID when available.
