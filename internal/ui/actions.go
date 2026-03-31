//go:build windows

package ui

import progress "usbsync/internal/sync"

type FormState struct {
	WorkRoot    string
	DisplayName string
	BackupDir   string
}

type ActionResult struct {
	WorkRoot          string
	DisplayName       string
	BackupDir         string
	DriveStatus       string
	Results           ResultSummary
	ProgressRows      []progress.Event
	InitializeEnabled bool
	SyncEnabled       bool
}

type ProgressHandler func(progress.Event)
type ActionHandler func(FormState, ProgressHandler) ActionResult
type DraftHandler func(FormState)
