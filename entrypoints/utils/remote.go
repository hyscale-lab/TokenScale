package utils

import (
	"entrypoints/config"
	"fmt"
	"os"

	"golang.org/x/crypto/ssh"
)

func ConnectWithPrivateKey(host, user, keyPath string, port int) (*ssh.Client, error) {
	key, err := os.ReadFile(keyPath)
	if err != nil {
		ErrorPrintf("Can not read private key file! Error: %v\n", err)
		return nil, err
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		ErrorPrintf("Cannot parse ssh key! Error: %v\n", err)
		return nil, err
	}

	sshConfig := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	address := fmt.Sprintf("%s:%d", host, port)
	client, err := ssh.Dial("tcp", address, sshConfig)
	if err != nil {
		ErrorPrintf("Can not connect to server! Error: %v\n", err)
		return nil, err
	}

	return client, nil
}

// Execute Shell Command Remotely
func ExecRemoteShellCmd(pbsJobID string ,sshConfig config.SSHConfig, cmd string, pars ...any) (string, error) {
	// Prepare command with parameters
	var cmdWithParams string
	if len(pars) > 0 {
		cmdWithParams = fmt.Sprintf(cmd, pars...)
	} else {
		cmdWithParams = cmd
	}
	out, err := ExecLocalShellCmd("PBS_JOBID=%s ssh -o StrictHostKeyChecking=no %s@%s -i %s -p %d \"%s\"", pbsJobID, sshConfig.User, sshConfig.Host, sshConfig.PrivateKeyPath, sshConfig.Port, cmdWithParams)


	if err != nil {
		err = &ShellError{Msg: fmt.Sprintf("%v", err), ExitCode: -1}
		return "", err
	}

	// Log command execution details
	DebugPrintf("Executing remote shell command: %s\n", cmdWithParams)

	return out, err
}

// Execute Shell Command Remotely
func ExecRemoteShellCmdAsync(pbsJobID string, sshConfig config.SSHConfig, stdoutFile string, stderrFile string, cmd string, pars ...any) error {
	// Prepare command with parameters
	var cmdWithParams string
	if len(pars) > 0 {
		cmdWithParams = fmt.Sprintf(cmd, pars...)
	} else {
		cmdWithParams = cmd
	}
	ExecLocalShellCmdAsync(stdoutFile, stderrFile, "PBS_JOBID=%s ssh -o StrictHostKeyChecking=no %s@%s -i %s -p %d '%s'", pbsJobID, sshConfig.User, sshConfig.Host, sshConfig.PrivateKeyPath, sshConfig.Port, cmdWithParams)

	// Log command execution details
	DebugPrintf("Executing remote shell command async: %s\n", cmdWithParams)

	return nil
}
