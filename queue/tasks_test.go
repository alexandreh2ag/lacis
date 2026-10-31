package queue

import (
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTasks_MatchConfig(t *testing.T) {
	kinds := make([]string, 0, len(Tasks))
	for _, task := range Tasks {
		kinds = append(kinds, task.Kind)
		got, ok := TaskByKind(task.Kind)
		require.True(t, ok)
		assert.Equal(t, task.Kind, got.args.Kind())
	}
	assert.Equal(t, config.TaskNames, kinds, "the configuration names the tasks the queue runs")
	_, ok := TaskByKind("nope")
	assert.False(t, ok)
	for name := range config.DefaultConfig().Workers.Schedules {
		assert.Contains(t, kinds, name)
	}
}

func TestPeriodicJobs(t *testing.T) {
	jobs, err := periodicJobs(config.DefaultConfig().Workers.Schedules)
	require.NoError(t, err)
	assert.Len(t, jobs, 4, "purge_inactive_hosts has no schedule by default")

	_, err = periodicJobs(map[string]string{"purge_snapshots": "every day"})
	assert.ErrorContains(t, err, "workers.schedules.purge_snapshots")

	s, err := ParseSchedule("15 3 * * *")
	require.NoError(t, err)
	from := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	assert.Equal(t, time.Date(2026, 10, 31, 3, 15, 0, 0, time.UTC), s.Next(from))
}
