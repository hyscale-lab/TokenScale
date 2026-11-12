package utils

import (
	"fmt"
	"os"
	"strings"
)

type ContainerType int
const (
	SINGULARITY ContainerType = iota
	ENROOT
	DOCKER
)
func (c ContainerType) IsValid() bool {
	switch c {
	case SINGULARITY, ENROOT, DOCKER:
		return true
	default:
		return false
	}
}

func (c ContainerType) String() string {
	switch c {
	case SINGULARITY:
		return "SINGULARITY"
	case ENROOT:
		return "ENROOT"
	case DOCKER:
		return "DOCKER"
	default:
		return "UNKNOWN"
	}
}

var (
	PLATFORM_TO_CONTAINER_MAP = map[string]ContainerType{
		NSCC_2A_PLATFORM_STR: SINGULARITY,
		LOCAL_TEST_PLATFORM_STR: DOCKER,
		NSCC_2P_PLATFORM_STR: ENROOT,
	}
)

const LIQUIDLET_CONTAINER_NAME = "liquidlet"
const PROMETHEUS_CONTAINER_NAME = "prometheus"
const DISTSIM_CONTAINER_NAME = "distsim"

type ContainerManager struct {
	ContainerType ContainerType
	ContainerImagePath string
	Params string
	EnvMap map[string]string
	MountMap map[string]string
}

func NewContainerManager(containerType ContainerType, containerImagePath string, params string) (*ContainerManager, error) {
	if !containerType.IsValid() {
		return nil, fmt.Errorf("invalid container type: %d", containerType)
	}
	return &ContainerManager{ContainerType: containerType, ContainerImagePath: containerImagePath, Params: params, EnvMap: make(map[string]string), MountMap: make(map[string]string)}, nil
}

func (m *ContainerManager) AddNewEnv(key string, val string) {
	m.EnvMap[key] = val
}

func (m *ContainerManager) AddNewMount(hostFilePath string, containerFilePath string) {
	m.MountMap[hostFilePath] = containerFilePath
}

func (m *ContainerManager) GetContainerStartCmd(containerName string) string {
	switch m.ContainerType {
		case SINGULARITY:
			return m.getSingularityStartCmd()	
		case ENROOT:
			return m.getEnrootStartCmd(containerName)
		case DOCKER:
			return m.getDockerStartCmd(containerName)
		default:
			return ""
	}
}

func (m *ContainerManager) getSingularityStartCmd() string {
	platform := os.Getenv("PLATFORM")
	var cmd string
	if strings.HasPrefix(platform, "NSCC") {
		cmd = "module load singularity && singularity run --nv"
	} else {
		cmd = "singularity run --nv"
	}

	// Add environment variables
	for k, v := range m.EnvMap {
		cmd += fmt.Sprintf(" --env %s=%s", k, v)
	}

	// Add mounts
	for hostPath, containerPath := range m.MountMap {
		cmd += fmt.Sprintf(" --bind %s:%s", hostPath, containerPath)
	}

	// Add image path
	cmd += fmt.Sprintf(" %s", m.ContainerImagePath)

	// Add parameters
	cmd += fmt.Sprintf(" %s", m.Params)

	return cmd
}

func (m *ContainerManager) getEnrootStartCmd(containerName string) string {
	var cmdBuilder strings.Builder

	// Always create the container first
	cmdBuilder.WriteString(fmt.Sprintf("enroot create --name %s %s && ", containerName, m.ContainerImagePath))

	// Start the container by name
	cmdBuilder.WriteString("enroot start")

	// Add environment variables
	for k, v := range m.EnvMap {
		cmdBuilder.WriteString(fmt.Sprintf(" --env %s=%s", k, v))
	}

	// Add mounts
	for hostPath, containerPath := range m.MountMap {
		cmdBuilder.WriteString(fmt.Sprintf(" --mount %s:%s", hostPath, containerPath))
	}

	// Use the container name (not the image path)
	cmdBuilder.WriteString(fmt.Sprintf(" %s", containerName))

	// Add optional parameters
	if m.Params != "" {
		cmdBuilder.WriteString(fmt.Sprintf(" %s", m.Params))
	}

	return cmdBuilder.String()
}

// getDockerStartCmd generates the docker run command.
// Unlike enroot, docker run handles creation and starting in one step.
// It uses --network host to pass through the network stack.
func (m *ContainerManager) getDockerStartCmd(containerName string) string {
	var cmdBuilder strings.Builder

	// Start with docker run
	// --rm: Automatically remove the container when it exits (optional, but good practice for "run" commands)
	// --network host: Use the host's network stack directly
	cmdBuilder.WriteString(fmt.Sprintf("docker run --name %s --network host --gpus all", containerName))

	// Add environment variables
	// Docker uses -e or --env
	for k, v := range m.EnvMap {
		cmdBuilder.WriteString(fmt.Sprintf(" --env %s=%s", k, v))
	}

	// Add mounts
	// Docker uses -v (volume) or --mount. 
	// Format is -v hostPath:containerPath
	for hostPath, containerPath := range m.MountMap {
		cmdBuilder.WriteString(fmt.Sprintf(" -v %s:%s", hostPath, containerPath))
	}

	// Add the image (Docker image path/name)
	// In Docker run syntax, the image comes AFTER flags but BEFORE the command/params
	cmdBuilder.WriteString(fmt.Sprintf(" %s", m.ContainerImagePath))

	// Add optional parameters (command to run inside the container)
	if m.Params != "" {
		cmdBuilder.WriteString(fmt.Sprintf(" %s", m.Params))
	}

	return cmdBuilder.String()
}

