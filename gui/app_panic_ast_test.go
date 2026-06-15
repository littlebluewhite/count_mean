package gui

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestApp_AllMethodsHaveDefer_ASTGuarantee 是 long-term solution:
// 用 go/ast 掃描整個 gui/ 套件,枚舉所有 `func (a *App)` / `func (*App)` 方法,
// 對每個 method body 的第一個 statement 斷言它必須是
// `defer recoverHandlerPanic{,Void,Value}(...)`。
//
// # 為何需要 AST 而非 reflect / closure list
//
// reflect 能看到 method signature(name / params / returns)但看不到 method
// body — 「body 有沒有 defer」屬於 source-level 屬性,只有 AST 抓得到。原本的
// TestApp_AllMethodsHaveDefer_StaticGuarantee 用 closure 列舉 6 個 method 做
// 編譯期 signature 檢查,但無法強制 body 起始一定有 defer — 看不到 chart_helpers.go
// 三個 method (修補中) 被漏修。長期解只能靠 AST。
//
// Wails 並發 RPC 場景下任何 exported method panic 都會擊潰整個 desktop process,
// 而開發者容易在新增 method 時忘記加 defer;此 test 用結構性測試把規則編成可機讀
// 契約。
//
// # 已知缺口(known-gap)policy
//
// 部分 method 目前刻意 *沒有* defer。每個 entry 都附 explicit reason(由
// TestKnownGap_AllEntriesHaveReason 在 CI 強制執行,空 reason 直接 fail)。
//
//   - `Startup` / `Shutdown` (app.go):lifecycle hook,由 Wails 透過
//     OnStartup/OnShutdown 註冊,不在 Wails generated bindings
//     (wailsjs/go/gui/App.js) 之列 — 不算 user-callable RPC。
//
// 過去 known-gap 含 `GetTranslations` / `SetLanguage` /
// `GetAvailablePhases` 三個 i18n / synchronizer dispatch shim,以「邊際效益低」
// 為由暫緩。本輪補上 defer (defense-in-depth),從 known-gap 移除。
// 舊「資料做圖」path 的 chart 家族 method 已於 Chart Composer Slice E 移除,
// 不再需要 panic safety net 條目。
//
// # 失敗模式
//
//   - 在 known-gap 之外的 exported method 缺 defer → t.Errorf (regression)
//   - known-gap 內的 method 缺 defer → t.Logf (允許,但留紀錄供人類審視)
//   - 掃到 method 數 < sanityCheckMinMethods (19) → t.Fatalf (AST 沒解出來,
//     測試本身壞掉)
//
// # 已驗證 method 數量(2026-05-18 snapshot)
//
// 共掃到 ~38 個 *App method;扣掉 unexported helper / lifecycle hook 後 exported
// RPC method 28 個,其中 24 個 Wails 暴露為 RPC binding(對齊
// frontend/wailsjs/go/gui/App.js)。
func TestApp_AllMethodsHaveDefer_ASTGuarantee(t *testing.T) {
	guiDir, err := locateGUIDir()
	require.NoError(t, err, "找不到 gui/ 目錄 — AST scan 無法執行")

	methods, err := collectAppMethodsFromDir(guiDir)
	require.NoError(t, err, "解析 gui/ 套件失敗")

	// Sanity:method 總數遠少於預期代表 AST scan 沒抓到任何東西 — 測試本身壞掉。
	// 19 是 2026-05-18 snapshot 內已加 defer 的 method 數,作為 lower bound。
	const sanityCheckMinMethods = 19
	require.GreaterOrEqual(t, len(methods), sanityCheckMinMethods,
		"AST 只掃到 %d 個 *App method (< sanity %d) — 可能 parser config 錯或檔案被誤排除",
		len(methods), sanityCheckMinMethods)

	// knownGap:目前刻意不要求 defer 的 method。每個 entry 必須附 explicit reason
	// (空字串將被 TestKnownGap_AllEntriesHaveReason 拒絕)。補完任何一個都應該
	//把該名字從這個 map 移除。
	knownGap := knownGapEntries()

	// unexportedHelpers:Wails 只 bind exported method,unexported helper 由
	// exported caller 的 defer 保護,不在這裡強制要 defer。列出來只是讓 test
	// 對「不報錯」這條路徑顯式 — 未來新增 unexported helper 不需要動 test。
	unexportedHelpers := map[string]bool{
		"loadCtx":                   true,
		"context":                   true,
		"applyConfig":               true,
		"calculateMaxMeanSingle":    true,
		"calculateMaxMeanBatch":     true,
		"buildMaxMeanFileSource":    true,
		"readCSVWithPathValidation": true,
		"calculateWithTimeRange":    true,
	}

	var (
		missingExported []string // exported method, lack defer, NOT in known-gap → t.Errorf
		missingGap      []string // method, lack defer, IN known-gap → t.Logf
	)

	for _, m := range methods {
		if m.hasDeferRecover {
			continue
		}

		switch {
		case knownGap[m.name] != "":
			missingGap = append(missingGap, fmt.Sprintf("%s (%s) [%s]", m.name, knownGap[m.name], m.file))
		case unexportedHelpers[m.name]:
			// unexported helper without explicit known-gap entry — 不報錯也不 log,
			// 因為 helper 由 exported caller 的 defer 保護。
		case !ast.IsExported(m.name):
			// 其他 unexported method:同上,不強制要 defer。
		default:
			missingExported = append(missingExported, fmt.Sprintf("%s [%s]", m.name, m.file))
		}
	}

	// t.Logf 留 known-gap 紀錄,給人類審視「這些是不是該補了」。
	sort.Strings(missingGap)
	for _, name := range missingGap {
		t.Logf("known-gap (允許暫時缺 defer recoverHandlerPanic*): %s", name)
	}

	if len(missingExported) > 0 {
		sort.Strings(missingExported)
		t.Errorf(
			"以下 exported *App method 缺 defer recoverHandlerPanic*(共 %d 個 — "+
				"Wails RPC panic 會擊潰整個 desktop process,必須加 defer):\n  - %s",
			len(missingExported),
			strings.Join(missingExported, "\n  - "),
		)
	}

	t.Logf(
		"AST scan summary: 共 %d 個 *App method,其中 %d 個已有 defer,"+
			"%d 個在 known-gap (允許缺),%d 個違反契約 (Errorf)",
		len(methods),
		countWithDefer(methods),
		len(missingGap),
		len(missingExported),
	)
}

// appMethodInfo 是 AST scan 的單筆結果:method 名稱 + body 是否以 defer
// recoverHandlerPanic* 起始 + 來源檔案(供 t.Logf 排錯)。
type appMethodInfo struct {
	name            string
	file            string
	hasDeferRecover bool
}

// locateGUIDir 解析測試檔案絕對路徑,回傳 gui/ 套件目錄。
//
// 用 runtime.Caller 而非 os.Getwd — `go test` 的 cwd 是 gui/ 沒問題,但若改成
// t.Chdir 或從 repo root 跑 `go test ./...` 時 cwd 不一定。runtime.Caller 抓的
// 是源碼路徑,不受 cwd 影響。
func locateGUIDir() (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("runtime.Caller(0) 失敗 — 無法解測試檔案位置")
	}

	return filepath.Dir(thisFile), nil
}

// collectAppMethodsFromDir 用 go/parser 解析目錄下所有非 _test.go 的 .go 檔,
// 回傳所有 func (a *App) ... 或 func (*App) ... 的 method 資訊。
//
// 為何排除 _test.go:本檔案內(以及其他 _test.go)可能有 *App helper / mock,
// 不算 production code,加 defer 沒意義。
func collectAppMethodsFromDir(dir string) ([]appMethodInfo, error) {
	fset := token.NewFileSet()
	// parser.ParseDir 自 Go 1.25 標記 deprecated(建議改 x/tools/go/packages 以
	// 支援 build tags);但本測試只掃 gui/*.go 沒有 build tag 分裂的場景,且
	// 避免引入 x/tools 額外依賴。直接 ParseDir 即可。
	//nolint:staticcheck // SA1019: ParseDir 對 gui/ 無 build tag 場景足夠
	pkgs, err := parser.ParseDir(fset, dir, func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parser.ParseDir(%s): %w", dir, err)
	}

	var out []appMethodInfo

	for _, pkg := range pkgs {
		for fileName, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
					continue
				}

				if !isAppReceiver(fn.Recv.List[0].Type) {
					continue
				}

				out = append(out, appMethodInfo{
					name:            fn.Name.Name,
					file:            filepath.Base(fileName),
					hasDeferRecover: hasLeadingDeferRecover(fn),
				})
			}
		}
	}

	// 穩定排序,讓 t.Logf 輸出可重現。
	sort.Slice(out, func(i, j int) bool {
		if out[i].file != out[j].file {
			return out[i].file < out[j].file
		}

		return out[i].name < out[j].name
	})

	return out, nil
}

// isAppReceiver 判斷 receiver type 是不是 *App / App。
// 處理三種寫法:`(a *App)`、`(*App)`、`(a App)`(罕見但 syntactically 合法)。
func isAppReceiver(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.StarExpr:
		ident, ok := t.X.(*ast.Ident)

		return ok && ident.Name == "App"
	case *ast.Ident:
		return t.Name == "App"
	default:
		return false
	}
}

// hasLeadingDeferRecover 判斷 method body 的第一個 statement 是不是
// defer recoverHandlerPanic{,Void,Value}(...).
//
// 為何要求「第一個 statement」:defer 註冊順序就是執行順序的反向。若 defer
// recoverHandlerPanic 不是 body 第一行,後面任何在 defer 註冊「之前」拋出的
// panic 都不會被它接到。Wails 並發 RPC 場景下 panic 必須無條件接住,因此
// 強制 defer 必須在 body 起始。
func hasLeadingDeferRecover(fn *ast.FuncDecl) bool {
	if fn.Body == nil || len(fn.Body.List) == 0 {
		return false
	}

	deferStmt, ok := fn.Body.List[0].(*ast.DeferStmt)
	if !ok {
		return false
	}

	call := deferStmt.Call
	if call == nil {
		return false
	}

	var name string

	switch callee := call.Fun.(type) {
	case *ast.Ident:
		name = callee.Name
	case *ast.SelectorExpr:
		// 容許 `pkg.recoverHandlerPanic` 但目前在同 package 不會出現
		name = callee.Sel.Name
	case *ast.IndexExpr:
		// generic instantiation: recoverHandlerPanicValue[T any] 在 call site
		// 多半被推斷型別,不會走 IndexExpr。但 explicit `recoverHandlerPanicValue[string]`
		// 寫法會走這條 — 把 X 拆出來判斷。
		if ident, ok := callee.X.(*ast.Ident); ok {
			name = ident.Name
		}
	case *ast.IndexListExpr:
		// generic with multiple type args, e.g. Foo[K, V] — 罕見但完整處理。
		if ident, ok := callee.X.(*ast.Ident); ok {
			name = ident.Name
		}
	}

	return strings.HasPrefix(name, "recoverHandlerPanic")
}

// countWithDefer 是 t.Logf summary 用的小 helper。
func countWithDefer(methods []appMethodInfo) int {
	var n int

	for _, m := range methods {
		if m.hasDeferRecover {
			n++
		}
	}

	return n
}

// knownGapEntries 把「刻意不要求 defer」的 method 與其原因集中宣告;TestKnownGap_AllEntriesHaveReason
// 與 TestApp_AllMethodsHaveDefer_ASTGuarantee 共用此 single source of truth。
//
// 過去 knownGap 含 GetTranslations / SetLanguage / GetAvailablePhases 三個
// i18n 與 synchronizer dispatch shim,本輪已補 defer recoverHandlerPanic*,從清單
// 移除。剩下兩個 lifecycle hook 不在 Wails RPC bindings,不需要 defer。
//
// PRD #7 (wave-6) 後新增 entry:analysis handler 改走 [[HandlerRun]] /
// [[AnalysisHandler[P, R]]].Run 拿 panic safety,body 內不再直接 `defer
// recoverHandlerPanic` — 樣板把 panic recovery 從多處 boilerplate 收斂到 1 處
// (HandlerRun)。AST test 守的 invariant 換軌:從「method body 第一行 defer」
// 變成「method 透過樣板取得 panic safety」。樣板自身的 panic safety 契約由
// gui/handler_run_test.go 與 gui/analysis_handler_test.go 守住。
//
// 例外:AnalyzeCCI 因 Run 之後另有 chart/report/phases-write 三段(落在樣板
// recover 外),仍保留 body 第一行顯式 `defer recoverHandlerPanic`,故不在此 map。
func knownGapEntries() map[string]string {
	return map[string]string{
		"Startup":          "Wails lifecycle hook (OnStartup),非 RPC binding",
		"Shutdown":         "Wails lifecycle hook (OnShutdown),非 RPC binding",
		"AnalyzePhases":    "panic safety via AnalysisHandler[P, R].Run (PRD #7 / wave-6)",
		"AnalyzePhaseSync": "panic safety via AnalysisHandler[P, R].Run (PRD #7 / wave-6)",
		// AnalyzeCCI 不再列 known-gap:它在 Run 之後另有 GenerateCCIInteractiveChart /
		// GenerateReport / WriteCCIPhasesResult,落在樣板 recover 外,故 body 第一行
		// 顯式 `defer recoverHandlerPanic`(鏡像 DownloadCCIChart),AST 直接掃得到。
		"AnalyzeMuscleRatio":         "panic safety via AnalysisHandler[P, R].Run (PRD #7 / wave-6)",
		"AnalyzeNormalizedPhaseSync": "panic safety via HandlerRun (Tier 1, PRD #7 / wave-6)",
		// Chart Composer family (Slice C, PRD #15) — 2 個 handler 走 HandlerRun
		// Tier 1 拿 panic safety;body 內不再 `defer recoverHandlerPanic`。
		// DownloadChartComposerImage 不在此 known-gap 列表 — 該 handler 鏡像
		// DownloadCCIChart 的 dual-channel 模式,顯式 `defer recoverHandlerPanic`
		// 在 body 第一行,AST 直接掃得到。
		"LoadChartComposerSubjects": "panic safety via HandlerRun (Tier 1, Chart Composer PRD #15)",
		"GenerateChartComposer":     "panic safety via HandlerRun (Tier 1, Chart Composer PRD #15)",
	}
}

// TestKnownGap_AllEntriesHaveReason 釘住 規範:knownGap 中每個 method
// 必須附 explicit reason(非空字串)。若有人偷懶加 entry 但沒寫原因,CI 立刻
// 攔下 — 強制日後 review knownGap 時看得到「為什麼這個 method 被豁免」。
func TestKnownGap_AllEntriesHaveReason(t *testing.T) {
	for method, reason := range knownGapEntries() {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("knownGap entry %q 缺 reason — 必須說明為何此 method 不需要 defer recoverHandlerPanic*", method)
		}
	}
}
