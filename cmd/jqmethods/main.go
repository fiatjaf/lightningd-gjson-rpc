package main

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/fiatjaf/lightningd-gjson-rpc"
	"github.com/threatgrid/jqpipe-go"
	"github.com/tidwall/gjson"
	"gopkg.in/yaml.v2"
)

var ln *lightning.Client
var repo string
var jqmethods map[string]JQMethod

func plog(str string) {
	log.Print("plugin-jqmethods " + str)
}

type JQMethod struct {
	Version     int    `yaml:"version" json:"-"`
	Filter      string `yaml:"filter" json:"-"`
	RPC         string `yaml:"rpc" json:"call"`
	Description string `yaml:"description" json:"description"`
}

const manifest = `{
  "options": [
    {
      "name": "jq-source",
      "type": "string",
      "default": "fiatjaf/jqmethods",
      "description": "GitHub repository from which we will fetch the jq methods."
    }
  ],
  "rpcmethods": [
    {
      "name": "jq-refresh",
      "description": "Refreshes the list of available jq methods from the given source.",
      "usage": ""
    },
    {
      "name": "jq",
      "usage": "[method] [args...]",
      "description": "Call the given jq method if it exists, otherwise list all available methods."
    }
  ],
  "subscriptions": []
}`

func loadJQMethods() {
	jqmethods = make(map[string]JQMethod)

	resp, err := http.Get("https://api.github.com/repos/" + repo + "/contents/")
	if err != nil {
		plog("Failed to load jq methods from " + repo + ": " + err.Error())
		return
	}

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		plog("Invalid response from GitHub repository " + repo + ": " + err.Error())
		return
	}

	// loop through each entry in the directory
	gjson.ParseBytes(b).ForEach(func(_, file gjson.Result) bool {
		name := file.Get("name").String()
		if !strings.HasSuffix(name, ".yaml") {
			return true
		}
		name = name[:len(name)-5]

		// load each file individually
		resp, err := http.Get(file.Get("download_url").String())
		if err != nil {
			plog("Error loading jqmethod " + name + " from " + repo + ": " + err.Error())
			return true
		}

		contents, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			plog("Error reading jqmethod " + name + " from " + repo + ": " + err.Error())
			return true
		}

		// parse yaml
		var m JQMethod
		err = yaml.Unmarshal(contents, &m)
		if err != nil {
			plog("Invalid jqmethod definition on " + name + ": " + err.Error())
			return true
		}

		jqmethods[name] = m
		return true
	})
}

func main() {
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
			plog("failed to decode request, killing: " + err.Error())
			return
		}

		switch msg.Method {
		case "init":
			init := msg.Params.(map[string]interface{})

			// get source repo and load methods
			ioptions := init["options"]
			options := ioptions.(map[string]interface{})
			u := options["jq-source"]
			repo = u.(string)

			// load methods
			loadJQMethods()

			// init client
			iconf := init["configuration"]
			conf := iconf.(map[string]interface{})
			ilnpath := conf["lightning-dir"]
			irpcfile := conf["rpc-file"]
			rpc := path.Join(ilnpath.(string), irpcfile.(string))
			ln = &lightning.Client{Path: rpc}
			plog("Initialized jqmethods plugin.")
		case "getmanifest":
			json.Unmarshal([]byte(manifest), &response.Result)
		case "jq-refresh":
			loadJQMethods()
			json.Unmarshal([]byte(`{"repo": "`+repo+`"}`), &response.Result)
		case "jq":
			var method string
			var args interface{}

			switch p := msg.Params.(type) {
			case []interface{}:
				if len(p) == 0 {
					goto listmethods
				}

				method = p[0].(string)
				args = p[1:]
			case map[string]interface{}:
				if len(p) == 0 {
					goto listmethods
				}

				method = p["method"].(string)
				if method == "" {
					goto listmethods
				}

				delete(p, "method")
				args = p
			}

			if def, ok := jqmethods[method]; !ok {
				goto listmethods
			} else {
				switch def.Version {
				case 0:
					var res gjson.Result
					var err error

					switch argsv := args.(type) {
					case []interface{}:
						res, err = ln.Call(def.RPC, argsv...)
					default:
						res, err = ln.Call(def.RPC, args)
					}

					if err != nil {
						response.Error = &lightning.JSONRPCError{
							Code:    -1,
							Message: "Error calling " + def.RPC + ": " + err.Error(),
						}
						goto end
					}

					jsons, err := jq.Eval(res.String(), def.Filter)
					if len(jsons) == 0 {
						response.Error = &lightning.JSONRPCError{
							Code:    -1,
							Message: "Error applying filter: " + err.Error(),
						}
						goto end
					}

					response.Result = jsons[0]
					goto end
				}
			}

		listmethods:
			result, _ := json.Marshal(struct {
				AvailableMethods map[string]JQMethod `json:"available_methods"`
			}{jqmethods})
			json.Unmarshal(result, &response.Result)
		}

	end:
		outgoing.Encode(response)
	}
}
