package app

import (
	"db-g/engine"
	"db-g/locales"
)

// localeError 是文案在显示时按当前语言解析的哨兵错误。
// 用指针类型保住 errors.Is 的同一性（函数不可比较，指针可以）。
type localeError struct{ text func() string }

func (e *localeError) Error() string { return e.text() }

func newLocaleError(text func() string) error { return &localeError{text: text} }

// ErrorText 把引擎/驱动的结构化错误翻成当前语言；其它错误（数据库返回的原始
// 错误、文件系统错误等）原样返回，不做二次包装。
func ErrorText(err error) string {
	if err == nil {
		return ""
	}
	ue, ok := engine.AsUserError(err)
	if !ok {
		return err.Error()
	}
	switch ue.Code {
	case engine.ErrParamsFormat:
		return locales.AppErrParamsFormat(ue.Arg)
	case engine.ErrSessionClosed:
		return locales.AppErrSessionClosed()
	case engine.ErrHostRequired:
		return locales.AppErrHostRequired()
	case engine.ErrNoDatabaseSelected:
		return locales.AppErrNoDatabaseSelected()
	case engine.ErrNoSchemaSelected:
		return locales.AppErrNoSchemaSelected()
	case engine.ErrStatsTableOnly:
		return locales.AppErrStatsTableOnly()
	case engine.ErrDDLUnsupported:
		return locales.AppErrDDLUnsupported()
	case engine.ErrDDLNotFound:
		return locales.AppErrDDLNotFound(ue.Arg)
	case engine.ErrAnalyzeUnsupported:
		return locales.AppErrAnalyzeUnsupported()
	}
	return err.Error()
}
