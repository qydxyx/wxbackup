// Package capture is an optional DeviceSession stub for a future LAN
// backup adapter driven only by PCAPs the operator captured from their
// own phone talking to their own NAS.
//
// That capture is a new authorized scope, distinct from the public
// GitHub/README reverse-engineering grant used for this rebuild. It is
// not enabled in production. It must not be filled in from a third-party
// FPK or any decompiled WeChat Mac/iPad protocol stack.
//
// Until that scope is granted and own-device PCAP fixtures exist,
// CaptureSession returns ErrNotAuthorized or ErrNotImplemented and never
// starts a backup or binds ports 8011/24011.
package capture
