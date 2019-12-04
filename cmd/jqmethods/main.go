package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/fiatjaf/lightningd-gjson-rpc/plugin"
	"github.com/threatgrid/jqpipe-go"
	"github.com/tidwall/gjson"
	"gopkg.in/yaml.v2"
)

var repo string
var jqmethods map[string]JQMethod

type JQMethod struct {
	Version     int    `yaml:"version" json:"-"`
	Filter      string `yaml:"filter" json:"-"`
	RPC         string `yaml:"rpc" json:"call"`
	Description string `yaml:"description" json:"description"`
}

func main() {
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	http.DefaultTransport.(*http.Transport).Proxy = http.ProxyFromEnvironment

	p := plugin.Plugin{
		Name:    "jqmethods",
		Version: "v1.0",
		Options: []plugin.Option{
			{
				"jq-source",
				"string",
				"fiatjaf/jqmethods",
				"GitHub repository from which we will fetch the jq methods.",
			},
		},
		RPCMethods: []plugin.RPCMethod{
			{
				"jq-refresh",
				"",
				"Refreshes the list of available jq methods from the given source.",
				"",
				func(p *plugin.Plugin, params plugin.Params) (resp interface{}, errCode int, err error) {
					loadJQMethods(p)
					return map[string]bool{"ok": true}, 0, nil
				},
			},
			{
				"jq-method",
				"[method] [args...]",
				"Call the given jq method if it exists, otherwise list all available methods.",
				"",
				func(p *plugin.Plugin, params plugin.Params) (resp interface{}, errCode int, err error) {
					method := params.Get("method").String()
					var args []interface{}
					params.Get("args").ForEach(func(_, value gjson.Result) bool {
						args = append(args, value.Value())
						return true
					})

					if def, ok := jqmethods[method]; !ok {
						goto listmethods
					} else {
						switch def.Version {
						case 0:
							var res gjson.Result
							var err error

							res, err = p.Client.Call(def.RPC, args...)

							if err != nil {
								return nil, -1, errors.New("Error calling " + def.RPC + ": " + err.Error())
							}

							jsons, err := jq.Eval(res.String(), def.Filter)
							if len(jsons) == 0 {
								return nil, -2, errors.New("Error applying filter: " + err.Error())
							}

							var response interface{}
							json.Unmarshal(jsons[0], &response)
							return response, 0, nil
						}
					}

				listmethods:
					return struct {
						AvailableMethods map[string]JQMethod `json:"available_methods"`
					}{jqmethods}, 0, nil
				},
			},
		},
		OnInit: func(p *plugin.Plugin) {
			repo = p.Args.Get("jq-source").String()
			loadJQMethods(p)
		},
	}

	p.Run()
}

func loadJQMethods(p *plugin.Plugin) {
	jqmethods = make(map[string]JQMethod)

	resp, err := http.Get("https://api.github.com/repos/" + repo + "/contents/")
	if err != nil {
		p.Log("Failed to load jq methods from " + repo + ": " + err.Error())
		return
	}

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		p.Log("Invalid response from GitHub repository " + repo + ": " + err.Error())
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
			p.Log("Error loading jqmethod " + name + " from " + repo + ": " + err.Error())
			return true
		}

		contents, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			p.Log("Error reading jqmethod " + name + " from " + repo + ": " + err.Error())
			return true
		}

		// parse yaml
		var m JQMethod
		err = yaml.Unmarshal(contents, &m)
		if err != nil {
			p.Log("Invalid jqmethod definition on " + name + ": " + err.Error())
			return true
		}

		jqmethods[name] = m
		return true
	})
}
