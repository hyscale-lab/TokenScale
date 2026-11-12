package gateway

import (
	"encoding/json"
	"fmt"
)

type OpenAIPrompt struct {
	Text   *string
	Tokens []int
}

func (p OpenAIPrompt) MarshalJSON() ([]byte, error) {
	if p.Text != nil {
		return json.Marshal(*p.Text)
	}
	if p.Tokens != nil {
		return json.Marshal(p.Tokens)
	}
	// If both are nil, encode as null
	return json.Marshal(nil)
}

func (p *OpenAIPrompt) UnmarshalJSON(data []byte) error {
	// Try string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		p.Text = &s
		return nil
	}

	// Try []int
	var arr []int
	if err := json.Unmarshal(data, &arr); err == nil {
		p.Tokens = arr
		return nil
	}

	return fmt.Errorf("prompt must be string or []int")
}

func (p *OpenAIPrompt) Length() int {
	if p == nil {
		return 0
	}
	if p.Text != nil {
		return len(*p.Text)
	}
	if p.Tokens != nil {
		return len(p.Tokens)
	}
	return 0
}

// AppendRandomToken takes an OpenAIPrompt and returns a new one with an extra token/text appended.
func AppendRandomToken(p OpenAIPrompt) OpenAIPrompt {
	if p.Text != nil {
		newText := *p.Text + " happy"
		p.Text = &newText
	} else if p.Tokens != nil {
		p.Tokens = append(p.Tokens, 6480)
	}
	return p
}
