# 驗證詞彙依 context：讀端 sanity、注入防禦只在寫端、filename 只擋檔案系統非法、刪 patterns

**Status**: accepted · **implemented** (2026-10-09) · amends [[ADR-0015]]

## Decision

1. **讀端只做 sanity（plan 3.6）**：`validation/csv.CheckCells(records)` 只檢查每個 cell ≤ 32KB，錯誤只指出 row / col，不回顯內容。掛在 `ValidateCSVData`、`parsers.ReadCSVRecords`、`PhaseManifestParser.ParseFile`。`ValidateCSVData` 另保留結構檢查與既有 UTF-8 檢查（僅 user-picked pipeline）。讀端移除所有 formula / script / SQL / command / control / suspicious-extension 檢查。
2. **manifest / domain pipeline 不加 UTF-8 規則（Controller Ruling 1）**：`CheckCells` 是 size-only。使用者真實的 V.16 manifest 是 Big5 編碼（byte 46 起即非合法 UTF-8），加 UTF-8 規則會讓真實資料無法讀取，與計畫自身的風險註記衝突。
3. **注入防禦只在寫端**：CSV 寫出時由 `csvutil` 對 formula starter 做逸出（`csvutil.formulaStarters`），這是唯一的 formula 觸發字元清單；讀端不再猜測 cell 是否「像攻擊」。見 GLOSSARY **Read sanity / write escape**。
4. **filename 只擋檔案系統非法（plan 3.7）**：`filename.Validator.ValidateFilename` 擋：空白、tab / 控制字元、Unicode Cf / Cs、路徑分隔符 `/` `\`、Windows 磁碟代號前綴、`<>:"|?*`、保留裝置名（逐段比對）、長度 > 255、非 `.csv` 副檔名。放行 `'`、`&`、`--`、`sp_` 等常見 EMG 命名（`resp_01`、`grasp_EMG`、`O'Neil`、`trial--1`、`R&D`）。
5. **保留名搬進 `validation/filename`**：私有 `isReservedName`，`ValidateFilename` 與 `Sanitize` 共用；ADR-0015 的「reserved-name 集合單一真相、validate / sanitize 同居」不變，只是真相的位置從 `patterns` 變為 `filename` 內。
6. **刪除 `internal/validation/patterns` 整個套件**（registry、`InjectionDetectorImpl`、各類別 pattern 與測試）。3.6 之後它唯一的 importer 是 `validation/filename`，而 filename 只需要保留名與 `<>:"|?*`，其餘類別沒有 consumer。

## Why

- 同一份「危險字樣」清單被用在所有 context，造成假陽性：讀端把 `R.Shoulder` header 誤判為「可疑副檔名 .sh」；filename 把 `resp_01`（含 `sp_`）、`O'Neil`、`R&D` 當注入。這些都是正常 EMG 資料。
- 讀端內容永遠不被當成程式或 SQL 執行；真正的風險是輸出 CSV 被試算表開啟時的 formula 執行，防線放在寫端 escape 才有效，也只需一份清單。
- filename 的威脅是檔案系統與路徑逸出（非法字元、分隔符、磁碟代號、裝置名），不是注入。輸出檔名由輸入檔名推導，讀門（`ReadCSV`）與寫路徑（`WriteCSV`，ADR-0039）共用同一個 `ValidateFilename`，讀得進來的名字必須寫得出去，所以規則只能是「檔案系統非法」這個最小集合。
- 刪 `patterns` 後沒有跨套件的共用 registry，每類規則只在它的 context 內有一個擁有者。

## Considered Options

1. **讀端加 UTF-8 規則到 manifest**：真實 V.16 manifest 為 Big5，會破壞真實資料，否決（見 Decision 2）。
2. **保留 `patterns` 但縮減類別**：剩下的只有保留名與一串非法字元，單一 consumer，多一層 registry 只是 pass-through，否決。
3. **讀端保留「可疑」pattern 但改用 word-boundary**：已被前幾輪修補過多次仍有假陽性；讀端偵測無法阻止寫端風險，否決。
4. **filename 保留 `'` `&` `;` 等 shell metacharacter 拒絕**：程式不經 shell 使用檔名；這些字元在檔案系統合法且常見，否決。
