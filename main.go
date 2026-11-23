package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
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

func loadconfig(tgt_name string) error{
	data, err := os.ReadFile("buildtool.json")
	if err != nil {
		return fmt.Errorf("couldn't read buildtool.json due to: %w", err)
	}

	var config Config

	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("couldn't unpack json: %w", err)
	}

	byOutput := make(map[string]Target)

	for _, target := range config.Targets {
		byOutput[target.Output] = target
	}

	index := slices.IndexFunc(config.Targets, func(target Target) bool {
		return target.Name == tgt_name
	})

	if index == -1 {
		return fmt.Errorf("target %q not found", tgt_name)
	}

	target := config.Targets[index]
	states := make(map[string]int)
	var order []Target
	var visited []Target
	if err := collectBuildOrder(target, byOutput, states, &order, visited); err != nil {
    	return err
	}
	for _, target := range order {

		fmt.Println(target.Command)
		if len(target.Command) == 0 {
			return fmt.Errorf("target %q has no command",target.Name)
		}

		cmd := exec.Command(target.Command[0], target.Command[1:]...)
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stdout
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("target %q failed:%w", target.Name, err)
		}
	}
	return nil
}

func collectBuildOrder(target Target, byOutput map[string]Target, 
	states map[string]int, order *[]Target, visited []Target) error{
	//0 is unvisted, 1 is visiting, 2 is finished
	state := states[target.Output]
	visited = append(visited,target)
	if state == 2 {
		return nil
	} else if state == 1 {
		start := slices.IndexFunc(visited, func(step Target) bool {
    	return step.Output == target.Output})
		
		var names []string
		for _, step := range visited[start:] {
    		names = append(names,step.Name)
		}
		return fmt.Errorf("cycle found: %s", strings.Join(names, " -> "))
	}

	states[target.Output] = 1
	for _, input := range target.Inputs {
		producer, ok := byOutput[input]
		if ok {
			if err := collectBuildOrder(producer, byOutput, states, order, visited); err != nil {
    			return err
			}
		}
	}
	states[target.Output] = 2
	*order = append(*order, target)
	return nil
}

func main() {
	if len(os.Args)!=3 || os.Args[1] != "build"{
		fmt.Fprintln(os.Stderr,"wrong usage\nuse 'go run . build <target_name>'")
		os.Exit(1)
	}
	if err:= loadconfig(os.Args[2]); err!= nil{
		fmt.Fprintln(os.Stderr,err)
		os.Exit(1)
	}
}
