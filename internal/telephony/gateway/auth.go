package gateway

import "encoding/base64"

func basicAuth(username string, password string) string {
	return base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
}
