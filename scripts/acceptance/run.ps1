param(
    [ValidateSet("all", "baseline", "state", "skill-eval", "bootstrap", "launcher", "contracts", "native-candidate", "verify-candidate", "supply-chain", "upgrade-rollback", "user-journey", "canary", "ac", "final-summary", "verify-gate")]
    [string]$Suite = "all",
    [int]$Trials = 1,
    [int]$Jobs = 1,
    [string]$CaseSet = "",
    [string]$DriverCommand = "",
    [string]$Model = $env:OCEAN_WATCH_EVAL_MODEL,
    [string]$Reasoning = $env:OCEAN_WATCH_EVAL_REASONING,
    [string]$EvidenceDir = $env:OCEAN_WATCH_EVIDENCE_DIR,
    [string]$CandidateDir = $env:OCEAN_WATCH_CANDIDATE_DIR,
    [string]$FirstCandidateDir = "",
    [string]$SecondCandidateDir = "",
    [string]$ApprovalFile = "",
    [string]$CanaryDriverCommand = "",
    [string]$ExpectedPlatform = "",
    [string]$ShardRoot = "",
    [string]$ExternalRoot = "",
    [string]$SummaryFile = "",
    [string]$SignoffFile = "",
    [string]$GitSha = "",
    [string]$Gate = "G5",
    [string]$ExceptionsFile = "",
    [switch]$RequireReady,
    [switch]$RequireRelease,
    [switch]$RejectTrackedSignoff
)

$ErrorActionPreference = "Stop"
$Root = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
$Python = Join-Path $Root ".venv/Scripts/python.exe"
if (-not (Test-Path $Python)) {
    $Python = "python"
}

function Run-PythonContracts {
    $env:PYTHONPATH = Join-Path $Root "skills/ads-plan-monitor/src"
    & $Python -m unittest -q tests.test_migration_contracts tests.test_state_compatibility tests.test_skill_eval_contracts tests.test_evidence_redaction tests.test_g0_evidence tests.test_ac_runner tests.test_final_acceptance tests.test_acceptance_docs tests.test_p5_acceptance tests.test_plugin_metadata
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}

function Run-State {
    param(
        [string]$Out = ""
    )
    $Arguments = @((Join-Path $Root "scripts/acceptance/probe_state_compatibility.py"))
    if ($Out) { $Arguments += @("--out", $Out) }
    & $Python @Arguments
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}

function Run-SkillEval {
    param(
        [switch]$AllowNotRun,
        [string]$Out = ""
    )
    $Arguments = @((Join-Path $Root "scripts/acceptance/run_skill_eval.py"), "--trials", $Trials, "--jobs", $Jobs)
    if ($CaseSet) { $Arguments += @("--case-set", $CaseSet) }
    if ($DriverCommand) { $Arguments += @("--driver-command", $DriverCommand) }
    if ($Model) { $Arguments += @("--model", $Model) }
    if ($Reasoning) { $Arguments += @("--reasoning", $Reasoning) }
    if ($AllowNotRun) { $Arguments += "--allow-not-run" }
    if ($Out) { $Arguments += @("--out", $Out) }
    & $Python @Arguments
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}

function Run-Bootstrap {
    $env:GOTOOLCHAIN = "go1.26.5"
    Push-Location (Join-Path $Root "prototype/runtime-bootstrap")
    try {
        & go test ./...
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
        & go test -race ./...
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    } finally {
        Pop-Location
    }
    & $Python (Join-Path $Root "scripts/acceptance/build_bootstrap_matrix.py")
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}

function Require-PathArgument {
    param(
        [string]$Value,
        [string]$Name
    )
    if (-not $Value) {
        throw "$Name is required for Suite $Suite"
    }
}

function Run-P5 {
    param(
        [string]$Command,
        [string[]]$Arguments
    )
    & $Python (Join-Path $Root "scripts/acceptance/p5.py") $Command @Arguments
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}

function Run-Contracts {
    if (-not $EvidenceDir) {
        $GitSha = (& git -C $Root rev-parse HEAD).Trim()
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
        $ContractEvidence = Join-Path $Root "artifacts/go-sdk-acceptance/$GitSha/contracts"
    } else {
        $GitSha = (& git -C $Root rev-parse HEAD).Trim()
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
        $ContractEvidence = $EvidenceDir
    }
    $GoModule = Join-Path $Root "prototype/ocean-watch-go"
    $Candidate = Join-Path $ContractEvidence "bin/ocean-watch.exe"
    New-Item -ItemType Directory -Force (Split-Path $Candidate) | Out-Null
    $env:GOTOOLCHAIN = "go1.26.5"
    & go -C $GoModule test ./internal/contractrunner ./internal/cli ./internal/adapters/filesystem
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    & go -C $GoModule build -o $Candidate ./cmd/ocean-watch
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    & go -C $GoModule run ./cmd/contract-runner capture-python `
        --manifest (Join-Path $Root "contracts/commands.yaml") `
        --fixtures (Join-Path $Root "testdata/contracts/python") `
        --out (Join-Path $ContractEvidence "python") `
        --git-sha $GitSha `
        --python $Python
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    & go -C $GoModule run ./cmd/contract-runner compare `
        --manifest (Join-Path $Root "contracts/commands.yaml") `
        --baseline (Join-Path $ContractEvidence "python") `
        --candidate $Candidate `
        --out (Join-Path $ContractEvidence "go") `
        --git-sha $GitSha `
        --python $Python
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    & $Python (Join-Path $Root "scripts/acceptance/scan_evidence.py") $ContractEvidence
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}

switch ($Suite) {
    "baseline" { Run-PythonContracts }
    "state" { Run-State }
    "skill-eval" { Run-SkillEval }
    "bootstrap" { Run-Bootstrap }
    "launcher" {
        Require-PathArgument $CandidateDir "-CandidateDir"
        $Arguments = @("--candidate-dir", $CandidateDir)
        if ($ExpectedPlatform) { $Arguments += @("--expected-platform", $ExpectedPlatform) }
        if ($EvidenceDir) { $Arguments += @("--out", (Join-Path $EvidenceDir "release/ac-124-platform.json")) }
        Run-P5 "launcher" $Arguments
    }
    "contracts" { Run-Contracts }
    "native-candidate" {
        Require-PathArgument $CandidateDir "-CandidateDir"
        Require-PathArgument $EvidenceDir "-EvidenceDir"
        $Arguments = @(
            (Join-Path $Root "scripts/acceptance/native_candidate.py"),
            "--candidate-dir", $CandidateDir,
            "--out-dir", $EvidenceDir,
            "--python", $Python
        )
        if ($ExpectedPlatform) { $Arguments += @("--expected-platform", $ExpectedPlatform) }
        if ($GitSha) { $Arguments += @("--expected-commit", $GitSha) }
        if ($RequireRelease) { $Arguments += "--require-release" }
        & $Python @Arguments
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    }
    "verify-candidate" {
        Require-PathArgument $CandidateDir "-CandidateDir"
        $Arguments = @("--candidate-dir", $CandidateDir)
        if ($EvidenceDir) { $Arguments += @("--out", (Join-Path $EvidenceDir "candidate.json")) }
        Run-P5 "verify-candidate" $Arguments
    }
    "supply-chain" {
        Require-PathArgument $FirstCandidateDir "-FirstCandidateDir"
        Require-PathArgument $SecondCandidateDir "-SecondCandidateDir"
        $Arguments = @("--first-dir", $FirstCandidateDir, "--second-dir", $SecondCandidateDir)
        if ($EvidenceDir) { $Arguments += @("--out", (Join-Path $EvidenceDir "security/ac-125-supply-chain.json")) }
        Run-P5 "supply-chain" $Arguments
    }
    "upgrade-rollback" {
        Require-PathArgument $CandidateDir "-CandidateDir"
        $Arguments = @("--candidate-dir", $CandidateDir)
        if ($EvidenceDir) { $Arguments += @("--out", (Join-Path $EvidenceDir "release/ac-126-upgrade-rollback.json")) }
        Run-P5 "upgrade-rollback" $Arguments
    }
    "user-journey" {
        Require-PathArgument $CandidateDir "-CandidateDir"
        $Arguments = @("--candidate-dir", $CandidateDir)
        if ($EvidenceDir) { $Arguments += @("--out", (Join-Path $EvidenceDir "contracts/ac-128-user-journeys.json")) }
        Run-P5 "user-journey" $Arguments
    }
    "canary" {
        Require-PathArgument $CandidateDir "-CandidateDir"
        $Arguments = @("--candidate-dir", $CandidateDir)
        if ($ApprovalFile) { $Arguments += @("--approval-file", $ApprovalFile) }
        if ($CanaryDriverCommand) { $Arguments += @("--driver-command", $CanaryDriverCommand) }
        if ($EvidenceDir) { $Arguments += @("--evidence-dir", $EvidenceDir) }
        Run-P5 "canary" $Arguments
    }
    "ac" {
        Require-PathArgument $EvidenceDir "-EvidenceDir"
        $Arguments = @("--out-dir", $EvidenceDir, "--external-root", $EvidenceDir)
        & $Python (Join-Path $Root "scripts/acceptance/ac.py") @Arguments
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    }
    "final-summary" {
        Require-PathArgument $ShardRoot "-ShardRoot"
        Require-PathArgument $ExternalRoot "-ExternalRoot"
        Require-PathArgument $SummaryFile "-SummaryFile"
        $Arguments = @("--shard-root", $ShardRoot, "--external-root", $ExternalRoot, "--out", $SummaryFile, "--gate", $Gate)
        if ($GitSha) { $Arguments += @("--git-sha", $GitSha) }
        if ($ExceptionsFile) { $Arguments += @("--exceptions", $ExceptionsFile) }
        if ($RequireReady) { $Arguments += "--require-ready" }
        & $Python (Join-Path $Root "scripts/acceptance/build_final_summary.py") @Arguments
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    }
    "verify-gate" {
        Require-PathArgument $SignoffFile "-SignoffFile"
        Require-PathArgument $SummaryFile "-SummaryFile"
        $Arguments = @($SignoffFile, "--summary", $SummaryFile)
        if ($GitSha) { $Arguments += @("--expected-git-sha", $GitSha) }
        if ($RejectTrackedSignoff) { $Arguments += "--reject-tracked-signoff" }
        & $Python (Join-Path $Root "scripts/acceptance/verify_gate_signoff.py") @Arguments
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    }
    "all" {
        $EvidenceRoot = Join-Path $Root "artifacts/go-sdk-acceptance/p0"
        Run-PythonContracts
        Run-State -Out (Join-Path $EvidenceRoot "state-probe.json")
        Run-SkillEval -AllowNotRun -Out (Join-Path $EvidenceRoot "skill-contract.json")
        & $Python (Join-Path $Root "scripts/acceptance/check_docs_links.py")
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
        Run-Bootstrap
        Run-Contracts
        & $Python (Join-Path $Root "scripts/acceptance/build_g0_summary.py") --out (Join-Path $EvidenceRoot "summary.json")
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
        & $Python (Join-Path $Root "scripts/acceptance/scan_evidence.py") $EvidenceRoot
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    }
}
