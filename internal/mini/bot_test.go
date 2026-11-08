package mini_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DmitryKolbin/santa-tg-web/internal/mini"
	minimocks "github.com/DmitryKolbin/santa-tg-web/mocks/internal_/mini"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
)

const testToken = "123:test-secret"

func TestBot(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Telegram Bot Suite")
}

func telegramResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewBufferString(body)), Header: make(http.Header)}
}

var _ = Describe("Telegram bot", func() {
	It("configures the Mini App and handles failed delivery", func() {
		client := minimocks.NewHTTPClient(GinkgoT())
		delivered := make(chan map[string]any, 1)
		configured := map[string]bool{}
		polls := 0
		client.EXPECT().Do(mock.Anything).RunAndReturn(func(req *http.Request) (*http.Response, error) {
			method := req.URL.Path[strings.LastIndex(req.URL.Path, "/")+1:]
			var body map[string]any
			_ = json.NewDecoder(req.Body).Decode(&body)
			switch method {
			case "getMe":
				return telegramResponse(http.StatusOK, `{"ok":true,"result":{"username":"test_bot"}}`), nil
			case "setChatMenuButton", "setMyCommands":
				configured[method] = true
				if method == "setChatMenuButton" {
					button := body["menu_button"].(map[string]any)
					if button["type"] != "web_app" {
						return nil, errors.New("wrong menu type")
					}
				}
				return telegramResponse(http.StatusOK, `{"ok":true,"result":true}`), nil
			case "getUpdates":
				polls++
				if polls == 1 {
					return telegramResponse(http.StatusOK, `{"ok":true,"result":[{"update_id":1,"message":{"text":"/start","chat":{"id":42,"type":"private"}}}]}`), nil
				}
				<-req.Context().Done()
				return nil, req.Context().Err()
			case "sendMessage":
				delivered <- body
				return telegramResponse(http.StatusForbidden, `{"ok":false,"error_code":403}`), nil
			default:
				return nil, errors.New("unexpected Telegram method: " + method)
			}
		})

		bot := mini.NewBotWithClient(testToken, "https://app.example", client)
		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)

		name, err := bot.Configure(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(name).To(Equal("test_bot"))
		Expect(configured).To(HaveKeyWithValue("setChatMenuButton", true))
		Expect(configured).To(HaveKeyWithValue("setMyCommands", true))

		done := make(chan struct{})
		go func() {
			defer GinkgoRecover()
			bot.Run(ctx)
			close(done)
		}()
		var body map[string]any
		Eventually(delivered, 3*time.Second).Should(Receive(&body))
		Expect(body).To(HaveKeyWithValue("chat_id", BeNumerically("==", 42)))
		Expect(body).To(HaveKey("reply_markup"))
		cancel()
		Eventually(done, 3*time.Second).Should(BeClosed())
	})
})
