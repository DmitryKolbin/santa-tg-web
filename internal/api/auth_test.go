package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const testToken = "123:test-secret"

func signed(id int64, stamp int64) string {
	values := url.Values{"auth_date": {strconv.FormatInt(stamp, 10)}, "user": {fmt.Sprintf(`{"id":%d,"first_name":"User %d"}`, id, id)}}
	var pairs []string
	for key := range values {
		pairs = append(pairs, key+"="+values.Get(key))
	}
	sort.Strings(pairs)
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = secret.Write([]byte(testToken))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	_, _ = mac.Write([]byte(strings.Join(pairs, "\n")))
	values.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	return values.Encode()
}

var _ = Describe("Telegram init data", func() {
	It("accepts a current valid signature and rejects unsafe inputs", func() {
		now := time.Now()
		raw := signed(1, now.Unix())
		user, err := validateInitData(raw, testToken, now)
		Expect(err).NotTo(HaveOccurred())
		Expect(user.ID).To(Equal(int64(1)))

		for _, invalid := range []string{"", raw + "&user=x", strings.Replace(raw, "User", "Fake", 1), signed(1, now.Add(-2*time.Hour).Unix()), signed(1, now.Add(time.Minute).Unix()), signed(0, now.Unix())} {
			_, err := validateInitData(invalid, testToken, now)
			Expect(err).To(HaveOccurred(), "accepted invalid initData: %q", invalid)
		}
	})
})
