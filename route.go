package lightning

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

var (
	lastSynced = time.Now().AddDate(0, 0, -1)

	g *Graph
)

type Graph struct {
	client *Client

	channelsFrom map[string][]*Channel
	channelsTo   map[string][]*Channel
	channelMap   map[string]*Channel

	maxhops       int
	maxchannelfee int64
	msatoshi      int64
}

func (g *Graph) SearchDualBFS(start string, end string) (path []*Channel) {
	fromEnd := map[string][]*Channel{
		end: []*Channel{},
	}
	fromStart := map[string][]*Channel{
		start: []*Channel{},
	}

	for i := 1; i <= g.maxhops/2; i++ {
		// search backwards from end
		fromEndNext := make(map[string][]*Channel)
		for node, routeFrom := range fromEnd {
			for _, channel := range g.channelsTo[node] {
				if g.msatoshi < channel.HtlcMinimumMsat ||
					g.msatoshi > channel.HtlcMaximumMsat ||
					channel.Fee(g.msatoshi, 0, 0) > g.maxchannelfee {
					continue
				}

				routeFromNext := append([]*Channel{channel}, routeFrom...)
				fromEndNext[channel.Source] = routeFromNext
			}
		}
		fromEnd = fromEndNext

		// search frontwards from start
		fromStartNext := make(map[string][]*Channel)
		for node, routeUntil := range fromStart {
			for _, channel := range g.channelsFrom[node] {
				if g.msatoshi < channel.HtlcMinimumMsat ||
					g.msatoshi > channel.HtlcMaximumMsat ||
					channel.Fee(g.msatoshi, 0, 0) > g.maxchannelfee {
					continue
				}

				routeUntilNext := make([]*Channel, i)
				copy(routeUntilNext, routeUntil)
				routeUntilNext[i-1] = channel
				fromStartNext[channel.Destination] = routeUntilNext

				// check for a match
				if routeFrom, ok := fromEnd[channel.Destination]; ok {
					// combine routes and return
					return append(routeUntilNext, routeFrom...)
				}
			}
		}
		fromStart = fromStartNext
	}

	return
}

func (g *Graph) Sync() error {
	// reset our data
	g.channelsFrom = make(map[string][]*Channel)
	g.channelsTo = make(map[string][]*Channel)
	g.channelMap = make(map[string]*Channel)

	// get channels data
	res, err := g.client.Call("listchannels")
	if err != nil {
		return err
	}

	for _, ch := range res.Get("channels").Array() {
		htlcmin, _ := strconv.ParseInt(strings.Split(ch.Get("htlc_minimum_msat").String(), "m")[0], 10, 64)
		htlcmax, _ := strconv.ParseInt(strings.Split(ch.Get("htlc_maximum_msat").String(), "m")[0], 10, 64)

		source := ch.Get("source").String()
		destination := ch.Get("destination").String()
		direction := 0
		if source > destination {
			direction = 1
		}

		channel := &Channel{
			g:                   g,
			Source:              source,
			Destination:         destination,
			ShortChannelID:      ch.Get("short_channel_id").String(),
			BaseFeeMillisatoshi: ch.Get("base_fee_millisatoshi").Int(),
			FeePerMillionth:     ch.Get("fee_per_millionth").Int(),
			Delay:               ch.Get("delay").Int(),
			Direction:           direction,
			HtlcMinimumMsat:     htlcmin,
			HtlcMaximumMsat:     htlcmax,
		}

		g.channelsFrom[channel.Source] = append(g.channelsFrom[channel.Source], channel)
		g.channelsTo[channel.Destination] = append(g.channelsTo[channel.Destination], channel)
		g.channelMap[channel.ShortChannelID+"/"+strconv.Itoa(channel.Direction)] = channel
	}

	// reset counter
	lastSynced = time.Now()

	return nil
}

type Channel struct {
	g *Graph

	Source              string `json:"source"`
	Destination         string `json:"destination"`
	ShortChannelID      string `json:"short_channel_id"`
	BaseFeeMillisatoshi int64  `json:"base_fee_millisatoshi"`
	FeePerMillionth     int64  `json:"fee_per_millionth"`
	Delay               int64  `json:"delay"`
	Direction           int    `json:"direction"`
	HtlcMinimumMsat     int64  `json:"htlc_minimum_msat"`
	HtlcMaximumMsat     int64  `json:"htlc_maximum_msat"`
}

func (c *Channel) Fee(msatoshi, riskfactor int64, fuzzpercent float64) int64 {
	fee := int64(math.Ceil(
		float64(c.BaseFeeMillisatoshi) + float64(c.FeePerMillionth*msatoshi)/1000000,
	))
	fuzz := int64(rand.Float64() * fuzzpercent * float64(fee) / 100)
	riskfee := c.Delay * msatoshi * riskfactor / 5259600
	return fee + fuzz + riskfee
}

func (ln *Client) GetRoute(
	id string,
	msatoshi int64,
	riskfactor int64,
	cltv int64,
	fromid string,
	fuzzpercent float64,
	exclude []string,
	maxhops int,
	maxchannelfeepercent float64,
) (route []RouteHop, err error) {
	// fail obvious errors
	if id == fromid {
		return nil, errors.New("start == end")
	}

	path, err := ln.GetPath(id, msatoshi, fromid, exclude, maxhops, maxchannelfeepercent)
	if err != nil {
		return nil, fmt.Errorf("failed to query path: %w", err)
	}

	// turn the path into a lightning route
	route = PathToRoute(path, msatoshi, cltv, riskfactor, fuzzpercent)

	return
}

func (ln *Client) GetPath(
	id string,
	msatoshi int64,
	fromid string,
	exclude []string,
	maxhops int,
	maxchannelfeepercent float64,
) (path []*Channel, err error) {
	// init graph
	if g == nil {
		g = &Graph{client: ln}
	}

	// sync graph
	if lastSynced.Before(time.Now().Add(-(time.Minute * 30))) {
		if err = g.Sync(); err != nil {
			return
		}
	}

	// exclude channels
	for _, scid := range exclude {
		if channel, ok := g.channelMap[scid]; ok {
			defer unexclude(channel, channel.HtlcMaximumMsat)
			channel.HtlcMaximumMsat = 0
		}
	}

	// set globals
	g.msatoshi = msatoshi
	g.maxhops = maxhops
	g.maxchannelfee = int64(maxchannelfeepercent * float64(msatoshi) / 100)

	// get the best path
	path = g.SearchDualBFS(fromid, id)

	if len(path) == 0 {
		return nil, errors.New("no path found")
	}

	return path, nil
}

func PathToRoute(
	path []*Channel,
	msatoshi int64,
	cltv int64,
	riskfactor int64,
	fuzzpercent float64,
) (route []RouteHop) {
	plen := len(path)
	route = make([]RouteHop, plen)

	// the last hop
	channel := path[plen-1]
	route[plen-1] = RouteHop{
		Channel:   channel.ShortChannelID,
		Direction: channel.Direction,
		Id:        channel.Destination,
		Msatoshi:  msatoshi, // no fees for the last channel
		Delay:     cltv,

		arrivingFee:   channel.Fee(msatoshi, riskfactor, fuzzpercent),
		arrivingDelay: channel.Delay,
	}

	if plen == 1 {
		// single-hop payment, end here
		return route
	}

	// build the route from the ante-last hop backwards
	for i := plen - 2; i >= 0; i-- {
		nexthop := route[i+1]
		channel := path[i]
		amount := nexthop.Msatoshi + nexthop.arrivingFee
		route[i] = RouteHop{
			Channel:   channel.ShortChannelID,
			Direction: channel.Direction,
			Id:        channel.Destination,
			Msatoshi:  amount,
			Delay:     nexthop.Delay + nexthop.arrivingDelay,

			arrivingFee:   channel.Fee(amount, riskfactor, fuzzpercent),
			arrivingDelay: channel.Delay,
		}
	}

	return route
}

type RouteHop struct {
	Id        string `json:"id"`
	Channel   string `json:"channel"`
	Direction int    `json:"direction"`
	Msatoshi  int64  `json:"msatoshi"`
	Delay     int64  `json:"delay"`

	// fee and delay that must arrive here, so must be applied at the previous hop
	arrivingFee   int64
	arrivingDelay int64
}

func unexclude(channel *Channel, htlcmax int64) {
	if channel == nil {
		return
	}
	channel.HtlcMaximumMsat = htlcmax
}
