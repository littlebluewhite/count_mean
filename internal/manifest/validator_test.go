package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"count_mean/internal/models"
)

// TestValidateAllEMGFiles_PartialMissing 釘住核心契約:
// 給定 manifest 內 2 row(1 row disk 上對應檔案存在、1 row 不存在),
// ValidateAllEMGFiles 應只回 1 個 MissingRow,Subject / EMGFile / Err
// 都對應「不存在那 row」。
//
// 動機:V.14 manifest 升級時 NSF1/2/3 EMGFile 欄被誤改 → 用戶在 Chart
// Composer 跑時才在 Generate 階段炸,沒有早期 surface;此 validator 用於
// Load 階段就掃出所有 missing,讓 caller 早 surface 給 user。
func TestValidateAllEMGFiles_PartialMissing(t *testing.T) {
	t.Parallel()

	base := t.TempDir()

	// disk fixture:只建第一個檔,第二個故意不存在
	existsPath := filepath.Join(base, "exists.csv")
	if err := os.WriteFile(existsPath, []byte("time\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile fixture failed: %v", err)
	}

	manifests := []models.PhaseManifest{
		{Subject: "A", EMGFile: "exists.csv"},
		{Subject: "B", EMGFile: "missing.csv"},
	}

	missing := ValidateAllEMGFiles(manifests, base)

	if len(missing) != 1 {
		t.Fatalf("ValidateAllEMGFiles 應回 1 個 MissingRow,實際 %d 個:%+v", len(missing), missing)
	}

	m := missing[0]
	if m.Subject != "B" {
		t.Errorf("MissingRow.Subject = %q, want %q", m.Subject, "B")
	}

	if m.EMGFile != "missing.csv" {
		t.Errorf("MissingRow.EMGFile = %q, want %q", m.EMGFile, "missing.csv")
	}

	if !errors.Is(m.Err, ErrManifestEMGFileMissing) {
		t.Errorf("MissingRow.Err 應命中 ErrManifestEMGFileMissing 哨兵,實際 err=%v", m.Err)
	}
}

// TestValidateAllEMGFiles_AllPresent 對偶測試:全部 row 對應 disk 上存在
// 的檔 → 回 nil(或 empty slice)。防止 validator 退化成「總是回 missing」。
func TestValidateAllEMGFiles_AllPresent(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	for _, name := range []string{"a.csv", "b.csv"} {
		path := filepath.Join(base, name)
		if err := os.WriteFile(path, []byte("time\n"), 0o600); err != nil {
			t.Fatalf("os.WriteFile fixture failed: %v", err)
		}
	}

	manifests := []models.PhaseManifest{
		{Subject: "A", EMGFile: "a.csv"},
		{Subject: "B", EMGFile: "b.csv"},
	}

	missing := ValidateAllEMGFiles(manifests, base)
	if len(missing) != 0 {
		t.Errorf("ValidateAllEMGFiles 全 row 存在時應回 0 個 MissingRow,實際 %d 個:%+v", len(missing), missing)
	}
}

// TestValidateAllEMGFiles_OrderPreserved 釘住:回傳 MissingRow 列表的順序
// 應對應 manifest 內 row 出現順序;caller(如 GUI dropdown)可依此推斷
// 「第 N 個 subject 缺檔」與用戶看 dropdown 順序對應。
func TestValidateAllEMGFiles_OrderPreserved(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	// 只建中間那個 row 對應的檔;前後兩 row 都缺檔
	if err := os.WriteFile(filepath.Join(base, "mid.csv"), []byte("time\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile fixture failed: %v", err)
	}

	manifests := []models.PhaseManifest{
		{Subject: "First", EMGFile: "first_missing.csv"},
		{Subject: "Mid", EMGFile: "mid.csv"},
		{Subject: "Last", EMGFile: "last_missing.csv"},
	}

	missing := ValidateAllEMGFiles(manifests, base)
	if len(missing) != 2 {
		t.Fatalf("應有 2 個 missing,實際 %d:%+v", len(missing), missing)
	}

	if missing[0].Subject != "First" {
		t.Errorf("missing[0].Subject = %q, want %q (順序應對齊 manifest)", missing[0].Subject, "First")
	}

	if missing[1].Subject != "Last" {
		t.Errorf("missing[1].Subject = %q, want %q", missing[1].Subject, "Last")
	}
}
