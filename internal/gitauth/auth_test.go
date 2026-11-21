package gitauth

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestAskpassKeepsCredentialOutOfArgumentsAndCleansUp(t *testing.T) {
	env, cleanup, err := Environment(Credential{Username: "token-user", Token: "private-example-token"})
	if err != nil {
		t.Fatal(err)
	}
	path := ""
	for _, item := range env {
		if strings.HasPrefix(item, "GIT_ASKPASS=") {
			path = strings.TrimPrefix(item, "GIT_ASKPASS=")
		}
	}
	if path == "" {
		t.Fatal("askpass hook missing")
	}
	for _, prompt := range []struct{ name, want string }{{"Username for HTTPS", "token-user"}, {"Password for HTTPS", "private-example-token"}} {
		command := exec.Command(path, prompt.name)
		command.Env = env
		output, err := command.Output()
		if err != nil || strings.TrimSpace(string(output)) != prompt.want {
			t.Fatalf("askpass %s: %q %v", prompt.name, output, err)
		}
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("askpass script remains: %v", err)
	}
}
