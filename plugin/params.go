package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/fiatjaf/lightningd-gjson-rpc"
)

type Params map[string]interface{}

func (params Params) String(key string) (s string, err error) {
	v, ok := params[key]
	if !ok {
		err = errKey(key)
		return
	}
	s, ok = v.(string)
	if !ok {
		err = errType(key)
	}

	if s == "null" {
		s = ""
		err = errType(key)
		return
	}

	return
}

func (params Params) Bool(key string) (b bool, err error) {
	v, ok := params[key]
	if !ok {
		err = errKey(key)
		return
	}
	b, ok = v.(bool)
	if !ok {
		err = errType(key)
	}
	return
}

func (params Params) Int(key string) (i int, err error) {
	v, ok := params[key]
	if !ok {
		err = errKey(key)
		return
	}
	switch n := v.(type) {
	case int:
		i = n
	case int64:
		i = int(n)
	case float64:
		i = int(n)
	default:
		err = errType(key)
	}
	return
}

func (params Params) Float64(key string) (i float64, err error) {
	v, ok := params[key]
	if !ok {
		err = errKey(key)
		return
	}
	switch n := v.(type) {
	case int:
		i = float64(n)
	case int64:
		i = float64(n)
	case float64:
		i = n
	default:
		err = errType(key)
	}
	return
}

func errKey(key string) error {
	return fmt.Errorf("no such key: %q", key)
}

func errType(key string) error {
	return fmt.Errorf("key: %q failed type conversion", key)
}

func GetParams(msg lightning.JSONRPCMessage, usage string) (params Params, err error) {
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
