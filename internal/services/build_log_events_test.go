package services

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildLogsStructuredRecords(t *testing.T) {
	f := newBuildFixture(t)
	ctx := WithCliAuth(context.Background(), CliCredential{AppID: testBuildApp, KeyID: 7})
	_, err := f.service.Start(ctx, testBuildApp, testBuildIdentifier, testBuildID, f.startInput())
	require.NoError(t, err)
	start := `{"time":"2026-09-09T10:00:00Z","level":30,"msg":"Start step","buildStepDisplayName":"Run Gradle","buildStepId":"gradle","marker":"START_STEP"}`
	end := `{"time":"2026-09-09T10:00:03Z","level":30,"msg":"End step","buildStepDisplayName":"Run Gradle","buildStepId":"gradle","marker":"END_STEP","result":"success","durationMs":3000}`
	content := start + "\n" + end + "\n"
	require.NoError(t, f.service.AppendLogs(ctx, testBuildApp, testBuildIdentifier, testBuildID, 0, content))
	require.Equal(t, content, f.repo.logs[0].Content)
	// Every chunk must contain complete NDJSON events.
	require.Error(t, f.service.AppendLogs(ctx, testBuildApp, testBuildIdentifier, testBuildID, int32(len(content)), "old output\n"))

	for _, invalid := range []string{start[:len(start)-1], start + "\nnot JSON", start + end, "null"} {
		require.Error(t, f.service.AppendLogs(ctx, testBuildApp, testBuildIdentifier, testBuildID, 0, invalid))
	}
	require.Len(t, f.repo.logs, 1)
}

func TestBuildLogsStepValidation(t *testing.T) {
	valid := map[string]any{
		"time": "2026-09-09T10:00:03Z", "level": 30, "msg": "",
		"buildStepId": "gradle", "buildStepDisplayName": "Run Gradle",
		"marker": "END_STEP", "result": "success", "durationMs": 3000,
	}
	for field, invalid := range map[string]any{
		"time": "invalid", "level": 31, "msg": nil,
		"buildStepId": strings.Repeat("x", 256), "buildStepDisplayName": strings.Repeat("x", 256),
		"marker": "FINISHED", "result": "done", "durationMs": -1,
	} {
		t.Run(field, func(t *testing.T) {
			event := make(map[string]any, len(valid))
			for key, value := range valid {
				event[key] = value
			}
			event[field] = invalid
			content, err := json.Marshal(event)
			require.NoError(t, err)
			require.Error(t, validateBuildLogEvents(string(content)))
		})
	}
	for _, result := range []string{"success", "failed", "warning", "skipped"} {
		valid["result"] = result
		content, err := json.Marshal(valid)
		require.NoError(t, err)
		require.NoError(t, validateBuildLogEvents(string(content)))
	}
}

func TestBuildLogsRequireStepIdentity(t *testing.T) {
	for _, marker := range []string{"", "START_STEP", "END_STEP"} {
		event := map[string]any{
			"time": "2026-09-09T10:00:00Z", "level": 30, "msg": "",
			"buildStepId": "gradle", "buildStepDisplayName": "Building signed APK",
			"marker": marker, "result": "success", "durationMs": 1000,
		}
		content, err := json.Marshal(event)
		require.NoError(t, err)
		require.NoError(t, validateBuildLogEvents(string(content)))
		for _, field := range []string{"buildStepId", "buildStepDisplayName"} {
			saved := event[field]
			for _, invalid := range []any{nil, ""} {
				event[field] = invalid
				content, err := json.Marshal(event)
				require.NoError(t, err)
				require.Error(t, validateBuildLogEvents(string(content)), "%s requires %s", marker, field)
			}
			event[field] = saved
		}
	}
}

func TestBuildLogsGeneralMessage(t *testing.T) {
	require.NoError(t, validateBuildLogEvents(`{"buildStepId":"general","buildStepDisplayName":"Build","time":"2026-09-09T10:00:00Z","level":30,"msg":"Build started"}`))
}
