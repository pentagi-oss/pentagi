package tools

import (
	"fmt"
	"testing"
)

func TestPrimaryTerminalName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		flowID int64
		want   string
	}{
		{1, "pentagi-terminal-1"},
		{0, "pentagi-terminal-0"},
		{12345, "pentagi-terminal-12345"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("flowID=%d", tt.flowID), func(t *testing.T) {
			t.Parallel()

			if got := PrimaryTerminalName(tt.flowID); got != tt.want {
				t.Errorf("PrimaryTerminalName(%d) = %q, want %q", tt.flowID, got, tt.want)
			}
		})
	}
}
