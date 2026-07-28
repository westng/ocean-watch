package contractrunner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func CapturePython(ctx context.Context, options CaptureOptions) (Capture, error) {
	if err := validateGitSHA(options.GitSHA); err != nil {
		return Capture{}, err
	}
	commands, manifestDigest, err := ReadCommandManifest(options.ManifestPath)
	if err != nil {
		return Capture{}, err
	}
	fixtures, err := ReadFixtureManifest(options.FixturesPath)
	if err != nil {
		return Capture{}, err
	}
	cases := BuiltinCases(commands)
	seen := map[string]struct{}{}
	for _, item := range cases {
		seen[item.Spec.ID] = struct{}{}
	}
	for _, spec := range fixtures.Cases {
		if _, exists := seen[spec.ID]; exists {
			return Capture{}, fmt.Errorf("fixture case ID collides with a built-in case: %s", spec.ID)
		}
		seen[spec.ID] = struct{}{}
		cases = append(cases, CapturedCase{Category: "behavior", Spec: spec})
	}
	outputRoot := options.OutputPath
	if err := os.MkdirAll(outputRoot, 0o700); err != nil {
		return Capture{}, err
	}
	for index := range cases {
		var fixtureSource string
		if cases[index].Spec.Fixture != "" {
			fixtureSource, err = safeJoin(options.FixturesPath, cases[index].Spec.Fixture)
			if err != nil {
				return Capture{}, err
			}
			snapshot := filepath.Join("fixtures", cases[index].Spec.ID)
			snapshotPath := filepath.Join(outputRoot, snapshot)
			if err := os.RemoveAll(snapshotPath); err != nil {
				return Capture{}, err
			}
			if err := copyDirectory(fixtureSource, snapshotPath); err != nil {
				return Capture{}, fmt.Errorf("snapshot fixture %s: %w", cases[index].Spec.ID, err)
			}
			cases[index].FixtureSnapshot = filepath.ToSlash(snapshot)
		}
		result, err := executeCase(ctx, options.Program, cases[index].Spec, fixtureSource)
		if err != nil {
			return Capture{}, err
		}
		cases[index].Result = result
	}
	capture := Capture{
		SchemaVersion: CaptureSchemaVersion, Kind: "python-baseline",
		GitSHA: options.GitSHA, Platform: nativePlatformID(), ManifestSHA256: manifestDigest,
		CommandCount: len(commands), Cases: cases,
	}
	if err := writeJSON(filepath.Join(outputRoot, "capture.json"), capture); err != nil {
		return Capture{}, err
	}
	return capture, nil
}
