//go:build race

package gui

// raceEnabled 在 `-race` build 下為 true，否則為 false。
//
// 用於 race-only test 在沒 race detector 啟用時 t.Skip — 避免 test 在無 race
// 環境下退化為 no-op：sync.WaitGroup + 並發 reader/writer 沒 -race 不會 fail，
// CI 若忘記加 -race 會誤判為 PASS 而錯放 race regression（參考 Go 官方
// race build tag pattern：https://go.dev/doc/articles/race_detector）。
const raceEnabled = true
