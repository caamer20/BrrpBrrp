//go:build windows

package metrics

import (
	"testing"

	"golang.org/x/sys/windows"
)

func TestFiletimeDurationSeconds(t *testing.T) {
	for _, tc := range []struct {
		value windows.Filetime
		want  float64
	}{
		{windows.Filetime{}, 0},
		{windows.Filetime{LowDateTime: 15_000_000}, 1.5},
		{windows.Filetime{HighDateTime: 1, LowDateTime: 10_000_000}, 430.4967296},
	} {
		if got := filetimeSeconds(tc.value); got != tc.want {
			t.Fatalf("%+v duration = %v seconds, want %v", tc.value, got, tc.want)
		}
	}
}
