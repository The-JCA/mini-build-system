package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
)

type Config struct {
	Targets []Target `json:"targets"`
}

type Target struct {
	Name    string   `json:"name"`
	Inputs  []string `json:"inputs"`
	Output  string   `json:"output"`
	Command []string `json:"command"`
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
		byOutput[target.Output] = target
	}

	index := slices.IndexFunc(config.Targets, func(target Target) bool {
		return target.Name == "app"
	})

	if index == -1 {
		fmt.Fprintln(os.Stderr, "target app not found")
		os.Exit(1)
	}

	target := config.Targets[index]
	states := make(map[string]int)
	var order []Target
	collectBuildOrder(target, byOutput, states, &order)
	for _, target := range order {

		fmt.Println(target.Command)
		if len(target.Command) == 0 {
			fmt.Fprintln(os.Stderr, target.Name, " has no command")
			os.Exit(1)
		}

		cmd := exec.Command(target.Command[0], target.Command[1:]...)
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stdout
		if err := cmd.Run(); err != nil {
			fmt.Fprintln(os.Stderr, target.Name, "has error:", err)
			os.Exit(1)
		}
	}
}

func collectBuildOrder(target Target, byOutput map[string]Target, states map[string]int, order *[]Target) {
	//0 is unvisted, 1 is visiting, 2 is finished
	state := states[target.Output]

	if state == 2 {
		return
	} else if state == 1 {
		fmt.Fprintln(os.Stderr, "Cycle found at", target.Name)
		os.Exit(1)
	}

	states[target.Output] = 1
	for _, input := range target.Inputs {
		producer, ok := byOutput[input]
		if ok {
			collectBuildOrder(producer, byOutput, states, order)
		}
	}
	states[target.Output] = 2
	*order = append(*order, target)
}

func main() {
	loadconfig()
}
