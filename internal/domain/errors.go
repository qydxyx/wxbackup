package domain

import (
	"errors"
	"fmt"
)

type Code string

const (
	CodeNotLoggedIn        Code = "not_logged_in"
	CodeSameWiFiRequired   Code = "same_wifi_required"
	CodePhoneNotForeground Code = "phone_not_foreground"
	CodeMediaNeverOpened   Code = "media_never_opened"
	CodeDiscoveryPortInUse Code = "discovery_port_in_use"
	CodeBackupCancelled    Code = "backup_cancelled"
)

type Error struct {
	Code    Code
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok || e == nil || t == nil {
		return false
	}
	return e.Code == t.Code
}

var (
	ErrNotLoggedIn        = &Error{Code: CodeNotLoggedIn, Message: "account is not logged in"}
	ErrSameWiFiRequired   = &Error{Code: CodeSameWiFiRequired, Message: "phone and NAS must be on the same Wi-Fi"}
	ErrPhoneNotForeground = &Error{Code: CodePhoneNotForeground, Message: "phone WeChat must stay in the foreground"}
	ErrMediaNeverOpened   = &Error{Code: CodeMediaNeverOpened, Message: "media is missing because it was never opened on the phone"}
	ErrDiscoveryPortInUse = &Error{Code: CodeDiscoveryPortInUse, Message: "WeChat ports 8011/24011 are already in use"}
	ErrBackupCancelled    = &Error{Code: CodeBackupCancelled, Message: "backup was cancelled"}
)

func DiscoveryPortInUse(port int) *Error {
	return &Error{
		Code:    CodeDiscoveryPortInUse,
		Message: fmt.Sprintf("WeChat port %d is already in use", port),
	}
}

func AsError(err error) (*Error, bool) {
	var de *Error
	if errors.As(err, &de) {
		return de, true
	}
	return nil, false
}
