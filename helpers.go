package lightning

import (
	"encoding/hex"
	"errors"
	"strconv"
)

func toFloat(val interface{}) (float64, error) {
	switch v := val.(type) {
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case uint32:
		return float64(v), nil
	case float64:
		return v, nil
	case string:
		return strconv.ParseFloat(v, 64)
	}

	return 0, errors.New("Can't convert to float.")
}

func as32(v []byte) (res [32]byte) {
	for i := 0; i < 32; i++ {
		res[i] = v[i]
	}
	return
}

func asByte32(hexstr string) (res [32]byte, err error) {
	v, err := hex.DecodeString(hexstr)
	if err != nil {
		return
	}
	return as32(v), nil
}
