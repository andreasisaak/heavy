package statusview

import (
	"encoding/json"
	"fmt"

	"github.com/andreasisaak/heavy/internal/protocol"
)

// JSON prints the runs as an indented JSON list with every field of design G14; an empty queue is [].
func JSON(runs []protocol.RunStatus) (string, error) {
	if runs == nil {
		runs = []protocol.RunStatus{}
	}
	data, err := json.MarshalIndent(runs, "", "  ")
	if err != nil {
		return "", fmt.Errorf("heavy: encoding the queue: %w", err)
	}
	return string(data) + "\n", nil
}
