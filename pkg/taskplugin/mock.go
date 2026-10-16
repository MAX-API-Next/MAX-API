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
	"fmt"
	"sync"

	"github.com/MAX-API-Next/MAX-API/pkg/taskusage"
	"github.com/MAX-API-Next/MAX-API/types"
)

const (
	MockTaskStatusInProgress = "in_progress"
	MockTaskStatusSuccess    = "success"
)

type MockDriver struct {
	mu       sync.Mutex
	contract types.TaskUsageContract
	usage    *types.TaskUsage
	nextID   uint64
	tasks    map[string]*mockTask
}

type MockTaskResult struct {
	TaskID        string
	Status        string
	UsageEnvelope *types.TaskUsageEnvelope
}

type mockTask struct {
	status        string
	polls         int
	usageEnvelope *types.TaskUsageEnvelope
}

func NewMockDriver(contract types.TaskUsageContract, usage *types.TaskUsage) (*MockDriver, error) {
	if _, err := taskusage.ContractDigest(contract); err != nil {
		return nil, fmt.Errorf("validate mock task usage contract: %w", err)
	}
	return &MockDriver{
		contract: cloneUsageContract(contract),
		usage:    types.CloneTaskUsage(usage),
		tasks:    make(map[string]*mockTask),
	}, nil
}

func (driver *MockDriver) UsageContract() types.TaskUsageContract {
	if driver == nil {
		return types.TaskUsageContract{}
	}
	driver.mu.Lock()
	defer driver.mu.Unlock()
	return cloneUsageContract(driver.contract)
}

func (driver *MockDriver) UsageProducerKind() string {
	return types.TaskUsageProducerKindTaskPlugin
}

func (driver *MockDriver) ProduceUsage(context types.TaskUsageContext) (*types.TaskUsageEnvelope, error) {
	if driver == nil {
		return nil, fmt.Errorf("mock task plugin driver is nil")
	}
	driver.mu.Lock()
	contract := cloneUsageContract(driver.contract)
	usage := types.CloneTaskUsage(driver.usage)
	driver.mu.Unlock()
	if usage != nil {
		usage.Source = context.Stage
		if usage.Completeness == "" {
			usage.Completeness = types.TaskUsageCompletenessComplete
		}
	}
	return taskusage.BuildEnvelope(types.TaskUsageProducerKindTaskPlugin, contract, context.Stage, usage)
}

func (driver *MockDriver) Submit() (string, error) {
	if driver == nil {
		return "", fmt.Errorf("mock task plugin driver is nil")
	}
	driver.mu.Lock()
	defer driver.mu.Unlock()
	driver.nextID++
	taskID := fmt.Sprintf("mock-task-%d", driver.nextID)
	driver.tasks[taskID] = &mockTask{status: MockTaskStatusInProgress}
	return taskID, nil
}

func (driver *MockDriver) Poll(taskID string) (MockTaskResult, error) {
	if driver == nil {
		return MockTaskResult{}, fmt.Errorf("mock task plugin driver is nil")
	}
	driver.mu.Lock()
	defer driver.mu.Unlock()
	task, ok := driver.tasks[taskID]
	if !ok {
		return MockTaskResult{}, fmt.Errorf("mock task %q not found", taskID)
	}
	if task.status == MockTaskStatusSuccess {
		return MockTaskResult{TaskID: taskID, Status: task.status, UsageEnvelope: types.CloneTaskUsageEnvelope(task.usageEnvelope)}, nil
	}
	task.polls++
	if task.polls == 1 {
		return MockTaskResult{TaskID: taskID, Status: MockTaskStatusInProgress}, nil
	}
	usage := types.CloneTaskUsage(driver.usage)
	if usage != nil {
		usage.Source = types.TaskUsageSourceProviderResponse
		if usage.Completeness == "" {
			usage.Completeness = types.TaskUsageCompletenessComplete
		}
	}
	envelope, err := taskusage.BuildEnvelope(types.TaskUsageProducerKindTaskPlugin, driver.contract, types.TaskUsageSourceProviderResponse, usage)
	if err != nil {
		return MockTaskResult{}, fmt.Errorf("produce mock task usage: %w", err)
	}
	task.status = MockTaskStatusSuccess
	task.usageEnvelope = types.CloneTaskUsageEnvelope(envelope)
	return MockTaskResult{TaskID: taskID, Status: task.status, UsageEnvelope: types.CloneTaskUsageEnvelope(envelope)}, nil
}

func cloneUsageContract(contract types.TaskUsageContract) types.TaskUsageContract {
	contract.Fields = append([]types.TaskUsageFieldContract(nil), contract.Fields...)
	for index := range contract.Fields {
		if contract.Fields[index].MinValue != nil {
			value := *contract.Fields[index].MinValue
			contract.Fields[index].MinValue = &value
		}
		if contract.Fields[index].MaxValue != nil {
			value := *contract.Fields[index].MaxValue
			contract.Fields[index].MaxValue = &value
		}
	}
	return contract
}
