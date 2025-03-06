package main

import (
	"context"
	"fmt"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/client"
	"gopkg.in/yaml.v3"
)

type ComposeService struct {
	Image           string              `yaml:"image,omitempty"`
	Ports           []string            `yaml:"ports,omitempty"`
	Volumes         []string            `yaml:"volumes,omitempty"`
	Environment     map[string]string   `yaml:"environment,omitempty"`
	Restart         string              `yaml:"restart,omitempty"`
	Resources       map[string]string   `yaml:"resources,omitempty"`
	Networks        []string            `yaml:"networks,omitempty"`
	CapAdd          []string            `yaml:"cap_add,omitempty"`
	CapDrop         []string            `yaml:"cap_drop,omitempty"`
	Privileged      bool                `yaml:"privileged,omitempty"`
	Healthcheck     *ComposeHealthcheck `yaml:"healthcheck,omitempty"`
	Tty             bool                `yaml:"tty,omitempty"`
	User            string              `yaml:"user,omitempty"`
	Cmd             []string            `yaml:"command,omitempty"`
	Entrypoint      []string            `yaml:"entrypoint,omitempty"`
	Labels          map[string]string   `yaml:"labels,omitempty"`
	Hostname        string              `yaml:"hostname,omitempty"`
	Domainname      string              `yaml:"domainname,omitempty"`
	OpenStdin       bool                `yaml:"open_stdin,omitempty"`
	StdinOnce       bool                `yaml:"stdin_once,omitempty"`
	WorkingDir      string              `yaml:"working_dir,omitempty"`
	NetworkDisabled bool                `yaml:"network_disabled,omitempty"`
	StopSignal      string              `yaml:"stop_signal,omitempty"`
	StopTimeout     *int                `yaml:"stop_timeout,omitempty"`
	Shell           []string            `yaml:"shell,omitempty"`
}

type ComposeHealthcheck struct {
	Test        []string      `yaml:"test,omitempty"`
	Interval    time.Duration `yaml:"interval,omitempty"`
	Timeout     time.Duration `yaml:"timeout,omitempty"`
	Retries     int           `yaml:"retries,omitempty"`
	StartPeriod time.Duration `yaml:"start_period,omitempty"`
}

type ComposeFile struct {
	Version  string                    `yaml:"version"`
	Services map[string]ComposeService `yaml:"services"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: docker-compose-gen <container_id> [output_file]")
		os.Exit(1)
	}

	containerID := os.Args[1]
	var outputFile string

	if len(os.Args) > 2 {
		outputFile = os.Args[2]
	}

	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating Docker client: %v\n", err)
		os.Exit(1)
	}
	defer cli.Close()

	containerJSON, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error inspecting container %s: %v\n", containerID, err)
		os.Exit(1)
	}

	imageJSON, err := cli.ImageInspect(ctx, containerJSON.Config.Image)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error inspecting image %s: %v\n", containerJSON.Config.Image, err)
		os.Exit(1)
	}

	compose := generateCompose(containerJSON, imageJSON)

	yamlData, err := yaml.Marshal(compose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshalling YAML: %v\n", err)
		os.Exit(1)
	}

	if outputFile != "" {
		err = os.WriteFile(outputFile, yamlData, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing to file %s: %v\n", outputFile, err)
			os.Exit(1)
		}
		fmt.Printf("Compose file written to %s\n", outputFile)
	} else {
		fmt.Println(string(yamlData))
	}
}

func generateCompose(containerJSON container.InspectResponse, imageJSON image.InspectResponse) ComposeFile {
	compose := ComposeFile{
		Version:  "3.8",
		Services: make(map[string]ComposeService),
	}

	service := ComposeService{
		Image:           containerJSON.Config.Image,
		Ports:           make([]string, 0),
		Volumes:         make([]string, 0),
		Environment:     make(map[string]string),
		Restart:         string(containerJSON.HostConfig.RestartPolicy.Name),
		Resources:       make(map[string]string),
		Networks:        make([]string, 0),
		CapAdd:          containerJSON.HostConfig.CapAdd,
		CapDrop:         containerJSON.HostConfig.CapDrop,
		Privileged:      containerJSON.HostConfig.Privileged,
		Healthcheck:     nil,
		Tty:             containerJSON.Config.Tty,
		User:            containerJSON.Config.User,
		Cmd:             nil,
		Entrypoint:      nil,
		Labels:          make(map[string]string),
		Hostname:        "",
		Domainname:      containerJSON.Config.Domainname,
		OpenStdin:       containerJSON.Config.OpenStdin,
		StdinOnce:       containerJSON.Config.StdinOnce,
		WorkingDir:      "",
		NetworkDisabled: containerJSON.Config.NetworkDisabled,
		StopSignal:      containerJSON.Config.StopSignal,
		StopTimeout:     containerJSON.Config.StopTimeout,
		Shell:           containerJSON.Config.Shell,
	}

	for p, bindings := range containerJSON.HostConfig.PortBindings {
		for _, binding := range bindings {
			portMapping := binding.HostPort + ":" + p.Port()
			if binding.HostIP != "" && binding.HostIP != "0.0.0.0" {
				portMapping = binding.HostIP + ":" + binding.HostPort + ":" + p.Port()
			}
			if p.Proto() == "udp" {
				portMapping += "/udp"
			}
			service.Ports = append(service.Ports, portMapping)
		}
	}

	for _, mount := range containerJSON.Mounts {
		service.Volumes = append(service.Volumes, fmt.Sprintf("%s:%s", mount.Source, mount.Destination))
	}

	containerEnv := parseEnv(containerJSON.Config.Env)
	imageEnv := parseEnv(imageJSON.Config.Env)

	for key, value := range containerEnv {
		if imageEnv[key] != value {
			service.Environment[key] = value
		}
	}

	if containerJSON.HostConfig.CPUPeriod > 0 {
		service.Resources["cpus"] = fmt.Sprintf("%.2f", float64(containerJSON.HostConfig.CPUQuota)/float64(containerJSON.HostConfig.CPUPeriod))
	}

	if containerJSON.HostConfig.Memory > 0 {
		service.Resources["mem_limit"] = strconv.FormatInt(containerJSON.HostConfig.Memory, 10)
	}

	// Network filtering
	for networkName := range containerJSON.NetworkSettings.Networks {
		if !isComposeNetwork(networkName) {
			service.Networks = append(service.Networks, networkName)
		}
	}

	// Healthcheck comparison
	if containerJSON.Config.Healthcheck != nil {
		if imageJSON.Config.Healthcheck == nil || !healthchecksEqual(containerJSON.Config.Healthcheck, imageJSON.Config.Healthcheck) {
			service.Healthcheck = &ComposeHealthcheck{
				Test:        containerJSON.Config.Healthcheck.Test,
				Interval:    time.Duration(containerJSON.Config.Healthcheck.Interval),
				Timeout:     time.Duration(containerJSON.Config.Healthcheck.Timeout),
				Retries:     int(containerJSON.Config.Healthcheck.Retries),
				StartPeriod: time.Duration(containerJSON.Config.Healthcheck.StartPeriod),
			}
		}
	}

	// Label comparison
	for key, value := range containerJSON.Config.Labels {
		if imageJSON.Config.Labels[key] != value && !strings.HasPrefix(key, "com.docker.compose") {
			service.Labels[key] = value
		}
	}

	// Entrypoint comparison
	if !strSlicesEqual(containerJSON.Config.Entrypoint, imageJSON.Config.Entrypoint) {
		service.Entrypoint = containerJSON.Config.Entrypoint
	}

	// Cmd comparison
	if !strSlicesEqual(containerJSON.Config.Cmd, imageJSON.Config.Cmd) {
		service.Cmd = containerJSON.Config.Cmd
	}

	// WorkingDir comparison
	if containerJSON.Config.WorkingDir != imageJSON.Config.WorkingDir {
		service.WorkingDir = containerJSON.Config.WorkingDir
	}

	// Hostname comparison
	if containerJSON.Config.Hostname != "" && !isRandomHostname(containerJSON.Config.Hostname, containerJSON.ID) {
		service.Hostname = containerJSON.Config.Hostname
	}

	compose.Services[containerJSON.Name[1:]] = service
	return compose
}

func healthchecksEqual(a, b *container.HealthConfig) bool {
	if len(a.Test) != len(b.Test) || a.Interval != b.Interval || a.Timeout != b.Timeout || a.Retries != b.Retries || a.StartPeriod != b.StartPeriod {
		return false
	}
	for i, v := range a.Test {
		if v != b.Test[i] {
			return false
		}
	}
	return true
}

func isComposeNetwork(networkName string) bool {
	return strings.Contains(networkName, "_default") || strings.Contains(networkName, "_")
}

func isRandomHostname(hostname, containerID string) bool {
	return len(hostname) == 12 && containerID != "" && containerID != hostname && containerID[:12] == hostname
}

func strSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

func parseEnv(envVars []string) map[string]string {
	envMap := make(map[string]string)
	for _, env := range envVars {
		parts := stringParts(env, "=")
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}
	return envMap
}

func stringParts(s, sep string) []string {
	idx := -1
	for i := 0; i < len(s); i++ {
		if string(s[i]) == sep {
			idx = i
			break
		}
	}
	if idx == -1 {
		return []string{s}
	}
	return []string{s[:idx], s[idx+1:]}
}
