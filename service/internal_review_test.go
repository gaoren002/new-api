package service

import "testing"

func boolPtr(v bool) *bool {
	return &v
}

func TestNormalizeInternalReviewResponse(t *testing.T) {
	tests := []struct {
		name    string
		resp    internalReviewResponse
		blocked bool
	}{
		{
			name:    "allowed false blocks",
			resp:    internalReviewResponse{Allowed: boolPtr(false), Reason: "policy"},
			blocked: true,
		},
		{
			name:    "blocked true blocks",
			resp:    internalReviewResponse{Blocked: boolPtr(true), Reason: "risk"},
			blocked: true,
		},
		{
			name:    "flagged true blocks",
			resp:    internalReviewResponse{Flagged: boolPtr(true)},
			blocked: true,
		},
		{
			name:    "decision allow overrides flagged",
			resp:    internalReviewResponse{Flagged: boolPtr(true), Decision: "allow"},
			blocked: false,
		},
		{
			name:    "action block blocks",
			resp:    internalReviewResponse{Allowed: boolPtr(true), Action: "block"},
			blocked: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeInternalReviewResponse(tt.resp)
			if got.Blocked != tt.blocked {
				t.Fatalf("Blocked = %v, want %v", got.Blocked, tt.blocked)
			}
			if got.Allowed == got.Blocked {
				t.Fatalf("Allowed and Blocked should be inverse, got allowed=%v blocked=%v", got.Allowed, got.Blocked)
			}
		})
	}
}

func TestInternalReviewModelAllowed(t *testing.T) {
	tests := []struct {
		name      string
		modelName string
		filter    string
		want      bool
	}{
		{name: "empty applies all", modelName: "gpt-4o", filter: "", want: true},
		{name: "include contains match", modelName: "claude-4-sonnet", filter: "gpt,claude", want: true},
		{name: "include miss denies", modelName: "gemini-pro", filter: "gpt,claude", want: false},
		{name: "exclude wins", modelName: "test-model", filter: "test,!test-model", want: false},
		{name: "newline separated", modelName: "qwen-plus", filter: "gpt\nqwen", want: true},
		{name: "only excludes allow others", modelName: "qwen-plus", filter: "!test", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := internalReviewModelAllowed(tt.modelName, tt.filter)
			if got != tt.want {
				t.Fatalf("internalReviewModelAllowed(%q, %q) = %v, want %v", tt.modelName, tt.filter, got, tt.want)
			}
		})
	}
}
