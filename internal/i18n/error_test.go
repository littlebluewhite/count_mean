package i18n

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"testing"
)

// useGlobalLocale 以只載內建 catalog 的新 instance 取代 global,locale 設為 locale;
// t.Cleanup 換回原 instance(nil 也還原)。
func useGlobalLocale(t *testing.T, locale Locale) {
	t.Helper()

	inst := NewI18n()
	if err := inst.LoadTranslations("./nonexistent"); err != nil {
		t.Fatalf("載入內建翻譯失敗: %v", err)
	}
	inst.SetLocale(locale)

	prev := globalI18n.Load()
	globalI18n.Store(inst)
	t.Cleanup(func() { globalI18n.Store(prev) })
}

// TestError_ErrorRendersZhTWRegardlessOfLocale 釘住 Error() 固定以內建 zh-TW catalog 渲染:
// 目前 locale 是 en-US 時,err.Error()(log、err 通道、byte-pinned 測試看到的文字)仍與
// key 化之前的 zh-TW 文字逐位元組相同。
func TestError_ErrorRendersZhTWRegardlessOfLocale(t *testing.T) {
	useGlobalLocale(t, LocaleEnUS)

	cases := []struct {
		name string
		err  error
		want string
	}{
		{"訊息形式(有 Args)", NewError(KeyErrorCCIChannelLenMismatch, 2, 1), "通道數據長度不一致: 2 vs 1"},
		{"訊息形式(無 Args,catalog 字面 % 原樣)", NewError(KeyErrorCCIMissingSLAnchor),
			"缺少 S 或 L 分期點，無法錨定步態週期（0%=S、100%=L）"},
		{"error 型別的 Arg", NewError(KeyErrorMuscleRatioSubjectParseEMGFailed, errors.New("boom")),
			"解析 EMG 檔案失敗:boom"},
		{"前綴: cause 形式", WrapError(io.EOF, KeyErrorCCIBuildChannelMapFailed), "建立通道映射失敗: EOF"},
		{"巢狀 *Error 為 Cause", WrapError(NewError(KeyErrorCCIMissingMuscleChannel, "ES"), KeyErrorCCIBuildChannelMapFailed),
			"建立通道映射失敗: 缺少必要的肌肉通道: ES"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestError_UnwrapReachesCause 釘住 errors.Is / As 經 Cause 走到 sentinel 與 typed cause,
// 也走得到 Cause 鏈上的 *Error。
func TestError_UnwrapReachesCause(t *testing.T) {
	sentinel := errors.New("phase value is zero or not set")
	pathErr := &fs.PathError{Op: "open", Path: "/tmp/x.csv", Err: fs.ErrNotExist}
	inner := NewError(KeyErrorCCIEMGEmpty)

	if err := WrapError(fmt.Errorf("wrap: %w", sentinel), KeyErrorCCIParseManifestFailed); !errors.Is(err, sentinel) {
		t.Errorf("errors.Is 應經 Cause 命中 sentinel: %v", err)
	}

	var gotPath *fs.PathError
	if err := WrapError(pathErr, KeyErrorCCIParseEMGFailed); !errors.As(err, &gotPath) || gotPath != pathErr {
		t.Errorf("errors.As 應經 Cause 取得 *fs.PathError: %v", err)
	}

	if err := WrapError(inner, KeyErrorCCIBuildChannelMapFailed); !errors.Is(err, inner) {
		t.Errorf("errors.Is 應命中 Cause 鏈上的 *Error: %v", err)
	}
}

// TestLocalize 釘住 Localize 依目前 locale 渲染:*Error 本身、Cause、error 型別的 Args
// 都換成該 locale 的文字;*Error 之上的一般 wrapper 保留自己的文字;鏈上沒有 *Error 時
// 回 err.Error()。zh-TW 下結果與 err.Error() 相同。
func TestLocalize(t *testing.T) {
	sentinel := NewError(KeyErrorMuscleRatioCancelled)

	cases := []struct {
		name string
		err  error
		want string // en-US
	}{
		{"nil", nil, ""},
		{"沒有 *Error", fmt.Errorf("外層: %w", io.EOF), "外層: EOF"},
		{"訊息形式", NewError(KeyErrorCCIChannelLenMismatch, 2, 1), "Channel data length mismatch: 2 vs 1"},
		{"訊息形式(無 Args,catalog 字面 % 原樣)", NewError(KeyErrorCCIMissingSLAnchor),
			"Missing S or L phase point; cannot anchor the gait cycle (0%=S, 100%=L)"},
		{"前綴: 一般 cause", WrapError(io.EOF, KeyErrorCCIBuildChannelMapFailed), "Failed to build channel map: EOF"},
		{"Cause 是一般 wrapper 包 *Error",
			WrapError(fmt.Errorf("中層: %w", NewError(KeyErrorCCIMissingMuscleChannel, "ES")), KeyErrorCCIBuildChannelMapFailed),
			"Failed to build channel map: 中層: Missing required muscle channel: ES"},
		{"error 型別的 Arg 是 *Error", NewError(KeyErrorMuscleRatioSubjectWriteOutput1Failed, NewError(KeyErrorCCIEMGEmpty)),
			"Failed to write Output 1: EMG data is empty"},
		{"一般 wrapper 在 *Error 之上", fmt.Errorf("外層 %d: %w", 7, WrapError(io.EOF, KeyErrorCCIParseEMGFailed)),
			"外層 7: Failed to parse EMG file: EOF"},
		{"*Error sentinel 在一般 wrapper 開頭", fmt.Errorf("%w: S = %v", sentinel, -0.5),
			"Muscle ratio batch cancelled: S = -0.5"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			useGlobalLocale(t, LocaleEnUS)
			if got := Localize(tc.err); got != tc.want {
				t.Errorf("en-US Localize = %q, want %q", got, tc.want)
			}

			useGlobalLocale(t, LocaleZhTW)
			want := ""
			if tc.err != nil {
				want = tc.err.Error()
			}
			if got := Localize(tc.err); got != want {
				t.Errorf("zh-TW Localize = %q, want err.Error() %q", got, want)
			}
		})
	}
}

// TestLocalize_NoGlobalInstanceRendersZhTW 釘住 global 未初始化(InitI18n 之前)時
// Localize 以內建 zh-TW 渲染(GetLocale 此時也回 zh-TW),不回 bare key。
func TestLocalize_NoGlobalInstanceRendersZhTW(t *testing.T) {
	prev := globalI18n.Load()
	globalI18n.Store(nil)
	t.Cleanup(func() { globalI18n.Store(prev) })

	err := WrapError(io.EOF, KeyErrorCCIBuildChannelMapFailed)
	if got, want := Localize(err), "建立通道映射失敗: EOF"; got != want {
		t.Errorf("Localize = %q, want %q", got, want)
	}
}
