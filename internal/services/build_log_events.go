package services

import (
	"encoding/json"
	"strings"
	"time"
	"xprem/internal/validation"
)

// Validate build log records without reserializing them: retry offsets refer to the exact bytes sent.
func validateBuildLogEvents(content string) error {
	for _, line := range strings.Split(strings.TrimSuffix(content, "\n"), "\n") {
		var event struct {
			Time        time.Time `json:"time"`
			Level       int       `json:"level"`
			Message     *string   `json:"msg"`
			StepID      string    `json:"buildStepId"`
			DisplayName string    `json:"buildStepDisplayName"`
			Marker      string    `json:"marker"`
			Result      string    `json:"result"`
			DurationMs  *int64    `json:"durationMs"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil || event.Time.IsZero() || event.Message == nil || event.Level < 10 || event.Level > 60 || event.Level%10 != 0 {
			return validation.Errorf("logs", "expected complete build JSON log records")
		}
		if len(event.StepID) > 255 || len(event.DisplayName) > 255 {
			return validation.Errorf("logs", "log identifiers and labels must fit in 255 bytes")
		}
		if event.StepID == "" || event.DisplayName == "" {
			return validation.Errorf("logs", "logs require buildStepId and buildStepDisplayName")
		}
		switch event.Marker {
		case "", "START_STEP", "END_STEP":
		default:
			return validation.Errorf("logs", "unknown step marker")
		}
		if event.Marker == "END_STEP" {
			if event.DurationMs == nil || *event.DurationMs < 0 {
				return validation.Errorf("logs", "step end requires a nonnegative durationMs")
			}
			switch event.Result {
			case "success", "failed", "warning", "skipped":
			default:
				return validation.Errorf("logs", "step end requires a result")
			}
		}
	}
	return nil
}
