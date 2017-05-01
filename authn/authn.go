package authn

import (
	"github.com/astaxie/beego"
	"github.com/astaxie/beego/plugins/auth"
	"strings"
)

var defaultRealm = "Authorization Required"

// NewAuthenticator initializes a new basic authenticator.
func NewAuthenticator(userpassList ...string) beego.FilterFunc {
	secrets := func(user, pass string) bool {
		for _, userpass := range userpassList {
			str := strings.Split(userpass, ":")
			username := str[0]
			password := str[1]

			if user == username && pass == password {
				return true
			}
		}
		return false
	}
	return auth.NewBasicAuthenticator(secrets, defaultRealm)
}
