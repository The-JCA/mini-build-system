package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
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

type fingerprintInput struct {
	Path     string `json:"path"`
	Contents []byte `json:"contents"`
}

type fingerprintData struct {
	Inputs  []fingerprintInput `json:"inputs"`
	Command []string           `json:"command"`
}

type BuildRecord struct {
	InputFingerprint string `json:"input_fingerprint"`
	OutputChecksum   string `json:"output_checksum"`
}

func loadconfig(tgt_name string) error {
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
	if err := os.MkdirAll("build", 0755); err != nil {
		return fmt.Errorf("couldn't create build directory: %w", err)
	}

	lockFile, err := os.OpenFile(".buildtool.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("open workspace lock: %w", err)
	}
	defer lockFile.Close()

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock workspace: %w", err)
	}

	if err := cleanBuildTemps("build"); err != nil {
		return err
	}

	if err := cleanBuildTemps("build"); err != nil {
		return err
	}

	fpdata, err := os.ReadFile("build/.fingerprints.json")
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("trouble opening build/.fingerprints.json: %w", err)
	}

	fpmap := make(map[string]BuildRecord)
	if err == nil {
		if err := json.Unmarshal(fpdata, &fpmap); err != nil {
			return fmt.Errorf("couldn't unpack build/.fingerprints.json: %w", err)
		}
	}

	for _, target := range order {

		if len(target.Command) == 0 {
			return fmt.Errorf("target %q has no command", target.Name)
		}

		fingerprint, err := targetFingerprint(target)
		if err != nil {
			return fmt.Errorf("couldn't fingerprint target %q: %w", target.Name, err)
		}

		fmt.Printf("%s fingerprint: %s\n", target.Name, fingerprint)

		record, ok := fpmap[target.Output]
		if ok && record.InputFingerprint == fingerprint {
			checksum, err := fileChecksum(target.Output)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("check output %q: %w", target.Output, err)
			}
			if err == nil && checksum == record.OutputChecksum {
				fmt.Printf("skipping %s: up to date\n", target.Name)
				continue
			}
		}

		if err := runTarget(target); err != nil {
			return err
		}
		if os.Getenv("BUILDTOOL_TEST_INTERRUPT") == "before-build-record-save" {
			os.Exit(86)
		}
		newchecksum, err := fileChecksum(target.Output)
		if err != nil {
			return fmt.Errorf("%q: %w", target.Name, err)
		}
		fpmap[target.Output] = BuildRecord{InputFingerprint: fingerprint,
			OutputChecksum: newchecksum}
		value, err := json.Marshal(fpmap)
		if err != nil {
			return fmt.Errorf("couldn't convert to Json: %w", err)
		}
		if err := saveFingerprints(value); err != nil {
			return err
		}
	}
	return nil
}

func collectBuildOrder(target Target, byOutput map[string]Target,
	states map[string]int, order *[]Target, visited []Target) error {
	//0 is unvisted, 1 is visiting, 2 is finished
	state := states[target.Output]
	visited = append(visited, target)
	if state == 2 {
		return nil
	} else if state == 1 {
		start := slices.IndexFunc(visited, func(step Target) bool {
			return step.Output == target.Output
		})

		var names []string
		for _, step := range visited[start:] {
			names = append(names, step.Name)
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

func saveFingerprints(value []byte) error {
	tmp, err := os.CreateTemp("build", ".fingerprints-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary fingerprint file: %w", err)
	}

	path := tmp.Name()
	defer os.Remove(path)

	if _, err := tmp.Write(value); err != nil {
		tmp.Close()
		return fmt.Errorf("write temporary fingerprint file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary fingerprint file: %w", err)
	}
	if err := os.Rename(path, "build/.fingerprints.json"); err != nil {
		return fmt.Errorf("replace fingerprint file: %w", err)
	}
	return nil
}

func targetFingerprint(target Target) (string, error) {
	payload := fingerprintData{Command: target.Command}
	for _, input := range target.Inputs {
		data, err := os.ReadFile(input)
		if err != nil {
			return "", fmt.Errorf("couldn't read %q: %w", input, err)
		}
		payload.Inputs = append(payload.Inputs, fingerprintInput{Path: input, Contents: data})
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("couldn't encode fingerprint data: %w", err)
	}

	sum := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", sum), nil
}

func runTarget(target Target) error {
	idx := slices.Index(target.Command[1:], "{output}")
	if idx == -1 {
		return fmt.Errorf("No {output} found in command for %q", target.Name)
	}
	idx += 1
	if slices.Index(target.Command[idx+1:], "{output}") != -1 {
		return fmt.Errorf("target %q has more than one {output}", target.Name)
	}

	tmpDir, err := os.MkdirTemp(filepath.Dir(target.Output), ".output-*")
	if err != nil {
		return fmt.Errorf("create temporary %q output file: %w", target.Name, err)
	}
	path := filepath.Join(tmpDir, filepath.Base(target.Output))
	defer os.RemoveAll(tmpDir)

	cmdslice := slices.Clone(target.Command)
	cmdslice[idx] = path

	fmt.Println(target.Command)
	cmd := exec.Command(cmdslice[0], cmdslice[1:]...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("target %q failed:%w", target.Name, err)
	}

	if os.Getenv("BUILDTOOL_TEST_INTERRUPT") == "before-output-rename" {
		os.Exit(86)
	}

	//pause for external kill recovery
	if marker := os.Getenv("BUILDTOOL_TEST_PAUSE_FILE"); marker != "" {
		if err := os.WriteFile(marker, []byte("ready\n"), 0600); err != nil {
			return fmt.Errorf("write pause marker: %w", err)
		}
		for {
			time.Sleep(time.Second)
		}
	}

	if err := os.Rename(path, target.Output); err != nil {
		return fmt.Errorf("replace %q output file: %w", target.Name, err)
	}
	return nil
}

func fileChecksum(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("couldn't read %q: %w", path, err)
	}

	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum), nil
}
func cleanBuildTemps(path string) error {
	contents, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range contents {
		name := entry.Name()
		if strings.HasPrefix(name, ".output-") {
			if err := os.RemoveAll(filepath.Join(path, name)); err != nil {
				return err
			}
		} else if entry.Type().IsRegular() && strings.HasPrefix(name, ".fingerprints-") &&
			strings.HasSuffix(name, ".tmp") {
			if err := os.Remove(filepath.Join(path, name)); err != nil {
				return err
			}
		}
	}
	return nil
}

func main() {
	if len(os.Args) != 3 || os.Args[1] != "build" {
		fmt.Fprintln(os.Stderr, "wrong usage\nuse 'go run . build <target_name>'")
		os.Exit(1)
	}
	if err := loadconfig(os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

}
