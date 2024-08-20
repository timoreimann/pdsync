package isolated

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/slack-go/slack"
)

func TestTest(t *testing.T) {
	obj := slack.NewTextBlockObject("plain_text", "here is a url: golang.org", false, false)
	block := slack.NewSectionBlock(obj, nil, nil)
	msg := slack.NewBlockMessage(block)

	b, err := json.MarshalIndent(msg, "", "    ")
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println(string(b))

}
