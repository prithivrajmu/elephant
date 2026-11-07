package memory

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// Use the test binary as a synthetic provider so neither adapter invokes a model.
func TestIdleValidationRunnerHelper(t *testing.T) {
	if os.Getenv("ELEPHANT_DREAM_VALIDATION_HELPER") != "1" {
		return
	}
	if os.Getenv("ELEPHANT_DREAM_VALIDATION_EXIT") == "1" {
		os.Exit(2)
	}
	result := os.Getenv("ELEPHANT_DREAM_VALIDATION_OUTPUT")
	if size := os.Getenv("ELEPHANT_DREAM_VALIDATION_SIZE"); size != "" {
		result = `{"replacements":[]}`
		padding := maxIdleRunnerOutputBytes - len(result)
		if size == "excess" {
			padding++
		}
		result += strings.Repeat(" ", padding)
	}
	for i, arg := range os.Args {
		if arg == "--output-last-message" && i+1 < len(os.Args) {
			if os.Getenv("ELEPHANT_DREAM_VALIDATION_MISSING") == "1" {
				os.Exit(0)
			}
			if err := os.WriteFile(os.Args[i+1], []byte(result), 0600); err != nil {
				os.Exit(3)
			}
			os.Exit(0)
		}
	}
	_, _ = io.WriteString(os.Stdout, result)
	os.Exit(0)
}

func validationRunner(t *testing.T, c *AutomationConfig, adapter, result string) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	settings := *c.IdleDream
	settings.Adapter = adapter
	settings.Runner = []string{binary, "-test.run=^TestIdleValidationRunnerHelper$", "--"}
	c.IdleDream = &settings
	t.Setenv("ELEPHANT_DREAM_VALIDATION_HELPER", "1")
	t.Setenv("ELEPHANT_DREAM_VALIDATION_OUTPUT", result)
}

func TestIdleRunnerRejectsAmbiguousJSON(t *testing.T) {
	c, _, old, _ := idleFixture(t)
	valid, err := json.Marshal(IdleDreamOutput{Replacements: []IdleDreamProposal{idleProposal(old)}})
	if err != nil {
		t.Fatal(err)
	}
	proposal := string(valid)
	for _, adapter := range []string{"command", "codex"} {
		validationRunner(t, &c, adapter, proposal)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		out, err := executeDreamRunner(ctx, c, DreamBatch{})
		cancel()
		if err != nil || len(out.Replacements) != 1 || out.Replacements[0].Checks[0].ID != old.ID {
			t.Fatalf("%s rejected complete proposal: %+v %v", adapter, out, err)
		}
	}
	cases := map[string]string{
		"duplicate root":        `{"replacements":[],"replacements":[]}`,
		"escaped duplicate":     `{"replacements":[],"replacemen\u0074s":[]}`,
		"case insensitive root": `{"Replacements":[]}`,
		"case insensitive leaf": strings.Replace(proposal, `"incident":`, `"Incident":`, 1),
		"duplicate leaf":        strings.Replace(proposal, `"kind":`, `"kind":"correct","kind":`, 1),
		"null scalar":           strings.Replace(proposal, `"kind":"correct"`, `"kind":null`, 1),
		"null array":            `{"replacements":null}`,
		"missing scalar":        strings.Replace(proposal, `"kind":"correct",`, ``, 1),
		"empty checks":          `{"replacements":[{"kind":"correct","checks":[],"incident":"x","lesson":"x","source":"x"}]}`,
		"invalid kind":          strings.Replace(proposal, `"kind":"correct"`, `"kind":"invented"`, 1),
		"missing array":         `{}`,
		"unknown field":         `{"replacements":[],"private runner diagnostic":true}`,
		"trailing result":       `{"replacements":[]} {"replacements":[]}`,
	}
	for name, result := range cases {
		t.Run(name, func(t *testing.T) {
			for _, adapter := range []string{"command", "codex"} {
				validationRunner(t, &c, adapter, result)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_, err := executeDreamRunner(ctx, c, DreamBatch{})
				cancel()
				if err == nil {
					t.Errorf("%s accepted invalid JSON: %s", adapter, name)
				} else if strings.Contains(err.Error(), "private runner diagnostic") {
					t.Fatal("runner result leaked into error")
				}
			}
		})
	}
}

func TestIdleRunnerResultFailureReceipts(t *testing.T) {
	for _, adapter := range []string{"command", "codex"} {
		for _, mode := range []string{"invalid", "failed", "oversize", "boundary", "no_change", "missing"} {
			t.Run(adapter+"/"+mode, func(t *testing.T) {
				if adapter == "command" && mode == "missing" {
					t.Skip("command adapter reads stdout")
				}
				c, path, _, lease := idleFixture(t)
				result, want := `{"private runner diagnostic":true}`, "invalid_output"
				if mode == "no_change" || mode == "boundary" {
					result, want = `{"replacements":[]}`, "no_change"
					if mode == "boundary" {
						t.Setenv("ELEPHANT_DREAM_VALIDATION_SIZE", "limit")
					}
				} else if mode == "failed" {
					want = "runner_failed"
					t.Setenv("ELEPHANT_DREAM_VALIDATION_EXIT", "1")
				} else if mode == "oversize" {
					t.Setenv("ELEPHANT_DREAM_VALIDATION_SIZE", "excess")
				} else if mode == "missing" {
					t.Setenv("ELEPHANT_DREAM_VALIDATION_MISSING", "1")
				}
				validationRunner(t, &c, adapter, result)
				writeIdleConfig(t, path, c)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := runIdleDream(ctx, path, c, lease, executeDreamRunner); err != nil {
					t.Fatal(err)
				}
				status, err := DreamScheduleStatus(path)
				if err != nil || status.State.LastResult != want {
					t.Fatalf("receipt = %+v, want %s; err = %v", status.State, want, err)
				}
				if (status.State.Reviewed == status.State.Generation) != (want == "no_change") {
					t.Fatal("invalid or failed result advanced the review cursor")
				}
				data, _ := json.Marshal(status)
				if strings.Contains(string(data), "private runner diagnostic") || len(status.State.LastSaved) != 0 {
					t.Fatal("failure receipt leaked output or saved a replacement")
				}
			})
		}
	}
}

func TestIdleRunnerOutputByteBoundary(t *testing.T) {
	var output boundedDreamOutput
	valid := `{"replacements":[]}`
	result := valid + strings.Repeat(" ", maxIdleRunnerOutputBytes-len(valid))
	if n, err := output.Write([]byte(result)); err != nil || n != maxIdleRunnerOutputBytes {
		t.Fatalf("exact byte limit rejected: %d %v", n, err)
	}
	if _, err := decodeIdleRunnerOutput(output.buffer.Bytes(), 3); err != nil {
		t.Fatalf("valid result at the byte limit rejected: %v", err)
	}
	if _, err := output.Write([]byte(" ")); err == nil || !output.exceeded || output.buffer.Len() != maxIdleRunnerOutputBytes {
		t.Fatalf("excess byte accepted or buffered: %v", err)
	}
	if _, err := decodeIdleRunnerOutput([]byte("{\"replacements\":[],\"\xff\":0}"), 3); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
}
