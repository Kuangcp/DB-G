package engine

import (
	"errors"
	"fmt"
)

// ErrorCode 是引擎/驱动产生的、需要按界面语言展示的错误码。
// engine 是纯叶层，不能 import locales；由 app.ErrorText 负责翻译，
// 驱动只返回错误码，不再拼中文/英文用户文案。
type ErrorCode string

const (
	ErrParamsFormat       ErrorCode = "params_format"
	ErrSessionClosed      ErrorCode = "session_closed"
	ErrHostRequired       ErrorCode = "host_required"
	ErrNoDatabaseSelected ErrorCode = "no_database_selected"
	ErrNoSchemaSelected   ErrorCode = "no_schema_selected"
	ErrStatsTableOnly     ErrorCode = "stats_table_only"
	ErrDDLUnsupported     ErrorCode = "ddl_unsupported"
	ErrDDLNotFound        ErrorCode = "ddl_not_found"
	ErrAnalyzeUnsupported ErrorCode = "analyze_unsupported"
)

// UserError 是一个可本地化的用户可见错误；Arg 供文案占位符使用。
type UserError struct {
	Code ErrorCode
	Arg  string
}

func (e *UserError) Error() string {
	if e.Arg != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Arg)
	}
	return string(e.Code)
}

func (e *UserError) ErrorCode() ErrorCode { return e.Code }

// NewUserError 构造一个可本地化的用户错误。
func NewUserError(code ErrorCode, arg string) error {
	return &UserError{Code: code, Arg: arg}
}

// AsUserError 取出错误链里的 UserError。
func AsUserError(err error) (*UserError, bool) {
	var ue *UserError
	if errors.As(err, &ue) {
		return ue, true
	}
	return nil, false
}
