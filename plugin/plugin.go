package plugin

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path"

	"github.com/fiatjaf/lightningd-gjson-rpc"
)

type Plugin struct {
	Client  *lightning.Client    `json:"-"`
	Log     func(...interface{}) `json:"-"`
	Name    string               `json:"-"`
	Version string               `json:"-"`

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
	Handler RPCHandler
}

func (h Hook) MarshalJSON() ([]byte, error) { return json.Marshal(h.Type) }

type RPCHandler func(p *Plugin, params Params) (resp interface{}, errCode int, err error)
type NotificationHandler func(p *Plugin, params Params)

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

func (p *Plugin) Listener(initialized chan<- bool) {
	rpcmethodmap := make(map[string]RPCMethod, len(p.RPCMethods))
	for _, rpcmethod := range p.RPCMethods {
		rpcmethodmap[rpcmethod.Name] = rpcmethod
	}
	submap := make(map[string]Subscription, len(p.Subscriptions))
	for _, sub := range p.Subscriptions {
		submap[sub.Type] = sub
	}
	hookmap := make(map[string]Hook, len(p.Hooks))
	for _, hook := range p.Hooks {
		hookmap[hook.Type] = hook
	}

	p.Log = func(args ...interface{}) {
		args = append([]interface{}{"plugin-", p.Name, " "}, args...)
		log.Print(args...)
	}

	var msg lightning.JSONRPCMessage

	incoming := json.NewDecoder(os.Stdin)
	outgoing := json.NewEncoder(os.Stdout)
	for {
		err := incoming.Decode(&msg)
		if err == io.EOF {
			return
		}

		response := lightning.JSONRPCResponse{
			Version: msg.Version,
			Id:      msg.Id,
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
			json.Unmarshal([]byte(jmanifest), &response.Result)
		default:
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
				resp, errCode, err := hook.Handler(p, Params(msg.Params.(map[string]interface{})))
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

			if sub, ok := submap[msg.Method]; ok {
				sub.Handler(p, Params(msg.Params.(map[string]interface{})))
				goto noanswer
			}
		}

	end:
		outgoing.Encode(response)

	noanswer:
	}
}
