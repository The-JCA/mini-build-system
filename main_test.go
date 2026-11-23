package main

import (
	"slices"
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
