//go:build !windows

package installer

import "context"

func nativeDesktop(context.Context, Plan) error { return nil }
func nativeOpenTray(Plan) error                 { return nil }

func nativeCloseTray() error { return nil }
