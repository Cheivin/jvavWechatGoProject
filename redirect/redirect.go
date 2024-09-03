package redirect

type MessageRedirector interface {
	SendMessage([]byte) error
}

type MessageReceiver interface {
	OnMessage(OnMessage)
}
type OnMessage func(payload []byte, receiver string, id string) error
