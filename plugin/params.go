package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

type Params map[string]interface{}

func (params Params) Get(path string) gjson.Result {
	j, _ := json.Marshal(params)
	return gjson.GetBytes(j, path)
}

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

func GetParams(givenParams interface{}, usage string) (params Params, err error) {
	usage = strings.TrimSpace(usage)
	if usage == "" {
		return
	}

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

	lastvariadic := false
	klen := len(keys)
	lastkey := keys[klen-1]
	if strings.HasSuffix(lastkey, "...") {
		lastvariadic = true
		keys[klen-1] = lastkey[:len(lastkey)-3]
	}

	params = make(map[string]interface{})
	switch p := givenParams.(type) {
	case []interface{}:
		for i, v := range p {

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

			if i < klen-1 {
				params[keys[i]] = value
			} else if i == klen-1 {
				if lastvariadic {
					value = []interface{}{value}
				}
				params[keys[i]] = value
			} else if lastvariadic {
				params[lastkey] = append(params[lastkey].([]interface{}), value)
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
