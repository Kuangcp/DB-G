package app

// localeError 是文案在显示时按当前语言解析的哨兵错误。
// 用指针类型保住 errors.Is 的同一性（函数不可比较，指针可以）。
type localeError struct{ text func() string }

func (e *localeError) Error() string { return e.text() }

func newLocaleError(text func() string) error { return &localeError{text: text} }
