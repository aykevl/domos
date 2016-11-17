package main

type MessageTimestamp struct {
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"`
}

type MessageValue struct {
	Message string      `json:"message"`
	Name    string      `json:"name"`
	Value   interface{} `json:"value"`
}
