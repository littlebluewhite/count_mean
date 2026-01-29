package errors

import "count_mean/internal/i18n"

// I18nError creates an internationalized error.
func I18nError(code ErrorCode, i18nKey, fallbackMessage string) *AppError {
	// Try to get translated message
	message := i18n.T(i18nKey)

	// If translation failed (returns the key), use fallback
	if message == i18nKey {
		message = fallbackMessage
	}

	return NewAppError(code, message)
}

// I18nErrorWithDetails creates an internationalized error with details.
func I18nErrorWithDetails(code ErrorCode, i18nKey, fallbackMessage, details string) *AppError {
	// Try to get translated message
	message := i18n.T(i18nKey)

	// If translation failed (returns the key), use fallback
	if message == i18nKey {
		message = fallbackMessage
	}

	return NewAppErrorWithDetails(code, message, details)
}

// I18nErrorWithCause creates an internationalized error with a cause.
func I18nErrorWithCause(code ErrorCode, i18nKey, fallbackMessage string, cause error) *AppError {
	// Try to get translated message
	message := i18n.T(i18nKey)

	// If translation failed (returns the key), use fallback
	if message == i18nKey {
		message = fallbackMessage
	}

	return NewAppErrorWithCause(code, message, cause)
}

// GetI18nFileNotFoundError 返回國際化的檔案未找到錯誤。
func GetI18nFileNotFoundError() *AppError {
	return I18nError(ErrCodeFileNotFound, "error.file_not_found", "檔案未找到")
}

// GetI18nInvalidPathError 返回國際化的無效路徑錯誤。
func GetI18nInvalidPathError() *AppError {
	return I18nError(ErrCodePathValidation, "error.invalid_path", "無效的檔案路徑")
}

// GetI18nInvalidCSVError 返回國際化的無效 CSV 格式錯誤。
func GetI18nInvalidCSVError() *AppError {
	return I18nError(ErrCodeFileFormat, "error.invalid_csv", "無效的 CSV 檔案格式")
}

// GetI18nFileTooLargeError 返回國際化的檔案過大錯誤。
func GetI18nFileTooLargeError() *AppError {
	return I18nError(ErrCodeFileTooLarge, "error.file_too_large", "檔案過大，請使用大文件處理功能")
}

// GetI18nInsufficientDataError 返回國際化的資料不足錯誤。
func GetI18nInsufficientDataError() *AppError {
	return I18nError(ErrCodeInsufficientData, "error.insufficient_data", "資料不足")
}

// GetI18nCalculationFailedError 返回國際化的計算失敗錯誤。
func GetI18nCalculationFailedError() *AppError {
	return I18nError(ErrCodeCalculation, "error.calculation_failed", "計算失敗")
}

// GetI18nMemoryLimitError 返回國際化的記憶體不足錯誤。
func GetI18nMemoryLimitError() *AppError {
	return I18nError(ErrCodeMemory, "error.memory_limit", "記憶體不足")
}

// GetI18nValidationFailedError 返回國際化的驗證失敗錯誤。
func GetI18nValidationFailedError() *AppError {
	return I18nError(ErrCodeDataValidation, "error.validation_failed", "驗證失敗")
}
