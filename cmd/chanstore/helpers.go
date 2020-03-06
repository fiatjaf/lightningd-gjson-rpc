package main

import (
	"fmt"

	lightning "github.com/fiatjaf/lightningd-gjson-rpc"
)

func halfchannelsAreOk(halfchannels []lightning.Channel) error {
	// check halfchannels well-formed and not abusive or stupid
	if len(halfchannels) != 2 ||
		halfchannels[0].Source != halfchannels[1].Destination ||
		halfchannels[0].Destination != halfchannels[1].Source {
		return fmt.Errorf("invalid halfchannels!")
	}
	for _, half := range halfchannels {
		if half.Source < half.Destination && half.Direction != 0 ||
			half.Destination < half.Source && half.Direction != 1 ||
			half.HtlcMinimumMsat > half.HtlcMaximumMsat {
			return fmt.Errorf("invalid halfchannel: %v", half)
		}
		if half.HtlcMaximumMsat < 10000000 ||
			half.FeePerMillionth > 10000 ||
			half.BaseFeeMillisatoshi > 10000 {
			return fmt.Errorf("halfchannel isn't good enough: %v", half)
		}
	}

	return nil
}
