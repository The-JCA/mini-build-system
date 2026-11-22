package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	Targets []Target `json:"targets"`
	
}

type Target struct {
	Name string `json:"name"`
	Inputs []string `json:"inputs"`
	Output string `json:"output"`
}

func loadconfig() {
	data, err := os.ReadFile("buildtool.json")
	if err != nil {
		fmt.Fprintln(os.Stderr, "couldn't read file due to: ", err)
		os.Exit(1)
	}

	var config Config

	if err := json.Unmarshal(data, &config); err != nil {
		fmt.Fprintln(os.Stderr, "couldn't unpack json: ", err)
		os.Exit(1)
	}

	byOutput := make(map[string]Target)

	for _, target := range config.Targets {
		fmt.Fprintln(os.Stdout, target.Name)
		byOutput[target.Output] = target
	}

	fmt.Fprintln(os.Stdout,byOutput["build/main.o"].Name)
}

func main() {
	loadconfig()
	// args := os.Args[1:]
	// if len(args) == 0 {
	// 	fmt.Fprintln(os.Stderr, "usage: gcc-runner <gcc arguments>")
	// 	os.Exit(2)
	// }

	// command := exec.Command("gcc", args...)
	// command.Stdout = os.Stdout
	// command.Stderr = os.Stderr

	// fmt.Fprintln(os.Stderr, "gcc ", args)

	// if err := command.Run(); err != nil {
	// 	fmt.Fprintf(os.Stderr, "gcc failed: %v\n", err)

	// 	var exitError *exec.ExitError
	// 	if errors.As(err, &exitError) && exitError.ExitCode() > 0 {
	// 		os.Exit(exitError.ExitCode())
	// 	}
	// 	os.Exit(1)
	// }
}
