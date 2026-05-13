//go:build !race

package gui

// raceEnabled 在無 `-race` build 下為 false。詳細說明見 race_enabled.go。
const raceEnabled = false
