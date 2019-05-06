package lightning

import "fmt"

type ErrorConnect struct {
	Path string
	Msg  string
}

type ErrorCommand struct {
	Msg  string
	Code int
}

type ErrorTimeout struct {
	Seconds int
}

type ErrorJSONDecode struct {
	Msg string
}

type ErrorConnectionBroken struct{}

func (c ErrorConnect) Error() string {
	return fmt.Sprintf("unable to dial socket %s:%s", c.Path, c.Msg)
}
func (l ErrorCommand) Error() string {
	return fmt.Sprintf("lightningd replied with error: %s (%d)", l.Msg, l.Code)
}
func (t ErrorTimeout) Error() string {
	return fmt.Sprintf("call timed out after %ds", t.Seconds)
}
func (j ErrorJSONDecode) Error() string {
	return "error decoding JSON response from lightningd: " + j.Msg
}
func (c ErrorConnectionBroken) Error() string {
	return "got an EOF while reading response, it seems the connection is broken"
}
