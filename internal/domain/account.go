package domain

type LoginState string

const (
	LoginStateLoggedOut LoginState = "logged_out"
	LoginStatePending   LoginState = "pending"
	LoginStateLoggedIn  LoginState = "logged_in"
)

func (s LoginState) Valid() bool {
	switch s {
	case LoginStateLoggedOut, LoginStatePending, LoginStateLoggedIn:
		return true
	default:
		return false
	}
}

// Account is one WeChat account as projected onto this NAS.
type Account struct {
	ID            string     `json:"id"`
	WxID          string     `json:"wxid"`
	Nickname      string     `json:"nickname"`
	Avatar        string     `json:"avatar"`
	LoginState    LoginState `json:"login_state"`
	BackupRoot    string     `json:"backup_root"`
	AccessPwdHash string     `json:"-"`
}
