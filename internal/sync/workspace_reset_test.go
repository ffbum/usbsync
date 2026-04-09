package sync

import "testing"

func TestWorkspaceResetRequiresExplicitConfirmation(t *testing.T) {
	err := ApplyWorkspaceReset(false)
	if err == nil {
		t.Fatal("expected confirmation requirement")
	}
}

func TestWorkspaceResetPromptFollowsGenerationChange(t *testing.T) {
	if NeedsWorkspaceResetPrompt(2, 2) {
		t.Fatal("did not expect prompt when generation is unchanged")
	}
	if !NeedsWorkspaceResetPrompt(2, 3) {
		t.Fatal("expected prompt when generation increases")
	}
}
