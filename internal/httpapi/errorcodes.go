// Package httpapi maps domain results to HTTP responses and registers every
// client-visible error code in one place.
package httpapi

// ErrorCode is a stable machine-readable failure identifier.
type ErrorCode string

// Registered API error codes. Keep in sync with llms.txt (enforced by test).
const (
	CodeTokenNotFound            ErrorCode = "token_not_found"
	CodeTokenExpired             ErrorCode = "token_expired"
	CodeTokenRevoked             ErrorCode = "token_revoked"
	CodeUploadInProgress         ErrorCode = "upload_in_progress"
	CodeArchiveTooLarge          ErrorCode = "archive_too_large"
	CodeArchiveUnpackedTooLarge  ErrorCode = "archive_unpacked_too_large"
	CodeArchiveTooManyFiles      ErrorCode = "archive_too_many_files"
	CodeArchiveMalformed         ErrorCode = "archive_malformed"
	CodeArchiveUnsafeEntry       ErrorCode = "archive_unsafe_entry"
	CodeUnsupportedFormat        ErrorCode = "unsupported_format"
	CodeEntrypointNotFound       ErrorCode = "entrypoint_not_found"
	CodeChecksumMismatch         ErrorCode = "checksum_mismatch"
	CodeProjectNameInvalid       ErrorCode = "project_name_invalid"
	CodeRateLimited              ErrorCode = "rate_limited"
	CodeStorageCapacityExceeded  ErrorCode = "storage_capacity_exceeded"
	CodeServiceReadOnly          ErrorCode = "service_read_only"
	CodeInternalError            ErrorCode = "internal_error"
)

// AllCodes returns every registered error code for contract tests.
func AllCodes() []ErrorCode {
	return []ErrorCode{
		CodeTokenNotFound,
		CodeTokenExpired,
		CodeTokenRevoked,
		CodeUploadInProgress,
		CodeArchiveTooLarge,
		CodeArchiveUnpackedTooLarge,
		CodeArchiveTooManyFiles,
		CodeArchiveMalformed,
		CodeArchiveUnsafeEntry,
		CodeUnsupportedFormat,
		CodeEntrypointNotFound,
		CodeChecksumMismatch,
		CodeProjectNameInvalid,
		CodeRateLimited,
		CodeStorageCapacityExceeded,
		CodeServiceReadOnly,
		CodeInternalError,
	}
}
