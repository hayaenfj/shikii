**English** · [日本語](2026-10-21-project-isolation.ja.md)

# Project isolation audit (2026-10-21)

The target is `caee172..01d2634`. Two independent auditors checked per-project stores, the shared page, the CLI/MCP, and scoring and calibration. **There were 5 findings (2 medium, 3 low). All 5 were fixed in the fix request of 2026-10-21.** One duplicate was merged into one finding. The causes, reproductions and code locations below are as of the audited `01d2634`; the checks after the fix are in "Fixes and re-verification".

| ID | Severity | Main category | Problem | Status |
|---|---|---|---|---|
| SHK-20261021-001 | medium | data-protection | Simultaneous calibration of different projects mixes their reports and output destinations | Fixed |
| SHK-20261021-002 | medium | input-validation | init, new and tune use different stores from a subdirectory | Fixed |
| SHK-20261021-003 | low | data-protection | A store check error is ignored, and the default CLI saves to the link target | Fixed |
| SHK-20261021-004 | low | data-protection | Another record is read and written through a link on the final JSONL file | Fixed |
| SHK-20261021-005 | low | input-validation | Redirecting a URL without a trailing / drops the spec and trace given | Fixed |

## Assumptions and scope

Local use by one person: HTTP listens on loopback, and the UI and API have no user authentication. A project is chosen by the hash of its registered real path. There are no per-project user permissions and no tenant authentication. POST requests are checked for Origin, JSON content type and body size.

We followed the path from the unauthenticated shared HTTP to the per-project HTTP, how the CLI/MCP resolve the store, and how conversations, lessons, provisional scores, truths and calibration are saved. We also checked projects with the same name, subdirectories, relative URL references, links, initialization, concurrent runs, and restoring the output destination. No real data, settings, real models or running servers were changed; everything was reproduced with synthetic data, a fake model, isolated directories and a Go overlay.

The usual 221 Go tests, the 8 page checks and the production build pass. The following are boundary bugs the usual checks did not catch. A reproduction test passes to confirm that "the problem happened"; it does not mean the implementation is correct.

## SHK-20261021-001: a simultaneous calibration's report goes into another project

- **Condition**: automatic calibration or tune for different projects runs at the same time from the shared page. It happens with normal operation alone; no file permissions or special settings are needed.
- **Impact**: A's calibration report goes into B's HTTP response, and A's report is missing. After it ends, the output destination is left pointing at a buffer that has already finished. That the calibration file itself was written to another job was not confirmed in this reproduction.
- **Evidence**: `cmd/shikii/project.go:262` keeps a separate server per job. `cmd/shikii/serve.go:537` and `cmd/shikii/serve_tune.go:40` swap the process-wide stdout under each server's own calibMu.
- **Reproduction result**: we sent `/v1/label` to a real hub with synthetic A/B and overlapped calibrations by making the fake model wait. We confirmed that A's calibration target got into B's response, A's report was missing, and stdout was not restored correctly. In another reproduction, the race detector also detected a race.
- **Cause**: the state being protected is process-wide, but the lock is per project.
- **Recommended fix**: pass the report destination as an explicit io.Writer. As a stopgap, use a process-wide lock on every path that captures stdout.
- **Acceptance condition**: even when A/B calibrations and tunes overlap, each response holds only its own report, the output destination is restored, and the race check passes.
- **Difference from known items**: "tune makes calibration wait because it captures stdout" is known. In this hub, the lock of a separate server does not apply, so besides waiting, reports cross over. The same finding from both auditors was merged into this item.

## SHK-20261021-002: the CLI's default store is not unified

- **Condition**: run init, new or tune from a subdirectory of a Git repository without giving a store as an option.
- **Impact**: a spec made with new does not appear on the default page or in MCP. tune fails with "0 truths" even though truths exist. init makes a second store and MCP settings in the subdirectory.
- **Evidence**: `cmd/shikii/new.go:29`, `cmd/shikii/init.go:28`, `:35`, `:45` and `cmd/shikii/tune.go:70` use the cwd's `.shikii`. On the other hand, bindStore, MCP and serve use the project root.
- **Reproduction result**: running new in docs/ of a synthetic repo saved to docs/.shikii/decisions. init made docs/.shikii/wiki and docs/.mcp.json. With 2 synthetic labels saved at the project root, tune from docs/ failed with "0 truths < eval.min_n=2"; with the same cwd, spec and fake model, naming the proper store explicitly scored the 2 and succeeded.
- **Cause**: these were missed when moving to the code that resolves the project root.
- **Recommended fix**: keep a store that is given explicitly, and bring default creation, initialization, scoring and MCP settings onto the same project resolution.
- **Acceptance condition**: running creation, initialization and tune from the project root and from a subdirectory uses the same spec, wiki, truths and MCP target, and makes no second store.
- **Difference from other items**: it happens even without links or check errors. A separate fix unit from SHK-20261021-003.

## SHK-20261021-003: a rejected store is undone by the default value

- **Condition**: with file operations under the same UID, make A's `.shikii` a link to B's store. Escalation to remote or another UID was not confirmed, so it is rated low.
- **Impact**: A's CLI input is saved to B's records.
- **Evidence**: defaultProjectStore at `cmd/shikii/project.go:79` replaces currentProjectStore's check error with `.shikii`. Callers such as `cmd/shikii/main.go:176` do not check again.
- **Reproduction result**: currentProjectStore rejected the link. Running the real cmdDecide in the same cwd without --store saved A's synthetic input to B's records. That MCP/serve use the same default resolution was confirmed statically.
- **Cause**: an error rejecting the store is not distinguished from simply choosing a default value.
- **Recommended fix**: return store resolution with an error, and stop reading, writing and starting when the store was rejected.
- **Acceptance condition**: even when `.shikii` or a main subdirectory is replaced by a link, the default CLI/MCP/serve reject it and add no files at the link target.
- **Difference from other items**: the check on the main folders can already reject. The next item is caused by a final file inside a normal folder that passes that check.

## SHK-20261021-004: links on the final JSONL file are followed

- **Condition**: with operations under the same UID, A's records/intent.jsonl can be made a link to B's record. A's main folders themselves are normal folders. Escalation to remote or another UID was not confirmed, so it is rated low.
- **Impact**: A's page API returns B's input, and records appended from A also reach B.
- **Evidence**: the check at `cmd/shikii/project.go:91` covers only the main folders. Reading and writing JSONL at `internal/store/store.go:644` and `:653` follows a link on the final file.
- **Reproduction result**: A's store and hub registration passed the checks. A synthetic marker saved only in B appeared in A's GET queue, and A's append through the normal AppendRecord also reached B.
- **Cause**: checking folders alone cannot guarantee where the files under them point.
- **Recommended fix**: when reading and writing JSONL, also confirm it is a normal file and reject links. Bring the intermediate folders of generated areas under the same rule.
- **Acceptance condition**: even when the final files and intermediate folders of records, labels, feedback and reviews are replaced by links, no other store is read or written.
- **Difference from other items**: this path remains even after SHK-20261021-003 is fixed. The original-text files for long text have O_NOFOLLOW, unlike the JSONL here.

## SHK-20261021-005: completing the trailing / loses the query

- **Condition**: open a URL without the trailing /, such as `/p/<id>?spec=done-as-asked&trace=<synthetic ID>`. The normal links generated have a trailing /.
- **Impact**: the 307 redirect goes only to `/p/<id>/`, and the decision and record to open are lost. No records disappear.
- **Evidence**: newProjectHub in `cmd/shikii/project.go` redirects to p.URL without the query when there is no trailing /.
- **Reproduction result**: an isolated hub returned 307, and we confirmed that the Location did not contain the original spec and trace.
- **Cause**: RawQuery is not carried over when building the canonical path.
- **Recommended fix**: redirect to the normalized path while keeping the original query.
- **Acceptance condition**: the same spec and trace open with or without the trailing /. Unknown projects are still rejected.

## Fixes and re-verification (2026-10-21)

- **001**: calibration, tune, adoption and in-process runs inside the hook pass the report destination as an io.Writer on each call. HTTP handling does not swap stdout, and different projects can be handled in parallel. The lock on saving specs for the same job is kept. `TestProjectCalibrationsKeepTheirReports` checks simultaneous calibration and responses on a real hub, and `TestProjectTuningKeepsProcessOutput` checks the output destination of tune with a controlled finishing order.
- **002**: init, new and tune also use the project root. init's MCP settings and AGENTS.md go to the same place, and the output destination or store given explicitly to new is kept. Checked on 3 paths with `TestProjectCommandsShareRootFromSubdirectories`.
- **003**: the store is resolved after all flags are read, and the check error is returned as is. It is rejected before the CLI/MCP/serve start, infer or save. `TestProjectDefaultCLIRejectsLinkedStore` confirms that the link target does not grow and that MCP/serve also reject it.
- **004**: Store keeps normal folders with os.Root and checks the identity of intermediate folders and the type of the final file. JSONL, bulk appends of truths, conversations, provisional scores and their locks, and long original text are opened through the same path. GC replacements are also done from the kept parent folder. `TestStoreRejectsLinkedJSONLPaths` checks 5 kinds × links on the final file, an intermediate folder and the store itself, and does not change the link target.
- **005**: RawQuery is carried over when completing the trailing / too. `TestProjectHubRedirectKeepsQuery` confirms that spec and trace are kept.
- **OPS-SEED-001**: only a fully written temporary file is published under the final name, and an existing normal file is not overwritten. A file size limit is set only for the throwaway child process; `TestSeedFailureDoesNotPublishPartialSpec` confirmed that with a real write error the final name is not left behind, and a retry reads the 4 specs correctly.

Before the fix, the 5 regression tests failed at run time, and RED was also confirmed for the store link for original text and the initialization write failure. After the fix, all 228 Go tests, the targeted race checks (58 results), the 8 page checks and make test (including gofmt, vet and the README/skill checks) passed. 7 mutations in total, reverting the report destination, init's placement, the store error, file links, the query and initialization, plus one using tune's old stdout capture, were also confirmed RED at run time.

Regressions after the fix are checked with the following. The past reproduction overlays below are material for the old version, used to confirm the problems happen.

```sh
go test ./cmd/shikii ./internal/store \
  -run '^TestProject|^TestStoreRejectsLinkedJSONLPaths|^TestSeedFailure' -count=1
go test -race ./cmd/shikii ./internal/store \
  -run '^TestProject|^TestStoreRejectsLinkedJSONLPaths|^TestSeedFailure|^TestHook|^TestNotebook|^TestReview|^TestRecordText' -count=1
```

## Reproduction material

The reproduction code, overlays and logs are kept in isolated directories on this machine. Only synthetic markers are used; no real conversations or secrets are included.

```sh
go test -overlay /tmp/shikii-independent-project-audit/overlay.json ./cmd/shikii \
  -run '^TestAuditReproduce(ProjectDefaultSymlink|HubCalibrationOutputCrossesProjects|HubFollowsLeafRecordSymlink)$' \
  -count=1 -v

go test -overlay /tmp/shikii-audit-ui-dvr_ndb2/overlay.json ./cmd/shikii \
  -run '^TestAuditUI(SubdirCommands|SubdirTuneReadsDifferentStore|HubPath|HostRead)$' \
  -count=1 -v
```

- `/tmp/shikii-independent-project-audit/`: project_audit_test.go, reproduction.log, leaf-record-link.log.
- `/tmp/shikii-audit-ui-dvr_ndb2/`: audit_ui_runtime_test.go, reproductions.log, tune.log.
- Race detection: with the latter overlay, `-race -run '^TestAuditUIProjectConcurrentCapture$'` detects the race. It is a check meant to prove the race, so the failure is the reproduction result.

## Open operational items and unconfirmed scope

- **OPS-SEED-001 (existing behavior, low / availability, fixed)**: we confirmed that if one synthetic I/O interruption is injected right after seedAgentSpecs creates the final file, an empty spec remains as an existing file even after a retry. It is a resilience issue also present in the earlier keep-existing-files approach, and is not counted among the 5 findings of this diff. The fix condition is to combine atomic creation that never publishes an empty final name with keeping valid existing files. Evidence: `/tmp/shikii-independent-project-audit/interrupted-seed.log`.
- **CHK-HOST-001 (unconfirmed boundary)**: we confirmed that a GET with a synthetic invalid Host reaches the hub list and the synthetic markers in a per-project queue with HTTP 200. Reaching it through a real browser combined with DNS was not confirmed, and we do not conclude that a DNS rebinding attack works. It is a separate item from the existing S3 POST Origin protection.
- A re-audit of dependency packages and CI permission settings, authentication and authorization between different UIDs, real-use load, and fault tolerance on each OS were not verified. The acceptance conditions that could be confirmed are recorded in the regression tests above.

This audit does not guarantee there are no vulnerabilities. The 5 findings and OPS-SEED-001 were fixed. CHK-HOST-001 remains as an unconfirmed boundary. Nothing was pushed or published.
