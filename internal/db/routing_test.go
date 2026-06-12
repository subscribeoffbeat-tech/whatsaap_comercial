package db_test

import (
	"testing"

	"whatsapptool/internal/db"
)

func TestChooseAgent(t *testing.T) {
	t.Run("nil when no agents available", func(t *testing.T) {
		if got := db.ChooseAgent(nil); got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("nil when empty agents slice", func(t *testing.T) {
		if got := db.ChooseAgent([]db.AgentLoad{}); got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("single agent is always chosen", func(t *testing.T) {
		agents := []db.AgentLoad{{AgentID: "agent-1", OpenConvs: 5}}
		got := db.ChooseAgent(agents)
		if got == nil || *got != "agent-1" {
			t.Errorf("got %v, want agent-1", got)
		}
	})

	t.Run("agent with fewest open convs is chosen", func(t *testing.T) {
		agents := []db.AgentLoad{
			{AgentID: "agent-A", OpenConvs: 10},
			{AgentID: "agent-B", OpenConvs: 2},  // ← least loaded
			{AgentID: "agent-C", OpenConvs: 7},
		}
		got := db.ChooseAgent(agents)
		if got == nil || *got != "agent-B" {
			t.Errorf("got %v, want agent-B (fewest convs)", got)
		}
	})

	t.Run("tie: first in slice wins (stable order)", func(t *testing.T) {
		agents := []db.AgentLoad{
			{AgentID: "agent-X", OpenConvs: 3},
			{AgentID: "agent-Y", OpenConvs: 3},
			{AgentID: "agent-Z", OpenConvs: 3},
		}
		got := db.ChooseAgent(agents)
		// Round-robin tie-break: first agent in the sorted result wins.
		// The DB query orders by open_convs ASC, then created_at ASC, so
		// the pure function just takes index 0 on a tie.
		if got == nil || *got != "agent-X" {
			t.Errorf("got %v, want agent-X (first on tie)", got)
		}
	})

	t.Run("zero-load agent wins over non-zero", func(t *testing.T) {
		agents := []db.AgentLoad{
			{AgentID: "busy", OpenConvs: 1},
			{AgentID: "free", OpenConvs: 0}, // ← completely free
		}
		got := db.ChooseAgent(agents)
		if got == nil || *got != "free" {
			t.Errorf("got %v, want free (zero open convs)", got)
		}
	})

	t.Run("preserves agent ID value exactly", func(t *testing.T) {
		id := "550e8400-e29b-41d4-a716-446655440000"
		agents := []db.AgentLoad{{AgentID: id, OpenConvs: 0}}
		got := db.ChooseAgent(agents)
		if got == nil || *got != id {
			t.Errorf("got %v, want %s", got, id)
		}
	})
}
