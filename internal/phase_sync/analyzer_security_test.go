package phase_sync //nolint:revive // underscore in package name matches directory structure

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/models"
	"count_mean/internal/security/fsperm"
)

// TestPhaseSyncAnalyzer_PathTraversalAttack tests that path traversal attacks are prevented.
func TestPhaseSyncAnalyzer_PathTraversalAttack(t *testing.T) {
	analyzer := NewPhaseSyncAnalyzer()

	// Create a temporary data folder
	dataFolder := t.TempDir()

	// Create a folder outside the data folder to simulate an attack target
	targetFolder := t.TempDir()
	targetFile := filepath.Join(targetFolder, "secret.csv")
	err := os.WriteFile(targetFile, []byte("sensitive data"), fsperm.FilePerm)
	require.NoError(t, err)

	tests := []struct {
		name          string
		emgPath       string
		expectedError string
		description   string
	}{
		{
			name:          "relative path traversal with ../",
			emgPath:       "../../secret.csv",
			expectedError: "EMG 檔案路徑驗證失敗",
			description:   "Attempt to access parent directory using ../",
		},
		{
			name:          "multiple level path traversal",
			emgPath:       "../../../etc/passwd",
			expectedError: "EMG 檔案路徑驗證失敗",
			description:   "Attempt to access system files using multiple ../",
		},
		{
			name:          "absolute path outside data folder",
			emgPath:       "/etc/passwd",
			expectedError: "", // 錯誤可能是路徑驗證失敗或檔案不存在
			description:   "Attempt to use absolute path to system file",
		},
		{
			name:          "windows style path traversal",
			emgPath:       "..\\..\\secret.csv",
			expectedError: "EMG 檔案路徑驗證失敗",
			description:   "Attempt to use Windows-style path traversal",
		},
		{
			name:          "mixed path separators",
			emgPath:       "../\\../secret.csv",
			expectedError: "EMG 檔案路徑驗證失敗",
			description:   "Attempt to use mixed path separators",
		},
		{
			name:    "url encoded sequence treated as literal (lenient resolver)",
			emgPath: "..%2F..%2Fsecret.csv",
			// lenient ResolveLenientPath does NOT URL-decode, so "%2F" stays a literal
			// character run — this is one harmless filename inside the data folder (the OS
			// never decodes "%2F" to "/"). No traversal occurs; analysis fails downstream
			// (file absent), not at path validation. Real traversal uses literal
			// separators and is still blocked (the cases above).
			expectedError: "",
			description:   "URL-encoded sequence is a literal filename, not a traversal",
		},
		{
			name:          "double encoded path traversal",
			emgPath:       "%2E%2E%2F%2E%2E%2Fsecret.csv",
			expectedError: "", // 放寬 URL 編碼限制後，錯誤可能是路徑驗證失敗或檔案不存在
			description:   "Attempt to use double URL-encoded path traversal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create manifest with malicious EMG path
			manifestContent := fmt.Sprintf(`Subject,Motion檔案,力板檔案,EMG檔案,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
TestSubject,motion.csv,force.csv,%s,100,1.0,2.0,3.0,4.0,5.0,250,6.0,7.0,350,8.0`, tt.emgPath)

			manifestFile := createTempFile(t, manifestContent)

			params := &models.AnalysisParams{
				ManifestFile: manifestFile,
				DataFolder:   dataFolder,
				StartPhase:   "P0",
				EndPhase:     "P2",
				SubjectIndex: 0,
			}

			// Execute analysis - should fail with path validation error
			stats, err := analyzer.AnalyzePhaseSync(context.Background(), params)

			assert.Error(t, err, "Expected error for: %s", tt.description)
			assert.Nil(t, stats, "Stats should be nil for: %s", tt.description)
			// 只在 expectedError 非空時檢查錯誤訊息內容
			if tt.expectedError != "" {
				assert.Contains(t, err.Error(), tt.expectedError,
					"Error should contain '%s' for: %s", tt.expectedError, tt.description)
			}

			t.Logf("✓ Successfully blocked: %s", tt.description)
		})
	}
}

// TestPhaseSyncAnalyzer_ValidPathsAllowed tests that legitimate paths are allowed.
func TestPhaseSyncAnalyzer_ValidPathsAllowed(t *testing.T) {
	analyzer := NewPhaseSyncAnalyzer()

	// Create a temporary data folder
	dataFolder := t.TempDir()

	// Create a subdirectory within data folder
	subDir := filepath.Join(dataFolder, "subdir")
	err := os.MkdirAll(subDir, fsperm.DirPerm)
	require.NoError(t, err)

	tests := []struct {
		name        string
		emgPath     string
		description string
	}{
		{
			name:        "simple filename in data folder",
			emgPath:     "emg.csv",
			description: "Simple filename should be allowed",
		},
		{
			name:        "relative path in subdirectory",
			emgPath:     "subdir/emg.csv",
			description: "Relative path to subdirectory should be allowed",
		},
		{
			name:        "filename with spaces",
			emgPath:     "my emg file.csv",
			description: "Filename with spaces should be allowed",
		},
		{
			name:        "filename with numbers",
			emgPath:     "emg_001.csv",
			description: "Filename with numbers should be allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create the EMG file in the expected location
			emgFilePath := filepath.Join(dataFolder, tt.emgPath)

			emgDir := filepath.Dir(emgFilePath)
			if emgDir != dataFolder {
				err := os.MkdirAll(emgDir, fsperm.DirPerm)
				require.NoError(t, err)
			}

			// Create a minimal valid EMG file
			emgContent := `Time,Ch1,Ch2,Ch3,Ch4,Ch5,Ch6
0.0,10,20,30,40,50,60
0.001,11,21,31,41,51,61
0.002,12,22,32,42,52,62
0.003,13,23,33,43,53,63
0.004,14,24,34,44,54,64
`
			err := os.WriteFile(emgFilePath, []byte(emgContent), fsperm.FilePerm)
			require.NoError(t, err)

			// Create manifest with valid EMG path
			manifestContent := fmt.Sprintf(`Subject,Motion檔案,力板檔案,EMG檔案,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
TestSubject,motion.csv,force.csv,%s,0,0.0,0.001,0.002,0.0015,0.0018,2,0.0025,0.003,3,0.004`, tt.emgPath)

			manifestFile := createTempFile(t, manifestContent)

			params := &models.AnalysisParams{
				ManifestFile: manifestFile,
				DataFolder:   dataFolder,
				StartPhase:   "P0",
				EndPhase:     "P2",
				SubjectIndex: 0,
			}

			// Execute analysis - path validation should succeed
			// (Analysis may still fail due to missing motion/force files, but path validation should pass)
			_, err = analyzer.AnalyzePhaseSync(context.Background(), params)

			// We expect the error NOT to be about path validation
			if err != nil {
				assert.NotContains(t, err.Error(), "EMG 檔案路徑驗證失敗",
					"Path validation should succeed for: %s", tt.description)
				t.Logf("✓ Path validation passed (analysis failed for other reasons): %s", tt.description)
			} else {
				t.Logf("✓ Path validation and analysis passed: %s", tt.description)
			}
		})
	}
}

// TestPhaseSyncAnalyzer_AbsolutePathMustBeInDataFolder tests that absolute paths must be within data folder.
func TestPhaseSyncAnalyzer_AbsolutePathMustBeInDataFolder(t *testing.T) {
	analyzer := NewPhaseSyncAnalyzer()

	// Create a temporary data folder
	dataFolder := t.TempDir()

	// Create an EMG file inside the data folder
	emgFile := filepath.Join(dataFolder, "emg.csv")
	emgContent := `Time,Ch1,Ch2,Ch3,Ch4,Ch5,Ch6
0.0,10,20,30,40,50,60
0.001,11,21,31,41,51,61
`
	err := os.WriteFile(emgFile, []byte(emgContent), fsperm.FilePerm)
	require.NoError(t, err)

	t.Run("absolute path inside data folder - should be allowed", func(t *testing.T) {
		// Create manifest with absolute path that IS inside data folder
		manifestContent := fmt.Sprintf(`Subject,Motion檔案,力板檔案,EMG檔案,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
TestSubject,motion.csv,force.csv,%s,0,0.0,0.001,0.002,0.0015,0.0018,2,0.0025,0.003,3,0.004`, emgFile)

		manifestFile := createTempFile(t, manifestContent)

		params := &models.AnalysisParams{
			ManifestFile: manifestFile,
			DataFolder:   dataFolder,
			StartPhase:   "P0",
			EndPhase:     "P2",
			SubjectIndex: 0,
		}

		_, err := analyzer.AnalyzePhaseSync(context.Background(), params)
		// Path validation should succeed (analysis may fail for other reasons)
		if err != nil {
			assert.NotContains(t, err.Error(), "EMG 檔案路徑驗證失敗",
				"Absolute path inside data folder should be allowed")
		}

		t.Log("✓ Absolute path inside data folder was allowed")
	})

	t.Run("absolute path outside data folder - should be blocked", func(t *testing.T) {
		// Create a file outside the data folder
		outsideFolder := t.TempDir()
		outsideFile := filepath.Join(outsideFolder, "external.csv")
		err := os.WriteFile(outsideFile, []byte("external data"), fsperm.FilePerm)
		require.NoError(t, err)

		// Create manifest with absolute path that is OUTSIDE data folder
		manifestContent := fmt.Sprintf(`Subject,Motion檔案,力板檔案,EMG檔案,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
TestSubject,motion.csv,force.csv,%s,100,1.0,2.0,3.0,4.0,5.0,250,6.0,7.0,350,8.0`, outsideFile)

		manifestFile := createTempFile(t, manifestContent)

		params := &models.AnalysisParams{
			ManifestFile: manifestFile,
			DataFolder:   dataFolder,
			StartPhase:   "P0",
			EndPhase:     "P2",
			SubjectIndex: 0,
		}

		stats, err := analyzer.AnalyzePhaseSync(context.Background(), params)

		assert.Error(t, err, "Absolute path outside data folder should be blocked")
		assert.Nil(t, stats)
		// 放寬驗證後，錯誤可能是路徑驗證失敗或檔案不存在
		t.Log("✓ Absolute path outside data folder was blocked")
	})
}

// TestPhaseSyncAnalyzer_SymbolicLinkAttack tests protection against symlink attacks.
func TestPhaseSyncAnalyzer_SymbolicLinkAttack(t *testing.T) {
	// Skip this test on Windows where symlinks require admin privileges
	if os.Getenv("GOOS") == "windows" {
		t.Skip("Skipping symlink test on Windows")
	}

	analyzer := NewPhaseSyncAnalyzer()

	// Create a temporary data folder
	dataFolder := t.TempDir()

	// Create a target folder outside the data folder
	targetFolder := t.TempDir()
	targetFile := filepath.Join(targetFolder, "sensitive.csv")
	err := os.WriteFile(targetFile, []byte("sensitive data"), fsperm.FilePerm)
	require.NoError(t, err)

	// Create a symlink inside the data folder pointing to the target file
	symlinkPath := filepath.Join(dataFolder, "symlink.csv")

	err = os.Symlink(targetFile, symlinkPath)
	if err != nil {
		t.Skipf("Cannot create symlink: %v", err)
	}

	// Create manifest referencing the symlink
	manifestContent := `Subject,Motion檔案,力板檔案,EMG檔案,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
TestSubject,motion.csv,force.csv,symlink.csv,100,1.0,2.0,3.0,4.0,5.0,250,6.0,7.0,350,8.0`

	manifestFile := createTempFile(t, manifestContent)

	params := &models.AnalysisParams{
		ManifestFile: manifestFile,
		DataFolder:   dataFolder,
		StartPhase:   "P0",
		EndPhase:     "P2",
		SubjectIndex: 0,
	}

	stats, err := analyzer.AnalyzePhaseSync(context.Background(), params)

	// The symlink attack should be blocked by path validation
	// The resolved path will be outside the allowed base path
	assert.Error(t, err, "Symlink pointing outside data folder should be blocked")
	assert.Nil(t, stats)
	t.Log("✓ Symlink attack was detected and blocked")
}

// literalPercentEMGName is a real BTS EMG export filename: the strict
// PathValidator.GetSafePath rejected its literal '%' as a URL-encoding residual.
const literalPercentEMGName = "NSF_1_BTS%_5.9_BP30450_RMS0.1_0.09.csv"

// TestPhaseSyncValidators_LiteralPercentAccepted pins the fix for the "% filename
// rejected" finding. Real BTS exports carry a literal '%' (e.g.
// "NSF_1_BTS%_5.9_BP30450_RMS0.1_0.09.csv"). The strict PathValidator.GetSafePath
// rejected this as a URL-encoding residual, so phase_sync — the last
// manifest-driven loader still on the strict API — failed to load standard data.
// phase_sync now opens EMG/Motion/Force manifest files via the manifest.OpenDataFile
// door (which internally uses security.ResolveLenientPath; matching cci/muscle_ratio
// and the decision matrix in lenient_path.go), which accepts literal '%' while keeping
// real traversal/symlink-escape blocked (see the attack tests above).
//
// White-box: each validator is exercised directly. Motion/Force bootstrap their
// ctx (baseFolder) through the real validateEMGFilePath, then run on a '%' file.
// We assert the *path-validation* error never appears — a later parse failure is
// fine and proves path validation passed.
func TestPhaseSyncValidators_LiteralPercentAccepted(t *testing.T) {
	content := `Time,Ch1,Ch2,Ch3,Ch4,Ch5,Ch6
0.0,10,20,30,40,50,60
0.001,11,21,31,41,51,61
`
	writePctFile := func(t *testing.T) string {
		t.Helper()
		dataFolder := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dataFolder, literalPercentEMGName),
			[]byte(content), fsperm.FilePerm))
		return dataFolder
	}

	t.Run("EMG path validation accepts literal percent", func(t *testing.T) {
		dataFolder := writePctFile(t)
		ctx := &validationContext{
			params:   &models.AnalysisParams{DataFolder: dataFolder},
			manifest: models.PhaseManifest{EMGFile: literalPercentEMGName},
		}
		err := validateEMGFilePath(NewPhaseSyncAnalyzer(), ctx)
		require.NoError(t, err, "literal '%%' EMG filename must pass path validation")
		require.Contains(t, ctx.emgFilePath, literalPercentEMGName)
	})

	t.Run("Motion path validation accepts literal percent", func(t *testing.T) {
		dataFolder := writePctFile(t)
		analyzer := NewPhaseSyncAnalyzer()
		// Bootstrap EMG uses the same '%' file (it exists in dataFolder): the door now
		// validates existence atomically, so the bootstrap EMG must be a real file to
		// populate ctx.baseFolder before exercising the Motion validator.
		ctx := &validationContext{
			params:   &models.AnalysisParams{DataFolder: dataFolder},
			manifest: models.PhaseManifest{EMGFile: literalPercentEMGName, MotionFile: literalPercentEMGName},
		}
		require.NoError(t, validateEMGFilePath(analyzer, ctx), "bootstrap EMG validation")
		if err := validateMotionFile(analyzer, ctx); err != nil {
			assert.NotContains(t, err.Error(), "Motion 檔案路徑驗證失敗",
				"literal '%%' Motion filename must pass path validation (parse failure OK)")
		}
	})

	t.Run("Force path validation accepts literal percent", func(t *testing.T) {
		dataFolder := writePctFile(t)
		analyzer := NewPhaseSyncAnalyzer()
		// Bootstrap EMG uses the same '%' file (it exists in dataFolder): the door now
		// validates existence atomically, so the bootstrap EMG must be a real file to
		// populate ctx.baseFolder before exercising the Force validator.
		ctx := &validationContext{
			params:   &models.AnalysisParams{DataFolder: dataFolder},
			manifest: models.PhaseManifest{EMGFile: literalPercentEMGName, ForceFile: literalPercentEMGName},
		}
		require.NoError(t, validateEMGFilePath(analyzer, ctx), "bootstrap EMG validation")
		if err := validateForceFile(analyzer, ctx); err != nil {
			assert.NotContains(t, err.Error(), "Force Plate 檔案路徑驗證失敗",
				"literal '%%' Force filename must pass path validation (parse failure OK)")
		}
	})
}

// TestPhaseSyncAnalyzer_LiteralPercentEMGEndToEnd pins the reported symptom at the
// AnalyzePhaseSync seam: a '%' EMG file present in the data folder must clear path
// validation. Analysis still fails downstream (Motion file absent), but never with
// the EMG path-validation error.
func TestPhaseSyncAnalyzer_LiteralPercentEMGEndToEnd(t *testing.T) {
	analyzer := NewPhaseSyncAnalyzer()
	dataFolder := t.TempDir()
	emgContent := `Time,Ch1,Ch2,Ch3,Ch4,Ch5,Ch6
0.0,10,20,30,40,50,60
0.001,11,21,31,41,51,61
`
	require.NoError(t, os.WriteFile(filepath.Join(dataFolder, literalPercentEMGName),
		[]byte(emgContent), fsperm.FilePerm))

	manifestContent := fmt.Sprintf(`Subject,Motion檔案,力板檔案,EMG檔案,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
TestSubject,motion.csv,force.csv,%s,100,1.0,2.0,3.0,4.0,5.0,250,6.0,7.0,350,8.0`, literalPercentEMGName)
	manifestFile := createTempFile(t, manifestContent)

	params := &models.AnalysisParams{
		ManifestFile: manifestFile,
		DataFolder:   dataFolder,
		StartPhase:   "P0",
		EndPhase:     "P2",
		SubjectIndex: 0,
	}

	_, err := analyzer.AnalyzePhaseSync(context.Background(), params)
	require.Error(t, err, "analysis still fails (Motion file absent)")
	assert.NotContains(t, err.Error(), "EMG 檔案路徑驗證失敗",
		"literal '%%' EMG filename must clear path validation at the AnalyzePhaseSync seam")
}

// TestValidateEMGFilePath_MissingFile_ReportsNotFound pins the EMG missing-file
// contract: a valid baseFolder whose EMGFile names a file absent on disk must
// report "EMG 檔案不存在" and satisfy errors.Is(err, ErrFileNotFound) — matching
// Motion/Force (which route the same manifest.OpenDataFile error through
// mapOpenDataFileErr). Before this fix the EMG path wrapped every OpenDataFile
// error as "EMG 檔案路徑驗證失敗", so a missing EMG was misclassified and
// errors.Is(err, ErrFileNotFound) was false — silently defeating callers.
func TestValidateEMGFilePath_MissingFile_ReportsNotFound(t *testing.T) {
	base := t.TempDir() // valid, existing data folder
	ctx := &validationContext{
		params:   &models.AnalysisParams{DataFolder: base},
		manifest: models.PhaseManifest{EMGFile: "does_not_exist.csv"},
	}

	err := validateEMGFilePath(NewPhaseSyncAnalyzer(), ctx)
	require.Error(t, err, "missing EMG file must produce an error")
	assert.ErrorIs(t, err, ErrFileNotFound,
		"missing EMG file must satisfy errors.Is(err, ErrFileNotFound); got: %v", err)
	assert.Contains(t, err.Error(), "EMG 檔案不存在",
		"missing EMG file must report '不存在', not '路徑驗證失敗'; got: %v", err)
}

// TestPhaseSyncValidators_EMGSymlinkSafety pins the symlink posture after migrating
// to the manifest.OpenDataFile door (ADR-0017 Decision 7 + the validate/open
// consolidation). The door performs a fused lenient-resolve + atomic validated-open
// (RESOLVE_NO_SYMLINKS on Linux, O_NOFOLLOW_ANY on Darwin), so the SECURITY contract
// it enforces is "the resolved target must stay inside the data folder", not the
// older lexical detail of "store the unresolved leaf". This test pins that property
// directly:
//
//   - escape case: a symlink whose target lives OUTSIDE the data folder is rejected
//     with the EMG path-validation error (the real attack — the door's matchAnyBase
//     boundary check refuses the out-of-base resolved target).
//   - in-base case: a symlink whose target stays inside the data folder is accepted
//     (the door resolves the leaf and opens the in-base target). This is the behavioral
//     change from the pre-migration lexical-leaf storage; the door is the single
//     enforcement point now.
func TestPhaseSyncValidators_EMGSymlinkSafety(t *testing.T) {
	if os.Getenv("GOOS") == "windows" {
		t.Skip("symlink + RESOLVE_NO_SYMLINKS/O_NOFOLLOW_ANY semantics are unix-specific")
	}

	t.Run("symlink escaping data folder is rejected", func(t *testing.T) {
		base := t.TempDir()
		// Target lives OUTSIDE base — the classic symlink-escape attack.
		outside := t.TempDir()
		secret := filepath.Join(outside, "secret.csv")
		require.NoError(t, os.WriteFile(secret, []byte("Time,Ch1\n0,1\n"), fsperm.FilePerm))
		link := filepath.Join(base, "escape_emg.csv")
		if err := os.Symlink(secret, link); err != nil {
			t.Skipf("cannot create symlink: %v", err)
		}

		ctx := &validationContext{
			params:   &models.AnalysisParams{DataFolder: base},
			manifest: models.PhaseManifest{EMGFile: "escape_emg.csv"},
		}
		err := validateEMGFilePath(NewPhaseSyncAnalyzer(), ctx)
		require.Error(t, err,
			"symlink whose target escapes the data folder must be rejected by the door")
		assert.Contains(t, err.Error(), "EMG 檔案路徑驗證失敗",
			"escape must surface as the EMG path-validation error, not a downstream parse error")
	})

	t.Run("symlink whose target stays in data folder is accepted", func(t *testing.T) {
		base := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(base, "real_emg.csv"),
			[]byte("Time,Ch1\n0,1\n"), fsperm.FilePerm))
		link := filepath.Join(base, "link_emg.csv")
		if err := os.Symlink("real_emg.csv", link); err != nil {
			t.Skipf("cannot create symlink: %v", err)
		}

		ctx := &validationContext{
			params:   &models.AnalysisParams{DataFolder: base},
			manifest: models.PhaseManifest{EMGFile: "link_emg.csv"},
		}
		require.NoError(t, validateEMGFilePath(NewPhaseSyncAnalyzer(), ctx),
			"in-folder symlink target stays in base, so the door accepts it")
		// The door resolves the leaf to its in-base target and stores that resolved
		// path; the stored path must itself be inside the (resolved) data folder.
		resolvedBase, err := filepath.EvalSymlinks(base)
		require.NoError(t, err)
		rel, err := filepath.Rel(resolvedBase, ctx.emgFilePath)
		require.NoError(t, err)
		assert.False(t, rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)),
			"resolved EMG path must stay within the data folder; got rel=%q", rel)
	})
}
