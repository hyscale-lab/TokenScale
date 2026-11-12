package utils

import (
	"entrypoints/config"
	"os"
	"testing"
)

func TestRemoteProxy(t *testing.T) {
	ip := "x1000c1s2b0n0"
	os.Setenv("PBS_JOBID", "9759267.pbs101")
	sshConfig := &config.SSHConfig{
		User: os.Getenv("USER"),
		Host: ip,
		Port: 22,
		PrivateKeyPath: "/path/to/.ssh/id_rsa",
	}	
	hp, err := NewHostProxy(ip, sshConfig)
	out, err := hp.ExecShellCmd("nvidia-smi")
	if err != nil {
		t.Fatalf("Executing failed, error: %v\n", err)
		return
	}
	InfoPrintf("Output: %s\n", out)
}