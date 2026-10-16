/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues
*/

package taskplugin

import (
	"sync"
	"testing"

	"github.com/MAX-API-Next/MAX-API/pkg/taskusage"
	"github.com/MAX-API-Next/MAX-API/types"
)

func TestMockDriverProducesTrustedTaskPluginEnvelope(t *testing.T) {
	outputDuration := int64(5_000)
	imageCount := int64(0)
	driver, err := NewMockDriver(taskusage.MiniMaxH3Contract(), &types.TaskUsage{
		OutputDurationMs: &outputDuration,
		InputImageCount:  &imageCount,
		Completeness:     types.TaskUsageCompletenessComplete,
	})
	if err != nil {
		t.Fatalf("create mock driver: %v", err)
	}
	taskID, err := driver.Submit()
	if err != nil {
		t.Fatalf("submit mock task: %v", err)
	}
	first, err := driver.Poll(taskID)
	if err != nil {
		t.Fatalf("first mock poll: %v", err)
	}
	if first.Status != MockTaskStatusInProgress || first.UsageEnvelope != nil {
		t.Fatalf("unexpected first poll: %#v", first)
	}
	terminal, err := driver.Poll(taskID)
	if err != nil {
		t.Fatalf("terminal mock poll: %v", err)
	}
	if terminal.Status != MockTaskStatusSuccess || terminal.UsageEnvelope == nil {
		t.Fatalf("unexpected terminal poll: %#v", terminal)
	}
	if err := taskusage.ValidateEnvelope(taskusage.MiniMaxH3Contract(), terminal.UsageEnvelope, types.TaskUsageProducerKindTaskPlugin); err != nil {
		t.Fatalf("validate mock envelope: %v", err)
	}
	if terminal.UsageEnvelope.Presence != types.TaskUsagePresencePresentValid {
		t.Fatalf("expected valid usage presence, got %q", terminal.UsageEnvelope.Presence)
	}
	if terminal.UsageEnvelope.Usage.InputImageCount == nil || *terminal.UsageEnvelope.Usage.InputImageCount != 0 {
		t.Fatal("explicit zero image count was not preserved")
	}
}

func TestMockDriverTerminalPollReplayIsStable(t *testing.T) {
	outputDuration := int64(4_000)
	imageCount := int64(0)
	driver, err := NewMockDriver(taskusage.MiniMaxH3Contract(), &types.TaskUsage{
		OutputDurationMs: &outputDuration,
		InputImageCount:  &imageCount,
		Completeness:     types.TaskUsageCompletenessComplete,
	})
	if err != nil {
		t.Fatalf("create mock driver: %v", err)
	}
	taskID, err := driver.Submit()
	if err != nil {
		t.Fatalf("submit mock task: %v", err)
	}
	if _, err := driver.Poll(taskID); err != nil {
		t.Fatalf("first mock poll: %v", err)
	}
	firstTerminal, err := driver.Poll(taskID)
	if err != nil {
		t.Fatalf("terminal mock poll: %v", err)
	}
	replay, err := driver.Poll(taskID)
	if err != nil {
		t.Fatalf("terminal replay: %v", err)
	}
	if firstTerminal.UsageEnvelope.EvidenceDigest != replay.UsageEnvelope.EvidenceDigest {
		t.Fatalf("terminal replay changed evidence digest: %q vs %q", firstTerminal.UsageEnvelope.EvidenceDigest, replay.UsageEnvelope.EvidenceDigest)
	}
	if firstTerminal.UsageEnvelope.Usage == replay.UsageEnvelope.Usage {
		t.Fatal("terminal replay returned mutable usage pointer")
	}
}

func TestMockDriverConcurrentPollsDoNotDuplicateTerminalEvidence(t *testing.T) {
	outputDuration := int64(5_000)
	imageCount := int64(0)
	driver, err := NewMockDriver(taskusage.MiniMaxH3Contract(), &types.TaskUsage{
		OutputDurationMs: &outputDuration,
		InputImageCount:  &imageCount,
		Completeness:     types.TaskUsageCompletenessComplete,
	})
	if err != nil {
		t.Fatalf("create mock driver: %v", err)
	}
	taskID, err := driver.Submit()
	if err != nil {
		t.Fatalf("submit mock task: %v", err)
	}
	var group sync.WaitGroup
	results := make(chan MockTaskResult, 8)
	errors := make(chan error, 8)
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			result, pollErr := driver.Poll(taskID)
			if pollErr != nil {
				errors <- pollErr
				return
			}
			results <- result
		}()
	}
	group.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Fatalf("concurrent poll failed: %v", err)
	}
	terminalDigest := ""
	terminalCount := 0
	for result := range results {
		if result.Status != MockTaskStatusSuccess {
			continue
		}
		terminalCount++
		if result.UsageEnvelope == nil {
			t.Fatal("successful mock poll omitted usage envelope")
		}
		if terminalDigest == "" {
			terminalDigest = result.UsageEnvelope.EvidenceDigest
		} else if terminalDigest != result.UsageEnvelope.EvidenceDigest {
			t.Fatal("concurrent terminal polls produced different evidence")
		}
	}
	if terminalCount == 0 {
		t.Fatal("concurrent polls never reached terminal state")
	}
}

func TestMockDriverRejectsUnknownTask(t *testing.T) {
	driver, err := NewMockDriver(taskusage.MiniMaxH3Contract(), nil)
	if err != nil {
		t.Fatalf("create mock driver: %v", err)
	}
	if _, err := driver.Poll("missing"); err == nil {
		t.Fatal("expected unknown task to be rejected")
	}
}
