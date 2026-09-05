package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zkfriendly/zone/internal/config"
)

type codexCapture struct {
	Args   []string
	Dir    string
	Prompt string
}

// Re-execute the Go test binary as a fake CLI to exercise actual process IO,
// exit statuses, and cancellation without requiring Codex or an account in CI.
func helperCodex(t *testing.T, mode string) codexProvider {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return codexProvider{
		command: executable,
		commandContext: func(ctx context.Context, command string, args ...string) *exec.Cmd {
			cmd := exec.CommandContext(ctx, command, append([]string{"-test.run=^TestCodexHelperProcess$", "--"}, args...)...)
			cmd.Env = append(os.Environ(), "ZONE_CODEX_HELPER="+mode)
			return cmd
		},
	}
}

func TestCodexHelperProcess(t *testing.T) {
	mode := os.Getenv("ZONE_CODEX_HELPER")
	if mode == "" {
		return
	}
	args := os.Args[slices.Index(os.Args, "--")+1:]
	output := args[slices.Index(args, "--output-last-message")+1]
	if mode == "wait" {
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	if mode == "fail" {
		fmt.Fprint(os.Stderr, strings.Repeat("x", 10000)+" login required")
		os.Exit(7)
	}
	if mode == "missing" {
		os.Exit(0)
	}
	answer := ""
	if mode == "capture" {
		prompt, err := io.ReadAll(os.Stdin)
		if err != nil {
			os.Exit(2)
		}
		dir, _ := os.Getwd()
		raw, _ := json.Marshal(codexCapture{Args: args, Dir: dir, Prompt: string(prompt)})
		answer = string(raw)
	}
	if mode == "oversize" {
		answer = strings.Repeat("x", (1<<20)+1)
	}
	fmt.Fprint(os.Stdout, "progress: this must never become the answer")
	if err := os.WriteFile(output, []byte(answer), 0o600); err != nil {
		os.Exit(3)
	}
	os.Exit(0)
}

func TestCodexProcessContract(t *testing.T) {
	for _, model := range []string{"", "chosen-model"} {
		t.Run("model="+model, func(t *testing.T) {
			p := helperCodex(t, "capture")
			p.model = model
			privateText := "note with quotes: \"hello\"; $(do-not-execute)\nsecond line"
			req := Request{Messages: []Message{{Role: "system", Content: "Label only"}, {Role: "user", Content: privateText}}}
			answer, err := p.Complete(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			var got codexCapture
			if err := json.Unmarshal([]byte(answer), &got); err != nil {
				t.Fatal(err)
			}
			for flag, value := range map[string]string{
				"--ask-for-approval": "never", "--sandbox": "read-only", "--color": "never",
			} {
				i := slices.Index(got.Args, flag)
				if i < 0 || i+1 >= len(got.Args) || got.Args[i+1] != value {
					t.Fatalf("missing %s %s: %v", flag, value, got.Args)
				}
			}
			for _, flag := range []string{"exec", "--ephemeral", "--skip-git-repo-check", "--ignore-user-config"} {
				if !slices.Contains(got.Args, flag) {
					t.Fatalf("missing %s: %v", flag, got.Args)
				}
			}
			for _, feature := range []string{"shell_tool", "plugins", "apps", "hooks", "multi_agent"} {
				i := slices.Index(got.Args, feature)
				if i < 1 || got.Args[i-1] != "--disable" {
					t.Fatalf("feature %s not disabled: %v", feature, got.Args)
				}
			}
			if got.Args[len(got.Args)-1] != "-" || strings.Contains(strings.Join(got.Args, " "), privateText) {
				t.Fatal("prompt must travel only over stdin")
			}
			encoded, _ := json.Marshal(req.Messages)
			if !strings.HasSuffix(got.Prompt, string(encoded)) {
				t.Fatalf("messages lost: %s", got.Prompt)
			}
			idx := slices.Index(got.Args, "--model")
			if model == "" && idx >= 0 || model != "" && (idx < 0 || got.Args[idx+1] != model) {
				t.Fatalf("model override: %v", got.Args)
			}
			if !strings.HasPrefix(filepath.Base(got.Dir), "zone-codex-") {
				t.Fatalf("unexpected working directory: %s", got.Dir)
			}
			if _, err := os.Stat(got.Dir); !os.IsNotExist(err) {
				t.Fatalf("request directory not cleaned: %v", err)
			}
		})
	}
}

func TestCodexFailures(t *testing.T) {
	for mode, want := range map[string]string{"fail": "login required", "missing": "read Codex answer", "empty": "returned no text", "oversize": "exceeds 1 MiB"} {
		t.Run(mode, func(t *testing.T) {
			p := helperCodex(t, mode)
			answer, err := p.Complete(context.Background(), Request{})
			if err == nil || !strings.Contains(err.Error(), want) || answer != "" {
				t.Fatalf("answer=%q error=%v", answer, err)
			}
			if len(err.Error()) > 4600 {
				t.Fatal("diagnostics were not bounded")
			}
		})
	}
	t.Run("missing executable", func(t *testing.T) {
		p := codexProvider{command: filepath.Join(t.TempDir(), "no-such-codex")}
		_, err := p.Complete(context.Background(), Request{})
		if err == nil || !strings.Contains(err.Error(), "Codex command in Config") {
			t.Fatalf("expected setup hint, got %v", err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		_, err := helperCodex(t, "wait").Complete(ctx, Request{})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected deadline error, got %v", err)
		}
	})
}

// Opt-in smoke test uses a synthetic note, never the user's notes or database.
func TestCodexLive(t *testing.T) {
	if os.Getenv("ZONE_TEST_CODEX") != "1" {
		t.Skip("set ZONE_TEST_CODEX=1 with Codex installed and signed in")
	}
	client, err := NewClient(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	meta, err := client.EnrichNote("Plan to refactor the timer and add a pause button.")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Title == "" || meta.Emoji == "" {
		t.Fatalf("empty label: %+v", meta)
	}
	t.Logf("Codex label: %s %s", meta.Emoji, meta.Title)
}
