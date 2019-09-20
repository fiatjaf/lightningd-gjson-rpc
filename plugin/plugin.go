package plugin

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"path"
	"strings"

	"github.com/fiatjaf/lightningd-gjson-rpc"
)

type Plugin struct {
	Client *lightning.Client    `json:"-"`
	Log    func(...interface{}) ` json:"-"`
	Name   string               `json:"-"`

	Options       []Option    `json:"options"`
	RPCMethods    []RPCMethod `json:"rpcmethods"`
	Subscriptions []string    `json:"subscriptions"`
	Hooks         []string    `json:"hooks"`
	Dynamic       bool        `json:"dynamic"`
}

type Option struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Default     string `json:"default"`
	Description string `json:"description"`
}

type RPCMethod struct {
	Name            string     `json:"name"`
	Usage           string     `json:"usage"`
	Description     string     `json:"description"`
	LongDescription string     `json:"long_description"`
	Handler         RPCHandler `json:"-"`
}

type RPCHandler func(p *Plugin, params map[string]interface{}) (resp interface{}, errCode int, err error)

func (p *Plugin) Run() {
	rpcmethodmap := make(map[string]RPCMethod, len(p.RPCMethods))
	for _, rpcmethod := range p.RPCMethods {
		rpcmethodmap[rpcmethod.Name] = rpcmethod
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
			iconf := msg.Params.(map[string]interface{})["configuration"]
			conf := iconf.(map[string]interface{})
			ilnpath := conf["lightning-dir"]
			irpcfile := conf["rpc-file"]
			rpc := path.Join(ilnpath.(string), irpcfile.(string))
			p.Client = &lightning.Client{Path: rpc}
			p.Log("initialized plugin.")
		case "getmanifest":
			if p.Options == nil {
				p.Options = make([]Option, 0)
			}
			if p.RPCMethods == nil {
				p.RPCMethods = make([]RPCMethod, 0)
			}
			if p.Hooks == nil {
				p.Hooks = make([]string, 0)
			}
			if p.Subscriptions == nil {
				p.Subscriptions = make([]string, 0)
			}

			jmanifest, _ := json.Marshal(p)
			json.Unmarshal([]byte(jmanifest), &response.Result)
		default:
			if rpcmethod, ok := rpcmethodmap[msg.Method]; ok {
				params, err := GetParams(msg, rpcmethod.Usage)
				if err != nil {
					response.Error = &lightning.JSONRPCError{
						Code:    400,
						Message: "Error decoding params",
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
		}

	end:
		outgoing.Encode(response)
	}
}

func GetParams(msg lightning.JSONRPCMessage, usage string) (params map[string]interface{}, err error) {
	keys := strings.Split(usage, " ")
	requiredness := make([]bool, len(keys))

	for i, key := range keys {
		if strings.HasPrefix(key, "[") && strings.HasSuffix(key, "]") {
			requiredness[i] = false
			keys[i] = key[1 : len(key)-1]
		} else {
			requiredness[i] = true
		}
	}

	params = make(map[string]interface{})
	switch p := msg.Params.(type) {
	case []interface{}:
		for i, v := range p {
			if i < len(keys) { // ignore extra parameters

				// try to parse json if it's not json yet (if used through he cli, for ex)
				var value interface{}
				switch strv := v.(type) {
				case string:
					err := json.Unmarshal([]byte(strv), &value)
					if err != nil {
						value = strv
					}
				default:
					value = v
				}

				params[keys[i]] = value
			}
		}
	case map[string]interface{}:
		params = p
	}

	for i := 0; i < len(keys); i++ {
		if _, isSet := params[keys[i]]; !isSet && requiredness[i] == true {
			return params, errors.New("required parameter " + keys[i] + " missing.")
		}
	}

	return
}
