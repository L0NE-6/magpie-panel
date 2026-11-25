# Upstream Audit

Snapshot: 2026-11-25. Upstream remains
[`yetone/magpie`](https://github.com/yetone/magpie). The fork `main` is an
exact mirror; `panel` contains maintenance documentation, the upstream-sync
workflow, and verified local integrations.

## Open Pull Requests

| PR | Current state | Maintenance note |
| :--- | :--- | :--- |
| [#136](https://github.com/yetone/magpie/pull/136) | Conflicting / dirty | P1 candidate. Port only the isolated identity-forget commit `77f55168`; do not cherry-pick the old 5261-file base. |
| [#777](https://github.com/yetone/magpie/pull/777) | Conflicting / dirty | P3. Needs a current multi-login fixture; may break one-click sign-in. |
| [#930](https://github.com/yetone/magpie/pull/930) | Conflicting / dirty | P1. Windows Codex CLI update path; needs rollback and `.old` cleanup checks. |
| [#951](https://github.com/yetone/magpie/pull/951) | Conflicting / dirty | P3 wording change; product naming decision still required. |
| [#977](https://github.com/yetone/magpie/pull/977) | Conflicting / dirty | P2 usage forecast; needs rebase against the current Usage layout. |
| [#978](https://github.com/yetone/magpie/pull/978) | Conflicting / dirty | P2 Usage layout; needs narrow-screen and Playwright regression checks. |
| [#1017](https://github.com/yetone/magpie/pull/1017) | Conflicting / dirty | P2 candidate. Reimplement providers loading for the current Usage architecture and verify `?view=usage`. |
| [#1020](https://github.com/yetone/magpie/pull/1020) | Clean / mergeable | CI-only installed-font test. Integrated into `panel` as `f81f1a4`; upstream remains open. |
| [#1033](https://github.com/yetone/magpie/pull/1033) | Draft / conflicting | P2 Reasonix sessions; needs rebase, completeness tests, and a real binary check. |
| [#1072](https://github.com/yetone/magpie/pull/1072) | Conflicting / dirty | P2 library hash behavior; preserve old-hash compatibility. |
| [#1073](https://github.com/yetone/magpie/pull/1073) | Conflicting / dirty | P2 candidate. Use current-main port `3acb51e`; preserve saved offline values, groups, and image pickers. |
| [#1074](https://github.com/yetone/magpie/pull/1074) | Conflicting / dirty | P1 redaction/OTLP masking; core acceptance is not yet green. |
| [#1157](https://github.com/yetone/magpie/pull/1157) | Conflicting / dirty | P2 CI decision: run the whole provider race suite. |
| [#1181](https://github.com/yetone/magpie/pull/1181) | Conflicting / dirty | P2 routing key plan windows; adapt to the current `cardFor` refactor. |
| [#1258](https://github.com/yetone/magpie/pull/1258) | Conflicting / dirty | P1 signal handling; needs Linux drain and ignored-SIGINT tests. |
| [#1331](https://github.com/yetone/magpie/pull/1331) | Conflicting / dirty | P1 local HTTPS for Microsoft 365; requires certificate lifecycle and trust-store review. |

## Maintenance Rules

- A PR being mergeable is not proof that its behavior is safe.
- A local panel integration is not an upstream merge.
- Do not claim macOS Office, Windows, Linux signal, or real-agent behavior
  unless that environment was actually exercised.
- The fork never publishes upstream releases or distribution channels.

## Next Batch

1. `#136` isolated identity-forget port.
2. `#1073` current-main Settings model validation port.
3. `#1017` Usage providers loading, with a browser check.