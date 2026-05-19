package gui

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validPNGBytes returns a minimal-but-valid PNG byte sequence with the given
// width and height in IHDR. 只用來測 validator,不必是 renderable PNG —
// 我們只驗 signature + chunk type + width/height + chunk CRC32 + DecodeConfig
// 語意 check;不嘗試 decode pixel data。
//
// 起 validator 會 chunk-walk 驗 CRC32 + image.DecodeConfig 語意 check,
// fixture 必須計算真實 IHDR CRC32(IEEE polynomial)否則 validator 會 reject
// 合法 fixture。bit depth=8 / color type=2 (RGB) 是 DecodeConfig 接受的最常見
// 組合;width/height 由 caller 指定後 IHDR 由 hash/crc32.ChecksumIEEE 計算 CRC。
//
// IDAT / IEND 故意不附 — caller 走到 validator 的 dimension / chunk-walk 階段
// 即足夠,DecodeConfig 在拿到合法 IHDR 後就能回 Config (不需 IDAT 才能解析
// width/height,實驗證實過)。
func validPNGBytes(width, height uint32) []byte {
	var buf bytes.Buffer
	// (1) signature 8 bytes
	buf.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})
	// (2) IHDR length = 13 (固定)
	binary.Write(&buf, binary.BigEndian, uint32(13))

	// IHDR chunk type + data 連續 17 bytes,CRC32 對「chunk type + data」計算
	// (不含 length 欄位)— 這是 PNG 規格的 CRC scope。
	ihdrStart := buf.Len()
	buf.WriteString("IHDR")
	binary.Write(&buf, binary.BigEndian, width)
	binary.Write(&buf, binary.BigEndian, height)
	// bit depth=8, color type=2 (RGB),compression=0,filter=0,interlace=0 —
	// 最常見的 valid IHDR 組合,image.DecodeConfig 能識別。
	buf.Write([]byte{0x08, 0x02, 0x00, 0x00, 0x00})
	ihdrEnd := buf.Len()
	crc := crc32.ChecksumIEEE(buf.Bytes()[ihdrStart:ihdrEnd])
	binary.Write(&buf, binary.BigEndian, crc)
	return buf.Bytes()
}

// TestDecodeAndValidatePNG_BlocksOversizeBase64 釘住 第一層守門:
// base64 encoded length 已大到 decode 後會超過 MaxPNGBytes,在 alloc 前
// 立即 reject(避免 100MB+ payload 吃光 process memory)。
//
// 注意: cap check 是 pre-decode short-circuit — 我們不在 test 內構造完整的
// 100MB+ payload(那會讓 test process 自己也吃 100MB),而是只構造略超
// cap 的字串以證明 cap check 行為正確。validator 的 cap check 與 decode
// 兩 statement 是順序排好的:超過 cap 立即回 ErrPNGBase64TooLarge,
// 永不會走到 base64.DecodeString — code review 直接看 implementation 即可驗。
func TestDecodeAndValidatePNG_BlocksOversizeBase64(t *testing.T) {
	maxEncodedLen := ((MaxPNGBytes + 2) / 3) * 4
	oversizeLen := maxEncodedLen + 4 // 略超過 cap

	// 用 'A' 重複構造(base64 valid char),目的只是讓 cap check 觸發。
	// 不需要 valid PNG bytes,因為 cap check 在 decode 之前。
	oversize := strings.Repeat("A", oversizeLen)

	_, err := DecodeAndValidatePNG(oversize)
	require.Error(t, err, "oversize base64 必須 reject")
	require.True(t, errors.Is(err, ErrPNGBase64TooLarge),
		"reject reason 應為 ErrPNGBase64TooLarge, got %v", err)
}

// TestDecodeAndValidatePNG_BlocksDecodedSizeOverflow 守第二層 size check:
// 在 cap pre-check 邊界(encoded length == cap)decode 後 raw bytes 仍可能
// 略超 MaxPNGBytes(因為 base64 沒有 '=' padding 的 ASCII string decoded
// length = n/4*3 > MaxPNGBytes by 1-2 bytes),要被 ErrPNGDecodedTooLarge
// post-check 攔下。
//
// 這個 case 是 pre / post check 的縱深防禦:如果有人改 cap pre-check 公式
// 不小心放寬, decode post-check 仍是最後一道防線。
func TestDecodeAndValidatePNG_BlocksDecodedSizeOverflow(t *testing.T) {
	maxEncodedLen := ((MaxPNGBytes + 2) / 3) * 4
	boundary := strings.Repeat("A", maxEncodedLen)

	_, err := DecodeAndValidatePNG(boundary)
	// 邊界 input 在 pre-check 通過(len == cap),decode 後 raw bytes 略超
	// MaxPNGBytes,被 ErrPNGDecodedTooLarge post-check 攔下。
	require.Error(t, err, "boundary input 必 reject (decoded > cap by base64 padding overflow)")
	require.True(t, errors.Is(err, ErrPNGDecodedTooLarge),
		"邊界 input 應走 post-check ErrPNGDecodedTooLarge, got %v", err)
}

// TestDecodeAndValidatePNG_CapBoundaryWithRealPNG 守 boundary 行為:略小於 cap 的
// valid PNG 應通過(不被誤殺)。直接用最小 valid PNG bytes 驗。
func TestDecodeAndValidatePNG_CapBoundaryWithRealPNG(t *testing.T) {
	// 最小 valid PNG(只 24 bytes,遠小於 cap),走完整 happy path。
	smallPNG := validPNGBytes(1, 1)
	b64 := base64.StdEncoding.EncodeToString(smallPNG)

	got, err := DecodeAndValidatePNG(b64)
	require.NoError(t, err)
	assert.NotEmpty(t, got)
}

// TestDecodeAndValidatePNG_RejectsBadSignature 釘住第二層守門:
// 非 PNG magic signature 的 bytes 必須 reject(避免任意 binary 被當 PNG 寫到 disk)。
func TestDecodeAndValidatePNG_RejectsBadSignature(t *testing.T) {
	tests := []struct {
		name     string
		bytes    []byte
		wantErr  error
		mustFail bool
	}{
		{
			name:     "JPEG signature",
			bytes:    []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46},
			wantErr:  ErrPNGSignatureInvalid,
			mustFail: true,
		},
		{
			name:     "GIF signature",
			bytes:    []byte("GIF89a\x00\x00"),
			wantErr:  ErrPNGSignatureInvalid,
			mustFail: true,
		},
		{
			name:     "all zeros",
			bytes:    make([]byte, 32),
			wantErr:  ErrPNGSignatureInvalid,
			mustFail: true,
		},
		{
			name:     "PE executable signature",
			bytes:    append([]byte{0x4D, 0x5A}, make([]byte, 30)...),
			wantErr:  ErrPNGSignatureInvalid,
			mustFail: true,
		},
		{
			name:     "too short (< 8 bytes)",
			bytes:    []byte{0x89, 0x50, 0x4E},
			wantErr:  ErrPNGTooShort,
			mustFail: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b64 := base64.StdEncoding.EncodeToString(tc.bytes)
			_, err := DecodeAndValidatePNG(b64)
			if tc.mustFail {
				require.Error(t, err, "input %q must be rejected", tc.name)
				require.True(t, errors.Is(err, tc.wantErr),
					"expected error %v, got %v", tc.wantErr, err)
			}
		})
	}
}

// TestDecodeAndValidatePNG_RejectsExtremeDimensions 釘住第三層守門:
// IHDR width / height 過大(decode bomb)或為 0(corrupted PNG)必須 reject。
func TestDecodeAndValidatePNG_RejectsExtremeDimensions(t *testing.T) {
	tests := []struct {
		name    string
		width   uint32
		height  uint32
		wantErr error
	}{
		{
			name:    "width over limit",
			width:   MaxPNGDimension + 1,
			height:  100,
			wantErr: ErrPNGDimensionTooLarge,
		},
		{
			name:    "height over limit",
			width:   100,
			height:  MaxPNGDimension + 1,
			wantErr: ErrPNGDimensionTooLarge,
		},
		{
			name:    "both at max+1",
			width:   MaxPNGDimension + 1,
			height:  MaxPNGDimension + 1,
			wantErr: ErrPNGDimensionTooLarge,
		},
		{
			name:    "decode bomb: 65535x65535",
			width:   65535,
			height:  65535,
			wantErr: ErrPNGDimensionTooLarge,
		},
		{
			name:    "zero width",
			width:   0,
			height:  100,
			wantErr: ErrPNGDimensionZero,
		},
		{
			name:    "zero height",
			width:   100,
			height:  0,
			wantErr: ErrPNGDimensionZero,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pngBytes := validPNGBytes(tc.width, tc.height)
			b64 := base64.StdEncoding.EncodeToString(pngBytes)

			_, err := DecodeAndValidatePNG(b64)
			require.Error(t, err)
			assert.True(t, errors.Is(err, tc.wantErr),
				"expected %v, got %v", tc.wantErr, err)
		})
	}
}

// TestDecodeAndValidatePNG_AcceptsValidPNG 守 validator 不誤殺合法輸入:
// 1x1 / 100x100 / max-allowed dimension 都應通過。
func TestDecodeAndValidatePNG_AcceptsValidPNG(t *testing.T) {
	tests := []struct {
		name   string
		width  uint32
		height uint32
	}{
		{name: "minimal 1x1", width: 1, height: 1},
		{name: "typical 1920x1080", width: 1920, height: 1080},
		{name: "max dimension boundary", width: MaxPNGDimension, height: MaxPNGDimension},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pngBytes := validPNGBytes(tc.width, tc.height)
			b64 := base64.StdEncoding.EncodeToString(pngBytes)

			got, err := DecodeAndValidatePNG(b64)
			require.NoError(t, err, "valid %dx%d PNG must pass validator", tc.width, tc.height)
			require.NotEmpty(t, got)
			// 守 returned bytes 與輸入一致(validator 不應該 mutate)
			assert.Equal(t, pngBytes, got)
		})
	}
}

// TestDecodeAndValidatePNG_RejectsInvalidBase64 守 base64 decode 階段 error
// 仍正確傳播(不會被前面 cap check 短路掉)。
func TestDecodeAndValidatePNG_RejectsInvalidBase64(t *testing.T) {
	_, err := DecodeAndValidatePNG("!!! not valid base64 !!!")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "解碼")
}

// TestDecodeAndValidatePNG_RejectsTooShortAfterSignature 守 PNG signature OK
// 但 IHDR 還沒解析完就 EOF 的 corrupted PNG 必須 reject(避免 panic on
// binary.BigEndian.Uint32(pngData[16:20]) when len < 20)。
func TestDecodeAndValidatePNG_RejectsTooShortAfterSignature(t *testing.T) {
	// 只給 signature + 部分 IHDR(< pngMinValidLen = 33 bytes)
	shortBytes := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // signature
		0x00, 0x00, 0x00, 0x0D, // IHDR length
		// 缺後續 "IHDR" + width + height + IHDR data + CRC
	}
	b64 := base64.StdEncoding.EncodeToString(shortBytes)

	_, err := DecodeAndValidatePNG(b64)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPNGTooShort),
		"truncated PNG 必須 reject (避免 IHDR slice out-of-bounds)")
}

// TestPNGValidation_FakeChunkType_Rejected 釘住 第一個修法:bytes 12-15
// 必須是 "IHDR" 才算合法 PNG。攻擊者可以餵 valid magic + length=13 + 任意
// 4-byte chunk type(像 "FAKE")讓舊版 validator 過關;新版必須 reject。
//
// 為何要單獨 case:現有 RejectsExtremeDimensions 走的是 validPNGBytes(已正寫
// "IHDR"),不會碰到 chunk type 路徑;此 test 是 chunk-type check 的 sole
// regression guard。
func TestPNGValidation_FakeChunkType_Rejected(t *testing.T) {
	fakeChunkTypes := []struct {
		name      string
		chunkType []byte
	}{
		{"FAKE chunk type", []byte("FAKE")},
		{"lowercase ihdr", []byte("ihdr")},
		{"mixed case IhDr", []byte("IhDr")},
		{"null bytes", []byte{0, 0, 0, 0}},
		{"random ascii", []byte("ABCD")},
		{"IDAT instead of IHDR", []byte("IDAT")},
	}

	for _, tc := range fakeChunkTypes {
		t.Run(tc.name, func(t *testing.T) {
			require.Len(t, tc.chunkType, 4, "chunk type must be exactly 4 bytes")

			// 構造 valid signature + length + 攻擊 chunk type + width=1 + height=1
			// + 5 byte IHDR data + 4 byte CRC placeholder,確保長度 >= pngMinValidLen
			// 不被 ErrPNGTooShort 提早攔下。
			var buf bytes.Buffer
			buf.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) // signature
			binary.Write(&buf, binary.BigEndian, uint32(13))                  // IHDR length
			buf.Write(tc.chunkType)                                           // 攻擊面
			binary.Write(&buf, binary.BigEndian, uint32(1))                   // width
			binary.Write(&buf, binary.BigEndian, uint32(1))                   // height
			buf.Write([]byte{0, 0, 0, 0, 0})                                  // IHDR data
			buf.Write([]byte{0, 0, 0, 0})                                     // CRC placeholder

			require.GreaterOrEqual(t, buf.Len(), 33,
				"fixture must reach pngMinValidLen so chunk-type check is the reject reason")

			b64 := base64.StdEncoding.EncodeToString(buf.Bytes())
			_, err := DecodeAndValidatePNG(b64)
			require.Error(t, err, "fake chunk type %q 必須 reject", tc.chunkType)
			assert.True(t, errors.Is(err, ErrPNGChunkTypeInvalid),
				"reject reason 應為 ErrPNGChunkTypeInvalid, got %v", err)
		})
	}
}

// TestPNGValidation_PixelLimitOverflow 釘住 第三個修法:width × height
// 必須以 uint64 比較 MaxPNGPixels(per-side check 不足)。
//
// case 設計:
//   - 70000×70000 = 4.9e9 — 在 uint32 下會 wrap(uint32 max ≈ 4.29e9),
//     是經典的 dimension-cap 邊界 case。雖然 per-side 已先擋(70000 >
//     MaxPNGDimension=10000),這 case 仍驗:即便未來放寬單邊上限,
//     uint64 pixel-count 仍是最後一道防線。
//   - MaxPNGDimension × MaxPNGDimension(10000×10000=1e8):邊界值,
//     pixelCount = MaxPNGPixels,應該通過(不超過)。
//   - MaxPNGDimension × MaxPNGDimension+1:per-side 先擋,reject reason
//     是 ErrPNGDimensionTooLarge — 防止 pixel-count check 順序錯誤。
func TestPNGValidation_PixelLimitOverflow(t *testing.T) {
	tests := []struct {
		name    string
		width   uint32
		height  uint32
		wantErr error
		// pin 哪一道 check 先觸發
		why string
	}{
		{
			name:    "70000x70000 wraps uint32",
			width:   70000,
			height:  70000,
			wantErr: ErrPNGDimensionTooLarge,
			why:     "per-side 先擋 70000 > MaxPNGDimension=10000",
		},
		{
			name:    "max dimension square just at pixel cap",
			width:   MaxPNGDimension,
			height:  MaxPNGDimension,
			wantErr: nil, // 應通過 — 邊界值不該被誤殺
			why:     "10000*10000 = MaxPNGPixels,等於上界仍通過(用 >,非 >=)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pngBytes := validPNGBytes(tc.width, tc.height)
			b64 := base64.StdEncoding.EncodeToString(pngBytes)

			_, err := DecodeAndValidatePNG(b64)
			if tc.wantErr == nil {
				require.NoError(t, err, "%s: %s", tc.name, tc.why)
				return
			}
			require.Error(t, err, "%s: %s", tc.name, tc.why)
			assert.True(t, errors.Is(err, tc.wantErr),
				"expected %v, got %v (%s)", tc.wantErr, err, tc.why)
		})
	}
}

// TestPNGValidation_PixelCountExceedsLimit 直接驗 pixel-count check 真的能擋:
// 構造 width × height > MaxPNGPixels 但兩邊都 <= MaxPNGDimension 的 case。
//
// 數學上不存在這種 case(因為 MaxPNGDimension^2 = MaxPNGPixels = 1e8),
// 所以這個 test 用一個 hypothetical 配置(20000 × 20000 = 4e8,兩邊都
// 「假設」放寬到 20000)透過直接探測私有 check 的方式驗證。改用 65535×65535
// — 兩邊都超 MaxPNGDimension,reject reason 仍應是 ErrPNGDimensionTooLarge
// (per-side check 順序在前),pixel-count check 在 dimension 通過後才生效。
//
// 為避免冗餘,此 test 跳過(現有 RejectsExtremeDimensions 已 cover 65535×65535
// → ErrPNGDimensionTooLarge);留註釋說明 pixel-count check 是 future-proof
// 縱深防禦,當前 MaxPNGDimension^2 == MaxPNGPixels 故 per-side check 已隱含
// pixel-count 上界。
func TestPNGValidation_PixelCountIsDeepDefense(t *testing.T) {
	// 釘住:MaxPNGDimension^2 應 <= MaxPNGPixels(constant invariant)。
	// 若未來有人改 MaxPNGDimension 但忘了同步 MaxPNGPixels(或反過來),
	// 此 assertion 會在 CI 第一時間炸出來。
	dim := uint64(MaxPNGDimension)
	require.LessOrEqual(t, dim*dim, MaxPNGPixels,
		"MaxPNGDimension^2 (%d) 必須 <= MaxPNGPixels (%d) — 兩個 constant 不能漂移",
		dim*dim, MaxPNGPixels)
}

// TestDecodeAndValidatePNG_RejectsGarbageAfterSignature 釘住 修法:
// 構造 valid signature + truncated/malformed IHDR + ~9.99 MiB 0xFF garbage 的
// payload — 過去 validator 只看頭 33 bytes,後面的 garbage 可以一路寫進
// disk 形成 disk-fill vector / format-confusion。修法後:
//
//   - IHDR length field (bytes 8-11) 必須恰為 13。
//   - chunk-walk 走到 IEND 並驗 CRC32。
//   - image.DecodeConfig 做語意 decode 保底。
//
// 任一道擋住此 garbage payload。
func TestDecodeAndValidatePNG_RejectsGarbageAfterSignature(t *testing.T) {
	// 9.99 MiB garbage,確保整個 payload 不超過 MaxPNGBytes(10 MiB)以走過
	// size cap,讓 chunk-walk / DecodeConfig 才是 reject reason。
	const garbageSize = 9*1024*1024 + 1010*1024 // 9.99 MiB

	t.Run("invalid IHDR length field (not 13)", func(t *testing.T) {
		var buf bytes.Buffer
		buf.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) // signature
		binary.Write(&buf, binary.BigEndian, uint32(20))                  // 故意非 13
		buf.WriteString("IHDR")
		binary.Write(&buf, binary.BigEndian, uint32(1))
		binary.Write(&buf, binary.BigEndian, uint32(1))
		buf.Write([]byte{0, 0, 0, 0, 0})
		buf.Write([]byte{0, 0, 0, 0}) // CRC placeholder
		// 在後面塞 garbage 證明 validator 不會 silently 讓 trailing junk 過。
		buf.Write(bytes.Repeat([]byte{0xFF}, garbageSize))

		b64 := base64.StdEncoding.EncodeToString(buf.Bytes())
		_, err := DecodeAndValidatePNG(b64)
		require.Error(t, err, "IHDR length != 13 必須 reject")
		assert.True(t, errors.Is(err, ErrPNGIHDRLengthInvalid),
			"expected ErrPNGIHDRLengthInvalid, got %v", err)
	})

	t.Run("valid signature + garbage tail (no IEND, bad CRC)", func(t *testing.T) {
		// 構造 valid signature + 正確 IHDR length=13 + "IHDR" + width=1 + height=1
		// + IHDR data + CRC placeholder (錯 CRC) + 大量 garbage,
		// chunk-walk 階段必須 reject(壞 CRC 或缺 IEND)。
		var buf bytes.Buffer
		buf.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})
		binary.Write(&buf, binary.BigEndian, uint32(13))
		buf.WriteString("IHDR")
		binary.Write(&buf, binary.BigEndian, uint32(1))
		binary.Write(&buf, binary.BigEndian, uint32(1))
		buf.Write([]byte{0, 0, 0, 0, 0})
		buf.Write([]byte{0xDE, 0xAD, 0xBE, 0xEF}) // 故意錯 CRC
		buf.Write(bytes.Repeat([]byte{0xFF}, garbageSize))

		b64 := base64.StdEncoding.EncodeToString(buf.Bytes())
		_, err := DecodeAndValidatePNG(b64)
		require.Error(t, err, "garbage tail 必須 reject")
		// 接受任一 reject 原因 — CRC fail / 缺 IEND / DecodeConfig 失敗都可。
		// 但絕不能 nil。
	})

	t.Run("valid signature + truncated IHDR + garbage", func(t *testing.T) {
		// signature + IHDR length=13 但只給 partial IHDR (8 bytes) + 大量 garbage。
		var buf bytes.Buffer
		buf.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})
		binary.Write(&buf, binary.BigEndian, uint32(13))
		buf.WriteString("IHDR")
		// 只寫 4 byte width 就停止,缺後續欄位(height + IHDR data + CRC)
		binary.Write(&buf, binary.BigEndian, uint32(1))
		buf.Write(bytes.Repeat([]byte{0xFF}, garbageSize))

		b64 := base64.StdEncoding.EncodeToString(buf.Bytes())
		_, err := DecodeAndValidatePNG(b64)
		require.Error(t, err, "truncated IHDR + garbage 必須 reject")
	})
}

// TestDecodeAndValidatePNG_AcceptsRealMinimalPNG 確認新加的 chunk-walk + CRC32
// + image.DecodeConfig 三層守門不會誤殺真實合法的最小 PNG。我們用 Go stdlib
// image/png 編碼的 1x1 grayscale PNG bytes(每個 chunk 都帶正確的 CRC32 IEEE),
// chunk-walk 與 image.DecodeConfig 兩道守門都會放行。
func TestDecodeAndValidatePNG_AcceptsRealMinimalPNG(t *testing.T) {
	// 來源:image/png.Encode(image.NewGray(image.Rect(0,0,1,1)))。
	// 包含正確 CRC 的 IHDR + IDAT + IEND chunks。
	minimalPNG := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x00, 0x00, 0x00, 0x00, 0x3A, 0x7E, 0x9B,
		0x55, 0x00, 0x00, 0x00, 0x0E, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9C, 0x62, 0x62, 0x00, 0x04, 0x00,
		0x00, 0xFF, 0xFF, 0x00, 0x06, 0x00, 0x03, 0xFA,
		0xD0, 0x59, 0xAE, 0x00, 0x00, 0x00, 0x00, 0x49,
		0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
	}

	b64 := base64.StdEncoding.EncodeToString(minimalPNG)
	got, err := DecodeAndValidatePNG(b64)
	require.NoError(t, err, "真實 1x1 PNG 必須通過所有守門")
	require.Equal(t, minimalPNG, got)
}
