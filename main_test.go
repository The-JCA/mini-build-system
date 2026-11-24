package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCollectBuildOrder(t *testing.T) {
	app := Target{
		Name:   "app",
		Inputs: []string{"build/main.o", "build/parser.o"},
		Output: "build/app",
	}

	mainTarget := Target{Name: "main", Output: "build/main.o"}
	parser := Target{Name: "parser", Output: "build/parser.o"}

	byOutput := map[string]Target{
		app.Output:        app,
		mainTarget.Output: mainTarget,
		parser.Output:     parser,
	}

	var order []Target

	err := collectBuildOrder(app, byOutput, make(map[string]int), &order, nil)
	if err != nil {
		t.Fatalf("collectBuildOrder returned an unexpected error: %v", err)
	}

	names := make([]string, len(order))
	for i, target := range order {
		names[i] = target.Name
	}
	want := []string{"main", "parser", "app"}

	if !slices.Equal(names, want) {
		t.Fatalf("build order = %v, want %v", names, want)
	}
}

func TestSharedDependency(t *testing.T) {
	app := Target{
		Name:   "app",
		Inputs: []string{"build/main.o", "build/parser.o"},
		Output: "build/app",
	}

	mainTarget := Target{Name: "main", Inputs: []string{"build/common.o"}, Output: "build/main.o"}
	parser := Target{Name: "parser", Inputs: []string{"build/common.o"}, Output: "build/parser.o"}
	common := Target{Name: "common", Output: "build/common.o"}

	byOutput := map[string]Target{
		app.Output:        app,
		mainTarget.Output: mainTarget,
		parser.Output:     parser,
		common.Output:     common,
	}

	var order []Target

	err := collectBuildOrder(app, byOutput, make(map[string]int), &order, nil)
	if err != nil {
		t.Fatalf("collectBuildOrder returned an unexpected error: %v", err)
	}

	names := make([]string, len(order))
	for i, target := range order {
		names[i] = target.Name
	}
	want := []string{"common", "main", "parser", "app"}

	if !slices.Equal(names, want) {
		t.Fatalf("build order = %v, want %v", names, want)
	}
}

func TestCycle(t *testing.T) {
	app := Target{
		Name:   "app",
		Inputs: []string{"build/main.o", "build/parser.o"},
		Output: "build/app",
	}

	mainTarget := Target{Name: "main", Inputs: []string{"build/parser.o"}, Output: "build/main.o"}
	parser := Target{Name: "parser", Inputs: []string{"build/main.o"}, Output: "build/parser.o"}

	byOutput := map[string]Target{
		app.Output:        app,
		mainTarget.Output: mainTarget,
		parser.Output:     parser,
	}

	var order []Target

	err := collectBuildOrder(app, byOutput, make(map[string]int), &order, nil)
	if err == nil {
		t.Fatal("expected a cycle error")
	}

	want := "cycle found: main -> parser -> main"
	if got := err.Error(); got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}

}

func TestChangeOutputfile(t *testing.T) {
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "buildtool")

	cmd := exec.Command("go", "build", "-o", binary, ".")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build tool: %v\n%s", err, output)
	}

	if err := os.MkdirAll(filepath.Join(workspace, "example"), 0755); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"buildtool.json", "example/main.c", "example/parser.h"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if err := os.WriteFile(filepath.Join(workspace, path), data, 0644); err != nil {
			t.Fatalf("copy %s: %v", path, err)
		}
	}

	buildMain := func() string {
		t.Helper()

		cmd := exec.Command(binary, "build", "main")
		cmd.Dir = workspace
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("build main: %v\n%s", err, output)
		}
		return string(output)
	}

	buildMain()

	sourcePath := filepath.Join(workspace, "example", "main.c")
	objectPath := filepath.Join(workspace, "build", "main.o")
	statePath := filepath.Join(workspace, "build", ".fingerprints.json")

	achecksum, err := fileChecksum(objectPath)
	if err != nil {
		t.Fatal(err)
	}

	sourceA, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read %s: %v", sourcePath, err)
	}

	outputAfp, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read %s: %v", statePath, err)
	}

	data, err := os.ReadFile("example/mainb.c")
	if err != nil {
		t.Fatalf("read %s: %v", "example/mainb.c", err)
	}
	if err := os.WriteFile(sourcePath, data, 0644); err != nil {
		t.Fatalf("copy %s: %v", sourcePath, err)
	}

	buildMain()
	bchecksum, err := fileChecksum(objectPath)
	if err != nil {
		t.Fatal(err)
	}
	if achecksum == bchecksum {
		t.Fatal("Checksums of A and B are equal")
	}

	if err := os.WriteFile(sourcePath, sourceA, 0644); err != nil {
		t.Fatalf("copy %s: %v", sourcePath, err)
	}

	if err := os.WriteFile(statePath, outputAfp, 0644); err != nil {
		t.Fatalf("copy %s: %v", statePath, err)
	}

	buildMain()
	testchecksum, err := fileChecksum(objectPath)
	if err != nil {
		t.Fatal(err)
	}
	if achecksum != testchecksum {
		t.Fatal("Checksum after restoration is not equal to Checksum of Version A")
	}

	if output := buildMain(); !strings.Contains(output, "skipping main: up to date") {
		t.Fatalf("expected unchanged build to skip main, got:\n%s", output)
	}
}

func TestFailedBuildPreservesPreviousOutput(t *testing.T) {
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "buildtool")

	cmd := exec.Command("go", "build", "-o", binary, ".")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build tool: %v\n%s", err, output)
	}

	if err := os.MkdirAll(filepath.Join(workspace, "example"), 0755); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"buildtool.json", "example/main.c", "example/parser.h"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if err := os.WriteFile(filepath.Join(workspace, path), data, 0644); err != nil {
			t.Fatalf("copy %s: %v", path, err)
		}
	}

	baseline := exec.Command(binary, "build", "main")
	baseline.Dir = workspace
	if output, err := baseline.CombinedOutput(); err != nil {
		t.Fatalf("baseline build: %v\n%s", err, output)
	}

	objectPath := filepath.Join(workspace, "build", "main.o")
	statePath := filepath.Join(workspace, "build", ".fingerprints.json")
	configPath := filepath.Join(workspace, "buildtool.json")
	objectBefore, err := os.ReadFile(objectPath)
	if err != nil {
		t.Fatal(err)
	}
	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var config Config
	if err = json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	for i := range config.Targets {
		if config.Targets[i].Name == "main" {
			config.Targets[i].Command = []string{"sh", "-c",
				`printf 'partial output\n' > "$1" || exit 2; echo 'partial output written' >&2; exit 1`,
				"sh", "{output}"}
			break
		}
	}

	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, encoded, 0644); err != nil {
		t.Fatalf("write modified build config: %v", err)
	}

	baseline = exec.Command(binary, "build", "main")
	baseline.Dir = workspace
	output, err := baseline.CombinedOutput()
	if err == nil {
		t.Fatalf("expected build to fail, got:\n%s", output)
	}
	if !slices.Contains(strings.Split(string(output), "\n"), "partial output written") {
		t.Fatalf("expected shell to confirm it wrote partial output, got:\n%s", output)
	}

	objectNow, err := os.ReadFile(objectPath)
	if err != nil {
		t.Fatal(err)
	}
	stateNow, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(stateBefore, stateNow) {
		t.Fatal("State before doesn't match StateNow")
	}

	if !slices.Equal(objectNow, objectBefore) {
		t.Fatal("Object before doesn't match StateNow")
	}

	matches, err := filepath.Glob(filepath.Join(workspace, "build", ".output-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("failed build left temporary output directories: %v", matches)
	}
}

func TestPlannedInteruption(t *testing.T) {

	workspace := t.TempDir()
	binary := filepath.Join(workspace, "buildtool")

	cmd := exec.Command("go", "build", "-o", binary, ".")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build tool: %v\n%s", err, output)
	}

	if err := os.MkdirAll(filepath.Join(workspace, "example"), 0755); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"buildtool.json", "example/main.c", "example/parser.h"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if err := os.WriteFile(filepath.Join(workspace, path), data, 0644); err != nil {
			t.Fatalf("copy %s: %v", path, err)
		}
	}

	baseline := exec.Command(binary, "build", "main")
	baseline.Dir = workspace
	if output, err := baseline.CombinedOutput(); err != nil {
		t.Fatalf("baseline build: %v\n%s", err, output)
	}

	objectPath := filepath.Join(workspace, "build", "main.o")
	statePath := filepath.Join(workspace, "build", ".fingerprints.json")
	//configPath := filepath.Join(workspace, "buildtool.json")
	sourcePath := filepath.Join(workspace, "example", "main.c")

	checksumA, err := fileChecksum(objectPath)
	if err != nil {
		t.Fatal(err)
	}

	dataobjectA, err := os.ReadFile(objectPath)
	if err != nil {
		t.Fatal(err)
	}

	fingerprintA, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile("example/mainb.c")
	if err != nil {
		t.Fatalf("read %s: %v", "example/mainb.c", err)
	}
	if err := os.WriteFile(sourcePath, data, 0644); err != nil {
		t.Fatalf("copy %s: %v", sourcePath, err)
	}

	baseline = exec.Command(binary, "build", "main")
	baseline.Dir = workspace
	if output, err := baseline.CombinedOutput(); err != nil {
		t.Fatalf("baseline build: %v\n%s", err, output)
	}

	bchecksum, err := fileChecksum(objectPath)
	if err != nil {
		t.Fatal(err)
	}

	fingerprintB, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read B's state: %v", err)
	}

	if bchecksum == checksumA {
		t.Fatal("expected versions A and B to produce different object files")
	}

	cases := []struct {
		checkpoint   string
		wantChecksum string
	}{
		{"before-output-rename", checksumA},
		{"before-build-record-save", bchecksum},
	}

	runCase := func(t *testing.T, checkpoint, wantChecksum string) {
		t.Helper()
		if err := os.WriteFile(objectPath, dataobjectA, 0644); err != nil {
			t.Fatalf("copy %s: %v", objectPath, err)
		}
		if err := os.WriteFile(statePath, fingerprintA, 0644); err != nil {
			t.Fatalf("copy %s: %v", statePath, err)
		}

		cmd := exec.Command(binary, "build", "main")
		cmd.Dir = workspace
		cmd.Env = append(cmd.Environ(),
			"BUILDTOOL_TEST_INTERRUPT="+checkpoint,
		)

		output, err := cmd.CombinedOutput()
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 86 {
			t.Fatalf("expected exit code 86, got %v\n%s", err, output)
		}

		newchecksum, err := fileChecksum(objectPath)
		if err != nil {
			t.Fatal(err)
		}

		if newchecksum != wantChecksum {
			t.Fatalf("interrupted output checksum = %s, want %s", newchecksum, wantChecksum)
		}

		data, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatal(err)
		}

		if !slices.Equal(data, fingerprintA) {
			t.Fatalf("fingerprint of A doesn't match current fingerprint")
		}

		//run normally after interuption with b source
		baseline = exec.Command(binary, "build", "main")
		baseline.Dir = workspace
		if output, err := baseline.CombinedOutput(); err != nil {
			t.Fatalf("baseline build: %v\n%s", err, output)
		}

		stateAfterRetry, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(stateAfterRetry, fingerprintB) {
			t.Fatal("state after retry differs from clean B build")
		}

		newchecksum, err = fileChecksum(objectPath)
		if err != nil {
			t.Fatal(err)
		}
		if bchecksum != newchecksum {
			t.Fatalf("Final object checksum %v is not equal to clean b checksum %v", newchecksum, bchecksum)
		}

		//running again
		baseline = exec.Command(binary, "build", "main")
		baseline.Dir = workspace
		output, err = baseline.CombinedOutput()
		if err != nil {
			t.Fatalf("baseline build: %v\n%s", err, output)
		}
		if !strings.Contains(string(output), "skipping main: up to date") {
			t.Fatalf("expected unchanged build to skip main, got:\n%s", output)
		}
	}

	for _, tc := range cases {
		t.Run(tc.checkpoint, func(t *testing.T) {
			runCase(t, tc.checkpoint, tc.wantChecksum)
		})
	}

	const seed int64 = 36
	rng := rand.New(rand.NewSource(seed))

	for i := 0; i < 200; i++ {
		tc := cases[rng.Intn(len(cases))]
		name := fmt.Sprintf("seed-%d-run-%02d-%s", seed, i, tc.checkpoint)
		t.Run(name, func(t *testing.T) {
			runCase(t, tc.checkpoint, tc.wantChecksum)
		})
	}
}
