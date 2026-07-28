package contractrunner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

func Compare(ctx context.Context, options CompareOptions) (ComparisonReport, error) {
	if err := validateGitSHA(options.GitSHA); err != nil {
		return ComparisonReport{}, err
	}
	if options.CandidateIdentity != nil {
		if err := options.CandidateIdentity.Validate(options.GitSHA); err != nil {
			return ComparisonReport{}, err
		}
	}
	commands, manifestDigest, err := ReadCommandManifest(options.ManifestPath)
	if err != nil {
		return ComparisonReport{}, err
	}
	baseline, baselineRoot, err := readCapture(options.BaselinePath)
	if err != nil {
		return ComparisonReport{}, err
	}
	if baseline.GitSHA != options.GitSHA {
		return ComparisonReport{}, fmt.Errorf("baseline does not belong to the requested git SHA")
	}
	if baseline.ManifestSHA256 != manifestDigest || baseline.CommandCount != len(commands) {
		return ComparisonReport{}, fmt.Errorf("baseline does not belong to the requested command manifest")
	}
	report := ComparisonReport{
		SchemaVersion: CaptureSchemaVersion, Kind: "contract-comparison",
		GitSHA: options.GitSHA, Platform: nativePlatformID(),
		ManifestSHA256: manifestDigest, CandidateIdentity: options.CandidateIdentity,
		Total: len(baseline.Cases),
		Cases: make([]ComparedCase, 0, len(baseline.Cases)),
	}
	for _, expected := range baseline.Cases {
		var fixtureSource string
		if expected.FixtureSnapshot != "" {
			fixtureSource, err = safeJoin(baselineRoot, expected.FixtureSnapshot)
			if err != nil {
				return ComparisonReport{}, err
			}
		}
		actual, err := executeCase(ctx, options.Candidate, expected.Spec, fixtureSource)
		if err != nil {
			return ComparisonReport{}, err
		}
		differences := compareResults(expected.Result, actual)
		compared := ComparedCase{
			ID: expected.Spec.ID, Category: expected.Category,
			Passed: len(differences) == 0, Differences: differences,
		}
		if compared.Passed {
			report.Passed++
		} else {
			report.Failed++
		}
		report.Cases = append(report.Cases, compared)
	}
	if err := os.MkdirAll(options.OutputPath, 0o700); err != nil {
		return ComparisonReport{}, err
	}
	if err := writeJSON(filepath.Join(options.OutputPath, "report.json"), report); err != nil {
		return ComparisonReport{}, err
	}
	if err := writeJUnit(filepath.Join(options.OutputPath, "junit.xml"), report); err != nil {
		return ComparisonReport{}, err
	}
	return report, nil
}

func compareResults(expected, actual CaseResult) []Difference {
	differences := []Difference{}
	add := func(field string, expectedValue, actualValue any) {
		if !reflect.DeepEqual(expectedValue, actualValue) {
			differences = append(differences, Difference{
				Field: field, Expected: mapString(expectedValue), Actual: mapString(actualValue),
			})
		}
	}
	add("exit_code", expected.ExitCode, actual.ExitCode)
	add("timed_out", expected.TimedOut, actual.TimedOut)
	add("stdout_kind", expected.StdoutKind, actual.StdoutKind)
	if expected.StdoutKind == "json" && actual.StdoutKind == "json" {
		var expectedJSON any
		var actualJSON any
		_ = json.Unmarshal(expected.StdoutJSON, &expectedJSON)
		_ = json.Unmarshal(actual.StdoutJSON, &actualJSON)
		add("stdout_json", expectedJSON, actualJSON)
	} else {
		add("stdout_text", expected.StdoutText, actual.StdoutText)
	}
	add("stderr", expected.Stderr, actual.Stderr)
	add("presentation", expected.Presentation, actual.Presentation)
	add("before_files", expected.BeforeFiles, actual.BeforeFiles)
	add("after_files", expected.AfterFiles, actual.AfterFiles)
	return differences
}
