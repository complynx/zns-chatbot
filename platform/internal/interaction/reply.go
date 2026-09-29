package interaction

// ReplyOrigin describes the actual text producer, independently of formatting
// and of whether the saved proposal contains a command.
type ReplyOrigin string

const (
	DerivedReply ReplyOrigin = "agent"
	TrustedReply ReplyOrigin = "authoritative"
)

type Reply struct {
	Text   string
	Origin ReplyOrigin
}

// TrustedResult is used only at catalog/domain renderer call sites after execution.
func TrustedResult(text string, err error) (Reply, error) {
	return Reply{Text: text, Origin: TrustedReply}, err
}
