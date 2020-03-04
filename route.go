package lightning

import (
	"errors"
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

	maxhops  int
	msatoshi int64
}

func (g *Graph) Search(start string, end string) (path []*Channel) {
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
				routeFromNext := make([]*Channel, i)
				copy(routeFromNext, routeFrom)
				routeFromNext[i-1] = channel
				fromEndNext[channel.Source] = routeFromNext
			}
		}
		fromEnd = fromEndNext

		// search frontwards from start
		fromStartNext := make(map[string][]*Channel)
		for node, routeUntil := range fromStart {
			for _, channel := range g.channelsFrom[node] {
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

	// get data from normal LN gossip
	res, err := g.client.Call("listchannels")
	if err != nil {
		return err
	}

	for _, ch := range res.Get("channels").Array() {
		htlcmin, _ := strconv.ParseInt(strings.Split(ch.Get("htlc_minimum_msat").String(), "m")[0], 10, 64)
		htlcmax, _ := strconv.ParseInt(strings.Split(ch.Get("htlc_maximum_msat").String(), "m")[0], 10, 64)

		channel := &Channel{
			g:                   g,
			Source:              ch.Get("source").String(),
			Destination:         ch.Get("destination").String(),
			ShortChannelID:      ch.Get("short_channel_id").String(),
			Satoshis:            ch.Get("satoshis").Int(),
			BaseFeeMillisatoshi: ch.Get("base_fee_millisatoshi").Int(),
			FeePerMillionth:     ch.Get("fee_per_millionth").Int(),
			Delay:               ch.Get("delay").Int(),
			HtlcMinimumMsat:     htlcmin,
			HtlcMaximumMsat:     htlcmax,
		}

		g.channelsFrom[channel.Source] = append(g.channelsFrom[channel.Source], channel)
		g.channelsTo[channel.Destination] = append(g.channelsTo[channel.Destination], channel)
	}

	// get data from our custom servers
	// TODO

	// reset counter
	lastSynced = time.Now()

	return nil
}

type Channel struct {
	g *Graph

	Source              string `json:"source"`
	Destination         string `json:"destination"`
	ShortChannelID      string `json:"short_channel_id"`
	Satoshis            int64  `json:"satoshis"`
	BaseFeeMillisatoshi int64  `json:"base_fee_millisatoshi"`
	FeePerMillionth     int64  `json:"fee_per_millionth"`
	Delay               int64  `json:"delay"`
	HtlcMinimumMsat     int64  `json:"htlc_minimum_msat"`
	HtlcMaximumMsat     int64  `json:"htlc_maximum_msat"`
}

func (c *Channel) Fee(msatoshi int64) int64 {
	return int64(math.Ceil(
		float64(c.BaseFeeMillisatoshi) + float64(c.FeePerMillionth*msatoshi)/1000000,
	))
}

type RouteHop struct {
	Id         string `json:"id"`
	Channel    string `json:"channel"`
	Msatoshi   int64  `json:"msatoshi"`
	AmountMsat string `json:"amount_msat,omitempty"`
	Delay      int64  `json:"delay"`
	Style      string `json:"style,omitempty"`
}

func (ln *Client) GetRoute(
	id string,
	msatoshi int64,
	riskfactor int,
	cltv int64,
	fromid string,
	fuzzpercent float64,
	exclude []string,
	maxhops int,
) (route []RouteHop, err error) {
	// fail obvious errors
	if id == fromid {
		return nil, errors.New("start == end")
	}

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
	// TODO
	// (set maxhtlc to zero in these)

	// set globals
	g.msatoshi = msatoshi
	g.maxhops = maxhops

	// get the best path
	path := g.Search(fromid, id)
	plen := len(path)

	if plen == 0 {
		return nil, errors.New("no path found")
	}

	// calculate fuzz
	if fuzzpercent > 0 {
		msatoshi = msatoshi * int64(1+rand.Intn(int(fuzzpercent))/100)
	}

	// turn the path into a lightning route
	route = make([]RouteHop, plen)

	// the last hop
	channel := path[plen-1]
	route[plen-1] = RouteHop{
		Channel:  channel.ShortChannelID,
		Id:       id,
		Msatoshi: msatoshi, // no fees for the last channel, just the fuzz
		Delay:    channel.Delay,
	}

	if plen == 1 {
		// single-hop payment, end here
		return route, nil
	}

	// build the route from the ante-last hop backwards
	for i := plen - 2; i >= 0; i-- {
		nexthop := route[i+1]
		channel := path[i]
		route[i] = RouteHop{
			Channel:  channel.ShortChannelID,
			Id:       channel.Destination,
			Msatoshi: nexthop.Msatoshi + channel.Fee(nexthop.Msatoshi),
			Delay:    nexthop.Delay + channel.Delay,
		}
	}

	return
}
