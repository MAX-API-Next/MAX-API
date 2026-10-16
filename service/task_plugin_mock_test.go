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

package service

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/pkg/taskusage"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func TestTaskPluginMockDriverEnvelopeAtPollingBoundary(t *testing.T) {
	outputDuration := int64(5_000)
	imageCount := int64(0)
	driver, err := taskplugin.NewMockDriver(taskusage.MiniMaxH3Contract(), &types.TaskUsage{
		OutputDurationMs: &outputDuration,
		InputImageCount:  &imageCount,
		Completeness:     types.TaskUsageCompletenessComplete,
	})
	require.NoError(t, err)
	taskID, err := driver.Submit()
	require.NoError(t, err)
	_, err = driver.Poll(taskID)
	require.NoError(t, err)
	terminal, err := driver.Poll(taskID)
	require.NoError(t, err)
	require.NotNil(t, terminal.UsageEnvelope)

	adaptor := &usageFactPollingResponseAdaptor{
		contract:     driver.UsageContract(),
		producerKind: driver.UsageProducerKind(),
		envelope:     terminal.UsageEnvelope,
	}
	taskResult := &relaycommon.TaskInfo{}
	require.NoError(t, applyTaskUsageFacts(adaptor, []byte(`{}`), taskResult))
	require.NotNil(t, taskResult.UsageEnvelope)
	require.Equal(t, types.TaskUsageProducerKindTaskPlugin, taskResult.UsageEnvelope.ProducerKind)
	require.EqualValues(t, 5_000, *taskResult.Usage.OutputDurationMs)
}
