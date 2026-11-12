package mocks
import (
	"encoding/json"
	"math/rand"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
)

type OpenAIRequestBody struct {
	Prompt      []int       `json:"prompt"`
	MaxTokens   int         `json:"max_tokens"`
	MinTokens   int         `json:"min_tokens"`
	Temperature float64     `json:"temperature"`
	Model       string      `json:"model"`
	Stream      bool        `json:"stream"`
}

// GenerateRequestContext generates a Hertz RequestContext with a body suitable for your Route function
func GenerateRequestContext(inputLength int, outputLength int, model string) *app.RequestContext {
	c := &app.RequestContext{}

	// Generate random prompt IDs
	prompt := make([]int, inputLength)
	rand.Seed(time.Now().UnixNano())
	for i := 0; i < inputLength; i++ {
		prompt[i] = rand.Intn(100) + 1 // generate token IDs from 1 to 100
	}

	body := OpenAIRequestBody{
		Prompt:      prompt,
		MaxTokens:   outputLength,
		MinTokens:   outputLength,
		Temperature: 0,
		Model:       model,
		Stream:      false,
	}

	data, err := json.Marshal(body)
	if err != nil {
		panic(err) // in tests, panic is fine
	}

	c.Request.SetBody(data)
	return c
}