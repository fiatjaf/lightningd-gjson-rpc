package plugin

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/fiatjaf/lightningd-gjson-rpc"
)

type Plugin struct {
	Client  *lightning.Client            `json:"-"`
	Log     func(...interface{})         `json:"-"`
	Logf    func(string, ...interface{}) `json:"-"`
	Name    string                       `json:"-"`
	Version string                       `json:"-"`

	Options       []Option       `json:"options"`
	RPCMethods    []RPCMethod    `json:"rpcmethods"`
	Subscriptions []Subscription `json:"subscriptions"`
	Hooks         []Hook         `json:"hooks"`
	Dynamic       bool           `json:"dynamic"`

	Args   Params        `json:"-"`
	OnInit func(*Plugin) `json:"-"`
}

type Option struct {
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	Default     interface{} `json:"default"`
	Description string      `json:"description"`
}

type RPCMethod struct {
	Name            string     `json:"name"`
	Usage           string     `json:"usage"`
	Description     string     `json:"description"`
	LongDescription string     `json:"long_description"`
	Handler         RPCHandler `json:"-"`
}

type Subscription struct {
	Type    string
	Handler NotificationHandler
}

func (s Subscription) MarshalJSON() ([]byte, error) { return json.Marshal(s.Type) }

type Hook struct {
	Type    string
	Handler HookHandler
}

func (h Hook) MarshalJSON() ([]byte, error) { return json.Marshal(h.Type) }

type RPCHandler func(p *Plugin, params Params) (resp interface{}, errCode int, err error)
type NotificationHandler func(p *Plugin, params Params)
type HookHandler func(p *Plugin, params Params) (resp interface{})

func (p *Plugin) Run() {
	initialized := make(chan bool)

	go func() {
		<-initialized
		if p.OnInit != nil {
			p.OnInit(p)
		}
	}()

	p.Listener(initialized)
}

func (p *Plugin) colorize(text string) string {
	h := sha256.Sum256([]byte(p.Name))
	n := binary.BigEndian.Uint64(h[:])
	colors := []string{
		"\x1B[01;91m",
		"\x1B[01;92m",
		"\x1B[01;93m",
		"\x1B[01;94m",
		"\x1B[01;95m",
		"\x1B[01;96m",
		"\x1B[01;97m",
	}
	return colors[n%uint64(len(colors))] + text + "\x1B[0m"
}

var rpcmethodmap = make(map[string]RPCMethod)
var submap = make(map[string]Subscription)
var hookmap = make(map[string]Hook)

func (p *Plugin) Listener(initialized chan<- bool) {
	for _, rpcmethod := range p.RPCMethods {
		rpcmethodmap[rpcmethod.Name] = rpcmethod
	}
	for _, sub := range p.Subscriptions {
		submap[sub.Type] = sub
	}
	for _, hook := range p.Hooks {
		hookmap[hook.Type] = hook
	}

	// logging
	prefix := p.colorize("plugin-" + p.Name)
	p.Log = func(args ...interface{}) {
		args = append([]interface{}{prefix + " "}, args...)
		fmt.Fprintln(os.Stderr, args...)
	}
	p.Logf = func(b string, args ...interface{}) {
		fmt.Fprintf(os.Stderr, prefix+" "+b+"\n", args...)
	}

	var msg lightning.JSONRPCMessage

	incoming := json.NewDecoder(os.Stdin)
	outgoing := json.NewEncoder(os.Stdout)
	for {
		err := incoming.Decode(&msg)
		if err == io.EOF {
			return
		}

		if err != nil {
			p.Log("failed to decode request. killing: " + err.Error())
			return
		}

		switch msg.Method {
		case "init":
			params := msg.Params.(map[string]interface{})

			iconf := params["configuration"]
			conf := iconf.(map[string]interface{})
			ilnpath := conf["lightning-dir"]
			irpcfile := conf["rpc-file"]
			rpc := path.Join(ilnpath.(string), irpcfile.(string))

			p.Client = &lightning.Client{Path: rpc}
			p.Args = Params(params["options"].(map[string]interface{}))

			p.Log("initialized plugin " + p.Version)
			initialized <- true
			outgoing.Encode(lightning.JSONRPCResponse{
				Version: msg.Version,
				Id:      msg.Id,
			})
		case "getmanifest":
			if p.Options == nil {
				p.Options = make([]Option, 0)
			}
			if p.RPCMethods == nil {
				p.RPCMethods = make([]RPCMethod, 0)
			}
			if p.Hooks == nil {
				p.Hooks = make([]Hook, 0)
			}
			if p.Subscriptions == nil {
				p.Subscriptions = make([]Subscription, 0)
			}

			jmanifest, _ := json.Marshal(p)
			response := lightning.JSONRPCResponse{
				Version: msg.Version,
				Id:      msg.Id,
			}
			json.Unmarshal([]byte(jmanifest), &response.Result)
			outgoing.Encode(response)
		default:
			go handleMessage(p, outgoing, msg)
		}
	}
}

func handleMessage(p *Plugin, outgoing *json.Encoder, msg lightning.JSONRPCMessage) {
	response := lightning.JSONRPCResponse{
		Version: msg.Version,
		Id:      msg.Id,
	}

	if rpcmethod, ok := rpcmethodmap[msg.Method]; ok {
		params, err := GetParams(msg, rpcmethod.Usage)
		if err != nil {
			response.Error = &lightning.JSONRPCError{
				Code:    400,
				Message: "Error decoding params: " + rpcmethod.Usage,
			}
			goto end
		}

		resp, errCode, err := rpcmethod.Handler(p, params)
		if err != nil {
			if errCode == 0 {
				errCode = -1
			}

			response.Error = &lightning.JSONRPCError{
				Code:    errCode,
				Message: err.Error(),
			}
			goto end
		}

		jresp, err := json.Marshal(resp)
		if err != nil {
			response.Error = &lightning.JSONRPCError{
				Code:    500,
				Message: "Error encoding method response.",
			}
			goto end
		}

		response.Result = jresp
	}

	if hook, ok := hookmap[msg.Method]; ok {
		resp := hook.Handler(p, Params(msg.Params.(map[string]interface{})))
		jresp, err := json.Marshal(resp)
		if err != nil {
			response.Error = &lightning.JSONRPCError{
				Code:    500,
				Message: "Error encoding hook response.",
			}
			goto end
		}
		response.Result = jresp
	}

	if sub, ok := submap[msg.Method]; ok {
		sub.Handler(p, Params(msg.Params.(map[string]interface{})))
		goto noanswer
	}

end:
	outgoing.Encode(response)

noanswer:
}
