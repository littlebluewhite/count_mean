package gui

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBoundMethods_FirstStatementIsMatchingRecover 用 go/ast 掃描 gui/ 套件所有
// 非 _test.go 檔,對每個 exported *App method(Wails 全數 bind)斷言 body 的第一個
// statement 是與其簽章相符的 recover variant(ADR-0035):
//
//   - 無回傳值                → defer recoverHandlerPanicVoid(name, a.logger)
//   - 最後一個回傳值是 error  → defer recoverHandlerPanic(name, a.logger, &<error 具名回傳>)
//   - 單一非 error 回傳值    → defer recoverHandlerPanicValue(name, a.logger, &<具名回傳>)
//
// # 為何要求「第一個 statement」
//
// defer 只接得到註冊之後的 panic。首句之前任何一行(例如先打 a.logger.Info、先
// a.state.Load() 再 deref)panic 都會繞過 recover 直達 Wails runtime,擊潰整個
// desktop process。沒有豁免清單:新增 bound method 只要不是這個形狀就 fail。
//
// # 為何用 AST 而非 reflect
//
// reflect 看得到 method signature,看不到 body —「首句是哪個 defer」屬 source-level
// 屬性。runtime 面(零依賴呼叫不讓 panic 逃出)由 TestBoundMethods_NilDeps_PanicNeverEscapes 守。
// 這裡仍用 reflect 比對 method 集合,確保 AST scan 沒漏檔。
func TestBoundMethods_FirstStatementIsMatchingRecover(t *testing.T) {
	guiDir, err := locateGUIDir()
	require.NoError(t, err, "找不到 gui/ 目錄 — AST scan 無法執行")

	methods, err := collectExportedAppMethods(guiDir)
	require.NoError(t, err, "解析 gui/ 套件失敗")

	// AST 掃到的 exported method 集合必須與 reflect 看到的一致,否則 scan 漏檔,
	// 測試本身壞掉。reflect 的 method 依名稱排序。
	appType := reflect.TypeFor[*App]()
	want := make([]string, 0, appType.NumMethod())
	for i := range appType.NumMethod() {
		want = append(want, appType.Method(i).Name)
	}

	got := make([]string, 0, len(methods))
	for _, fn := range methods {
		got = append(got, fn.Name.Name)
	}
	sort.Strings(got)

	require.Equal(t, want, got, "AST scan 的 exported *App method 集合與 reflect 不一致")

	for _, fn := range methods {
		if problem := leadingRecoverProblem(fn); problem != "" {
			t.Errorf("%s: %s", fn.Name.Name, problem)
		}
	}
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

// collectExportedAppMethods 用 go/parser 解析目錄下所有非 _test.go 的 .go 檔,
// 回傳所有 exported 的 func (a *App) ... / func (*App) ... 宣告。
func collectExportedAppMethods(dir string) ([]*ast.FuncDecl, error) {
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

	var out []*ast.FuncDecl

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
					continue
				}

				if isAppReceiver(fn.Recv.List[0].Type) && ast.IsExported(fn.Name.Name) {
					out = append(out, fn)
				}
			}
		}
	}

	return out, nil
}

// isAppReceiver 判斷 receiver type 是不是 *App / App。
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

// leadingRecoverProblem 檢查 method 首句是否為與簽章相符的 recover variant;
// 符合回空字串,否則回人類可讀的違規描述。
func leadingRecoverProblem(fn *ast.FuncDecl) string {
	wantFunc, wantPtr, problem := expectedRecover(fn.Type.Results)
	if problem != "" {
		return problem
	}

	if fn.Body == nil || len(fn.Body.List) == 0 {
		return fmt.Sprintf("body 為空,首句應為 defer %s(...)", wantFunc)
	}

	deferStmt, ok := fn.Body.List[0].(*ast.DeferStmt)
	if !ok {
		return fmt.Sprintf("首句不是 defer,應為 defer %s(...)", wantFunc)
	}

	call := deferStmt.Call
	if got := calleeName(call.Fun); got != wantFunc {
		return fmt.Sprintf("首句應為 defer %s(...),實際為 defer %s(...)", wantFunc, got)
	}

	wantArgs := 2
	if wantPtr != "" {
		wantArgs = 3
	}

	if len(call.Args) != wantArgs {
		return fmt.Sprintf("defer %s 應有 %d 個引數,實際 %d 個", wantFunc, wantArgs, len(call.Args))
	}

	if sel, ok := call.Args[1].(*ast.SelectorExpr); !ok || sel.Sel.Name != "logger" {
		return fmt.Sprintf("defer %s 第 2 個引數應為 <receiver>.logger", wantFunc)
	}

	if wantPtr == "" {
		return ""
	}

	unary, ok := call.Args[2].(*ast.UnaryExpr)
	if !ok || unary.Op != token.AND {
		return fmt.Sprintf("defer %s 第 3 個引數應為 &%s", wantFunc, wantPtr)
	}

	if ident, ok := unary.X.(*ast.Ident); !ok || ident.Name != wantPtr {
		return fmt.Sprintf("defer %s 第 3 個引數應為 &%s(具名回傳)", wantFunc, wantPtr)
	}

	return ""
}

// expectedRecover 依回傳值形狀決定首句該用的 recover variant 與其指向的具名回傳。
// 回傳形狀不受支援(例如 error 回傳未具名,defer 無從改寫)時 problem 非空。
func expectedRecover(results *ast.FieldList) (wantFunc, wantPtr, problem string) {
	if results == nil || len(results.List) == 0 {
		return "recoverHandlerPanicVoid", "", ""
	}

	last := results.List[len(results.List)-1]
	if ident, ok := last.Type.(*ast.Ident); ok && ident.Name == "error" {
		if len(last.Names) == 0 {
			return "", "", "error 回傳值須具名,defer recoverHandlerPanic 才能改寫"
		}

		return "recoverHandlerPanic", last.Names[len(last.Names)-1].Name, ""
	}

	if results.NumFields() != 1 {
		return "", "", "回傳值多於一個且最後一個不是 error,沒有對應的 recover variant"
	}

	if len(last.Names) == 0 {
		return "", "", "回傳值須具名,defer recoverHandlerPanicValue 才能改寫"
	}

	return "recoverHandlerPanicValue", last.Names[0].Name, ""
}

// calleeName 取出 defer 呼叫的函式名稱;顯式 generic instantiation
// (recoverHandlerPanicValue[string](...))走 IndexExpr。
func calleeName(fun ast.Expr) string {
	switch callee := fun.(type) {
	case *ast.Ident:
		return callee.Name
	case *ast.IndexExpr:
		if ident, ok := callee.X.(*ast.Ident); ok {
			return ident.Name
		}
	}

	return fmt.Sprintf("%T", fun)
}
