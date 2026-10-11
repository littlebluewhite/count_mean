package manifest

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"count_mean/internal/i18n"
	"count_mean/internal/parsers"
)

// TestLoadSubject_FindsRowByName 釘住以名稱取 row:回傳的是名稱相同的那一列。
func TestLoadSubject_FindsRowByName(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	writeFileHelper(t, base, "manifest.csv", manifestCSV("SF1", "SF2", "SF3"))

	row, err := LoadSubject(filepath.Join(base, "manifest.csv"), "SF2")
	if err != nil {
		t.Fatalf("LoadSubject 不該失敗:%v", err)
	}
	if row.Subject != "SF2" || row.EMGFile != "emg.csv" {
		t.Errorf("row = %+v; want Subject SF2", row)
	}
}

// TestLoadSubject_NotFound 釘住找不到名稱:errors.Is ErrSubjectNotFound、不是 row 驗證失敗;
// Error() 是 zh-TW(與搬家前 Composer 的文字逐位元組相同),Localize 依目前 locale 渲染。
func TestLoadSubject_NotFound(t *testing.T) {
	_ = i18n.InitI18n("./nonexistent") // 不存在路徑 → 內建 catalog
	t.Cleanup(func() { i18n.SetLocale(i18n.LocaleZhTW) })
	i18n.SetLocale(i18n.LocaleEnUS)

	base := t.TempDir()
	writeFileHelper(t, base, "manifest.csv", manifestCSV("SF1"))

	row, err := LoadSubject(filepath.Join(base, "manifest.csv"), "Nope")

	if row != nil {
		t.Errorf("row = %+v; want nil", row)
	}
	if !errors.Is(err, ErrSubjectNotFound) || errors.Is(err, ErrSubjectRowInvalid) {
		t.Fatalf("err = %v; want ErrSubjectNotFound only", err)
	}
	if got, want := err.Error(), `Subject "Nope" 不存在於分期總檔案`; got != want {
		t.Errorf("Error() = %q; want %q", got, want)
	}
	if got, want := i18n.Localize(err), `Subject "Nope" not found in the manifest file`; got != want {
		t.Errorf("Localize = %q; want %q", got, want)
	}
}

// TestLoadSubject_RowInvalid 釘住找到的 row 一定經 ValidatePhaseManifest:分期點不單調的列
// 回 ErrSubjectRowInvalid,Error() 與驗證錯誤逐字相同(前綴由 caller 加);同檔其他合法列
// 照常可取。
func TestLoadSubject_RowInvalid(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	// Bad 的 S(4.0)早於 P2(9.0):力板時間不單調。
	content := manifestCSV("Good") +
		"Bad,motion.csv,force.anc,emg.csv,100,1.0,2.0,9.0,4.0,5.0,150,6.0,7.0,200,8.0\n"
	writeFileHelper(t, base, "manifest.csv", content)
	path := filepath.Join(base, "manifest.csv")

	row, err := LoadSubject(path, "Bad")

	if row != nil {
		t.Errorf("row = %+v; want nil", row)
	}
	if !errors.Is(err, ErrSubjectRowInvalid) || errors.Is(err, ErrSubjectNotFound) {
		t.Fatalf("err = %v; want ErrSubjectRowInvalid only", err)
	}

	manifests, loadErr := LoadManifests(path)
	if loadErr != nil {
		t.Fatalf("LoadManifests: %v", loadErr)
	}
	want := parsers.ValidatePhaseManifest(&manifests[1])
	if want == nil || err.Error() != want.Error() {
		t.Errorf("Error() = %q; want ValidatePhaseManifest 原文 %v", err.Error(), want)
	}

	if _, err := LoadSubject(path, "Good"); err != nil {
		t.Errorf("合法列 Good 不該失敗:%v", err)
	}
}

// TestLoadSubject_ManifestErrorPassesThrough 釘住讀檔門的錯誤原樣回傳:不是找不到、也不是 row 驗證。
func TestLoadSubject_ManifestErrorPassesThrough(t *testing.T) {
	t.Parallel()

	_, err := LoadSubject(filepath.Join(t.TempDir(), "missing.csv"), "SF1")

	if err == nil || errors.Is(err, ErrSubjectNotFound) || errors.Is(err, ErrSubjectRowInvalid) {
		t.Fatalf("err = %v; want LoadManifests 的錯誤", err)
	}
}

// TestLoadSubjectNames_SkipsBlankKeepsOrder 釘住名稱清單:manifest 順序、略過空 Subject。
func TestLoadSubjectNames_SkipsBlankKeepsOrder(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	writeFileHelper(t, base, "manifest.csv", manifestCSV("SF2", "", "SF1"))

	names, err := LoadSubjectNames(filepath.Join(base, "manifest.csv"))
	if err != nil {
		t.Fatalf("LoadSubjectNames 不該失敗:%v", err)
	}
	if want := []string{"SF2", "SF1"}; !reflect.DeepEqual(names, want) {
		t.Errorf("names = %q; want %q", names, want)
	}
}

// TestLoadSubject_Big5NameRoundTrip 釘住 Big5 Subject 經 webview 往返後仍取得到原列:
// LoadSubjectNames 回原始 bytes,JSON 把無效 UTF-8 逐 byte 換成 U+FFFD,前端送回的是換過的
// 名稱;LoadSubject 以 rune 序列比對,找回 bytes 原樣的那一列。
func TestLoadSubject_Big5NameRoundTrip(t *testing.T) {
	t.Parallel()

	const (
		big5Only  = "\xa4\xa4\xa4\xe5" // Big5「中文」,每個 byte 都不是合法 UTF-8
		big5Mixed = "SF\xa4\xa4"       // Big5「SF中」
	)

	base := t.TempDir()
	writeFileHelper(t, base, "manifest.csv", manifestCSV("SF1", big5Only, big5Mixed))
	path := filepath.Join(base, "manifest.csv")

	names, err := LoadSubjectNames(path)
	if err != nil {
		t.Fatalf("LoadSubjectNames 不該失敗:%v", err)
	}

	encoded, err := json.Marshal(names)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var decoded []string
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !strings.Contains(decoded[1], "�") {
		t.Fatalf("前提:JSON 應把 Big5 bytes 換成 U+FFFD,decoded = %q", decoded)
	}

	for i, want := range []string{"SF1", big5Only, big5Mixed} {
		row, err := LoadSubject(path, decoded[i])
		if err != nil {
			t.Errorf("LoadSubject(%q) 不該失敗:%v", decoded[i], err)
			continue
		}
		if row.Subject != want {
			t.Errorf("LoadSubject(%q).Subject = %q; want %q", decoded[i], row.Subject, want)
		}
	}
}
