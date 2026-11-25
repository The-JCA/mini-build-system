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
	"syscall"
	"testing"
	"time"
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

func gccCommandLines(output string) []string {
	var commands []string
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "[gcc ") {
			commands = append(commands, line)
		}
	}
	return commands
}

func TestFullAppIncrementalBuild(t *testing.T) {
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "buildtool")

	cmd := exec.Command("go", "build", "-o", binary, ".")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build tool: %v\n%s", err, output)
	}

	if err := os.MkdirAll(filepath.Join(workspace, "example"), 0755); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"buildtool.json", "example/main.c", "example/parser.h", "example/parser.c"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if err := os.WriteFile(filepath.Join(workspace, path), data, 0644); err != nil {
			t.Fatalf("copy %s: %v", path, err)
		}
	}

	buildApp := func() string {
		t.Helper()

		cmd := exec.Command(binary, "build", "app")
		cmd.Dir = workspace
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("build App: %v\n%s", err, output)
		}
		return string(output)
	}

	assertCommands := func(stage, output string, want ...string) {
		t.Helper()

		got := gccCommandLines(output)
		if len(got) != len(want) {
			t.Fatalf("%s ran %d GCC commands, want %d\n%s",
				stage, len(got), len(want), output)
		}
		for i, fragment := range want {
			if !strings.Contains(got[i], fragment) {
				t.Fatalf("%s command %d = %q, want it to contain %q",
					stage, i+1, got[i], fragment)
			}
		}
	}

	appendComment := func(path string) {
		t.Helper()

		file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			t.Fatalf("open %s: %v", path, err)
		}
		_, writeErr := file.WriteString("\n/* comment-only edit */\n")
		closeErr := file.Close()
		if writeErr != nil {
			t.Fatalf("append comment to %s: %v", path, writeErr)
		}
		if closeErr != nil {
			t.Fatalf("close %s: %v", path, closeErr)
		}
	}

	const (
		compileMain   = "-c example/main.c"
		compileParser = "-c example/parser.c"
		linkApp       = "build/main.o build/parser.o"
	)

	assertCommands("clean build", buildApp(), compileMain, compileParser, linkApp)
	assertCommands("unchanged build", buildApp())

	parserPath := filepath.Join(workspace, "example", "parser.c")
	appendComment(parserPath)
	output := buildApp()
	assertCommands("parser comment edit", output, compileParser)
	if !strings.Contains(output, "skipping app: up to date") {
		t.Fatalf("expected app to skip after the parser comment edit:\n%s", output)
	}

	source, err := os.ReadFile(parserPath)
	if err != nil {
		t.Fatalf("read parser source: %v", err)
	}
	edited := strings.Replace(string(source), "return digit - '0';", "return digit - '0' + 1;", 1)
	if edited == string(source) {
		t.Fatal("expected to find parse_digit's return statement")
	}
	if err := os.WriteFile(parserPath, []byte(edited), 0644); err != nil {
		t.Fatalf("write parser source: %v", err)
	}
	assertCommands("parser code edit", buildApp(), compileParser, linkApp)

	headerPath := filepath.Join(workspace, "example", "parser.h")
	appendComment(headerPath)
	output = buildApp()
	assertCommands("shared header comment edit", output, compileMain, compileParser)
	if !strings.Contains(output, "skipping app: up to date") {
		t.Fatalf("expected app to skip after the shared header comment edit:\n%s", output)
	}
}

func TestExternalKillRecovery(t *testing.T) {
	workspace := t.TempDir()
	binary := filepath.Join(workspace, "buildtool")

	cmd := exec.Command("go", "build", "-o", binary, ".")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build tool: %v\n%s", err, output)
	}

	if err := os.MkdirAll(filepath.Join(workspace, "example"), 0755); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"buildtool.json", "example/main.c", "example/parser.h", "example/parser.c"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if err := os.WriteFile(filepath.Join(workspace, path), data, 0644); err != nil {
			t.Fatalf("copy %s: %v", path, err)
		}
	}

	buildApp := func() string {
		t.Helper()

		cmd := exec.Command(binary, "build", "app")
		cmd.Dir = workspace
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("build App: %v\n%s", err, output)
		}
		return string(output)
	}

	buildApp()
	objectPath := filepath.Join(workspace, "build", "parser.o")
	statePath := filepath.Join(workspace, "build", ".fingerprints.json")
	parserAdata, err := os.ReadFile(objectPath)
	if err != nil {
		t.Fatalf("read parser source: %v", err)
	}
	stateAdata, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state source: %v", err)
	}

	parserPath := filepath.Join(workspace, "example", "parser.c")
	source, err := os.ReadFile(parserPath)
	if err != nil {
		t.Fatalf("read parser source: %v", err)
	}
	edited := strings.Replace(string(source), "return digit - '0';", "return digit - '0' + 1;", 1)
	if edited == string(source) {
		t.Fatal("expected to find parse_digit's return statement")
	}
	if err := os.WriteFile(parserPath, []byte(edited), 0644); err != nil {
		t.Fatalf("write parser source: %v", err)
	}

	marker := filepath.Join(workspace, "ready-to-kill")
	cmd = exec.Command(binary, "build", "app")
	cmd.Dir = workspace
	cmd.Env = append(cmd.Environ(), "BUILDTOOL_TEST_PAUSE_FILE="+marker)

	if err := cmd.Start(); err != nil {
		t.Fatalf("start build: %v", err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := os.Stat(marker)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			t.Fatalf("check pause marker: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("build never reached the pause before output rename")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill build: %v", err)
	}
	err = cmd.Wait()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != -1 {
		t.Fatalf("expected build to be killed, got %v", err)
	}

	buildPath := filepath.Join(workspace, "build")
	contents, err := os.ReadDir(buildPath)
	if err != nil {
		t.Fatalf("read build directory after kill: %v", err)
	}

	foundStagingDir := false
	for _, entry := range contents {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), ".output-") {
			foundStagingDir = true
			break
		}
	}
	if !foundStagingDir {
		t.Fatal("expected the kill to leave a .output-* staging directory")
	}

	data, err := os.ReadFile(objectPath)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(data, parserAdata) {
		t.Fatal("Parser output is affected")
	}

	data, err = os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(data, stateAdata) {
		t.Fatal("kill changed the save build state")
	}

	output := buildApp()
	lines := gccCommandLines(output)
	if len(lines) != 2 || !strings.Contains(lines[0], "-c example/parser.c") ||
		!strings.Contains(lines[1], "build/main.o build/parser.o") {
		t.Fatalf("Expected parser recompile and app relink\n%v", output)
	}
	contents, err = os.ReadDir(buildPath)
	if err != nil {
		t.Fatalf("read build directory after retry: %v", err)
	}

	var names []string
	for _, entry := range contents {
		names = append(names, entry.Name())
	}
	want := []string{".fingerprints.json", "app", "main.o", "parser.o"}
	if !slices.Equal(names, want) {
		t.Fatalf("build entries after retry = %q, want %q", names, want)
	}

	output = buildApp()
	lines = gccCommandLines(output)
	if len(lines) != 0 {
		t.Fatalf("Expected no recompile\n%v", output)
	}

	buildpath := filepath.Join(workspace, "build")
	recoveredbuildpath := filepath.Join(workspace, "recovered-build")

	if err := os.Rename(buildpath, recoveredbuildpath); err != nil {
		t.Fatalf("move recovered build aside: %v", err)
	}

	output = buildApp()
	if got := len(gccCommandLines(output)); got != 3 {
		t.Fatalf("clean reference build ran %d GCC commands, want 3\n%s", got, output)
	}

	for _, path := range []string{"main.o", "parser.o", ".fingerprints.json", "app"} {
		newbuilddata, err := os.ReadFile(filepath.Join(buildpath, path))
		if err != nil {
			t.Fatalf("error reading %q in new build: %v", path, err)
		}
		recoveredbuilddata, err := os.ReadFile(filepath.Join(recoveredbuildpath, path))
		if err != nil {
			t.Fatalf("error reading %q in recovered build: %v", path, err)
		}
		if !slices.Equal(newbuilddata, recoveredbuilddata) {
			t.Fatalf("Data of recovered build doesn't match clearn for %q", path)
		}
	}
}

func TestTimedKillRecovery(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "buildtool")

	cmd := exec.Command("go", "build", "-o", binary, ".")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build tool: %v\n%s", err, output)
	}

	buildApp := func(t *testing.T, workspace string) string {
		t.Helper()

		cmd := exec.Command(binary, "build", "app")
		cmd.Dir = workspace
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("build App: %v\n%s", err, output)
		}
		return string(output)
	}

	runTimedKillTrial := func(t *testing.T, binary string, delay time.Duration, edit string) bool {
		t.Helper()
		workspace := t.TempDir()
		if err := os.MkdirAll(filepath.Join(workspace, "example"), 0755); err != nil {
			t.Fatal(err)
		}

		for _, path := range []string{"buildtool.json", "example/main.c", "example/parser.h", "example/parser.c"} {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			if err := os.WriteFile(filepath.Join(workspace, path), data, 0644); err != nil {
				t.Fatalf("copy %s: %v", path, err)
			}
		}
		buildApp(t, workspace)

		switch edit {
		case "none":
			// Leave the source files as they are.
		case "main.c":
			Path := filepath.Join(workspace, "example", "main.c")
			source, err := os.ReadFile(Path)
			if err != nil {
				t.Fatalf("read main source: %v", err)
			}
			edited := strings.Replace(string(source), "parse_digit('7')", "parse_digit('8')", 1)
			if edited == string(source) {
				t.Fatal("expected to find 7 in main")
			}
			if err := os.WriteFile(Path, []byte(edited), 0644); err != nil {
				t.Fatalf("write main source: %v", err)
			}
		case "parser.c":
			parserPath := filepath.Join(workspace, "example", "parser.c")
			source, err := os.ReadFile(parserPath)
			if err != nil {
				t.Fatalf("read parser source: %v", err)
			}
			edited := strings.Replace(string(source), "return digit - '0';", "return digit - '0' + 1;", 1)
			if edited == string(source) {
				t.Fatal("expected to find parse_digit's return statement")
			}
			if err := os.WriteFile(parserPath, []byte(edited), 0644); err != nil {
				t.Fatalf("write parser source: %v", err)
			}
		case "parser.h":
			Path := filepath.Join(workspace, "example", "parser.h")
			source, err := os.ReadFile(Path)
			if err != nil {
				t.Fatalf("read main source: %v", err)
			}
			edited := string(source) + "\n/* timed-kill test edit */\n"
			if err := os.WriteFile(Path, []byte(edited), 0644); err != nil {
				t.Fatalf("write parser header: %v", err)
			}
		default:
			t.Fatalf("unknown edit %q", edit)
		}

		cmd := exec.Command(binary, "build", "app")
		cmd.Dir = workspace
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		if err := cmd.Start(); err != nil {
			t.Fatalf("start build: %v", err)
		}

		time.Sleep(delay)

		killErr := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		waitErr := cmd.Wait()

		if killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
			t.Fatalf("kill build process group: %v", killErr)
		}
		if waitErr == nil {
			return false
		}
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) {
			t.Fatalf("wait for build: %v", waitErr)
		}
		status, ok := exitErr.Sys().(syscall.WaitStatus)
		if !ok || status.Signal() != syscall.SIGKILL {
			t.Fatalf("expected SIGKILL after %s, got %v", delay, waitErr)
		}

		buildApp(t, workspace)

		buildpath := filepath.Join(workspace, "build")
		recoveredbuildpath := filepath.Join(workspace, "recovered-build")

		if err := os.Rename(buildpath, recoveredbuildpath); err != nil {
			t.Fatalf("move recovered build aside: %v", err)
		}

		output := buildApp(t, workspace)
		if got := len(gccCommandLines(output)); got != 3 {
			t.Fatalf("clean reference build ran %d GCC commands, want 3\n%s", got, output)
		}

		for _, path := range []string{"main.o", "parser.o", ".fingerprints.json", "app"} {
			newbuilddata, err := os.ReadFile(filepath.Join(buildpath, path))
			if err != nil {
				t.Fatalf("error reading %q in new build: %v", path, err)
			}
			recoveredbuilddata, err := os.ReadFile(filepath.Join(recoveredbuildpath, path))
			if err != nil {
				t.Fatalf("error reading %q in recovered build: %v", path, err)
			}
			if !slices.Equal(newbuilddata, recoveredbuilddata) {
				t.Fatalf("Data of recovered build doesn't match clean build for %q", path)
			}
		}
		return true
	}

	const seed = 36
	rng := rand.New(rand.NewSource(seed))

	kills, completed := 0, 0
	edits := []string{"none", "main.c", "parser.c", "parser.h"}
	for i := 0; i < 10; i++ {
		delay := time.Duration(1+rng.Intn(20)) * time.Millisecond
		edit := edits[i%len(edits)]
		name := fmt.Sprintf("seed-%d/trial-%02d/edit-%s/delay-%s", seed, i, edit, delay)
		t.Run(name, func(t *testing.T) {
			if runTimedKillTrial(t, binary, delay, edit) {
				kills++
			} else {
				completed++
			}
		})
	}
	t.Logf("confirmed kills: %d, completed before kill: %d", kills, completed)
	if kills == 0 {
		t.Fatal("no trial exercised crash recovery")
	}
}
