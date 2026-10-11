package manifest

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"count_mean/internal/i18n"
	"count_mean/internal/models"
	"count_mean/internal/security"
	"count_mean/internal/security/fsperm"
)

// writeFileHelper 在 base 下寫一個含 content 的檔，回傳寫入的 bytes 供讀回比對。
func writeFileHelper(t *testing.T, base, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(base, name), []byte(content), 0o600); err != nil {
		t.Fatalf("setup WriteFile %s: %v", name, err)
	}
}

// TestOpenDataFile_Success 釘住 happy path:in-base 真實檔 → 回非 nil *os.File、
// 內容可讀回比對、三條 sentinel 都不命中。OpenDataFile 交出的是已開啟 fd,caller 自行 Close。
func TestOpenDataFile_Success(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	const want = "col1,col2\n1.0,2.0\n"
	writeFileHelper(t, base, "subject_A.csv", want)

	f, err := OpenDataFile(base, "subject_A.csv")
	if err != nil {
		t.Fatalf("OpenDataFile 不該失敗:%v", err)
	}
	if f == nil {
		t.Fatal("OpenDataFile 成功時必須回 non-nil *os.File")
	}
	defer f.Close()

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("讀回開好的檔失敗:%v", err)
	}
	if string(got) != want {
		t.Errorf("讀回內容 = %q; want %q", got, want)
	}
}

// TestOpenDataFile_LiteralPercent 釘住 BTS 匯出檔名含字面 "%" 不可 regress:
// OpenLenientValidated 端到端接受 "%"(不 URL-decode),OpenDataFile 不另加處理、
// 必須讓含 "%" 的真實檔開得進來且內容正確。
func TestOpenDataFile_LiteralPercent(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	const name = "SF_8_BTS%_6.10_BP30450_RMS0.5_0.49.csv"
	const want = "emg-bts-percent-payload\n"
	writeFileHelper(t, base, name, want)

	f, err := OpenDataFile(base, name)
	if err != nil {
		t.Fatalf("OpenDataFile 對含字面 %% 的檔名不該失敗:%v", err)
	}
	if f == nil {
		t.Fatal("OpenDataFile 成功時必須回 non-nil *os.File")
	}
	defer f.Close()

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("讀回 %% 檔失敗:%v", err)
	}
	if string(got) != want {
		t.Errorf("讀回內容 = %q; want %q", got, want)
	}
}

// TestOpenDataFile_FileMissingErrorsIs 釘住 missing-file 分類:base 存在但 filename
// 指向不存在的檔 → OpenLenientValidated 的 ENOENT 經雙層 %w 仍保留 os.ErrNotExist,
// 故映射到 ErrManifestDataFileMissing,且**互斥於** ErrManifestDataFilePathInvalid。
// file 回 nil。
func TestOpenDataFile_FileMissingErrorsIs(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	f, err := OpenDataFile(base, "nonexistent_subject.csv")
	if err == nil {
		t.Fatal("OpenDataFile 必須對不存在的檔回 error")
	}
	if f != nil {
		t.Error("OpenDataFile 失敗時必須回 nil file")
	}

	if !errors.Is(err, ErrManifestDataFileMissing) {
		t.Errorf("errors.Is(err, ErrManifestDataFileMissing) = false; err=%v", err)
	}
	// distinguishability:missing 不該被誤判成 path-invalid
	if errors.Is(err, ErrManifestDataFilePathInvalid) {
		t.Error("errors.Is(err, ErrManifestDataFilePathInvalid) 不該為 true — file missing 應與路徑驗證失敗區分")
	}
}

// TestOpenDataFile_TraversalErrorsIs 釘住 traversal/escape 分類:filename 含 ".."
// path-traversal element → OpenLenientValidated 在 lenient 解析階段回 bare error
// (非 os.ErrNotExist),fall through 到 ErrManifestDataFilePathInvalid,且**互斥於**
// ErrManifestDataFileMissing。file 回 nil。
func TestOpenDataFile_TraversalErrorsIs(t *testing.T) {
	t.Parallel()

	f, err := OpenDataFile(t.TempDir(), "../escape.csv")
	if err == nil {
		t.Fatal("OpenDataFile 必須對 path traversal input 回 error")
	}
	if f != nil {
		t.Error("OpenDataFile 失敗時必須回 nil file")
	}

	if !errors.Is(err, ErrManifestDataFilePathInvalid) {
		t.Errorf("errors.Is(err, ErrManifestDataFilePathInvalid) = false; err=%v", err)
	}
	// distinguishability:path-invalid 不該被誤判成 file missing
	if errors.Is(err, ErrManifestDataFileMissing) {
		t.Error("errors.Is(err, ErrManifestDataFileMissing) 不該為 true — 路徑驗證失敗應與 file missing 區分")
	}
}

// TestOpenDataFile_BaseFolderNotFound 釘住 base 無法解析的 fail-closed 行為:
// 不存在的 baseFolder → EvalSymlinks 失敗 → 命中 ErrBaseFolderNotFound、file 回 nil。
// 錯誤文字不帶資料夾名(末段常是病患名,sink 端 redact.Paths 會保留它;ADR-0059)。
func TestOpenDataFile_BaseFolderNotFound(t *testing.T) {
	t.Parallel()

	// 在已被清掉的 temp dir 之下構造一條保證不存在的 base 路徑。
	missingBase := filepath.Join(t.TempDir(), "no_such_base_dir")
	f, err := OpenDataFile(missingBase, "subject_A.csv")
	if err == nil {
		t.Fatal("OpenDataFile 必須對不存在的 baseFolder 回 error")
	}
	if f != nil {
		t.Error("OpenDataFile 失敗時必須回 nil file")
	}

	if !errors.Is(err, ErrBaseFolderNotFound) {
		t.Errorf("errors.Is(err, ErrBaseFolderNotFound) = false; err=%v", err)
	}
	if strings.Contains(err.Error(), "no_such_base_dir") {
		t.Errorf("錯誤文字不可帶資料夾名: %v", err)
	}
}

const loadEMGTestCSV = "X [],R.RA: EMG 1\n0.000,1.0\n0.001,2.0\n0.002,3.0\n"

// TestLoadEMG_Success 釘住 happy path:row.EMGFile + 資料夾 → 該 Subject 的 EMG。
func TestLoadEMG_Success(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	writeFileHelper(t, base, "emg.csv", loadEMGTestCSV)

	data, err := LoadEMG(base, &models.PhaseManifest{EMGFile: "emg.csv"})
	if err != nil {
		t.Fatalf("LoadEMG 不該失敗:%v", err)
	}
	if len(data.Time) != 3 {
		t.Errorf("len(Time) = %d; want 3", len(data.Time))
	}
}

// TestLoadEMG_OpenErrorIsNotParseError 釘住兩階段可區分:開檔失敗原樣回
// OpenDataFile 的 sentinel,不是 *EMGParseError。
func TestLoadEMG_OpenErrorIsNotParseError(t *testing.T) {
	t.Parallel()

	_, err := LoadEMG(t.TempDir(), &models.PhaseManifest{EMGFile: "missing.csv"})
	if !errors.Is(err, ErrManifestDataFileMissing) {
		t.Fatalf("err = %v; want ErrManifestDataFileMissing", err)
	}
	var pe *EMGParseError
	if errors.As(err, &pe) {
		t.Error("開檔失敗不該是 *EMGParseError")
	}
}

// TestLoadEMG_ParseErrorIsEMGParseError 釘住解析失敗包成 *EMGParseError,
// 且 Error() 與內層錯誤逐字相同(不改變任何 caller 的輸出文字)。
func TestLoadEMG_ParseErrorIsEMGParseError(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	writeFileHelper(t, base, "bad.csv", "")

	_, err := LoadEMG(base, &models.PhaseManifest{EMGFile: "bad.csv"})
	var pe *EMGParseError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v; want *EMGParseError", err)
	}
	if err.Error() != pe.Err.Error() {
		t.Errorf("Error() = %q; want inner %q", err.Error(), pe.Err.Error())
	}
}

const loadManifestsTestCSV = "Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L\n" +
	"S1,motion1.csv,force1.anc,emg1.csv,100,1.0,2.0,3.0,4.0,5.0,150,6.0,7.0,200,8.0\n"

// writeSparseFile 在 dir 下建一個 size bytes 的 sparse file(Truncate,不佔磁碟)。
func writeSparseFile(t *testing.T, dir, name string, size int64) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path) //nolint:gosec // path 位於 t.TempDir()
	if err != nil {
		t.Fatalf("setup Create %s: %v", name, err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatalf("setup Truncate %s: %v", name, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("setup Close %s: %v", name, err)
	}
	return path
}

// TestLoadManifests_RejectsOversize 釘住 ADR-0056:分期總檔案超過 100 MB 在開檔時以 fstat
// 拒絕(fsperm.ErrTooLarge),不整檔讀進記憶體。
func TestLoadManifests_RejectsOversize(t *testing.T) {
	t.Parallel()

	path := writeSparseFile(t, t.TempDir(), "manifest.csv", fsperm.MaxReadBytes+1)

	_, err := LoadManifests(path)
	if !errors.Is(err, fsperm.ErrTooLarge) {
		t.Fatalf("err = %v; want errors.Is(fsperm.ErrTooLarge)", err)
	}
}

// TestOpenDataFile_RejectsOversize 釘住 ADR-0056:manifest 資料檔超過 100 MB 在開檔後以
// fstat 拒絕,錯誤同時命中 ErrManifestDataFilePathInvalid 與 fsperm.ErrTooLarge。
func TestOpenDataFile_RejectsOversize(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	writeSparseFile(t, base, "emg.csv", fsperm.MaxReadBytes+1)

	f, err := OpenDataFile(base, "emg.csv")
	if f != nil {
		_ = f.Close()
		t.Error("OpenDataFile 失敗時必須回 nil file")
	}
	if !errors.Is(err, ErrManifestDataFilePathInvalid) || !errors.Is(err, fsperm.ErrTooLarge) {
		t.Fatalf("err = %v; want ErrManifestDataFilePathInvalid + fsperm.ErrTooLarge", err)
	}
}

// TestLoadManifests_RejectsReservedName 釘住 ADR-0054:分期總檔的檔名由 filename 套件
// (ValidateName)把關,每個點分段都比對 Windows 保留名。舊的 security.checkFilename 只剝
// 最後一個副檔名,`CON.txt.csv` → `CON.txt` 不命中而放行;Windows 上開檔會導向 console device。
//
// 檔名檢查在開檔前,所以保留名的檔案不建在磁碟上(Windows 上寫 CON / LPT1 會碰到實體
// device);路徑不存在,若檔名檢查沒擋,錯誤會是「無法開啟檔案」而非保留字錯誤。
func TestLoadManifests_RejectsReservedName(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	writeFileHelper(t, base, "manifest.csv", loadManifestsTestCSV)
	if _, err := LoadManifests(filepath.Join(base, "manifest.csv")); err != nil {
		t.Fatalf("合法檔名的分期總檔不該失敗:%v", err)
	}

	for _, name := range []string{"CON.txt.csv", "CON.csv", "a.NUL.csv", "lpt1.bak.csv"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := LoadManifests(filepath.Join(base, name))
			if err == nil {
				t.Fatalf("LoadManifests(%q) 必須拒絕 Windows 保留名", name)
			}
			if !strings.Contains(err.Error(), "保留字") {
				t.Errorf("LoadManifests(%q) 應回保留字錯誤(檔名檢查),got %v", name, err)
			}
			if errors.Is(err, os.ErrNotExist) {
				t.Errorf("LoadManifests(%q) 應在開檔前被檔名檢查擋下,不該是 file-not-found: %v", name, err)
			}
		})
	}
}

// TestLoadManifests_FileNotFound 釘住開檔錯誤原樣保留:訊息「無法開啟檔案」(原 parsers
// ParseFile 的測試,ADR-0056 開檔搬到 LoadManifests),且仍可 errors.Is(os.ErrNotExist)。
func TestLoadManifests_FileNotFound(t *testing.T) {
	t.Parallel()

	_, err := LoadManifests(filepath.Join(t.TempDir(), "nonexistent_file.csv"))
	if err == nil || !strings.Contains(err.Error(), "無法開啟檔案") {
		t.Fatalf("err = %v; want 「無法開啟檔案」", err)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("errors.Is(err, os.ErrNotExist) = false; err=%v", err)
	}
}

// TestLoadManifests_RejectsTraversalPath 釘住讀檔門第一步 ValidateExternalPath:含 ".."
// element 的路徑在檔名檢查與開檔之前被擋。
func TestLoadManifests_RejectsTraversalPath(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	writeFileHelper(t, base, "manifest.csv", loadManifestsTestCSV)

	_, err := LoadManifests(filepath.Join(base, "sub") + "/../manifest.csv")
	if !errors.Is(err, security.ErrPathTraversal) {
		t.Fatalf("err = %v; want security.ErrPathTraversal", err)
	}
}

// manifestCSV 組出 header + 每個 subject 一列的分期總檔內容(其餘欄位固定合法值)。
func manifestCSV(subjects ...string) string {
	var b strings.Builder
	b.WriteString("Subject,MotionFile,ForceFile,EMGFile,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L\n")
	for _, s := range subjects {
		b.WriteString(s + ",motion.csv,force.anc,emg.csv,100,1.0,2.0,3.0,4.0,5.0,150,6.0,7.0,200,8.0\n")
	}
	return b.String()
}

// TestLoadManifests_RejectsSubjectCollision 釘住 ADR-0056:兩列 Subject 的輸出檔名身分
// (filename.SubjectOutputKey)相同時整份 manifest 拒絕。撞名檢查從 muscle_ratio 搬到讀檔門,
// 所有 caller 一致;兩列都是空 Subject(都 Sanitize 成 untitled)也拒絕,與搬家前 MR 相同。
func TestLoadManifests_RejectsSubjectCollision(t *testing.T) {
	t.Parallel()

	for name, subjects := range map[string][2]string{
		"sanitize 後同名":  {"A/B", "A:B"},
		"只差大小寫":         {"SF8", "sf8"},
		"NFC vs NFD":    {"caf\u00e9", "cafe\u0301"},
		"兩列都是空 Subject": {"", ""},
		"完全相同的 Subject": {"SF1", "SF1"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			base := t.TempDir()
			writeFileHelper(t, base, "manifest.csv", manifestCSV(subjects[0], "OK", subjects[1]))

			_, err := LoadManifests(filepath.Join(base, "manifest.csv"))
			e, ok := errors.AsType[*i18n.Error](err)
			if !ok || e.Key != i18n.KeyErrorManifestSubjectCollision {
				t.Fatalf("err = %v; want i18n key %s", err, i18n.KeyErrorManifestSubjectCollision)
			}
		})
	}

	base := t.TempDir()
	writeFileHelper(t, base, "manifest.csv", manifestCSV("SF1", "SF2", "NSF7&SF9"))
	if _, err := LoadManifests(filepath.Join(base, "manifest.csv")); err != nil {
		t.Errorf("不撞名的 manifest 不該失敗:%v", err)
	}
}

// TestLoadManifests_SubjectCollisionMessage 釘住撞名錯誤的文字:Error() 是 zh-TW,與搬家前
// (muscle_ratio)逐位元組相同;i18n.Localize 依目前 locale 渲染(ADR-0048)。
func TestLoadManifests_SubjectCollisionMessage(t *testing.T) {
	_ = i18n.InitI18n("./nonexistent") // 不存在路徑 → 內建 catalog
	t.Cleanup(func() { i18n.SetLocale(i18n.LocaleZhTW) })
	i18n.SetLocale(i18n.LocaleEnUS)

	base := t.TempDir()
	writeFileHelper(t, base, "manifest.csv", manifestCSV("A/B", "A:B"))
	_, err := LoadManifests(filepath.Join(base, "manifest.csv"))

	const zhTW = `輸出檔名衝突: subject "A/B" 與 "A:B" 經檔名安全化後同為 "A_B" (case-insensitive)`
	const enUS = `Output filename collision: subjects "A/B" and "A:B" both sanitize to "A_B" (case-insensitive)`
	if err == nil || err.Error() != zhTW {
		t.Fatalf("Error() = %v; want %q", err, zhTW)
	}
	if got := i18n.Localize(err); got != enUS {
		t.Errorf("Localize = %q; want %q", got, enUS)
	}
}

// TestOpenDataFile_RejectsInvalidName 釘住 ADR-0056:manifest 資料檔名開檔前經
// filename.ValidateName(Base) —— 新擋 `<>:"|?*`、控制字元、磁碟代號、每個點分段的保留名、
// 過長(原 security 的 RejectsLongFilename 搬來此處)。檔案不建在磁碟上:擋在開檔之前。
func TestOpenDataFile_RejectsInvalidName(t *testing.T) {
	t.Parallel()

	// want 是 ValidateName 的訊息:只斷言 PathInvalid 不夠,OS 也可能因自己的理由拒開
	// (macOS 256 字元 → ENAMETOOLONG、Windows `a<b` → ERROR_INVALID_NAME),同樣落到 PathInvalid。
	base := t.TempDir()
	for _, tc := range []struct{ name, want string }{
		{"a<b.csv", "檔案名稱包含非法字符: <"},
		{"a|b.csv", "檔案名稱包含非法字符: |"},
		{"a?b.csv", "檔案名稱包含非法字符: ?"},
		{`a"b.csv`, `檔案名稱包含非法字符: "`},
		{"a\x01b.csv", "檔案名稱包含非法控制字符"},
		{"C:emg.csv", "Windows 磁碟代號"},
		{"CON.csv", "保留字: CON"},
		{"emg.NUL.csv", "保留字: NUL"},
		{`sub\LPT1.x.csv`, "保留字: LPT1"},
		{strings.Repeat("a", 256) + ".csv", "檔案名稱過長 (最大 255 字符)"},
	} {
		f, err := OpenDataFile(base, tc.name)
		if f != nil {
			_ = f.Close()
		}
		if !errors.Is(err, ErrManifestDataFilePathInvalid) || err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("OpenDataFile(%q) err = %v; want ErrManifestDataFilePathInvalid + %q", tc.name, err, tc.want)
		}
	}
}

// TestOpenDataFile_BackslashSubdir 釘住 `\` 正規化:Windows 產生的 manifest 以 `\` 分隔子目錄,
// ValidateName 只看正規化後的 Base,不把 `\` 當成檔名裡的非法分隔符。
func TestOpenDataFile_BackslashSubdir(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, "sub"), 0o750); err != nil {
		t.Fatalf("setup Mkdir: %v", err)
	}
	writeFileHelper(t, filepath.Join(base, "sub"), "emg.csv", "x\n")

	f, err := OpenDataFile(base, `sub\emg.csv`)
	if err != nil {
		t.Fatalf("OpenDataFile(`sub\\emg.csv`) 不該失敗:%v", err)
	}
	_ = f.Close()
}
