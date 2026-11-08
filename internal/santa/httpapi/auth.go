package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type telegramUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

func validateInitData(raw, token string, now time.Time) (telegramUser, error) {
	fail := errors.New("откройте приложение заново через Telegram")
	var user telegramUser
	if len(raw) > 16384 {
		return user, fail
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return user, fail
	}
	signature, err := hex.DecodeString(values.Get("hash"))
	if err != nil || len(signature) != sha256.Size {
		return user, fail
	}
	var pairs []string
	for key, val := range values {
		if len(val) != 1 {
			return user, fail
		}
		if key != "hash" {
			pairs = append(pairs, key+"="+val[0])
		}
	}
	sort.Strings(pairs)
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(pairs, "\n")))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return user, fail
	}
	stamp, err := strconv.ParseInt(values.Get("auth_date"), 10, 64)
	if err != nil || stamp > now.Unix()+30 || stamp < now.Add(-time.Hour).Unix() {
		return user, fail
	}
	if json.Unmarshal([]byte(values.Get("user")), &user) != nil || user.ID <= 0 {
		return user, fail
	}
	return user, nil
}
