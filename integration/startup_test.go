package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A supervisor must distinguish a failed listener from a clean shutdown.
func TestFailedStartupExitsNonZero(t *testing.T) {
	for _, service := range []string{"menu-service", "order-service", "notification-service"} {
		t.Run(service, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, "go", "run", "./"+service)
			command.Dir = ".."
			command.Env = append(os.Environ(), "PORT=invalid-port", "DB_PATH="+filepath.Join(t.TempDir(), "test.db"))
			output, err := command.CombinedOutput()
			if !strings.Contains(string(output), "listen tcp:") {
				t.Fatalf("did not reach the listener: %s", output)
			}
			if ctx.Err() != nil {
				t.Fatalf("startup did not finish: %s", output)
			}
			if err == nil {
				t.Fatalf("failed startup returned exit code 0: %s", output)
			}
		})
	}
}
