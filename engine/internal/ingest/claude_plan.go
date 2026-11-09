package ingest

import (
	"context"

	"github.com/semyonfox/dagda/engine/internal/model"
)

// PlanWindows returns the windows `dagda statusline` last recorded for Claude
// Code. Expired windows are left in; the renderer drops them.
func (c *Claude) PlanWindows(ctx context.Context) ([]model.PlanWindow, error) {
	if c.planDir == "" {
		return nil, nil
	}
	return ReadPlanSnapshot(c.planDir, model.AgentClaude)
}
