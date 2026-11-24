package main

import (
	"slices"
	"testing"
	"path/filepath"
	"os/exec"
	"os"
	"strings"
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


func TestChangeOutputfile(t *testing.T){
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

	achecksum,err:=fileChecksum(objectPath)
	if err!=nil{
		t.Fatal(err)
	}

	sourceA,err := os.ReadFile(sourcePath)
	if err != nil {
			t.Fatalf("read %s: %v", sourcePath, err)
	}

	outputAfp,err := os.ReadFile(statePath)
	if err != nil {
			t.Fatalf("read %s: %v", statePath, err)
	}

	
	data,err := os.ReadFile("example/mainb.c")
	if err != nil {
			t.Fatalf("read %s: %v", "example/mainb.c", err)
	}
	if err := os.WriteFile(sourcePath, data, 0644); err != nil {
			t.Fatalf("copy %s: %v", sourcePath, err)
	}

	buildMain()
	bchecksum,err:=fileChecksum(objectPath)
	if err!=nil{
		t.Fatal(err)
	}
	if (achecksum==bchecksum){
		t.Fatal("Checksums of A and B are equal")
	}


	if err := os.WriteFile(sourcePath, sourceA, 0644); err != nil {
			t.Fatalf("copy %s: %v", sourcePath, err)
	}

	if err := os.WriteFile(statePath, outputAfp, 0644); err != nil {
			t.Fatalf("copy %s: %v", statePath, err)
	}


	buildMain()
	testchecksum,err:=fileChecksum(objectPath)
	if err!=nil{
		t.Fatal(err)
	}
	if achecksum!=testchecksum{
		t.Fatal("Checksum after restoration is not equal to Checksum of Version A")
	}

	if output := buildMain(); !strings.Contains(output, "skipping main: up to date") {
    t.Fatalf("expected unchanged build to skip main, got:\n%s", output)
}
}