package plugin

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/fiatjaf/lightningd-gjson-rpc"
)

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
