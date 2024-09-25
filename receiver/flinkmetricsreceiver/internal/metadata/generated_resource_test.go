// Code generated by mdatagen. DO NOT EDIT.

package metadata

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResourceBuilder(t *testing.T) {
	for _, tt := range []string{"default", "all_set", "none_set"} {
		t.Run(tt, func(t *testing.T) {
			cfg := loadResourceAttributesConfig(t, tt)
			rb := NewResourceBuilder(cfg)
			rb.SetFlinkJobName("flink.job.name-val")
			rb.SetFlinkResourceTypeJobmanager()
			rb.SetFlinkSubtaskIndex("flink.subtask.index-val")
			rb.SetFlinkTaskName("flink.task.name-val")
			rb.SetFlinkTaskmanagerID("flink.taskmanager.id-val")
			rb.SetHostName("host.name-val")

			res := rb.Emit()
			assert.Equal(t, 0, rb.Emit().Attributes().Len()) // Second call should return empty Resource

			switch tt {
			case "default":
				assert.Equal(t, 6, res.Attributes().Len())
			case "all_set":
				assert.Equal(t, 6, res.Attributes().Len())
			case "none_set":
				assert.Equal(t, 0, res.Attributes().Len())
				return
			default:
				assert.Failf(t, "unexpected test case: %s", tt)
			}

			val, ok := res.Attributes().Get("flink.job.name")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "flink.job.name-val", val.Str())
			}
			val, ok = res.Attributes().Get("flink.resource.type")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "jobmanager", val.Str())
			}
			val, ok = res.Attributes().Get("flink.subtask.index")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "flink.subtask.index-val", val.Str())
			}
			val, ok = res.Attributes().Get("flink.task.name")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "flink.task.name-val", val.Str())
			}
			val, ok = res.Attributes().Get("flink.taskmanager.id")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "flink.taskmanager.id-val", val.Str())
			}
			val, ok = res.Attributes().Get("host.name")
			assert.True(t, ok)
			if ok {
				assert.EqualValues(t, "host.name-val", val.Str())
			}
		})
	}
}
