package main

type MessageValue struct {
	Message string      `json:"message"`
	Name    string      `json:"name"`
	Value   interface{} `json:"value"`
}
