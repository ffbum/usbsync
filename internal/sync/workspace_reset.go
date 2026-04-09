package sync

import (
	"errors"
	"fmt"
)

var ErrWorkspaceResetConfirmationRequired = errors.New("explicit confirmation is required before replacing the existing work directory")

func ApplyWorkspaceReset(confirmed bool) error {
	if !confirmed {
		return ErrWorkspaceResetConfirmationRequired
	}

	return nil
}

func NeedsWorkspaceResetPrompt(lastGeneration, currentGeneration int64) bool {
	return currentGeneration > lastGeneration
}

func WorkspaceResetWarning(newWorkRoot string) string {
	return fmt.Sprintf("检测到另一台电脑已切换工作目录，请改用新的目录：%s", newWorkRoot)
}
