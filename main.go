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
}
