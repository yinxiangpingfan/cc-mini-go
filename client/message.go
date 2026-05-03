package client

type ChatCompletionMessage struct{}

type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

func NewSystemMessage(content string) *Message {
	return &Message{
		Role:    "system",
		Content: content,
	}
}

func NewUserMessage(content string) *Message {
	return &Message{
		Role:    "user",
		Content: content,
	}
}
