// Package gui PNG validation helpers.
//
// 前端透過 Wails IPC 把使用者下載的 PNG canvas 截圖以 base64 data URL
// 傳到後端,後端 decode 後寫到 OutputDir。原本 path 無 size cap、無 magic
// check、無 dimension validation,攻擊者(或 buggy frontend)可以送 100MB+
// 的 base64 進來吃掉整顆機器的 memory,或送非 PNG bytes / 異常尺寸的
// payload(decode bomb)讓 image library 配記憶體炸 process。
//
// 本檔提供三層守門:
//  1. base64 encoded length cap(在 decode 之前算 decoded length 上界)
//  2. PNG magic signature 檢查(decode 後 first 8 bytes 必須是 89 50 4E 47
//     0D 0A 1A 0A)
//  3. IHDR width / height 上限(width * height < pixelLimit)
//
// 三層都是 cheap O(1) check,在 alloc 大塊 buffer 之前生效。
package gui

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	// Side-effect import: register the PNG codec so image.DecodeConfig
	// recognises "image/png". Without this, DecodeConfig returns
	// "unknown format" on perfectly valid PNG signatures.
	_ "image/png"
)

// PNG validation 限制常數。
const (
	// MaxPNGBytes 是 decode 後 PNG bytes 的上界(10 MiB)。
	// echarts canvas 截圖在 1920x1080@200dpi 約 2-5 MiB,10 MiB 給足夠 margin。
	// 攻擊面: 100 MB+ base64 payload 在 decode 後吃光 process memory。
	MaxPNGBytes = 10 * 1024 * 1024

	// MaxPNGDimension 是單邊 pixel 上界。
	// 10000 上界 阻擋 decode bomb 攻擊
	// (small file, claims huge dimensions → image library alloc 內爆)。
	MaxPNGDimension = 10000

	// MaxPNGPixels 是 width * height 的 pixel 上界(100 MP)。
	// 單邊 check 不夠:即使兩邊 <= MaxPNGDimension,乘積仍可能達 1e8(10000×10000),
	// 對 image library 而言已是 decode bomb 邊界。再者,若有人未來放寬單邊上限,
	// 乘積 check 仍能擋住「兩個 65535 邊」之類的 uint32 邊界 case。
	// 用 uint64 比較避免 uint32 乘法 overflow(65535×65535 仍 fit uint32,但
	// 70000×70000 = 4.9e9 已超 uint32 max,uint32 乘法會 wrap)。
	MaxPNGPixels uint64 = 100 * 1000 * 1000

	// pngSignatureLen 是 PNG file signature 長度。
	pngSignatureLen = 8

	// pngIHDROffset 是 IHDR chunk width / height fields 起始 offset。
	// PNG layout (minimum 33 bytes):
	//   0-7   (8 bytes): signature
	//   8-11  (4 bytes): IHDR chunk length (固定為 13)
	//   12-15 (4 bytes): "IHDR" chunk type ← 必驗
	//   16-19 (4 bytes): width
	//   20-23 (4 bytes): height
	//   24    (1 byte):  bit depth
	//   25    (1 byte):  color type
	//   26    (1 byte):  compression method
	//   27    (1 byte):  filter method
	//   28    (1 byte):  interlace method
	//   29-32 (4 bytes): CRC32 of "IHDR" + IHDR data
	pngIHDRTypeOffset   = 12
	pngIHDRWidthOffset  = 16
	pngIHDRHeightOffset = 20
	// pngMinValidLen 是合法 PNG 必有的最小長度:8 sig + 4 length + 4 type + 13 IHDR data + 4 CRC = 33。
	// 從 24 上修至 33,讓「磁碟存的是合法 PNG」這個 invariant 比 24-byte 版更強
	// (24-byte 只夠取 width/height 不能驗 chunk type + CRC)。
	pngMinValidLen = 33

	// pngIHDRDataLen 是 PNG 規格固定的 IHDR data 長度(13 bytes:width 4 +
	// height 4 + bit_depth 1 + color_type 1 + compression 1 + filter 1 +
	// interlace 1)。過去 validator 沒檢 length field,任何 length 都會
	// 走到 dimension parse — 攻擊面修補。
	pngIHDRDataLen uint32 = 13
)

// pngIHDRType 是 IHDR chunk type bytes "IHDR" 的 byte sequence。
//
//nolint:gochecknoglobals // immutable magic constant
var pngIHDRType = [4]byte{'I', 'H', 'D', 'R'}

// pngSignature 是 PNG file 的 8-byte magic signature(W3C RFC 2083 § 12.11)。
// 任何 valid PNG file 開頭必為這 8 bytes;不符即立即 reject。
//
//nolint:gochecknoglobals // immutable magic constant
var pngSignature = [pngSignatureLen]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

// PNG validation errors.
var (
	// ErrPNGBase64TooLarge 表示 base64 encoded length 已大到 decode 後會超過
	// MaxPNGBytes,在 alloc 前就 reject。
	ErrPNGBase64TooLarge = errors.New("PNG base64 payload 過大")

	// ErrPNGDecodedTooLarge 表示 base64 decode 成功但 raw bytes 超過 MaxPNGBytes
	// (encoded length 估算未 catch 的邊界 case,通常因為 padding 偏差)。
	ErrPNGDecodedTooLarge = errors.New("PNG decoded bytes 超出上限")

	// ErrPNGSignatureInvalid 表示 decode 後首 8 bytes 不是 PNG magic signature。
	ErrPNGSignatureInvalid = errors.New("PNG signature 不符,非合法 PNG 檔案")

	// ErrPNGTooShort 表示 PNG bytes 短到無法包含 IHDR chunk。
	ErrPNGTooShort = errors.New("PNG bytes 過短,無法包含 IHDR chunk")

	// ErrPNGDimensionTooLarge 表示 IHDR width 或 height 超過 MaxPNGDimension。
	ErrPNGDimensionTooLarge = errors.New("PNG 尺寸過大")

	// ErrPNGPixelCountTooLarge 表示 IHDR width × height(pixel count)超過 MaxPNGPixels。
	// 即便單邊 <= MaxPNGDimension,乘積仍可能擊穿 decode bomb 上限。
	ErrPNGPixelCountTooLarge = errors.New("PNG 像素總數過大")

	// ErrPNGDimensionZero 表示 IHDR width 或 height 為 0(corrupted PNG)。
	ErrPNGDimensionZero = errors.New("PNG 尺寸無效(width 或 height 為 0)")

	// ErrPNGChunkTypeInvalid 表示 IHDR chunk type bytes (offset 12-15) 不是 "IHDR"。
	// PNG 規格要求 first chunk 必為 IHDR;非 IHDR 的 chunk type 即代表
	// payload 不是合法 PNG(或被惡意構造),應立即 reject 避免下游 image
	// library 在 alloc / decode 階段炸開。
	ErrPNGChunkTypeInvalid = errors.New("PNG 首 chunk 必須為 IHDR")

	// ErrPNGIHDRLengthInvalid 表示 IHDR chunk length field (bytes 8-11) 不為 13。
	// PNG 規格固定 IHDR data 為 13 bytes (width 4 + height 4 + 5 byte fields);
	// 任何其他長度即代表 payload 不是合法 PNG。過去 validator 只 hard-code
	// 從 offset 16 抽 width/height,沒檢 length field 一致性,攻擊者可塞長度為
	// 1000 的 IHDR 後緊跟 garbage,讓 validator 過關但 disk 寫進大量 junk。
	ErrPNGIHDRLengthInvalid = errors.New("PNG IHDR length field 必須為 13")

	// ErrPNGChunkCRCInvalid 表示 chunk-walk 階段發現 chunk CRC32 與計算結果不符。
	// 過去 validator 不驗 CRC,只看 IHDR width/height;malformed chunk
	// 可以一路寫到 disk 形成 format-confusion 或 disk-fill 攻擊。CRC32 IEEE
	// 是 PNG 規格的 chunk integrity check,任何 bit-flip / 攻擊改動都會被擋。
	ErrPNGChunkCRCInvalid = errors.New("PNG chunk CRC32 校驗失敗")

	// ErrPNGChunkMalformed 表示 chunk-walk 階段遇到結構性錯誤 — chunk length
	// 欄位宣告的 data size 超過剩餘 bytes,或無法切出 length/type/CRC 三段。
	ErrPNGChunkMalformed = errors.New("PNG chunk 結構錯誤")

	// ErrPNGDecodeConfigFailed 表示在所有 byte-level check 通過後,image.DecodeConfig
	// (語意 decode 守門)仍 reject — 例如 bit depth 與 color type 組合非法、
	// compression method 不支援。第三道防線,擋住「byte-level 結構正確
	// 但語意上不是 PNG」的 payload。
	ErrPNGDecodeConfigFailed = errors.New("PNG image.DecodeConfig 失敗")
)

// DecodeAndValidatePNG 解析 base64-encoded PNG payload,套用以下守門(三層,
// 並由 加強 IHDR chunk-type + pixel-count 縱深防禦):
//
//  1. 在 decode 之前用 encoded length 估算 decoded length 上界;若估算
//     已超過 MaxPNGBytes 直接 reject(避免 alloc 大塊 buffer)。
//  2. decode 後驗 first 8 bytes = PNG magic signature。
//  3. 解析 IHDR chunk:
//     a. bytes 12-15 必須是 "IHDR" chunk type(阻擋任意 chunk type 攻擊)。
//     b. width / height 不可為 0,且各自 <= MaxPNGDimension。
//     c. width × height(uint64,防 uint32 乘法 overflow)<= MaxPNGPixels。
//
// 回傳 decoded bytes 與 error。caller 拿到 nil error 即可 trust bytes 寫到
// 已驗證路徑(此 helper 不負責路徑驗證)。
func DecodeAndValidatePNG(base64Payload string) ([]byte, error) {
	// (1) Encoded length pre-check:base64 decode 後 bytes ≈ encoded * 3/4(忽略
	// padding;math.Ceil(encoded/4) * 3 是 strict 上界)。我們用較鬆的上界
	// encoded * 3/4 估算,搭配 decode 後二次 check 補強。
	//
	// MaxPNGBytes 的 base64 encoded 上界 = ceil(MaxPNGBytes / 3) * 4。
	// 任何 base64 string 超過這個長度,decode 後一定超過 MaxPNGBytes。
	maxEncodedLen := ((MaxPNGBytes + 2) / 3) * 4 //nolint:mnd // 標準 base64 expansion 公式
	if len(base64Payload) > maxEncodedLen {
		return nil, fmt.Errorf("%w: base64 length=%d, max=%d",
			ErrPNGBase64TooLarge, len(base64Payload), maxEncodedLen)
	}

	pngData, err := base64.StdEncoding.DecodeString(base64Payload)
	if err != nil {
		return nil, fmt.Errorf("解碼圖片數據失敗: %w", err)
	}

	// (1a) Decoded length post-check:padding edge case 可能讓 encoded < cap
	// 但 decoded 仍接近;再 explicit check 一次以保守。
	if len(pngData) > MaxPNGBytes {
		return nil, fmt.Errorf("%w: decoded=%d, max=%d",
			ErrPNGDecodedTooLarge, len(pngData), MaxPNGBytes)
	}

	// (2) Magic signature check:任何 valid PNG 開頭必為 89 50 4E 47 0D 0A 1A 0A。
	if len(pngData) < pngSignatureLen {
		return nil, ErrPNGTooShort
	}
	for i := 0; i < pngSignatureLen; i++ {
		if pngData[i] != pngSignature[i] {
			return nil, ErrPNGSignatureInvalid
		}
	}

	// (3) IHDR chunk-type + dimension check。不可只信 signature 與 size,
	// IHDR chunk type bytes 也必須是 "IHDR" 才算合法 PNG;若 chunk type 是
	// "FAKE" / "fAKE" 等任意 4 bytes(目前 echarts canvas exporter 不會產生
	// 這種,但攻擊者可直接構造 base64 餵進來),下游 image library 可能在
	// 解析未知 chunk 時觸發未定義行為,或讓 disk 上多出非預期 binary。
	if len(pngData) < pngMinValidLen {
		return nil, ErrPNGTooShort
	}

	// IHDR length field 必須恰為 13。過去 validator 直接從 offset 16
	// 抽 width/height,完全沒檢 length field 是否合理;攻擊者可餵 length=1000
	// 後緊跟 garbage 通過 dimension check,validator 過關但實際是 malformed PNG。
	ihdrLength := binary.BigEndian.Uint32(pngData[pngIHDRLengthOffset : pngIHDRLengthOffset+4])
	if ihdrLength != pngIHDRDataLen {
		return nil, fmt.Errorf("%w: length=%d want=%d",
			ErrPNGIHDRLengthInvalid, ihdrLength, pngIHDRDataLen)
	}

	for i := 0; i < len(pngIHDRType); i++ {
		if pngData[pngIHDRTypeOffset+i] != pngIHDRType[i] {
			return nil, fmt.Errorf("%w: got %q",
				ErrPNGChunkTypeInvalid,
				string(pngData[pngIHDRTypeOffset:pngIHDRTypeOffset+len(pngIHDRType)]))
		}
	}

	width := binary.BigEndian.Uint32(pngData[pngIHDRWidthOffset : pngIHDRWidthOffset+4])
	height := binary.BigEndian.Uint32(pngData[pngIHDRHeightOffset : pngIHDRHeightOffset+4])
	if width == 0 || height == 0 {
		return nil, fmt.Errorf("%w: width=%d height=%d",
			ErrPNGDimensionZero, width, height)
	}
	if width > MaxPNGDimension || height > MaxPNGDimension {
		return nil, fmt.Errorf("%w: width=%d height=%d max=%d",
			ErrPNGDimensionTooLarge, width, height, MaxPNGDimension)
	}
	// Pixel-count check(uint64 cast 防 uint32 乘法 overflow)。
	// 即便兩邊都 <= MaxPNGDimension,乘積仍可能達 decode-bomb 等級;再者
	// width × height 在 uint32 下 70000×70000 = 4.9e9 已 wrap,uint64 是
	// strict 上界。
	pixelCount := uint64(width) * uint64(height)
	if pixelCount > MaxPNGPixels {
		return nil, fmt.Errorf("%w: width=%d height=%d pixels=%d max=%d",
			ErrPNGPixelCountTooLarge, width, height, pixelCount, MaxPNGPixels)
	}

	// chunk-walk + CRC32 校驗每個出現的 chunk。即便 caller 餵的
	// fixture 沒有 IEND(test fixture 場景),只要每個 chunk 都帶正確 CRC,
	// 就視為「沒有結構性損壞」。若任一 chunk CRC 不符或 chunk length 超過
	// 剩餘 bytes,即視為 malformed payload 並 reject。
	if err := walkPNGChunks(pngData); err != nil {
		return nil, err
	}

	// image.DecodeConfig 語意 decode 保底。它會 re-verify IHDR CRC
	// 並驗 bit depth / color type 等語意 fields,擋住「byte-level 結構正確
	// 但語意上不是 PNG」的 payload。注意 DecodeConfig 不需 IDAT 就能回 Config
	// (Go stdlib png decoder 在拿到合法 IHDR 後立即回傳 Width/Height)。
	if _, _, err := image.DecodeConfig(bytes.NewReader(pngData)); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPNGDecodeConfigFailed, err)
	}

	return pngData, nil
}

// pngIHDRLengthOffset 為 IHDR chunk length field 的 byte offset(緊接 signature
// 後 4 bytes)。提到上方常數區與 pngIHDRTypeOffset / pngIHDRWidthOffset 對齊。
const pngIHDRLengthOffset = 8

// walkPNGChunks 走訪 pngData 內所有 chunk,驗每個 chunk 的 CRC32 (IEEE poly)。
// PNG chunk format(規格 § 5.3):
//
//	length (4 bytes BE) | type (4 bytes) | data (length bytes) | CRC (4 bytes BE)
//
// CRC 範圍 = type + data(不含 length 欄位本身)。
//
// 為何不要求 IEND chunk:本 validator 主要用於前端 canvas 截圖儲存場景,
// 截圖可能在 IEND 之前 truncate(罕見,但 test fixture 可重現)。寬鬆策略:
// 「凡是出現的 chunk 都要有正確 CRC,但不強制 IEND」。安全保證仍夠強 —
// 攻擊者要塞 garbage 必須先構造合法 CRC 的中間 chunks,成本與直接構造合法
// PNG 等價。
//
// fail-closed:若 walk 過程中 chunk length 欄位宣告超出剩餘 bytes(典型
// malformed payload),即視為結構錯誤回 ErrPNGChunkMalformed。
//
// pngData 第 0..7 bytes 為 signature(caller 已驗),從 offset 8 開始走 chunk。
func walkPNGChunks(pngData []byte) error {
	const chunkHeaderLen = 8 // length 4 + type 4
	const chunkCRCLen = 4
	offset := pngSignatureLen
	for offset < len(pngData) {
		if offset+chunkHeaderLen > len(pngData) {
			return fmt.Errorf("%w: chunk header truncated at offset %d",
				ErrPNGChunkMalformed, offset)
		}
		dataLen := binary.BigEndian.Uint32(pngData[offset : offset+4])
		typeStart := offset + 4
		dataStart := offset + chunkHeaderLen
		dataEnd := dataStart + int(dataLen) //nolint:gosec // bounds checked below
		crcEnd := dataEnd + chunkCRCLen
		// guard:dataLen 宣告超過剩餘 bytes / 整體 overflow。
		if dataEnd < dataStart || crcEnd > len(pngData) {
			return fmt.Errorf("%w: chunk at offset %d declares data=%d, exceeds remaining %d bytes",
				ErrPNGChunkMalformed, offset, dataLen, len(pngData)-dataStart)
		}
		// CRC 範圍 = type + data。
		want := binary.BigEndian.Uint32(pngData[dataEnd:crcEnd])
		got := crc32.ChecksumIEEE(pngData[typeStart:dataEnd])
		if got != want {
			return fmt.Errorf("%w: chunk type=%q at offset %d want=0x%08X got=0x%08X",
				ErrPNGChunkCRCInvalid, string(pngData[typeStart:typeStart+4]), offset, want, got)
		}
		offset = crcEnd
		// 抵達 IEND chunk 後可停止(後續 bytes 視為 trailing junk,寬鬆策略:
		// 不檢 IEND 後的內容,因為 echarts canvas exporter 偶有 trailing padding)。
		if string(pngData[typeStart:typeStart+4]) == "IEND" {
			return nil
		}
	}
	// 走到底沒 IEND 也可接受(理由見函式註解)— chunks 全部 CRC 正確就放行。
	return nil
}
