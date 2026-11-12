package utils

import (
	"bufio"
	"entrypoints/config"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
)


type HostProxy struct {
	IP string
	SSHConfig *config.SSHConfig
	PbsJobID string
	remoteClient *ssh.Client
	mu           sync.Mutex
	absCurrentPath		string
	envs		map[string](string)
}

var LocalProxy *HostProxy
// var WorkerProxys []*HostProxy

func NewHostProxy(ip string, sshConfig *config.SSHConfig) (*HostProxy, error) {
    hp := &HostProxy{
        IP:   ip,
        SSHConfig: sshConfig,
    }

	// if err := ensureAndClearFile(stdoutFilePath); err != nil {
    //     return nil, fmt.Errorf("Failed to prepare stdout file: %v", err)
    // }
    // if err := ensureAndClearFile(stderrFilePath); err != nil {
    //     return nil, fmt.Errorf("Failed to prepare stderr file: %v", err)
    // }

    if !hp.isLocalIP() {
        hp.ensureReliableSSHClient()
    }
	hp.envs = make(map[string]string)

    return hp, nil
}

// ensureAndClearFile checks if the file exists; if not, it creates it.
// If it exists, it clears the content.
func ensureAndClearFile(filePath string) error {
    file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
    if err != nil {
        return err
    }
    defer file.Close()
    return nil
}

// getReliableSSHClient ensures hp.remoteClient is established and valid.
func (hp *HostProxy) ensureReliableSSHClient() {
    hp.mu.Lock()
    defer hp.mu.Unlock()

    // If no existing client, establish a new connection
    if hp.remoteClient == nil {
        hp.connectSSH()
    }

    // Check if the existing connection is alive
    _, _, err := hp.remoteClient.SendRequest("keepalive@openssh.com", true, nil)
    if err == nil {
        return // Connection is still valid
    }

    // Connection is dead, reconnect
    fmt.Println("Reconnecting SSH client...")
    hp.connectSSH()
}

// connectSSH establishes a new SSH connection. Fail the whole program if it fails to establish connection
func (hp *HostProxy) connectSSH() {
	var client *ssh.Client
	var err error

	client, err = ConnectWithPrivateKey(hp.IP, hp.SSHConfig.User, hp.SSHConfig.PrivateKeyPath, hp.SSHConfig.Port)
	CheckErrorWithMsg(err, "Connect to remote ssh %s failed!\n", hp.IP)
    hp.remoteClient = client
}

func (hp *HostProxy) Chdir(path string) {
	absPath, err := filepath.Abs(path)
	CheckErrorWithMsg(err, "%s doesn't exist!", path)
	hp.absCurrentPath = absPath
}

func (hp *HostProxy) Setenv(key string, val string) {
	hp.envs[key] = val
}

func (hp *HostProxy) SetpbsJobID(pbsJobID string) {
	hp.PbsJobID = pbsJobID
}


func (hp *HostProxy) ExecShellCmd(cmd string, pars ...any) (string, error) {
	stdout := ""
	var err error
	// First cd to current path before running the cmd
	envVars := ""
	for key, value := range hp.envs {
		envVars += fmt.Sprintf("export %s=%s && ", key, value)
	}
	cmd = fmt.Sprintf("%scd %s && %s",envVars, hp.absCurrentPath, cmd)
	if hp.isLocalIP() {
		stdout, err = ExecLocalShellCmd(cmd, pars ...)
	} else {
		hp.ensureReliableSSHClient()
		stdout, err = ExecRemoteShellCmd(hp.PbsJobID, *hp.SSHConfig, cmd, pars ...)
	}
	return stdout, err
}

func (hp *HostProxy) ExecShellCmdAsync(stdoutFilePath, stderrFilePath, cmd string, pars ...any) {
	// First cd to current path before running the cmd
	err := ensureAndClearFile(stdoutFilePath)
	if err != nil {
		ErrorPrintf("error when ensuring file %s: %v\n", stdoutFilePath, err)
		return
	}
	err = ensureAndClearFile(stderrFilePath)
	if err != nil {
		ErrorPrintf("error when ensuring file %s: %v\n", stderrFilePath, err)
		return
	}
	envVars := ""
	for key, value := range hp.envs {
		envVars += fmt.Sprintf("export %s=%s && ", key, value)
	}
	cmd = fmt.Sprintf("%scd %s && %s",envVars, hp.absCurrentPath, cmd)
	if hp.isLocalIP() {
		ExecLocalShellCmdAsync(stdoutFilePath, stderrFilePath, cmd, pars ...)
	} else {
		hp.ensureReliableSSHClient()
		ExecRemoteShellCmdAsync(hp.PbsJobID, *hp.SSHConfig, stdoutFilePath, stderrFilePath, cmd, pars ...)
	}
	
}

func (hp* HostProxy) isLocalIP() bool {
	localIPMap := map[string]bool{
		"localhost": true,
		"0.0.0.0":   true,
		"127.0.0.1": true,
	}
    return localIPMap[hp.IP]
}
func (hp *HostProxy) KillProcessesByName(processName string, isRemote bool) {
	// Construct the command to find PIDs of the processes matching the given name
	cmd := fmt.Sprintf("ps aux | grep %s | grep -v grep | awk '{print $2}'", processName)
	if isRemote {
		cmd = fmt.Sprintf("ps aux | grep %s | grep -v grep | awk '{print \\$2}'", processName)
	}

	out, err := hp.ExecShellCmd(cmd)
	if err != nil {
		ErrorPrintf("Failed to execute command: %s\n", cmd)
		return
	}

	pids := strings.Fields(out)
	if len(pids) == 0 {
		InfoPrintf("No processes named %s found.\n", processName)
		return
	}

	for _, pid := range pids {
		_, err = hp.ExecShellCmd("kill -9 %s", pid)
		if err != nil {
			ErrorPrintf("Error terminating process with name: %s, PID: %s: %v\n",processName, pid, err)
			return
		}
		if err != nil {
			ErrorPrintf("Failed to kill process with name: %s, PID %s: %v\n",processName, pid, err)
		} else {
			InfoPrintf("Killed process with name: %s, PID: %s\n",processName, pid)
		}
	}

	InfoPrintf("All matching processes named %s have been killed.\n", processName)
}

// KillContainerByName terminates and removes Docker containers matching the given name.
// It follows the logic of KillProcessesByName: finding IDs via grep/awk and removing them one by one.
func (hp *HostProxy) KillContainerByName(containerName string, isRemote bool) {
	// Construct the command to find Container IDs of the containers matching the given name.
	// We use 'docker ps -a' to list all containers, 'grep' to filter by the name,
	// and 'awk' to print the first column (Container ID).
	cmd := fmt.Sprintf("docker ps -a | grep %s | awk '{print $1}'", containerName)
	if isRemote {
		// Escape the $ for remote execution, similar to the process version
		cmd = fmt.Sprintf("docker ps -a | grep %s | awk '{print \\$1}'", containerName)
	}

	out, err := hp.ExecShellCmd(cmd)
	if err != nil {
		ErrorPrintf("Failed to execute command: %s\n", cmd)
		return
	}

	ids := strings.Fields(out)
	if len(ids) == 0 {
		InfoPrintf("No containers named %s found.\n", containerName)
		return
	}

	for _, id := range ids {
		// docker rm -f effectively "kills" (stops) and removes the container
		_, err = hp.ExecShellCmd("docker rm -f %s", id)
		if err != nil {
			ErrorPrintf("Error removing container with name: %s, ID: %s: %v\n", containerName, id, err)
			return
		}

		InfoPrintf("Removed container with name: %s, ID: %s\n", containerName, id)
	}

	InfoPrintf("All matching containers named %s have been removed.\n", containerName)
}

func (hp* HostProxy) KillProcessGroupByName(processName string, isRemote bool) {
	// Construct the command to find PIDs of the processes matching the given name
	cmd := fmt.Sprintf("ps aux | grep %s | grep -v grep | awk '{print $2}'", processName)
	if isRemote == true {
		cmd = fmt.Sprintf("ps aux | grep %s | grep -v grep | awk '{print \\$2}'", processName)
	}
	out, err := hp.ExecShellCmd(cmd)
	if err != nil {
		ErrorPrintf("Failed to execute command: %s\n", cmd)
		return
	}
	pids := strings.Fields(out)
	if len(pids) == 0 {
		InfoPrintf("No processes named %s found.\n", processName)
		return
	} 

	pgidSet := make(map[string]struct{}) // use map to remove duplicates
	for _, pid := range pids {
		pgid, err := hp.ExecShellCmd("ps -o pgid= -p %s", pid)
		if err != nil {
			ErrorPrintf("Failed to get pgid for pid %s: %v\n", pid, err)
			continue
		}
		pgid = strings.TrimSpace(pgid) // clean up any spaces/newlines
		if pgid != "" {
			pgidSet[pgid] = struct{}{}
		}
	}

	// Convert map keys to a list
	var pgidList []string
	for pgid := range pgidSet {
		pgidList = append(pgidList, pgid)
	}

	for _, pgid := range pgidList {
		_, err = hp.ExecShellCmd("kill -2 -%s", pgid)

		if err != nil {
			ErrorPrintf("Failed to kill process group with name: %s, PGID %s: %v",processName, pgid, err)
		} else {
			InfoPrintf("Killed process group with name: %s, PGID: %s\n",processName, pgid)
		}
	}
	InfoPrintf("All processes group named %s have been killed.\n", processName)
}

// kill all processes running on GPUs
func (hp* HostProxy) KillGpu() {
	cmd := "nvidia-smi --query-compute-apps=pid --format=csv,noheader,nounits"
	out, err := hp.ExecShellCmd(cmd)
	if err != nil {
		ErrorPrintf("Failed to obtain pid running on GPUs\n")
		return
	}

	// Read the PIDs from the output and attempt to terminate each process
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		pid := scanner.Text()
		InfoPrintf("Trigger to end GPU process group with PID: %s\n", pid)

		// Convert PID to an integer and attempt to kill the process
		pgid, err := hp.ExecShellCmd("ps -o pgid= -p %s", pid)
		if err != nil {
			ErrorPrintf("Failed to obtain pgid for with GPU process, PID: %s, error: %v\n", pid, err)
			return
		}
		_, err = hp.ExecShellCmd("kill -9 -%s", pgid)
		if err != nil {
			ErrorPrintf("Error terminating GPU process %s: %v\n", pid, err)
			return
		}
	}
	if err := scanner.Err(); err != nil {
		ErrorPrintf("Error reading nvidia-smi output: %v\n", err)
		return
	}
	InfoPrintf("All processes running on GPU has been cleared\n")
}

func (hp* HostProxy) StopPBSJobsByName(jobName string) {
	_, err := hp.ExecShellCmd("qstat -u $USER | grep '%s' | awk '{print $1}' | xargs qdel", jobName)
	if err != nil {
		ErrorPrintf("Failed to stop pbs job with the name: %s, error: %v\n", jobName, err)
	}
	InfoPrintf("Delete all pbs jobs with the name: %s\n", jobName)
}

func (hp* HostProxy) StopPBSJobsByID(jobID string) {
	_ ,err := hp.ExecShellCmd("qdel %s", jobID)
	if err != nil {
		ErrorPrintf("Failed to stop pbs job with the ID: %s, error: %v\n", jobID, err)
	}
	InfoPrintf("Delete pbs job with the ID: %s\n", jobID)
}